# Source Atlas — plan

A navigable, LLM-summarised **network** of the transcript corpus, built by
clustering chunk embeddings and linking the clusters by similarity. It exists so
the author can **find source material to pull into their outline**. It is not a
summary of the document and it is not part of the document.

This plan supersedes `summarization_proposal.md` (which proposed a "hierarchical
summary" and a LanceDB store). Rejected from that proposal: LanceDB (§12), the
hierarchical/tree shape (§1), and any prose output. Adopted and changed: the
cluster→summarise→index pipeline, the memory-phased build, line-format LLM output
over JSON, and — newly in scope — switching the embedding model (§4).

---

## 0. The two networks — read this first

This codebase contains **two separate graph-shaped things**, and after this work
*both* are networks. They must never share vocabulary, types, tables, packages,
or handler files. An LLM editing this repo that confuses them will corrupt the
author's work.

| | **The Outline** | **The Source Atlas** |
|---|---|---|
| What it is | The non-linear document the author is writing | A derived network over the uploaded transcripts |
| Origin | Authored by a human, bullet by bullet | Computed from chunk embeddings + an LLM |
| Mutability | Precious. Edited constantly. Versioned (snapshots, branches). Recovered from `state.json`. | Disposable. Rebuilt from scratch on demand. Never recovered — just regenerated. |
| Elements | **node** (bullet), **edge** (linear / branch / jump), **thread**, **branch**, **snapshot**, **evidence** | **region** (a chunk cluster), **link** (weighted region↔region similarity), **member chunk**, **centroid**, **digest** |
| Graph shape | A **directed** DAG. Linear edges give parent/child; branch/jump edges cross-link. Has entry points. | An **undirected, weighted** graph. Regions connected by similarity links. **No root, no hierarchy, no containment, no tiers.** |
| Packages | `internal/outline`, `internal/dag`, `internal/stitch`, `internal/export` | `internal/atlas` (new) |
| Tables | `nodes`, `edges`, `evidence`, `threads`, … | `atlas_build`, `atlas_region`, `atlas_region_link`, `atlas_region_chunk` (all `atlas_`-prefixed) |
| Store types | `model.Node`, `model.Edge`, `model.Evidence` | `atlas.Region`, `atlas.Link`, `atlas.Digest`, `atlas.Build` |
| Web handler | `internal/web/outline_handler.go`, `dag_handler.go` | `internal/web/atlas_handler.go` (new) |
| Routes | `/outline`, `/outline/nodes/{id}/…`, `/stitch`, `/snapshots`, `/branches` | `/atlas`, `/atlas/regions/{id}`, `/atlas/graph.json` |
| Datastar signals | `focusId`, `threadId`, `readerChunk`, `centerView` | `atlasView`, `atlasRegionId`, `atlasChunkId`, `atlasQuery` |
| Gets stitched / exported? | Yes — this is the manuscript | **Never.** The Atlas produces no prose output. |

### Vocabulary rules (review these on every Atlas PR)

- The Outline has **nodes** and **edges**. The Atlas has **regions** and
  **links**. Never cross the terms.
  - ❌ "atlas node", "source node", "transcript node" → **region**
  - ❌ "atlas edge" → **link**
  - ❌ "outline region", "outline cluster", "document cluster" → the outline has **nodes**
  - ❌ "outline link" → the outline has **edges**
- The Atlas is flat. There is **no** `parent_region_id`, no `tier`, no
  `subregion`, no `root region`. If you are writing tree-walking code over
  regions, stop — the shape is wrong.
- The graph view (§6) renders **region nodes** and **chunk nodes** — "node"
  there is a *rendering* term for a dot on the canvas, never a `model.Node`.
- ❌ storing a `model.Node` for a region, or an `atlas.Region` for a bullet
- ❌ an `edges` row referencing an `atlas_region`; an `atlas_region_link`
  referencing a `node`
- ❌ putting regions into `outline.BuildTree` / `dag.Linearize`

### The single legitimate connection

The Atlas and the Outline touch in exactly **one** place, and it already
exists: the **evidence** flow. While exploring a region the author clicks
"Add as evidence" on a **member chunk** (or "Read in transcript →" first):

```
chunk  →  POST /outline/nodes/{focusId}/evidence  →  evidence row  →  locked chunk_ref node
```

The Atlas **never creates outline nodes or edges**. It only surfaces chunks into
the existing evidence pane. A region is an exploration aid, not a document
element.

---

## 1. What the Atlas is, precisely

Given the corpus of transcript **chunks** (each has an embedding in
`chunks.embedding_vec` / the HNSW index — dimension per §4):

1. **Partition** the chunk set into **regions** — small clusters of
   embedding-near chunks. Each chunk is a member of **exactly one** region. *The
   clustering method is an open design question — see §2 Phase A; do not
   implement before it is resolved.*
2. **Digest** each region with the shared local instruct LLM: summarise its
   member chunks into `{title (≤ 6 words), abstract (≤ 3 sentences),
   keywords[]}`. Regions are digested **independently** — there is no roll-up,
   because there is no hierarchy.
3. **Link** regions to each other: for each region, add an undirected weighted
   **link** to its few most similar regions (cosine of centroids, above a
   threshold). This is the network the author walks.
4. **Keyword-tag** every chunk with a few extractive terms (TF-IDF from the
   existing BM25 stats — no LLM). These are the labels in the chunk-level graph
   view (§6).
5. **Embed** each digest's `title + abstract` and keep the vectors in a small
   `atlas.RegionIndex` (separate from the chunk index) so the author can search
   the Atlas and rank regions against the bullet they are writing.

The result is one `atlas_build` row plus its regions, links, and chunk
keyword-tags. Rebuilding replaces the current build wholesale.

There is **no top-level list of regions to start from**. Entry into the network
is always via search (§5b), the focused-bullet affinity ranking (§5c), or the
graph view (§6); from a region the author follows **links** to neighbours.

---

## 2. Build pipeline — memory-phased for M1 Air / 8 GB

Phases stay separated so the instruct LLM and any embedding work do not stack
their peak memory. With the §4 embedder swap the embedder is small enough to
stay resident; only the instruct model needs careful lifecycle management (§8).

```
Phase A — PARTITION         Phase B — DIGEST                Phase C — LINK + INDEX
(pure Go, no model)         (instruct LLM resident)        (small embedder resident)

chunk vectors ──────────►   regions ───LLM──► digests  ──►  digest vectors → atlas.RegionIndex
   from HNSW / store         (independent, one              region↔region links (kNN)
                              call per region)              per-chunk TF-IDF keywords
```

- **Phase A — RESOLVED (`flying-shuttle-g3u`): bisecting spherical k-means with
  a size stop.** Start with all chunks in one region; repeatedly take the
  largest region still over `MaxRegionSize` and split it in two with k=2
  spherical k-means (cosine → renormalised centroids); stop when no region
  exceeds `MaxRegionSize` or a `MaxRegions` cap is hit; then merge any region
  under `MinRegionSize` into its nearest sibling by centroid cosine. Defaults
  `MaxRegionSize 15`, `MinRegionSize 4`, `MaxRegions 400`.
  - Deterministic: fixed RNG seed, farthest-pair seeding for the k=2 split, and
    all ties broken by chunk id.
  - Fast: each split is O(size·iters·dim); total ≈ O(n·log(n/max)·iters·dim) —
    sub-second for a few thousand vectors.
  - Chosen over Louvain-on-a-kNN-graph and HDBSCAN because both need a few
    hundred lines of subtle pure-Go graph code, while this is ~150 lines we can
    trust; bounded, readable region sizes matter more for a navigation aid than
    optimal modularity, and the region **link** graph (`link.go`) re-introduces
    the neighbourhood topology anyway. Revisit Louvain only if region quality
    proves poor in real use.
  - Implementation: `internal/atlas/cluster.go`, `ClusterChunks(ids []string,
    vectors [][]float32, params ClusterParams) []Region` (centroid + members
    with cosine distance filled; digests left empty for Phase B).
- **Phase B** — one LLM call per region, feeding up to ~15 member chunks nearest
  the centroid (truncated). Line-formatted output (`TITLE:` / `ABSTRACT:` /
  `KEYWORDS:`), **not JSON** — per the `flying-shuttle-hs8` concern about
  small-model JSON reliability. Model = the shared instruct model from
  `flying-shuttle-hkc` (§7). Requests are single-flight and the LLM subprocess
  is resource-capped (§8).
- **Phase C** — embed the `title + abstract` strings through the (now small,
  resident) embedder; build the region link graph by cosine kNN over centroids;
  compute per-chunk extractive keywords. `atlas.RegionIndex` is an in-memory
  `search.VectorIndex` or a flat cosine scan — region counts are in the hundreds.

### Degraded mode (no instruct LLM available)

Consistent with the app degrading to BM25-only elsewhere: if no instruct model
is reachable, Phase B falls back to **extractive** digests — `title` = top
TF-IDF terms for the region, `abstract` = the first sentences of the
centroid-nearest chunk. The Atlas is fully navigable without any LLM; digests
just read rougher.

---

## 3. Data model

```sql
-- migration 00X_atlas.sql

CREATE TABLE atlas_build (
    id           TEXT PRIMARY KEY,
    created_at   TIMESTAMP NOT NULL,
    status       TEXT NOT NULL,          -- building | ready | failed
    chunk_count  INTEGER NOT NULL,
    params_json  TEXT NOT NULL,          -- clustering params, model id, link-k, …
    error        TEXT
);

CREATE TABLE atlas_region (
    id               TEXT PRIMARY KEY,
    build_id         TEXT NOT NULL REFERENCES atlas_build(id) ON DELETE CASCADE,
    centroid_vec     BLOB NOT NULL,      -- float32 LE, same encoding as chunks.embedding_vec
    chunk_count      INTEGER NOT NULL,
    digest_title     TEXT NOT NULL,
    digest_abstract  TEXT NOT NULL,
    digest_keywords  TEXT NOT NULL,      -- newline-joined; the low-zoom graph label
    digest_source    TEXT NOT NULL,      -- "llm:<model>" | "extractive"
    digest_vec       BLOB               -- embedding of title+abstract; NULL until Phase C
    -- NOTE: no parent_region_id, no tier. The Atlas is flat.
);

CREATE TABLE atlas_region_link (         -- undirected; store one row per pair
    build_id      TEXT NOT NULL REFERENCES atlas_build(id) ON DELETE CASCADE,
    region_a_id   TEXT NOT NULL REFERENCES atlas_region(id) ON DELETE CASCADE,
    region_b_id   TEXT NOT NULL REFERENCES atlas_region(id) ON DELETE CASCADE,
    weight        REAL NOT NULL,         -- centroid cosine similarity
    PRIMARY KEY (region_a_id, region_b_id),
    CHECK (region_a_id < region_b_id)    -- canonical ordering, no duplicate pairs
);

CREATE TABLE atlas_region_chunk (        -- each chunk appears in exactly one region
    region_id  TEXT NOT NULL REFERENCES atlas_region(id) ON DELETE CASCADE,
    chunk_id   TEXT NOT NULL REFERENCES chunks(id) ON DELETE CASCADE,
    distance   REAL NOT NULL,            -- cosine distance to centroid (for ordering)
    keywords   TEXT NOT NULL DEFAULT '', -- newline-joined extractive terms; chunk-node label (§6)
    PRIMARY KEY (region_id, chunk_id)
);
```

- **One build at a time.** Rebuild = insert a new `atlas_build`, build it, then
  delete the previous build (cascade wipes its regions and links). Never merged,
  never diffed.
- No `state.json` / recovery involvement — the Atlas is regenerated, not
  restored. `workingdocs` does not touch it. Not in snapshots or branches.

---

## 4. Embedding model change — switch to a small model

**Adopt `google/embeddinggemma-300m` (MLX, ~300 MB, 768-dim)** in place of
Qwen3-Embedding-4B-4bit (~2.5 GB, 2560-dim).

Why it matters here and app-wide:

- **Query-time embedding becomes cheap.** A 300 MB model can stay resident and
  embed a query string in milliseconds without memory or GPU pressure. This
  makes §5b / §5c robust, and lets the chunk `HybridIndex` embed live queries on
  every keystroke instead of degrading to BM25-only.
- **Headroom for the instruct model.** ~300 MB embedder + ~2.5 GB instruct model
  + OS fits 8 GB far more comfortably than the old ~2.5 GB embedder did.

Migration (its own bd issue — see §13):

- `python/embed_server.py` + `python/embed.py`: load embeddinggemma via MLX, use
  its documented pooling (mean-pool, not Qwen's last-token), keep the same
  `/embed` HTTP contract.
- Re-embed the corpus: clear every `chunks.embedding_vec`, let the existing
  `indexer.Backfiller` repopulate; delete `shuttle.hnsw` so it rebuilds at the
  new dimension via `LoadAndReconcile`.
- `.fembed`: bump format to version 2 (the header already carries `dims`, so the
  Go reader in `internal/ingest/embedfile` needs only to accept 768 and v2).
  Any existing `.fembed` files are stale and must be regenerated offline.
- The `VectorIndex` / HNSW dimension is inferred from the vectors, so no Go
  signature change — but a mixed-dimension index is corrupt, so the re-embed
  must complete before search is trusted (surface an "index rebuilding" state).

The Atlas's Phase C digest embeddings use the same (new) embedder.

---

## 5. Integration with the Outline (always pull, never push)

### 5a. Explore → evidence (the core loop)

New pane / mode in the right-hand column, peer to `#evidence-candidates` and
`#transcript-reader`, toggled by `atlasRegionId`:

- Selecting a region shows its digest (title + abstract + keywords), its
  **member chunks** ordered by `distance`, and its **linked neighbour regions**
  (by descending link weight) as one-click hops.
- Each member chunk renders as the existing `viewmodel.Candidate` card with the
  current "Read in transcript →" and "Add as evidence" buttons. **No new attach
  path** — it reuses `POST /outline/nodes/{focusId}/evidence`.

### 5b. Search the Atlas

A query box (`atlasQuery`) → embed the query (cheap now, §4) → rank regions by
cosine against `digest_vec` → show the top regions as entry points into the
network.

### 5c. Focused-bullet affinity (the reason this beats plain chunk search)

When a bullet is focused (`focusId` set), embed the bullet's text and rank the
Atlas regions against it — "Sources for this bullet". Answers *"where in my
transcripts is there material for the point I'm making?"*, which chunk-level
keyword/passage search does poorly because a bullet is prose, not a query.

Read-only against the Outline: reads `node.Title`/`Body`, writes nothing. With
§4 this no longer depends on a heavyweight embedder being up.

---

## 6. Network view (visual exploration)

A second way to explore the Atlas, alongside the list-style pane (§5a): a
pan/zoom **graph canvas** of the region network with **semantic
level-of-detail**. Toggled by `atlasView` (`list` | `graph`).

### Region level (zoomed out — the default)

- One graph node per **region**. Its label is the region's top 2–3 **keywords**
  (`digest_keywords`), **always rendered and legible at low zoom** — this is the
  map legend; do not declutter it away.
- Node size ∝ `chunk_count`; digest `title` shown on hover and in the detail
  pane.
- Graph edges = `atlas_region_link` rows; edge width ∝ `weight`.
- Force-directed layout computed **client-side with a fixed seed** so the map is
  stable across renders. (Server-persisted coordinates: future — §12.)

### Chunk level (zoomed into one region)

- Zooming past a threshold onto a region — or clicking "expand" on it — replaces
  that region node with its **member chunks** as nodes; the other regions dim or
  collapse to keep the canvas readable.
- Each chunk node's label is its per-chunk **keywords** (`atlas_region_chunk.
  keywords`, the extractive TF-IDF tags from §1 step 4 — no LLM).
- Optional edges between member chunks whose pairwise cosine exceeds a
  threshold, so tightly-related passages sit together.
- Zooming back out / a "regions" control collapses the region again.

### Right-hand detail pane

- Selecting a **chunk node** opens a right-hand pane with that chunk's **full
  text** (`chunk.Content`, or a `transcript.Service` window for surrounding
  context), its source file, and the existing **"Read in transcript →"** and
  **"Add as evidence"** actions — the same evidence path as everywhere else
  (§0). `atlasChunkId` drives the pane.
- Selecting a **region node** shows its full digest (title + abstract +
  keywords) and its linked neighbours. `atlasRegionId` drives the pane.

### Wiring

- `GET /atlas/graph.json` → region nodes + links.
  `GET /atlas/graph.json?region={id}` → that region's chunk nodes +
  intra-region edges. Raw JSON to the client is deliberate here — the renderer
  is imperative JS, like `app.js`.
- Renderer: a small graph library **vendored** under
  `internal/web/static/vendor/` (no CDN — matches the Datastar vendoring).
  Candidates: Cytoscape.js, Sigma.js (WebGL), or d3-force + canvas; region
  counts are in the hundreds and chunk expansion adds dozens, so any of them
  cope. Pick during implementation.
- This graph script is the **second** piece of hand-written JS in the project
  (after `app.js`). Datastar signals stay the source of truth for selection so
  the list pane (§5a) and the graph pane stay in sync — clicking a region in
  either updates `atlasRegionId` and both reflect it.
- The view is read-only exploration; the only write is the evidence action on a
  selected chunk (§0).

---

## 7. Relationship to existing search code

- `internal/search/cluster.go` (`EmbeddingClusterer`, `LLMClusterer`) is scoped
  to "sub-themes of **one bullet's** retrieved chunks" — different granularity,
  different output type, dormant (`transition_decisions.md` §1.6). **Reuse** its
  low-level vector helpers (`ingest.CosineSimilarity`, centroid math); **do not
  reuse** the clusterer types for the Atlas.
- `sub_chunk_evaluation.md` is the retrieval-*granularity* plan (passage index,
  per-sentence locators; largely shipped). Orthogonal: it is about locating a
  hit inside a chunk; the Atlas is about discovering which chunks to look at.
  The Atlas does not touch the passage index.

---

## 8. Shared local instruct LLM — reconcile with existing bd issues

`flying-shuttle-hkc` (stitcher glue text) and `flying-shuttle-hs8` (per-bullet
clustering) already own the "which local instruct model on 8 GB" decision, and
both state **only one instruct model resident at a time**. The Atlas digester is
a **third consumer of that same model** — it must not introduce a second one.

All three call it through one shared `Completer` client backed by one supervised
subprocess (sibling to the embedder, same supervision pattern as
`ingest.PythonEmbedder`). Until that model is chosen, the Atlas ships in
extractive mode (§2). Fold a note into `flying-shuttle-hkc`.

---

## 9. Background processes & Python resource limits

Background work is **allowed**, but every background process must be
memory-aware, compute-limited, and torn down cleanly with the app.

### Lifecycle (all long-running work)

- Started from `main.go`, tracked in a `sync.WaitGroup`, driven by a `context`
  that is cancelled on `SIGINT`/`SIGTERM` (the pattern the `indexer.Snapshotter`
  and `Backfiller` already follow).
- On shutdown: stop accepting new work immediately; let an in-flight unit finish
  or abandon it on context cancel; `main.go` waits (bounded) for the WaitGroup
  before exiting.
- The Atlas rebuild job: **one at a time** (409 while building), runs at low
  priority, checks `ctx.Err()` between regions so a shutdown mid-build stops
  promptly. A partial build is discarded, not persisted as `ready`.

### Python subprocesses (embedder + instruct LLM)

- **Spawned with `exec.CommandContext` + own process group**; on app exit the
  supervisor sends `SIGTERM` to the group, then `SIGKILL` after a short grace
  period. No orphaned `python`/MLX processes after the Go binary exits.
- **Single-flight**: the Go side serialises requests to each server with a
  mutex; each server processes one batch at a time. Embed batch size capped
  (e.g. 16); instruct generation is 1 at a time.
- **Memory caps in the server**: set an MLX memory limit and disable/limit the
  MLX cache (`mx.set_memory_limit`, `mx.set_cache_limit`), release on idle.
- **Thread caps from the supervisor**: spawn with `OMP_NUM_THREADS`,
  `MKL_NUM_THREADS`, `VECLIB_MAXIMUM_THREADS` set to a small value so batch runs
  don't saturate the machine.
- **Instruct LLM is lazy + idle-shed**: started on first use, exits itself after
  an idle timeout (~2 min) to free ~2.5 GB; the supervisor restarts it on the
  next request. The embedder (~300 MB, §4) may stay resident.
- **Global budget**: never run an instruct-LLM digest pass and a large embed
  backfill at the same time — the Atlas builder yields to the backfiller (or
  vice-versa) via a shared semaphore.

---

## 10. Build triggers

- **Manual rebuild**: a button in the Atlas pane → `POST /atlas/rebuild` →
  the background job of §9. Status polled like the ingest drawer
  (`data-on-interval__duration.2s` until `ready`/`failed`).
- **Staleness hint**: compare `atlas_build.chunk_count` to the current chunk
  count; past ~10 % drift, show "Atlas is N chunks behind — rebuild". No
  automatic rebuild (too expensive per upload, unlike the embedding backfiller).
- **Incremental update**: out of scope for v1 (full rebuild only). Future:
  attach new chunks to the nearest existing region, re-digest only touched
  regions, recompute their links.
- **Empty / not-yet-embedded corpus**: the pane says "upload transcripts and let
  embeddings finish, then build the atlas"; `Builder.Build` errors cleanly with
  too few embedded chunks.

---

## 11. Package layout

```
internal/atlas/
    atlas.go        Region, Link, Digest, Build types; graph assembly helpers
    cluster.go      chunk-vector partitioning → regions   (METHOD TBD — §2 Phase A)
    digest.go       per-region summarisation; Summariser iface + extractive fallback
    keywords.go     per-chunk + per-region extractive TF-IDF terms
    link.go         kNN similarity graph over region centroids
    builder.go      Builder{Store, Chunks, Embedder, Summariser}.Build(ctx) → *Build
    store.go        atlas_* persistence (or methods on internal/store — TBD)
    index.go        RegionIndex — cosine search over digest_vec

internal/web/
    atlas_handler.go       GET /atlas, GET /atlas/regions/{id},
                           GET /atlas/graph.json, POST /atlas/rebuild
    components/atlas.templ
    static/atlas_graph.js          hand-written graph renderer (2nd JS file)
    static/vendor/<graph-lib>.js   vendored, no CDN

internal/model/          -- NOTHING atlas-related here; model/ is the Outline's
```

- `atlas.Summariser`: `Summarise(ctx, inputs []string) (Digest, error)` —
  `LLMSummariser{Completer}` and `ExtractiveSummariser{BM25}`.
- `Builder` depends only on interfaces → unit-testable with a stub summariser
  and hand-built vectors.

---

## 12. Explicitly out of scope

- LanceDB or any new vector database (breaks the CGO-free single-binary
  invariant; HNSW + a flat region scan suffice).
- **Any hierarchy in the Atlas** — it is a flat network by design (§0, §1).
- Any prose output from the Atlas — no generated outline, no auto-created nodes,
  no stitching. The author writes the document.
- Incremental Atlas maintenance; diffing/versioning Atlas builds.
- **Server-persisted / precomputed graph coordinates** — the network view (§6)
  uses a client-side force layout with a fixed seed; storing `x/y` on regions
  for a globally-stable map is a later refinement.

---

## 13. Suggested bd issues

1. **epic: Source Atlas** — source network for evidence discovery.
2. `search: switch embedder to embeddinggemma-300m (768-dim)` — re-embed corpus,
   rebuild HNSW, `.fembed` v2, enable live query embedding. (Prereq for 8, 10.)
3. `atlas: spike — choose the chunk→region clustering method` (§2 Phase A).
4. `atlas: schema + store (atlas_build / atlas_region / atlas_region_link / atlas_region_chunk)`.
5. `atlas: Summariser interface + extractive fallback` (Phase B, no LLM).
6. `atlas: region similarity-link graph` (Phase C).
7. `atlas: per-chunk + per-region extractive keywords` (feeds graph labels, §6).
8. `atlas: RegionIndex + digest embedding` — depends on 2.
9. `atlas: Builder + POST /atlas/rebuild background job` — memory/compute-aware,
   ctx-cancel clean shutdown (§9).
10. `atlas: search + focused-bullet affinity` (5b/5c) — depends on 8.
11. `atlas: explore pane — region digest, member-chunk cards (reuse evidence attach), link hops` (5a).
12. `atlas: network view — region graph, semantic zoom to chunk nodes, right-hand detail pane` (§6);
    depends on 4, 7, and `/atlas/graph.json`.
13. `atlas: LLM digests via shared instruct model` — depends on `flying-shuttle-hkc`.
14. `infra: Python subprocess resource caps + clean teardown` (§9) — applies to
    embedder and instruct LLM.
15. addendum to `flying-shuttle-hkc`: the chosen instruct model also serves Atlas digests.
