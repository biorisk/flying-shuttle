# Transition Decisions & Open Questions

Running log of decisions made (and questions deferred) while executing the
`flying-shuttle-6fv` epic from `datastar_transition.md`. Nothing here blocks
work — items are resolved with a sensible default and flagged for later review.

Format: **[D]** = decision taken, **[Q]** = open question for review.

---

## Global

### Tasks .5.2 + .5.3 — Snapshots & Branches (one commit)

- **[D]** Done together — they share the `components.Page` refactor (`Page` now
  takes a `PageContent` struct instead of a growing positional arg list),
  `internal/web/dag_handler.go`, and `components/dagbars.templ`.
- **[D]** Topbar now holds `#thread-bar` (`.5.1`), `#snapshot-bar`,
  `#branch-bar`. Snapshot bar: a "Snapshot" button (`@post('/app/snapshots')`,
  auto timestamp label) + a `<details>` menu with Restore/✕ per row. Branch bar:
  `<details>` showing the active branch, Switch/✕ per other branch, and an
  inline "new branch" form.
- **[D]** Routes under `/app`: `snapshots` (GET/POST), `snapshots/{id}/restore`
  (POST), `snapshots/{id}` (DELETE); same shape for `branches` +
  `branches/{id}/switch`. All wrap the existing `store` methods.
- **[D]** Restore and branch-switch rewrite the whole DAG, so they respond via
  `patchOutlineAndBars` — `#outline` + `#snapshot-bar` + `#branch-bar` patches +
  `{focusId:'', readerChunk:''}` signal reset.
- **[Q]** `<details>` menus are plain HTML (no Datastar) — fine, but they don't
  auto-close after an action. Minor polish for later (`data-on-click` toggling
  an `open` attr, or a signal).

### Task .5.6 — Stitch/preview view + glue slider

- **[D]** `GET /app/stitch?thread=&glue=` → `#stitch` fragment
  (`data-show="$centerView === 'preview'"`). Uses `dag.LinearizeAndStitch`
  (manuscript mode, or thread mode when `thread` set) with the injected
  `stitch.Stitcher` (`StubStitcher` by default; `web.Deps.Stitcher`, wired from
  `NewRouter`).
- **[D]** Verbatim passages → `<span class="span-chunk">`, AI glue →
  `<span class="span-glue">` (dimmed italic); stats line shows node count, char
  count, glue-ratio %. Glue `<input type=range data-bind-glue>` →
  `data-on-input__debounce.350ms="@get('/app/stitch?...')"`; `$glue` signal
  (default 50) added to Page.
- **[D]** The Preview tab button both flips `$centerView` and fires the fetch.
  `components.Page` gained a 4th `preview` arg.
- **[D]** Export link points at the existing `GET
  /api/v1/export/markdown/download?thread_id=` (wired live in `.5.7`).

### Task .4.1 — Outline drag-reorder replacement

- **[D]** Native HTML5 DnD, ~90 lines in `app.js`. A `.drag-handle`
  (`draggable="true"`, hover-revealed) on each non-evidence bullet. `dragover`
  computes a before/after/child zone from pointer-Y over the target row and
  paints `.drop-{zone}`. `drop` computes `{parent_id, position}` from the DOM
  tree, fills the hidden `#move-form`, and `requestSubmit()`s it →
  `@post('/app/outline/move')` → `#outline` morph.
- **[D]** `POST /app/outline/move` (form: `node_id, parent_id, position`) →
  `outline.Service.Move`, which rejects moving a node onto itself or a
  descendant (`ErrNoop`). Evidence (`chunk_ref`) bullets aren't draggable.
- **[Q]** Native DnD only — no touch support, no keyboard DnD, no drag image
  polish (the dnd-kit niceties are gone, as the plan predicted). Acceptable for
  desktop; revisit if touch matters.
- **[Q]** Not browser-tested (Playwright `.6.2`). Endpoint + `Service.Move` +
  fragment markup are covered by Go tests.

### Task .3.7 — Attach-evidence endpoint

- **[D]** `POST /app/outline/nodes/{id}/evidence` (form: `chunk_id`,
  `char_start?`, `char_end?`, `text?`) → `outline.Service.AttachEvidence`:
  creates a **locked `chunk_ref`** node (Title = 80-rune preview, Body = full
  excerpt, `Labels["source_file"]`), attaches it as the last child of `{id}`
  via `MoveNode`, and writes the `evidence` row. Degenerate/empty range →
  whole chunk. Returns `#outline` patch + `MarshalAndPatchSignals({focusId:
  parentID, readerChunk: ''})` — focus stays on the bullet you're editing, the
  reader closes.
- **[D]** `outline.Service` also gained `SetLocked(id, locked)` for the
  unlock affordance (used by `.5.5`/later; not yet surfaced in the UI).
- **[D]** The evidence bullet renders via the `.3.1` chunk_ref branch
  (`<blockquote class="bullet-evidence">`), so it appears immediately in the
  patched outline.
- **[Q]** Attach isn't one transaction (CreateNode → MoveNode → CreateEvidence).
  Same tradeoff as the other structural ops (`.1.5` decision). Worst case: an
  orphan locked node with no evidence row. Acceptable for now.
- Live-smoke: type bullet → upload transcript → attach chunk → locked evidence
  sub-bullet appears. ✔

### Task .3.6 — Highlight-to-excerpt selection JS

- **[D]** `internal/web/static/app.js` (vendored, `<script defer>` in Base) — the
  only imperative JS in the project. A `selectionchange` listener scoped to
  `.reader-body` maps the selection onto `{chunk_id, char_start, char_end,
  text}` and writes them into the hidden `#excerpt-form`.
- **[D]** Offsets are **relative to a single chunk's `Content`** (that's the
  evidence model). A selection crossing chunk boundaries is **clamped to the
  chunk it starts in** — chunks are ~160 words so a within-chunk selection is
  almost always what's wanted. `form.dataset.hasSelection` distinguishes
  excerpt vs whole-chunk.
- **[D]** `#excerpt-form` lives in `TranscriptReader`; its submit is already
  wired to `@post('/app/outline/nodes/'+$focusId+'/evidence', {contentType:
  'form'})` — the endpoint arrives in `.3.7`. No selection → `char_start/end`
  blank → whole `FocusChunk`.
- **[Q]** `selectionchange` fires a lot; the handler is cheap (DOM walk over a
  ~5-chunk window) so no debounce. Revisit if profiling says otherwise.
- **[Q]** Not browser-tested yet (Playwright `.6.2`). `node --check` passes;
  offset logic has unit-test-shaped risk — consider a jsdom test in `.6.2`.

### Task .3.5 — Transcript reader/scrubber fragment

- **[D]** `#evidence` now stacks two regions: `#evidence-candidates`
  (`data-show="!$readerChunk"`) and `#transcript-reader`
  (`data-show="!!$readerChunk"`). `$readerChunk` page signal toggles them.
- **[D]** `GET /app/evidence/transcript?chunk=&node=` → patches
  `#transcript-reader` + `MarshalAndPatchSignals({readerChunk})`. Uses
  `transcript.Service.WindowAround` (radius 2). Candidate cards get a "Read in
  transcript →" button; the reader has ‹ earlier / later › buttons that re-`@get`
  with `PrevChunk`/`NextChunk`, and a "← passages" button that clears
  `$readerChunk`.
- **[D]** Reader body renders one `<span class="reader-seg" data-chunk
  data-char-start>` per window segment (focus segment highlighted). The
  `data-char-start` (absolute source offset) is what `.3.6` maps a text
  selection back onto.
- **[Q]** Reader window doesn't reconstruct from the raw transcript file — it
  joins chunk `Content` with single spaces (see `.1.4` note). Good enough for
  `ChunkTranscript` output; revisit if real transcripts show spacing artifacts.

### Task .3.3 — Client-side collapse/expand

- **[D]** Purely client-side, no round-trip: parent bullets render a
  `.bullet-toggle` button with `data-on-click="$collapsed['<id>'] =
  !$collapsed['<id>']"` and `data-text` for the ▸/▾ glyph; the children `<ul>`
  gets `data-show="!$collapsed['<id>']"`. Server always renders the full tree.
- **[D]** Bracket notation `$collapsed['<uuid>']` (not dot) because node ids
  contain hyphens. `$collapsed` starts `{}` (in Page's `data-signals`);
  unset key → `undefined` → shown.
- **[Q]** Collapse state is lost on reload / lost when `#outline` is patched
  (Datastar re-runs `data-show` against the surviving `$collapsed` signal, so it
  should actually persist across patches — signal outlives the DOM). Matches the
  old client (collapse was ephemeral). Revisit only if users want it sticky.

### Task .3.2 — Bullet editing endpoints

- **[D]** Endpoints under `/app/outline`: `POST /roots`, and per node
  `PATCH /nodes/{id}` (title), `DELETE /nodes/{id}`, `POST /nodes/{id}/{sibling
  |child|indent|unindent}`. Structural ops return an `#outline` SSE patch **plus**
  `MarshalAndPatchSignals({focusId})` so the client focuses the right bullet.
- **[D]** Each bullet is a `<form>` wrapping `<input name="title">` +
  `<input type="hidden" name="version">`. Enter → `data-on-submit__prevent`
  = add sibling. Blur → `@patch(... {contentType:'form'})`. Tab/Shift-Tab/
  Backspace-empty/Arrows → one `data-on-keydown` expression
  (`components.keydownExpr`). All structural `@post`s use `{contentType:'form'}`
  and the handler **persists the anchor's title first** (`persistAnchorTitle`)
  so text typed-but-not-blurred isn't lost when Enter/Tab fires mid-edit.
- **[D]** `PATCH /nodes/{id}` returns **204** on success (input already shows the
  value — no fragment swap while editing); only a `store.ErrConflict` (stale
  version) triggers a full `#outline` resync.
- **[D]** Focus restoration: `data-effect` on `#outline` focuses
  `#bullet-<$focusId> .bullet-input` (and puts the caret at end) whenever
  `$focusId` changes — covers both click-focus and server-driven focus after a
  tree-swapping patch.
- **[D]** `outline.Service` gained `SetTitle(id,title,version)` and
  `FocusAfterDelete(id)`.
- **[Q]** The keydown handler is a single inline Datastar expression — works but
  dense. If it grows, move to a tiny vendored `outline.js`. Untested in a real
  browser yet (Playwright is `.6.2`); handler-level behaviour is covered.
- **[Q]** Datastar `@delete` sends no body (per spec), so Backspace-delete
  can't persist a pending title — but the bullet's being deleted, so moot.

### Task .3.1 — Read-only outline fragment

- **[D]** `outline.BuildTree` **extended** to include `chunk_ref` nodes as
  children (evidence sub-bullets) — outline nodes are still the only root
  candidates; `synth` and non-linear edges still ignored. Resolves the `.1.5`
  open question: evidence attaches as a real locked bullet in the tree.
- **[D]** `GET /app/outline` → SSE patch of `#outline`; shell SSRs it too.
  `components.Outline` owns `<section id="outline" data-show="$centerView===
  'outline'">`; `components.bullet` recurses. Each `<li id="bullet-{id}">`
  carries `data-node-id`, `data-prev-id`, `data-next-id` (flattened-order
  neighbours, computed in `outlineView`) and `data-depth`.
- **[D]** Read-only: titles render as `<span>`, evidence bullets as
  `<blockquote>` (body → title fallback). Editing / add-control / drag come in
  `.3.2`+.
- **[D]** `web.Mount` now fills `Deps.Outline/Transcript/Ingester` from
  `Deps.Store` when nil, so callers/tests can pass just the store.

### Task .2.3 — Ingest drawer fragment

- **[D]** New package `internal/pipeline` — the transcript ingestion pipeline
  (`Ingester{Store, UploadDir, Index, AfterIngest}`: `Accept`, `Start`,
  `StartPending`, `Rechunk`) extracted from `api.uploadHandler` so both the JSON
  API and the server UI share one path. It's a separate package because
  indexing pulls in `internal/search`, which already imports `internal/ingest`
  (would cycle). `api.uploadHandler` now just delegates; `NewRouter` builds one
  `*pipeline.Ingester` and passes it to both the handler and `web.Deps`.
- **[D]** `GET /app/ingest` (SSE patch of `#ingest`) + `POST /app/ingest`
  (multipart, field `files`, accepts many). The upload form uses
  `data-on-submit__prevent="@post('/app/ingest', {contentType:'form'})"` with
  `enctype="multipart/form-data"` — Datastar v1.0.3 sends the FormData (files
  included) as the body when the form is multipart. Verified in the runtime.
- **[D]** While any upload is pending/processing the fragment self-polls:
  `data-on-interval__duration.2s="@get('/app/ingest')"`. Drops the attribute
  once everything is `done`/`failed`.
- **[D]** Shell SSRs the drawer via `components.Ingest(h.ingestView())`;
  `components.Page` now takes a third `ingest templ.Component` arg.
- **[Q]** No dedicated `/events` SSE stream yet (bd `.2.3` scope was just the
  drawer). Interval-poll is the status mechanism for now; a single push stream
  can replace it later if it feels heavy.

### Task .1.6 — /evidence retrieval endpoint

- **[D]** `web.EvidenceFinder{Index, Store}` → `Find(ctx, query, limit)
  []viewmodel.Candidate`. Runs `HybridIndex.Search` (already degrades to
  BM25-only if the embedder is down), resolves hits to chunks, trims snippets
  to 320 runes. Blank query / nil index → `nil`.
- **[D]** `GET /app/evidence?q=&node=` always responds as a **Datastar SSE
  patch** of the `#evidence` fragment (`web.Patch` → `PatchElementTempl`).
  Consumers are always `@get` (wired in `.3.4`); the shell SSRs the idle pane
  directly via `components.Evidence`.
- **[D]** New leaf package `internal/web/viewmodel` for the plain structs shared
  between handlers and templ components (`Candidate`, `EvidencePane`) — keeps
  `components` free of a dependency on `internal/web`.
- **[D]** `components.Page` reworked so the outline/evidence **fragment
  components own their own root element** (`#outline`, `#evidence`); Page embeds
  the same component for SSR and renders a matching placeholder when nil. No
  more double-id.
- **[Q]** "Suggested sub-points" (ex-ghost-proposals) not built here — the
  `EmbeddingClusterer` needs a live embedder. Deferred to `.3.4`/`.5.x`; the
  candidate list is the core and is complete. Card buttons (read-in-transcript,
  attach) are layered on in `.3.5`–`.3.7`; cards currently carry
  `data-chunk`/`data-source` hooks only.

### Task .2.2 — Two-pane app shell + route

- **[D]** Server UI mounted under **`/app`** (shell at `/app/`) and **`/static/*`**,
  registered by `web.Mount(r, web.Deps{...})` from `api.NewRouter` *before* the
  React static catch-all. React stays at `/`. Cutover (`.6.1`) moves `/app` → `/`.
- **[D]** `web.Deps{Store, Outline, Transcript, Index}` assembled in `NewRouter`
  (`&outline.Service{Store:s}`, `&transcript.Service{Store:s}`, `idx`). Fields
  will be consumed by later fragment handlers.
- **[D]** `components.Page(outline, evidence templ.Component)` — the shell owns
  layout + the page signals (`data-signals="{ focusId:'', drawerOpen:false,
  centerView:'outline', threadId:'', evidenceWidth:420, collapsed:{} }"`).
  `#outline`/`#evidence`/`#ingest`/`#thread-bar`/`#preview` are empty regions
  filled by their own tasks. Shell handler currently passes `nil, nil`; `.3.1`
  changes it to SSR the outline.
- **[D]** Datastar v1 attribute forms verified against the vendored runtime:
  `data-on-click`, `data-class="{k: expr}"`, `data-show`, `data-signals`,
  `data-style-<css-prop>="expr"`. (v1 uses hyphens, not `data-on:click`.)
- **[D]** `DatastarScriptPath` const **moved to `components`** (was in `web`) to
  break a `web ↔ components` import cycle; `web` re-exports it.
- **[D]** templ wired as a **go.mod `tool` directive** (`go get -tool`); build via
  `go tool templ generate`. `go run .../templ generate` failed on missing
  transitive go.sum entries — the tool directive fixes that.
- **[D]** Real `static/app.css` written: dark theme ported from the React
  `index.css` palette (`#1a1a2e`/`#16213e`), CSS-grid shell with a
  `transition`-animated drawer column.
- Smoke-tested live: `/app/`, `/static/*`, `/api/v1/*` all 200.

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
