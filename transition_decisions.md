# Transition Decisions & Open Questions

Running log of decisions made (and questions deferred) while executing the
`flying-shuttle-6fv` epic from `datastar_transition.md`. Nothing here blocks
work — items are resolved with a sensible default and flagged for later review.

Format: **[D]** = decision taken, **[Q]** = open question for review.

---

## Global

### Task .2.1 — templ + Datastar build integration

- **[D]** `github.com/a-h/templ v0.3.1020` + `github.com/starfederation/datastar-go
  v1.2.2` added. Datastar **v1.0.3** runtime vendored at
  `internal/web/static/vendor/datastar-v1.0.3.js` (from the GitHub release —
  npm `@starfederation/datastar@latest` is a stale beta). SDK v1.2.2 speaks the
  v1.0 `datastar-patch-elements` protocol; runtime confirmed to match.
- **[Q][important]** templ's latest requires **Go ≥ 1.25**, so `go.mod` bumped
  `go 1.24.0` → `go 1.25.0` and the Go toolchain auto-downloads 1.25/1.26.
  `instruction.md` still says "Go 1.24+". Everything builds & tests green on the
  bumped toolchain. If a 1.25 requirement is unacceptable, pin templ to an
  older release (`v0.2.x` supports 1.21+) at some feature cost.
- **[D]** New package `internal/web`:
  - `web.go` — `//go:embed static`, `StaticFS()`, `DatastarScriptPath` const
    (bump path + vendored file together).
  - `render.go` — `Render` (plain HTML), `Patch`/`PatchInto` (Datastar SSE
    morph via `sse.PatchElementTempl`), `RenderString` (tests).
  - `components/base.templ` — the HTML document shell (`Base(title)` with
    `{ children... }`), links `/static/app.css` + the Datastar module.
  - `static/app.css` — placeholder; real styles land in `.2.2`.
- **[D]** `*_templ.go` **committed** (not gitignored) so `go build ./...` works
  without the templ CLI; marked `linguist-generated` in `.gitattributes`.
  `make build|test|lint` run `templ generate` first; `make clean` deletes them;
  `make dev` = `templ generate --watch --proxy ... --cmd 'go run ./cmd/shuttle'`.
- **[D]** `//go:generate` in `web.go` uses `go run github.com/a-h/templ/cmd/templ
  generate` — no separately installed binary needed. `make templ-tools` installs
  the standalone CLI for LSP/watch.
- **[D]** Nothing is wired into the router yet — that's `.2.2`. The React app is
  untouched and still the served frontend.

### Task .1.4 — Transcript-ordered retrieval service

- **[D]** New package `internal/transcript`: `Service{Store}` with
  `WindowAround(chunkID, radius)` and `WindowFrom(sourceFile, charOffset,
  radius)`. Returns a `Window` = ordered `Segment`s (one per chunk, `Focus`
  flag on the centered one) + a concatenated `Text` for continuous reading +
  `HasPrev/HasNext` and `PrevChunk/NextChunk` ids to page the window one step
  earlier/later (this is what `.3.5`'s `dir=prev|next` maps to).
- **[D]** New store method `ListChunksBySourceFile(sourceFile)` ordered by
  `start_offset` (added to the `Store` interface).
- **[D]** `DefaultRadius = 2` (5-chunk window). `.3.5` can override per request.
- **[D]** `Segment.CharStart/CharEnd` carry the chunk's absolute offset within
  the source, so `.3.6` selection math can map a highlight back to
  `{chunk_id, char_start, char_end}`.
- **[Q]** `Window.Text` joins segments with a single space. If chunk offsets
  actually abut (they should for `ChunkTranscript`, which advances `pos` by
  `len+1`), this is right; if a real transcript has odd spacing we may want to
  reconstruct from the raw file instead. Fine for now.

### Task .1.3 — Stitch/export use excerpt

- **[D]** `dag.collectChunks` now iterates `store.ListNodeEvidence(nodeID)` and
  feeds each evidence row's **`.Text`** (the chosen excerpt) into the stitcher,
  not the whole `chunk.Content`. Speaker is looked up from the chunk. Node body
  fallback (no evidence) unchanged.
- **[D]** Dedup key changed from chunk ID to `chunkID:charStart:charEnd`, so two
  nodes citing *different* spans of one chunk both render, but an identical span
  reused is emitted once.
- **[D]** Export (`internal/export`) needed no change — it consumes
  `LinearizeResult.Stitch` which is now excerpt-based end to end.
- **[D]** Regression test `TestLinearizeAndStitch_usesExcerptNotWholeChunk`:
  attaches a middle span of a chunk, asserts the manuscript contains it and not
  the surrounding `SECRET_PREAMBLE` / `SECRET_TAIL`.

### Task .1.5 — internal/outline service

- **[D]** New package `internal/outline`:
  - `tree.go` — pure: `TreeNode`, `BuildTree`, `Flatten`, `Find`, `Neighbors`
    (for `data-prev-id`/`data-next-id`). Port of `outlineStore.buildTree`:
    outline-only nodes, linear-only edges, roots = no incoming linear edge,
    children by edge weight then creation time, roots by creation time.
  - `service.go` — `Service{Store}` with `AddRoot/AddChild/AddChildAt/
    AddSibling/Indent/Unindent/Delete` + `Tree()`.
- **[D]** No `Store.WithTx` added. The structural ops compose the existing
  **atomic** `store.MoveNode` (delete-incoming-linear + reweight + insert, all
  in one tx) with `CreateNode`. Worst-case failure window = an orphan root node
  between `CreateNode` and `MoveNode` (recoverable, not corrupting). Revisit if
  we ever see it in practice.
- **[D]** `Indent`/`Unindent` read the tree, locate the node among its
  siblings, then delegate to `MoveNode`. `Indent` on the first sibling and
  `Unindent` on a root return `outline.ErrNoop` (caller treats as silent
  success — matches the React early-returns).
- **[Q]** `AddSibling` on a **root** node just creates another root; since roots
  order by `CreatedAt` the new node lands last, not necessarily immediately
  after the anchor. Matches the old client ("root level, no edge needed"). If
  we want true positional root ordering later, roots need explicit weights (a
  synthetic parent, or a `root_order` column).
- **[D]** `BuildTree` sorts outline nodes by `CreatedAt` (tie-break ID) rather
  than trusting input slice order, so callers don't have to pre-sort.

### Task .1.2 — Evidence-as-text-span schema

- **[D]** New `evidence` table (migration `005_evidence.sql`): `id, node_id,
  chunk_id, source_file, char_start, char_end, text, position, created_at`.
  `char_start/char_end` are **rune** offsets into `chunks.content`; whole-chunk
  = `0 .. len([]rune(content))`. `text` stores the resolved excerpt verbatim
  (plan §2 option B) so stitch/export never re-resolves.
- **[D]** Migration backfills `evidence` from existing `node_chunks` (whole-chunk
  spans), guarded by `NOT EXISTS` so re-running is a no-op. Migrations have no
  version table — they're plain idempotent SQL run in order.
- **[D]** `node_chunks` table is **kept** (not dropped) but no code writes it
  anymore. `GetNodeChunks` / `SetNodeChunks` / `ListUsedChunkIDs` were
  reimplemented on top of `evidence`: `SetNodeChunks` writes whole-chunk
  evidence rows, `GetNodeChunks` returns the distinct source chunks in evidence
  order, `ListUsedChunkIDs` unions both tables. Kept the old API surface so the
  React `PUT /nodes/{id}/chunks` path keeps working until cutover.
- **[D]** `SnapshotData.NodeChunks` → `SnapshotData.Evidence []model.Evidence`
  (json `evidence`). `NodeChunks` kept as a legacy read-only field; restore
  converts old-snapshot `node_chunks` entries into full-span evidence rows by
  looking up chunk content.
- **[D]** New Store methods: `CreateEvidence`, `ListNodeEvidence`,
  `ListAllEvidence`, `DeleteEvidence`, `DeleteNodeEvidence`.
- **[Q]** `node_chunks` table + `NodeChunkAssoc` type are now dead weight. Drop
  in a later cleanup once the React path is gone (`.6.1`).

### Task .1.1 — Drop audio ingest

- **[D]** Deleted `internal/ingest/transcribe.go` (Transcriber, StubTranscriber)
  and `internal/ingest/chunker.go` (the embedding-boundary "semantic" chunker
  `ChunkSegments` + `ChunkerConfig`). Text transcripts use `ChunkTranscript`
  (~160-word greedy chunks) which is all that remains.
- **[D]** `chunker_test.go` → renamed `embed_helpers_test.go`; dropped the
  `TestChunker_*` cases, kept the CosineSimilarity / Float32s / StubEmbedder
  tests (those helpers live in `embed.go`).
- **[D]** `NewRouter` signature changed: dropped `transcriber ingest.Transcriber`
  and `chunker *ingest.Chunker`; added `clusterEmbedder ingest.Embedder` (the
  cluster-suggestion feature still needs an embedder; `main.go` passes a
  `StubEmbedder` as before).
- **[D]** `upload_handler.go`: `create` now rejects any non-text extension;
  `startProcessing` always runs the transcript path; `transcribeAsync` deleted;
  `rechunk` re-runs `ChunkTranscript`.
- **[Q]** `model.UploadStatusTranscribing` is still the "processing" status for
  text ingest — misnamed now. Left as-is to avoid a migration; rename to
  `processing` during `.2.3` or `.6.1` if we touch the upload UI copy.
- **[Q]** React `AudioUpload.tsx` still lists audio extensions in its `accept`
  filter. Harmless (backend 400s them) and the file is deleted at `.6.1`; not
  touching the React app now.


- **[D]** Working on branch `plan/datastar-transition`. Each bd task = one commit,
  committed on completion, bd issue closed with a reason.
- **[D]** The repo has a large set of pre-existing uncommitted changes (main.go,
  handlers, web/src, internal/indexer, python/). These are left untouched; only
  files each task actually needs are staged.
- **[D]** templ + Datastar work builds alongside the existing React app until the
  cutover task (`.6.1`). The Go server can serve both.

---
