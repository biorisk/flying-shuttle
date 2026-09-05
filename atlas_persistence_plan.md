# Atlas — Expensive-Compute Persistence Plan

**Goal:** every expensive computation (LLM call, embedding call, or heavy
CPU job) is persisted the moment it finishes, in the smallest unit that makes
sense, so that:

- a crashed / killed / restarted build never re-does work it already finished;
- a rebuild after a small corpus change (one new transcript) only recomputes
  what actually changed;
- the LLM being down degrades gracefully and self-heals on the next build.

The model to copy is **chunk labels** (`atlas_chunk_label`, Phase E): a
build-independent table keyed by an immutable input identity, written per
batch, with a provisional/final distinction (`source = "head"` vs
`"llm:<model>"`) so provisional rows are retried later.

---

## Inventory

### A. LLM (instruct model) calls

| # | Operation | Call site | Volume / build | Persisted today? | Smallest sensible unit | Action |
|---|-----------|-----------|----------------|------------------|------------------------|--------|
| A1 | **Region digest** (title / abstract / keywords) | `LLMSummariser.Summarise` ← `builder.assemble` Phase B loop | 1 per region (~213) | ❌ held in memory, bulk-written by `InsertRegions` at the **end of Phase C** — a crash in Phase B loses every digest computed so far | 1 region, keyed by a hash of its member chunk-id set | **Fix — A1** |
| A2 | **Transcript digest** | `buildTranscriptDigests` ← Phase D | 1 per source file (~100+) | ❌ bulk-written by `InsertTranscriptDigests` at the **end of Phase D** | 1 file, keyed by `(source_file, member chunk-id set hash)` | **Fix — A2** |
| A3 | **Chunk label** | `ChunkLabeller.Label` ← Phase E | 1 per ~12 chunks | ✅ `PutChunkLabels` **per batch**, keyed by `chunk_id`, `head`/`llm:` distinction | 1 batch of ~12 chunks | ✅ done — this is the reference implementation |
| A4 | Cluster labelling | `search.LLMClusterer.Cluster` | on demand | ❌ | — | **Not wired** in the templ UI (`api.Deps.ClusterEmbedder` is unused, no route). Note only; if revived, cache keyed by the retrieved chunk-id set. |
| A5 | Manuscript stitching | `stitch.PromptStitcher` | per preview / export | ❌ regenerated every time | — | **Not wired** — prod uses `stitch.StubStitcher`. Note only; if a real completer is wired, cache stitched spans keyed by `(thread snapshot, glue setting)`. |

### B. Embedding (embed model) calls

| # | Operation | Call site | Persisted today? | Action |
|---|-----------|-----------|------------------|--------|
| B1 | **Chunk embeddings** | `indexer.Backfiller.runOnce` | ✅ `SetChunkEmbedding` **per batch** (default 16), `embedding_vec IS NULL` drives the work list | ✅ done |
| B2 | **Region digest vector** | `EmbedDigests` ← Phase C | ❌ one `EmbedBatch` for all regions, result bulk-written by `InsertRegions` | **Fix — B2**, folded into A1: embed each new digest and persist its vector with the digest row |
| B3 | Query embeddings | hybrid search, `SemanticLocate`, `atlas.Service.RankForText` | ❌ | **Out of scope** — inherently per-query, not reusable, already cheap (1–2 texts). |
| B4 | Cluster embeddings | `search.EmbeddingClusterer` | ❌ (stub embedder in prod) | Not wired. Note only. |

### C. Heavy CPU (no model)

| # | Operation | Call site | Notes | Action |
|---|-----------|-----------|-------|--------|
| C1 | k-means partition | `ClusterChunks` ← Phase A | O(n²) farthest-pair per bisect; deterministic for a seed; seconds for thousands of chunks. Rebuilt from scratch every build **by design** (this is *why* region ids are unstable and A1 must be content-hash-keyed, not region-id-keyed). | No persistence; acceptable |
| C2 | Region keyword TF-IDF | `Keyworder.TagRegions` ← Phase C | ~ms, deterministic, corpus-wide IDF | No persistence; acceptable |
| C3 | BM25 + HNSW index | `internal/indexer` | Already snapshotted to disk + reconciled incrementally on boot (`LoadAndReconcile`) | ✅ done |
| C4 | Chunk-chunk similarity graph | `atlas.BuildChunkEdges` ← graph endpoint | O(n²) cosine, recomputed per `/atlas/graph.json` request | Separate concern — cache later if it shows up in traces; not LLM/embed |

---

## Design — one shared cache table for digests

Region digests (A1) and transcript digests (A2) are the same shape and the
same problem: an LLM summary of a fixed set of immutable chunks. They share
one **build-independent, content-addressed** cache, exactly like
`atlas_chunk_label`.

### Migration `010_atlas_digest.sql`

```sql
CREATE TABLE IF NOT EXISTS atlas_digest (
    input_hash TEXT PRIMARY KEY,           -- sha256 of the summariser input identity
    kind       TEXT NOT NULL,              -- 'region' | 'transcript'
    title      TEXT NOT NULL DEFAULT '',
    abstract   TEXT NOT NULL DEFAULT '',
    keywords   TEXT NOT NULL DEFAULT '',   -- newline-joined
    vec        BLOB,                       -- digest embedding; NULL until embedded
    source     TEXT NOT NULL DEFAULT '',   -- 'llm:<model>' (final) | 'extractive' (retried)
    created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now'))
);
CREATE INDEX IF NOT EXISTS idx_atlas_digest_kind ON atlas_digest(kind);
```

Not cascade-linked to `atlas_build` — same lifecycle as `atlas_chunk_label`.

### Input hash

- **Region:** `sha256("region\0" + strings.Join(sortedMemberChunkIDs, "\n"))`
  Chunk ids are immutable and content is immutable, so identical membership ⇒
  identical digest input. k-means reshuffles clusters between builds, but an
  unchanged corpus reproduces every cluster exactly (fixed seed) → 100% cache
  hits; adding one transcript leaves most clusters intact → mostly hits.
- **Transcript:** `sha256("transcript\0" + sourceFile + "\0" + strings.Join(sortedChunkIDs, "\n"))`
  A transcript whose chunks didn't change reuses its digest across every
  future rebuild.

### Provisional vs final

Mirror the chunk-label `head`/`llm:` rule:

- `source = "llm:<model>"` → **final**, always reused.
- `source = "extractive"` → **provisional**: reused only when no LLM is
  configured; recomputed (and the row upserted) when an LLM is available.
  This auto-upgrades digests that were computed while the LLM was down.

### Store surface (`atlas.Store`)

```go
GetDigests(hashes []string) (map[string]CachedDigest, error) // batched IN, 400/query
PutDigest(d CachedDigest) error                              // upsert by input_hash
SetDigestVec(inputHash string, vec []float32) error
```

`CachedDigest{ InputHash, Kind string; Digest; Vec []float32; Source string }`.

### Phase B (regions) — new flow

```
Phase A: ClusterChunks -> regions (centroid, members)         [unchanged]
Phase B:
  hashes := hash(region.members) for each region
  cached := Store.GetDigests(hashes)                          [1 query]
  toCompute := regions whose cache row is missing OR provisional-with-LLM
  for each region in toCompute:
      ctx check
      d := summ.Summarise(region member texts)
      Store.PutDigest({hash, "region", d, nil, source})       <-- PERSIST NOW (per region)
      region.Digest = d
  # embed only the digests that just got (re)computed
  vecs := Embedder.EmbedBatch(toCompute digest texts)         [1 batch call]
  for each: Store.SetDigestVec(hash, vec)                     <-- persist vec per row
  for cache hits: region.Digest / region.DigestVec = cached
Phase C: TagRegions (member keywords), BuildLinks, InsertRegions/InsertLinks
         (InsertRegions still writes the per-build denormalized digest columns,
          now sourced from cache-or-fresh)                    [unchanged timing]
```

A crash anywhere in Phase B loses **at most one** in-flight LLM call; the next
build's `GetDigests` returns everything already done.

### Phase D (transcripts) — same shape

```
group corpus chunks by source_file, sort by StartOffset
hashes := hash(file, sortedChunkIDs)
cached := Store.GetDigests(hashes)
for each file not cached (or provisional-with-LLM):
    d := summ.Summarise(full ordered text)
    Store.PutDigest({hash, "transcript", d, nil, source})     <-- PERSIST NOW (per file)
InsertTranscriptDigests(buildID, ...)  # per-build denormalized copy, from cache-or-fresh
```

### GC

`atlas_digest` rows are small text. Growth is bounded by "distinct cluster
shapes ever produced" + "distinct transcript states ever seen". Add a sweep in
`PruneExcept` (or a periodic job): delete `atlas_digest` rows older than N days
that were not referenced by the current build. Low priority — defer until it
matters.

---

## Work items

1. **`010_atlas_digest.sql`** + register in `SQLiteStore.Migrate`.
2. **`atlas.CachedDigest`** type; `atlas.Store` methods `GetDigests`,
   `PutDigest`, `SetDigestVec` + `sqlStore` impl (batched IN like
   `chunkLabelsWhere`).
3. **`atlas.digestInputHash(kind, parts...)`** helper (sha256).
4. **Builder Phase B**: cache lookup → compute-missing → `PutDigest` per
   region → batch-embed the new ones → `SetDigestVec` per row. Keep
   `InsertRegions` where it is.
5. **Builder Phase D**: cache lookup → compute-missing → `PutDigest` per file.
   Keep `InsertTranscriptDigests` where it is.
6. **`EmbedDigests`**: keep for the "embed a set of digests" primitive; Phase B
   calls it only for the freshly-computed subset.
7. Tests: cache hit avoids the LLM; provisional (`extractive`) upgrades when an
   LLM appears; crash mid-Phase-B (simulated) resumes from cache; adding a
   transcript only recomputes changed clusters.
8. Chunk labels (A3) — already compliant, no change.
9. Doc-comment `search.LLMClusterer` / `stitch.PromptStitcher` as "cache
   before wiring to a real completer" so the next person doesn't wire an
   un-cached LLM path.

## Out of scope

- Query-time embeddings (B3) — per-query, not reusable.
- k-means / TF-IDF / graph edges (C1, C2, C4) — CPU, deterministic, fast
  enough; revisit only if a profile says otherwise.
- Cluster / stitch LLM paths (A4, A5, B4) — not wired in the current UI.
