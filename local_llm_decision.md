# Decision record — the shared local instruct LLM

Covers `flying-shuttle-hkc` (stitcher glue text) and `flying-shuttle-hs8`
(per-bullet clustering), which share one model. The Source Atlas region
digester (`flying-shuttle-6n5`) is the third consumer.

**Status: recommendation. One bench run on the target M1 Air confirms it —
see "Confirmation step".**

---

## Constraints (recap)

- Apple M1 Air, 8 GB unified memory.
- Exactly one instruct model resident at a time. Co-resident: the ~300 MB
  `embeddinggemma-300m` embedder (`flying-shuttle-kj0`) + macOS. Ceiling for
  the instruct model ≈ 3.5–4 GB.
- MLX / `mlx-lm` (same runtime as the embedder). 100 % local.
- Three workloads:
  1. **Stitcher glue** — 1–3 sentences between excerpts, interactive (~1–5 s OK).
  2. **Region digest** — `TITLE:` / `ABSTRACT:` / `KEYWORDS:` from ~15 chunks,
     ~25 calls per Atlas rebuild, batch (1–2 min budget).
  3. **Per-bullet clusters** (`hs8`) — see its own section; may not call the
     LLM at all.

## Recommendation: `mlx-community/Qwen2.5-3B-Instruct-4bit`

| | |
|---|---|
| MLX id | `mlx-community/Qwen2.5-3B-Instruct-4bit` |
| Resident size | ~1.9 GB weights + ~0.3–0.8 GB KV/activations at these context sizes → **well under the 4 GB ceiling with the embedder co-resident** |
| Speed on M1 (7-core GPU) | ~30–50 tok/s decode; a 120-token digest ≈ 3–5 s, a full 25-region rebuild ≈ 1.5–2 min — inside budget |
| Instruction following | Strong for short constrained formats; reliable at line-formatted `KEY: value` output (the reason we avoid JSON) |
| Ecosystem | Same family as the embedder; `mlx-lm` loads it directly |

### Why not the alternatives

- **Qwen2.5-7B-4bit (~4.3 GB)** — best quality, but with the embedder
  co-resident it pushes total RAM to ~5 GB+ and macOS starts swapping on an
  8 GB machine during a rebuild. Rejected for headroom. Revisit if the
  embedder is made lazy too.
- **Phi-3.5-mini-instruct-4bit (~2.2 GB)** — close second, comparable speed.
  Kept as the fallback if Qwen2.5-3B's prose reads stilted for the stitcher.
- **Llama-3.2-3B-Instruct-4bit (~1.8 GB)** — fine baseline; slightly weaker at
  holding a rigid output template than Qwen2.5-3B in informal testing reports.
- **Gemma-3-4B-it-4bit (~2.6 GB)** — good, but larger and no clear win over
  Qwen2.5-3B for these tasks.
- **SmolLM2-1.7B** — fast and tiny but prose coherence is marginal for glue
  text. Only if the M1 turns out to be more memory-starved than expected.

## Process & Go integration

One supervised subprocess, a sibling of `embed_server.py`, following the
existing `ingest.PythonEmbedder` pattern (already hardened in
`flying-shuttle-8cu`: own process group, SIGTERM→SIGKILL, thread caps,
single-flight, shared `ingest.ComputeGate`).

```
python/llm_server.py         stdlib http.server, holds the MLX model
  GET  /health   -> 200 once loaded
  POST /complete {"system": "...", "user": "..."} -> {"text": "..."}
```

- **Lazy + idle-shed**: start on first `/complete`; the server exits itself
  after ~2 min idle to free ~2 GB; the Go supervisor restarts on the next
  request. (The embedder stays resident — it is only 300 MB.)
- **Single model process for all three consumers.** No Ollama, no second
  daemon.
- **MLX memory hygiene in the server**: `mx.set_memory_limit(...)`,
  `mx.set_cache_limit(0)` (or small), `mx.metal.clear_cache()` after each
  generation; `gc.collect()` between requests.
- **Go side**: one shared `Completer` —
  `Complete(ctx, systemPrompt, userPrompt string) (string, error)` — which is
  already the interface at `internal/search/cluster.go:193` and in the
  `stitch` package. Implement `ingest.PythonCompleter` (HTTP client +
  supervisor) mirroring `PythonEmbedder`; it acquires the same `ComputeGate`
  instance so a digest batch and an embed backfill never overlap.
- Env: `SHUTTLE_LLM_AUTOSTART` (default 0 until the model is downloaded),
  `SHUTTLE_LLM_ADDR` (127.0.0.1:8072), `SHUTTLE_LLM_SCRIPT`, `SHUTTLE_LLM_MODEL`.

### Prompt templates

**Stitcher** (`GlueLevel` 0–100 → instruction):

```
system: You join transcript excerpts into a flowing narrative. Add ONLY the
        minimal connective prose needed. No new facts. No preamble.
user:   Verbosity: {level}/100 ({terse|moderate|expansive}).
        Excerpt A: {a}
        Excerpt B: {b}
        Write the bridge from A to B:
```

**Region digest** (line-format, never JSON):

```
system: You label a cluster of transcript passages. Reply with EXACTLY three
        lines and nothing else:
        TITLE: <=6 words
        ABSTRACT: <=3 sentences
        KEYWORDS: comma-separated, <=8
user:   {up to 15 member chunks, centroid-nearest first}
```

Parse leniently: split on the first `:` per line, tolerate missing lines
(fall back to the extractive digest for any field the model omits).

## Confirmation step (the one thing that needs the hardware)

On the M1 Air, after `make embed-setup` and downloading Qwen2.5-3B-4bit:

```
python -m mlx_lm.generate --model mlx-community/Qwen2.5-3B-Instruct-4bit \
  --prompt "$(cat a-real-region-of-15-chunks.txt)" --max-tokens 160
```

Check: (1) peak memory with the embedder also loaded stays < ~6 GB (Activity
Monitor, no swap), (2) decode ≥ ~25 tok/s, (3) the 3-line format holds over
~10 varied regions. If (1) fails → Phi-3.5-mini. If (3) fails → add a
one-shot example to the system prompt.

---

## `flying-shuttle-hs8` — per-bullet clustering

**Recommendation: do NOT wire `LLMClusterer` to the local model. Retire it in
favour of the Source Atlas.**

`cluster.go`'s `LLMClusterer` / `EmbeddingClusterer` answer "split the chunks
retrieved for one bullet into 3–4 sub-themes." The Source Atlas now computes a
corpus-wide region partition with digests. The better version of the
sub-theme feature is: for the current bullet's evidence (or top retrieved
chunks), look up which Atlas **regions** they fall into and show those
regions' digests as the sub-themes — no per-query LLM call, instant, and
consistent with the rest of the Atlas UI.

Concretely, when reviving that feature:
1. Retrieve top-N chunks for the bullet (existing `HybridIndex.Search`).
2. Map each to its `atlas_region_chunk.region_id` in the current build.
3. Group; each distinct region = one sub-theme, labelled by its
   `digest_title` / `digest_keywords`.
4. No `Completer`, no JSON, no extra model.

If a genuinely generative per-bullet clustering is still wanted later, it uses
the same shared Qwen2.5-3B process and the line-format contract above — never
JSON, never a second model.

Keep `EmbeddingClusterer` as-is for now (it is dormant and harmless); delete
`LLMClusterer` and its JSON prompt when the Atlas-backed version lands.
