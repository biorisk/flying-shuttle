# React → Datastar + templ: Redesign & Migration Plan

> This is **both a redesign and a stack migration**. The current React app's
> structure encodes an interaction model that turned out to be wrong (see §0).
> The Datastar rebuild is the moment to fix it — porting the current UI verbatim
> would carry the clunkiness forward.

---

## 0. What Flying Shuttle is for, and how the UI should serve that

### The purpose (unchanged)

Turn **transcripts into a book**. You have long transcripts of recorded speech
(interviews, dictation, conversations). You want a structured written work that
stays in the subject's own words. The thesis:

- **The raw words are the material.** Transcript text is immutable — never
  paraphrased, never grammar-corrected.
- **You write the argument, not the prose.** You build an outline of what the
  work should *say*.
- **The evidence rises to meet the structure.** As you write a point, the system
  surfaces the transcript passages that support it (hybrid BM25 + vector search).
- **You lock a point once it's grounded.** A locked bullet means "this is true
  and it is backed by this specific passage."
- **The manuscript assembles itself.** Export walks the outline in order and
  *stitches* the locked passages into continuous text. The LLM is on a very
  short leash: it may only add minimal "glue" (transitions, pronouns,
  conjunctions), never paraphrase or invent, and every span of output is
  attributed as verbatim-passage or AI-glue.

On top of that, the outline is a **DAG, not a document**:

- **Threads** — named reading paths. Different audiences traverse a different
  sequence of nodes through the same graph.
- **Branches / CYOA** — `branch` edges are choose-your-own-adventure forks;
  conditional edges route by audience. Branches are alternate working copies;
  snapshots save/restore/diff the whole DAG.

Canonical workflow: `Outline point → retrieval → locked evidence sub-bullets → synthesis`.

### The interaction model (this is what was wrong)

The app is **two working panes**, not three, running one tight loop:

**Outline (main, center)** + **Evidence (right)**. The **ingestion pane is a
collapsed drawer** on the left — pulled out only to load transcript files, then
dismissed. It is not part of the working surface.

The loop:

1. You edit a bullet in the outline.
2. **As you type, the bullet text is the query.** Candidate transcript passages
   stream into the Evidence pane live. **There is no separate search** — search
   *is* typing in the outline.
3. The Evidence pane shows a candidate **in the context of its source
   transcript**. You can **scrub backward and forward through that transcript**,
   crossing chunk boundaries seamlessly, because **chunks are not meaningful
   units** — they are only embedding-sized windows. You read around a hit to
   find the exact words you want.
4. You attach evidence with a **button**:
   - press it → the **whole chunk** becomes evidence, or
   - **highlight a sub-span** of the transcript text first, then press → just
     that **excerpt** becomes evidence.
5. Each press **appends the selected passage as a locked sub-bullet under the
   bullet you are currently editing**. It stays locked until you explicitly
   unlock it.
6. Repeat. Beneath each point, the outline grows locked, evidence-backed
   children as you think out loud in the parent bullet.

### Consequences for the rebuild

| Current app | Rebuild |
|---|---|
| Tri-fold; all three panes always visible | **Two panes** (Outline + Evidence); ingestion is a collapsible left drawer, collapsed by default |
| **Audio Ribbon** — semantic heatmap, cluster colors, draggable segments, drag "string" | **Deleted entirely.** Removes `AudioRibbon`, `RibbonDragString`, cluster-coloring code |
| **SearchBox** — standalone surface, draggable hits | **Deleted.** Folded into as-you-type outline → Evidence |
| **GhostSubList** — inline ambient proposals with ✓/✗ rendered in the outline | Ghost proposals become **"suggested sub-points" ranked in the Evidence pane**, attached via the same button/flow as any other evidence |
| Attach evidence by **dragging** a chunk onto a bullet | Attach by **button**, with optional text selection for a sub-span. No drag for evidence. |
| Attach = **whole chunk only** (`SetNodeChunks([]string)`) | Attach = **arbitrary text span**: whole chunk *or* a highlighted excerpt |
| Evidence Drawer = static list of a node's already-attached chunks + fidelity badges | Evidence pane = **live candidate retrieval + transcript reader/scrubber**. A bullet's *attached* evidence is just its locked sub-bullets in the outline — no separate list needed |
| Chunking framed as semantic ("topic boundaries") | Chunking is **purely to bound embeddings**. Boundaries are arbitrary. Sub-chunk excerpts are the normal case |
| **Audio** upload + transcription pipeline | **Dropped for simplicity.** Transcripts only (`.txt/.md/.markdown/.text`). No audio player; the Evidence scrubber is text-only. `StubTranscriber` / audio-format handling goes away |
| Outline **drag-reordering** (dnd-kit) | **Stays** — this is the one interaction that still needs a dnd-kit replacement |

### Design values the UI must preserve

- **Verbatim fidelity is sacred.** Attribution everywhere: passage-vs-glue span
  styling, glue-ratio %, "AI only provides glue." The manuscript must never
  contain text the writer did not explicitly choose — so the stitch/export path
  must use the **excerpt**, not the full chunk.
- **Retrieval is ambient and folded into writing** — no modal search, no
  separate query box; the Evidence pane just tracks the current bullet.
- **Keyboard-first structural editing** — the outline is a real outliner
  (Enter / Tab / Shift-Tab / arrows / Backspace-on-empty); its focus/caret loop
  is load-bearing and fragile under fragment swaps.
- **Non-linear from the ground up** — threads, branches, CYOA exits, conditional
  edges: one DAG, many readings.
- **Reversible exploration** — snapshots, branches, visual diff, rescue.

---

## 1. High-level overview of the stack change

| Concern | Today | After |
|---|---|---|
| Rendering | React VDOM in browser | templ components render HTML server-side |
| Derived data (tree, diff) | computed in TS stores | computed in Go |
| App state | 8 Zustand stores | SQLite (canonical) + a handful of Datastar signals (ephemeral UI) |
| Mutations | JS orchestrates multiple `fetch` calls, optimistic patch | one Go endpoint per user action, transactional, returns re-rendered fragment |
| Transport | JSON `{data,error,meta}` envelope | HTML fragments over SSE (`@get/@post/...` → morph) |
| Retrieval UI | separate SearchBox + Audio Ribbon + inline ghost sublist | one thing: `data-on:input__debounce` on the bullet → Evidence pane |
| Routing | react-router (1 real route) | server routes; single URL, no client router |
| Build | Vite + npm + tsc + React + dnd-kit + zustand | `templ generate` → `go build`; one vendored `datastar.js` |
| Real-time | implicit (refetch after actions) | short-lived action SSEs; optional `/events` stream for ingest status |
| DnD | dnd-kit (ribbon drag + outline reorder + drag-to-attach) | **outline reorder only** — native HTML5 DnD or a small vanilla helper |

**The shape of the app is already server-authoritative** — state lives in SQLite,
handlers are thin. What moves is *rendering* plus the derived/orchestration logic
in the TS stores (mostly `outlineStore`). What the redesign removes — the ribbon,
the search pane, the inline ghost sublist, audio, drag-to-attach — is a large
fraction of both the component tree **and** the dnd-kit / client-state surface,
so the rebuild is meaningfully smaller than the current app.

**What gets simpler:** one language, no JS build toolchain, no envelope parsing,
atomic multi-step mutations, far less client state, the whole retrieval story
collapses to one debounced input.

**What stays hard:** the outline editor (inline edit, keyboard structure ops,
focus management under morphing) and outline drag-reordering.

**Recommended sequencing bias:** rebuild the two-pane core (Outline + Evidence
loop) first on the new stack; port threads / snapshots / branches / diff after.

---

## 2. Core data architecture

### Domain types
`web/src/types/model.ts` is deleted. templ components take `model.Node`,
`model.Edge`, `model.Chunk` directly — typed against the structs the store
returns.

### Tree building — the centerpiece
`outlineStore.buildTree(nodes, edges)` → a Go function, e.g.
`internal/outline.BuildTree`. Preserve exactly (port with tests):

- only `type == "outline"` nodes (locked evidence sub-bullets are `chunk_ref`
  — decide whether they render in the tree as children or as an attached-evidence
  band; today `chunk_ref` nodes are created as children via a `linear` edge)
- only `type == "linear"` edges
- roots = outline nodes with no incoming linear edge
- children sorted by edge `weight`
- `TreeNode{Node, Children, ParentID, Depth}`

`dag` already has `FindRoots` / `TopologicalSort` — this belongs beside them.

### Evidence is a text span, not a chunk — schema change
The unit of evidence is now **an excerpt**: a range within a chunk's content, or
the whole chunk. The `node_chunks` association (and `store.SetNodeChunks(id,
[]string)`) is insufficient. Options:

- **A (leaner):** `node_chunks` gains `excerpt_start`, `excerpt_end` (rune
  offsets into `chunk.Content`; `NULL` = whole chunk). Excerpt text is derived.
- **B (safer for provenance):** a new `evidence` row: `{id, node_id, chunk_id,
  source_file, char_start, char_end, text}` — stores the chosen text verbatim
  plus a pointer back for re-scrubbing.

Recommend **B**: it survives re-chunking, and stitch/export can read `text`
directly. Either way, the locked sub-bullet that represents the evidence carries
a preview title + the excerpt in `body` + source pointer in `labels`.

### Transcript-ordered retrieval — new capability
"Scrub back and forth in the same transcript" needs: given a chunk (or
`source_file` + offset), return a window of surrounding transcript text with
prev/next handles. Chunks already carry `source_file` + `start_offset` /
`end_offset` and are ordered within a file, so this is a straightforward query
(`WHERE source_file = ? ORDER BY start_offset`). Expose it as a small service in
`internal/ingest` or a new `internal/transcript`.

### Stitch / export must use the excerpt
`dag.collectChunks` currently feeds full `chunk.Content` into the stitcher. With
excerpts it **must** use the chosen span, or the manuscript contains words the
writer never selected — a fidelity violation, which is the one thing the app
exists to prevent. Update `collectChunks` to read the `evidence.text` /
excerpt range.

### Computations that move from TS to Go
| Currently in | What | New home |
|---|---|---|
| `outlineStore.ts` | `buildTree` | `internal/outline` (+ tests) |
| `outlineStore.ts` | `computeDiff` (~60 lines: added/changed/removed + ghost nodes w/ nearest-surviving-ancestor) | Go — server renders diff classes and ghost rows directly |
| `outlineStore.ts` | `flattenTree` (keyboard nav) | render `data-prev-id` / `data-next-id` on each bullet; client needs no tree knowledge |
| `EvidenceDrawer.tsx` | `assessFidelity` badges (🎤 / 🔗 / ✨) | Go helper — **but** with user-chosen excerpts "near-exact quote" is nearly always true; consider dropping the badges or repurposing to "how strongly this passage matched your text" |
| `StitchView.tsx` | `glueLabel`, `glueOpacity` | inline in templ |
| `ghostStore.ts` | `isSuppressed` / rejected-label filtering | Go — rejected suggestions need a home (signal, cookie-session, or DB); today it's client-only and ephemeral |
| `AudioRibbon.tsx` | `buildCentroids` / `assignCluster` / `clusterColor` | **deleted** |
| `SearchBox.tsx` | query + N+1 `chunks.get` resolve + `seq` race guard | **deleted**; replaced by one server endpoint that returns candidates already joined |

### Multi-request client orchestration collapses into endpoints
`outlineStore.addSibling` today: create node → create edge → loop
deleting+recreating sibling edges to re-weight → refetch (N non-atomic
round-trips, racy). After: `POST /outline/nodes/{id}/sibling` does it all in one
SQL transaction and returns the re-rendered outline. Same for `addChild`,
`indent`, `unindent`, drag-reorder move, **attach-evidence** (create locked
`chunk_ref` child + write evidence row + return outline fragment), rescue.

**Implication:** a service layer (`internal/outline`) that composes `Store` calls
transactionally. `Store` likely needs a `WithTx` primitive and a real
"insert child at position N" that manages sibling weights in one statement.

### State ownership after the split
- **Persistent → SQLite** (unchanged).
- **Ephemeral UI → Datastar signals:** `$focusId` (the "current bullet" —
  attach-evidence targets it), `$collapsed` (JSON map), `$drawerOpen` (left
  ingestion drawer), `$evidencePaneWidth`, `$glueLevel`, `$centerView`
  (outline | preview), `$threadId`, `$diffAgainst`.
- **Collapse/expand:** keep 100% client-side — server renders the full tree,
  `data-show` / `data-class` hides subtrees from `$collapsed`. No round-trip.
- **Cross-request ephemeral with no current home** (rejected ghost labels,
  `diffActive`, pane widths): decide per item — signal (lost on reload, matches
  today), cookie-session (survives reload, behavior change), or DB.

### Envelope & pagination
- `{data,error,meta}` is for JSON consumers; HTML fragment routes don't use it.
- Errors → an out-of-band `#toasts` fragment appended over SSE, or inline error
  partials.
- Evidence candidates and transcript windows paginate by returning "more"
  fragments with **append** merge mode.

---

## 3. How the APIs shift

**Strategy: add HTML/SSE routes, leave `/api/v1/*` JSON intact.** Don't
content-negotiate the same handlers. chi stays; `jsonContent` middleware wraps
only `/api/v1`, not the HTML routes.

### New route surface (illustrative)
```
GET   /                                  → two-pane shell (templ Page)
GET   /outline?thread=&diff=             → #outline fragment (tree; optional thread filter / diff overlay)
POST  /outline/roots                     → #outline + MergeSignals{focusId}
POST  /outline/nodes/{id}/sibling|child  → #outline + focusId
POST  /outline/nodes/{id}/indent|unindent
POST  /outline/nodes/{id}/move           → body: parent + position (drag reorder) → #outline
PATCH /outline/nodes/{id}                → title / locked via form signals → #bullet-{id} only
DELETE /outline/nodes/{id}               → #outline + focusId

GET   /evidence?q=<bullet text>&node={id} → #evidence : ranked candidate passages + suggested sub-points
                                            (driven by data-on:input__debounce on the bullet)
GET   /evidence/transcript?chunk={id}&dir=prev|next|at   → #transcript-reader : a window of the
                                            source transcript around the chunk, with prev/next handles
POST  /outline/nodes/{id}/evidence        → body: {chunk_id, char_start?, char_end?}
                                            → create LOCKED chunk_ref sub-bullet under {id},
                                               write evidence row, return #outline + focusId

GET   /stitch?thread=&glue=              → #stitch-content (debounced on the glue slider)
POST  /threads ; PUT /threads/{id}/nodes → #thread-bar + #outline
.../snapshots, .../branches              → relevant bar fragment + #outline

GET   /ingest                           → left-drawer fragment (upload zone + file list + status)
POST  /uploads (multipart, text only)   → #ingest fragment ; POST /uploads/process
GET   /events                           → optional long-lived SSE: upload/index status

GET   /api/v1/export/markdown/download  → unchanged (file download)
```

Routes that **disappear**: `/search` (standalone), anything ribbon-related,
`/nodes/{id}/suggest-clusters` as a *separate* client call (its output now feeds
`/evidence`).

### Mechanics
- **Response body:** `writeJSON(w, …)` → `component.Render(ctx, w)`, or the
  Datastar Go SDK for multi-fragment + signal responses: `sse.MergeFragments`,
  `sse.MergeSignals`. **Pin the SDK version** — its API has been renamed across
  releases (`MergeFragment` → `PatchElements` in newer ones); match it to the
  `datastar.js` you vendor. Confirm the import path
  (`github.com/starfederation/datastar/sdk/go` family).
- **Request parsing:** JSON decode → `datastar.ReadSignals(r, &v)` for
  form/signal posts. Multipart uploads unchanged.
- **Selection → excerpt:** highlight-to-attach needs ~20 lines of JS —
  `window.getSelection()`, map the range to character offsets within the rendered
  transcript window (put `data-offset` on the container / spans), send
  `{chunk_id, char_start, char_end}` in the attach POST.
- **"Current bullet":** the attach button reads `$focusId` (or the bullet input's
  own id) and posts to `/outline/nodes/{$focusId}/evidence`.
- **DOM id convention — establish now:** `#bullet-{id}`, `#outline`,
  `#evidence`, `#transcript-reader`, `#stitch-content`, `#thread-bar`,
  `#snapshot-bar`, `#branch-bar`, `#ingest`, `#toasts`.
- **`main.go:119` `WriteTimeout: 30s`** kills any long-lived `/events` SSE —
  override per-route with `http.ResponseController`, or skip the persistent
  stream and let short-lived action SSEs carry status.
- **Session middleware:** none today; needed only if ephemeral state goes into a
  cookie-backed session.

---

## 4. Likely pitfalls

1. **Fragment swaps vs. active text editing.** The outline is a text editor.
   Typing a bullet fires `/evidence` on a debounce **while the input is
   focused** — that endpoint must morph only `#evidence`, never `#outline`, or it
   destroys what the user is typing / jumps the caret. Structural ops
   (add-sibling on Enter, indent on Tab) *do* re-render `#outline` mid-edit:
   scope them tightly and restore focus explicitly via `MergeSignals{focusId}` +
   a `data-on-signal-change` autofocus handler. Use `data-preserve-attr="value"`
   on the editing input.

2. **Outline drag-reordering (the remaining DnD).** dnd-kit gave pointer
   activation distance, nested before/after/child drop zones computed from
   pointer-Y, a drag overlay, keyboard DnD. Native HTML5 DnD is clunky for tree
   reordering (no drop-zone geometry, `dragover` floods, poor touch). Budget
   ~100–150 lines of vanilla JS for the drop math, or accept a coarser
   interaction (drop-between-rows only, no "make child"). This is now the single
   biggest interaction risk since the ribbon/search DnD is gone.

3. **Excerpt offset integrity.** Character offsets into `chunk.Content` must be
   rune-safe (Go `[]rune`, not `[]byte`) and must survive HTML rendering —
   whitespace collapsing, entity encoding, and `<span>` wrapping in the
   transcript window will shift naive `textContent` offsets. Anchor offsets to
   explicit `data-offset` markers, not to rendered text position. Store the
   resolved `text` alongside the offsets (schema option B) so a later render
   mismatch is detectable.

4. **Stitch/export fidelity regression.** If `dag.collectChunks` still passes
   full `chunk.Content` after the excerpt change, the manuscript silently gains
   text the writer never chose. Add a test that stitches a node with a
   half-chunk excerpt and asserts the other half is absent.

5. **Transcript scrubbing across chunk boundaries.** The window must stitch
   adjacent chunks of the same `source_file` into continuous text (chunk edges
   are arbitrary and should be invisible while reading). Watch for gaps/overlaps
   at `end_offset` vs next `start_offset` (the two text chunkers compute
   `pos` differently — `ChunkTranscript` adds `+1`).

6. **Loss of optimistic UI → perceived latency.** Every add-sibling / indent /
   lock-toggle / attach now round-trips. Keep collapse/expand purely
   client-side. Consider keeping the bullet title client-optimistic (draft in a
   signal; morph only touches badges).

7. **Client-only state with no home.** `ghostStore.rejectedLabels`, `diffActive`,
   pane widths — none persisted today. Decide signal vs. cookie vs. DB for each
   or "reject suggestion" silently stops working after the port.

8. **Tree-building & diff parity.** `buildTree` and `computeDiff` have precise
   semantics (outline-only, linear-only, roots = no incoming edge, weight sort;
   diff's nearest-surviving-ancestor walk). Port with unit tests.

9. **`addSibling` / child re-weighting.** The current client does one-by-one
   delete+recreate of sibling edges (racy, non-atomic). Do **not** port it
   faithfully — reimplement as one transaction that shifts weights. Needs a real
   "insert at position" primitive; watch off-by-one and orphaned edges.

10. **As-you-type retrieval load.** `/evidence` fires on every debounced
    keystroke and runs hybrid search + (ideally) query embedding. Confirm the
    managed Python embedder is up or the query degrades to BM25-only; cache by
    normalized query text; make sure Datastar aborts the in-flight `/evidence`
    request when a newer keystroke supersedes it.

11. **templ build integration.** `templ generate` must precede `go build` locally
    and in CI (`go generate ./...` + Makefile step). Decide commit-vs-gitignore
    for `*_templ.go`. Dev loop: `templ generate --watch` + `air` replaces Vite
    HMR — slower, set expectations.

12. **CSS conditional classes.** `BulletItem` string-builds ~10 state classes;
    across the app ~40. State-driven ones (focused, drop-target, locked) should
    become Datastar `data-class` bindings, not server renders, so they update
    without a round-trip. `index.css` itself is plain and mostly survives — but
    the ribbon/search rules can be deleted.

13. **Optimistic-concurrency `version`.** `model.Node` has a `version` field
    checked on update. Form-posted signals must round-trip `version` or you get
    spurious `ErrConflict` / lost updates.

14. **Datastar expression language is stringly-typed.** templ is type-checked
    against Go structs (good); `data-*` expressions are unchecked mini-JS —
    `$focusId` vs `$focusID` fails silently.

15. **Real 404s.** The SPA fallback in `router.go` serves `index.html` for every
    path. Server rendering gives real 404s — bookmarked `/anything` starts
    failing (arguably fine).

16. **Testing strategy shift.** Component tests disappear; gain Go handler tests
    asserting on rendered HTML (golden files / goquery). Browser tests
    (Playwright) matter more for the outline + excerpt-selection flow.

---

## Suggested order of attack

1. **Drop audio.** Remove the audio upload path, `StubTranscriber`, audio format
   handling. Uploads are text transcripts only. (Do this first — it shrinks the
   surface before you touch it.)
2. **Schema:** add the `evidence` table (or `node_chunks` excerpt columns);
   migrate `SetNodeChunks` callers; update `dag.collectChunks` + a fidelity test.
3. **Transcript service:** `internal/transcript` — ordered windows with
   prev/next around a chunk.
4. Stand up the templ shell + `/` route + vendored `datastar.js`, served
   alongside the existing React app (different path) for comparison.
5. **Rebuild the core loop on the new stack:** two-pane shell → read-only outline
   (port `buildTree` to Go, with tests) → bullet editing (title, add
   sibling/child, indent/unindent, delete) → `/evidence` on debounced input →
   transcript reader/scrubber → highlight-to-excerpt → attach-as-locked-sub-bullet.
6. **Outline drag-reorder** — decide native-HTML5 vs. small vanilla helper vs.
   coarser interaction. Keep the React outline as a fallback until this lands.
7. Port the DAG features: threads + brush, snapshots, branches, visual diff
   (move `computeDiff` to Go), CYOA exit widget, stitch/preview + glue slider,
   markdown export.
8. Delete `web/` toolchain; update Makefile / CI (`templ generate`, drop npm).
