# Tasks — separate the writing project from the corpus

Execution checklist for `corpus_separation_plan.md`. Rationale lives in the
plan; this file is mechanical steps only. Work top to bottom.

---

## Phase 1 — split the store interfaces in-process

Same `*sql.DB` behind both interfaces. No migration, no behaviour change.

### 1.1 Create `internal/corpus`

- [ ] Create package `internal/corpus` with a `Store` interface and a
      `sqlStore` impl that wraps an injected `*sql.DB`.
- [ ] Define `corpus.Reader` (read subset: `GetChunk`, `GetChunksByIDs`,
      `ListChunksBySourceFile`).
- [ ] Move to `corpus.Store` the `chunks` methods: `CreateChunk`,
      `CreateChunks`, `GetChunk`, `ListChunks`, `ListChunksPage`,
      `ListChunksBySourceFile`, `ListChunkIDs`, `ListChunkIDsWithEmbedding`,
      `GetChunksByIDs`, `ListChunksMissingEmbedding`,
      `CountChunksMissingEmbedding`, `SetChunkEmbedding`.
- [ ] Move the `uploads` methods: `CreateUpload`, `GetUpload`, `ListUploads`,
      `ListUploadsPage`, `UpdateUploadStatus`.
- [ ] Move the `transcript_segments` methods: `CreateTranscriptSegment`,
      `ListTranscriptSegments`.
- [ ] Move the `meta` methods: `GetMeta`, `SetMeta`.
- [ ] Add `corpus.Store.DB() *sql.DB` accessor for `atlas.NewStore`.

### 1.2 Narrow `internal/store` → `internal/doc`

- [ ] Rename package `internal/store` to `internal/doc` (type `store.Store`
      → `doc.Store`, `SQLiteStore` → `doc.sqlStore` or keep name).
- [ ] Keep only `nodes`, `edges`, `threads`, `thread_nodes`, `evidence`,
      `snapshots`, `branches` methods plus `ExportState`, `ImportState`,
      `MoveNode`, snapshot/branch surface.
- [ ] Update all import paths `.../internal/store` → `.../internal/doc`.

### 1.3 Delete legacy `node_chunks`

- [ ] Delete `GetNodeChunks`, `SetNodeChunks`, `ListUsedChunkIDs` from the
      interface and impl.
- [ ] Delete the `node_chunks` table (new migration, or drop from 001 if
      pre-release) and its Go model/refs.
- [ ] Delete the `node_chunks`-only tests.

### 1.4 Fix the three straddle sites

- [ ] `internal/outline/service.go:229` (`AttachEvidence`): change
      `s.Store.GetChunk` to a `corpus.Reader` field on `Service`; wire it in
      the constructor.
- [ ] `internal/web/evidence.go:124`: introduce `EvidenceFinder{Index,
      Corpus}`; replace the `GetChunk` call with `Corpus.GetChunk`.
- [ ] `internal/dag/linearize.go:101`: replace `GetChunk` (used only for
      `c.Speaker`) with an injected `speakerFor(chunkID) string` func, or
      drop the speaker annotation.

### 1.5 Unwind the `GetNodeChunks` join

- [ ] Replace `GetNodeChunks` callers with: `doc.ListNodeEvidence(nodeID)`
      → collect `chunk_id`s → `corpus.GetChunksByIDs` → reorder to the
      evidence order.
- [ ] Add `doc.ListNodeEvidence` if not already present.

### 1.6 Rewire dependency structs

- [ ] `web.Deps`: replace `Store store.Store` with `Doc doc.Store` +
      `Corpus corpus.Store`.
- [ ] `api.Deps`: same replacement.
- [ ] `atlas.NewStore`: pass `corpus.DB()` instead of the project `*sql.DB`.
- [ ] `cmd/shuttle/main.go`: construct one `*sql.DB`, wrap it in both
      `corpus.Store` and `doc.Store`, pass both into `web`/`api`.

### 1.7 Verify

- [ ] `go build ./...`
- [ ] `go test ./...`
- [ ] Manual smoke: ingest a transcript, attach evidence, view atlas.

---

## Phase 2 — two DB files, project↔corpus binding

### 2.1 Split migrations

- [ ] Create `internal/corpus/migrations/`: uploads (002), atlas (006, 008,
      009, 010), `chunks` half of 001, meta (007).
- [ ] Leave `internal/doc/migrations/` with the document tables (nodes,
      edges, threads, thread_nodes, evidence, snapshots, branches).
- [ ] Remove the `evidence.chunk_id` FK to `chunks` (recreate `evidence`
      without the constraint).
- [ ] Give each package its own migration runner + `schema_migrations`
      table.

### 2.2 Open functions

- [ ] `corpus.Open(path string, readOnly bool) (*Store, error)` — opens
      `corpus.db`, runs corpus migrations (skip when `readOnly`), sets
      `busy_timeout` and WAL.
- [ ] `doc.Open(path string) (*Store, error)` — opens `project.db`, runs
      doc migrations.

### 2.3 On-disk layout + resolution

- [ ] Implement path resolver for the new layout:
      `~/.shuttle/{config.json, corpora/<name>/, projects/<name>/}`.
- [ ] `projects/<name>/`: `project.db`, `project.json`, `outline.md`,
      `state.json`, `branches/`.
- [ ] `corpora/<name>/`: `corpus.db`, `corpus.bm25`, `corpus.hnsw`,
      `uploads/`, `corpus.lock`.
- [ ] Point `SHUTTLE_BM25_PATH` / `SHUTTLE_HNSW_PATH` defaults at the
      resolved corpus dir; move `uploads/` lookups there.

### 2.4 Binding files

- [ ] `project.json` reader/writer: `{"corpus": "<name>"}`.
- [ ] `~/.shuttle/config.json`: keep `{"current": "<project>"}`; add a
      corpus registry (list of corpus names or derive from `corpora/` dir
      listing).
- [ ] Corpus-name resolver: `project.json.corpus` → `corpora/<name>/`.

### 2.5 Boot sequence

- [ ] Implement: resolve project → read `project.json` → resolve corpus →
      `doc.Open(project.db)` (rw) → `corpus.Open(corpus.db)` (rw for now).
- [ ] `Deps.Corpus == nil` when `project.json` missing or corpus dir
      absent — do not fail boot.
- [ ] When `Corpus == nil`: hide evidence pane, transcript reader, atlas
      tab, ingest drawer; keep outline/threads/branches/snapshots/export.
- [ ] Add a "bind a corpus" picker stub in the project bar (full UI in
      Phase 3).

### 2.6 Migration command

- [ ] `shuttle migrate split` subcommand:
  - [ ] For each `~/.shuttle/<name>/`: create `corpora/<name>/` +
        `projects/<name>/`.
  - [ ] Copy `shuttle.db` → `corpora/<name>/corpus.db`; drop document
        tables.
  - [ ] Copy `shuttle.db` → `projects/<name>/project.db`; drop corpus
        tables; recreate `evidence` without the `chunk_id` FK.
  - [ ] Move `shuttle.bm25`, `shuttle.hnsw`, `uploads/` → corpus dir.
  - [ ] Move `outline.md`, `state.json`, `branches/` → project dir.
  - [ ] Write `projects/<name>/project.json = {"corpus": "<name>"}`.
  - [ ] Rename original `~/.shuttle/<name>/` → `~/.shuttle/<name>.pre-split/`.
- [ ] Refuse to run if target dirs already exist.

### 2.7 Verify

- [ ] `go build ./...` / `go test ./...`
- [ ] Run `shuttle migrate split` against a copy of a real `~/.shuttle`;
      confirm outline + evidence + atlas load from the split dirs.
- [ ] Confirm a project with no `project.json` boots degraded.

---

## Phase 3 — writer lock, read-only corpus sessions

### 3.1 Advisory writer lock

- [ ] On boot, after resolving the corpus, try to acquire
      `corpora/<name>/corpus.lock` (`O_EXCL` create with pid, or a
      `BEGIN IMMEDIATE` probe).
- [ ] Acquired → open `corpus.db` read-write; hold lock for process
      lifetime; release + remove on clean shutdown.
- [ ] Not acquired → open `corpus.db` read-only; record the holding
      project name (from lock file contents) for the UI message.
- [ ] Handle stale lock (pid not alive) → reclaim.

### 3.2 Gate the writers on lock ownership

- [ ] Only the lock holder starts: ingest pipeline, embedding `Backfiller`,
      index `Snapshotter`, `reconcileEmbeddingModel`, atlas rebuilds.
- [ ] Non-holder: load BM25/HNSW snapshots read-only, `atlas.LoadCurrent`,
      no snapshot writes.
- [ ] Set `busy_timeout` on both DB handles.

### 3.3 Read-only UI affordances

- [ ] Hide/disable the Ingest drawer and the atlas "rebuild" button for
      non-holders.
- [ ] Show "corpus is read-only — held by <project>" where those controls
      were.

### 3.4 Named-corpus picker

- [ ] Project-bar picker listing corpora from `corpora/` (name + maybe
      chunk count).
- [ ] Selecting one writes `project.json.corpus` and re-execs / reloads.
- [ ] "Create new corpus" path: make `corpora/<name>/`, run corpus
      migrations, bind.

### 3.5 Read-only sessions notice a new build (§5.4)

- [ ] Server: on `/atlas/status` (or a slow poll), read-only sessions
      re-check `CurrentBuild()` id/timestamp; swap `s.current` when it
      changed.
- [ ] Client: reuse the `atlasBuiltAt` signal →
      `window.atlasGraph.syncTo()` to repaint.

### 3.6 Verify

- [ ] Open the same corpus from two projects; confirm second is read-only
      with the message shown.
- [ ] Rebuild atlas in holder; confirm read-only session repaints.
- [ ] `go build ./...` / `go test ./...`

---

## Phase 4 — append-only chunks, evidence editing, doctor

### 4.1 Soft-delete chunks

- [ ] Add `deleted_at` (nullable) to `chunks`; migration in the corpus set.
- [ ] All chunk reads default to `deleted_at IS NULL`; add an
      `includeDeleted` variant for the doctor.
- [ ] Re-ingest / rechunk: insert new chunk rows with new ids, set
      `deleted_at` on the superseded rows instead of deleting.
- [ ] Atlas build: exclude `deleted_at IS NOT NULL` chunks.

### 4.2 Evidence write validation

- [ ] `corpus`-aware `CreateEvidence` (or an outline-service guard):
      reject unless `corpus.GetChunk(chunkID)` resolves.

### 4.3 `evidence.edited` flag

- [ ] Add `edited` boolean (or `edited_at` timestamp) to `evidence`;
      migration in the doc set.
- [ ] `model.Evidence` gets the field; include in scan/insert/update column
      lists.
- [ ] Set the flag in `EditQuote` (trim + splice) and in the new free-form
      op.

### 4.4 Free-form "edit quote" op

- [ ] `outline.Service`: add an op that replaces `evidence.text` with
      author-supplied text; update the `chunk_ref` node `Body`/`Title`;
      leave `char_start/end` as best-effort bounds.
- [ ] Web handler + route for the op.
- [ ] UI: editable quote field on `chunk_ref` bullets; on save call the op.
- [ ] UI: badge "edited — differs from source" when `edited` is set.
- [ ] "Jump to source" for an edited row targets the whole chunk, not the
      stale span.

### 4.5 `shuttle doctor` evidence classifier

- [ ] Command that walks every `evidence` row against the bound corpus and
      bins each into:
  1. chunk id unresolved,
  2. resolves but `text` diverges and `edited` is false,
  3. resolves, `text` diverges, `edited` is true (verbose only).
- [ ] Actions: detach (bin 1); re-anchor to current chunk or accept
      divergence (bin 2).
- [ ] Report "cites a superseded chunk" for evidence pointing at a
      soft-deleted chunk.
- [ ] Optional UI pane surfacing the same counts.

### 4.6 Verify

- [ ] Re-ingest a corrected transcript; confirm old evidence still resolves
      and atlas drops the old chunks.
- [ ] Edit a quote in two projects citing one chunk; confirm independence
      and no corpus write.
- [ ] Run `shuttle doctor` against a corpus with a detached chunk and an
      edited row; confirm correct binning.
- [ ] `go build ./...` / `go test ./...`
