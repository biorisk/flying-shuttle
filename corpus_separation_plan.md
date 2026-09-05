# Plan — separate the writing project from the corpus

**Goal:** make the vocabulary split the codebase keeps *asking* for
(`internal/atlas`: "The Atlas is not the authored document… outline has
nodes/edges, atlas has regions/links") into a **structural** split.

- A **corpus** owns transcripts, chunks, embeddings, the search index, and the
  atlas (regions / links / digests / labels). Built once.
- A **writing project** owns the outline: nodes, edges, threads, branches,
  snapshots, and evidence. Many projects bind to one corpus.
- The only thing a project row may say about a corpus row is a chunk id.

Supersedes §3 of `multi_project_corpus_plan.md` (this is that doc's
"Approach B", designed properly rather than as a bolt-on).

---

## 1. The seam

There is exactly **one** place a document row names a corpus row:

```sql
evidence.chunk_id        -- + denormalized source_file, char_start/end, text
node_chunks.chunk_id     -- legacy, dead in production (test-only)
```

Everything else in the document tables is self-contained. That single column
is the API between the two halves — and evidence already carries a
**denormalized copy of its text and source file**, which turns out to be the
property that makes the whole split safe (see §5.1).

Design rule to write down and enforce:

> A project database contains no corpus text. It contains chunk **ids** and
> the excerpt the author actually quoted. To show anything more, ask the
> corpus.

The phrase "the excerpt the author actually quoted" is load-bearing: that
excerpt is **the project's to edit**, independently of the chunk it came from
(see §5.6). The chunk id is a provenance pointer, not a live mirror.

---

## 2. On-disk layout

```
~/.shuttle/
  config.json                  {"current": "<project>"}

  corpora/
    <corpus>/
      corpus.db                chunks, uploads, transcript_segments,
                               atlas_*, meta(embed_model)
      corpus.bm25  corpus.hnsw search index snapshots
      uploads/                 raw uploaded files (ingest-time only)
      corpus.lock              advisory writer lock (§5.3)

  projects/
    <project>/
      project.db               nodes, edges, threads, thread_nodes,
                               evidence, snapshots, branches
      project.json             {"corpus": "<corpus>"}   <-- the binding
      outline.md  state.json
      branches/<b>.md
```

`project.json` is the binding. One corpus per project (N projects : 1 corpus).
Resolved at boot; a project with no corpus, or a corpus that's missing, still
opens — the outline works, and the evidence pane / atlas / ingest UI are
hidden (§5.5). That graceful degradation is worth having deliberately: it
means the writing tool is not hostage to the corpus.

**Corpora are first-class, named objects**, not "the thing next to this
project". A corpus has a user-chosen name (its `corpora/<name>/` directory),
independent of any project name, listed in `~/.shuttle/config.json` and
offered in a picker (§5.5). `project.json`'s `"corpus"` value is that name.
This keeps the N:1 story honest — "point three projects at `interviews-2026`"
reads better than three projects each claiming a corpus by adjacency.

---

## 3. Store split

Today's `store.Store` (≈70 methods) divides almost perfectly along existing
comment groupings.

### → `internal/corpus` (new package, `corpus.Store`)

| Tables | Methods |
|---|---|
| `chunks` | `CreateChunk`, `CreateChunks`, `GetChunk`, `ListChunks`, `ListChunksPage`, `ListChunksBySourceFile`, `ListChunkIDs`, `ListChunkIDsWithEmbedding`, `GetChunksByIDs`, `ListChunksMissingEmbedding`, `CountChunksMissingEmbedding`, `SetChunkEmbedding` |
| `uploads` | `CreateUpload`, `GetUpload`, `ListUploads`, `ListUploadsPage`, `UpdateUploadStatus` |
| `transcript_segments` | `CreateTranscriptSegment`, `ListTranscriptSegments` — retained (the only post-ingest reader is `Ingester.Rechunk`, re-chunk without re-upload; ~24k rows) |
| `meta` | `GetMeta`, `SetMeta` (embed model reconciliation) |
| `atlas_*` | unchanged — `atlas.Store` already is its own interface; it just takes the **corpus** `*sql.DB` instead of the project one (`atlas.NewStore(corpus.DB())`, a one-line change) |

Migrations 002 (uploads), 006/008/009/010 (atlas), the `chunks` half of 001,
and 007 (meta) move to a corpus migration set.

### → `internal/doc` (today's `internal/store`, narrowed)

`nodes`, `edges`, `threads`, `thread_nodes`, `evidence`, `snapshots`,
`branches` — plus `ExportState` / `ImportState`, `MoveNode`, and the
snapshot/branch surface. Vocabulary stays **node / edge / thread / branch /
snapshot / evidence**.

### The straddlers — and the good news

Only **three production call sites** cross the boundary today:

| Site | Call | Becomes |
|---|---|---|
| `internal/outline/service.go:229` | `GetChunk` when attaching evidence (needs the text to cut an excerpt) | takes a `corpus.Reader` |
| `internal/web/evidence.go:124` | `GetChunk` rendering a candidate card | `EvidenceFinder{Index, Corpus}` — it was already corpus-only apart from this |
| `internal/dag/linearize.go:101` | `GetChunk` **only to read `c.Speaker`** | pass a speaker-lookup func, or drop the nicety |

Plus one SQL join to unwind — `GetNodeChunks` (`evidence ⋈ chunks`) becomes
`ListNodeEvidence` → collect ids → `corpus.GetChunksByIDs` → reorder. And
`GetNodeChunks` / `SetNodeChunks` / `ListUsedChunkIDs` have **no production
callers at all** (tests only) — delete them with the legacy `node_chunks`
table.

Everything else is already single-sided:

- **corpus-only:** `internal/pipeline` (ingest), `internal/indexer`
  (backfill + reconcile), `internal/transcript`, `internal/search`,
  `internal/atlas`
- **document-only:** `internal/outline` (bar the one call), `internal/dag`
  (bar the one call), `internal/export`, `internal/workingdocs`

Note the evidence *editing* surface is already entirely document-only:
`Service.EditQuote` → `store.UpdateEvidence` touches only `evidence` (text +
offsets) and the `chunk_ref` node's body — no corpus read. §5.6 turns that
incidental fact into a guarantee and widens it to free-form edits.

`web.Deps` / `api.Deps` swap `Store store.Store` for `Doc doc.Store` +
`Corpus corpus.Store` (nil when unbound).

---

## 4. Runtime model

Project switching already re-execs the binary, so a process has exactly one
project open. After the split it opens **two** databases: its project DB
read-write, and its corpus DB read-write *if it holds the corpus writer lock*,
read-only otherwise.

```
boot → resolve project → read project.json → resolve corpus
     → open project.db (rw)
     → open corpus.db  (rw if lock acquired, else ro)
     → if rw: reconcileEmbeddingModel, Backfiller, Snapshotter, atlas rebuilds
     → if ro: load index snapshots, atlas LoadCurrent, no writers
```

---

## 5. The hard problems

### 5.1 Losing the foreign key

`evidence.chunk_id REFERENCES chunks(id)` cannot survive — SQLite does not
enforce foreign keys across attached or separate databases. So:

- Drop the constraint; **validate in `CreateEvidence`** (`corpus.GetChunk(id)`
  must resolve) — the app mediates every write already.
- Add `shuttle doctor` (or a pane in the UI) that classifies every evidence
  row against the bound corpus:
  1. **id unresolved** — chunk id no longer exists (corpus swapped, or an
     ancient pre-split project). Offer to detach.
  2. **id resolves, text diverges, row not flagged `edited`** — the chunk was
     superseded under the project (§5.2 re-ingest). Offer to re-anchor to the
     current chunk or accept the divergence.
  3. **id resolves, text diverges, row flagged `edited`** — the author
     customized the excerpt on purpose (§5.6). Not a problem; report it only
     under `--verbose`.
  Distinguishing 2 from 3 needs an `edited` boolean (or `edited_at`) on the
  evidence row — added in Phase 4 alongside the free-form edit op.
- **Degradation is soft**, because evidence carries its own `source_file`,
  `char_start/end` and `text`: a dangling row still renders in the outline and
  still exports. What it loses is "Read in transcript →" and re-locating the
  span. That is a materially better failure mode than a hard FK error, and it
  is the reason this split is safe at all.

### 5.2 Chunk deletion (the FK was also doing work)

`ON DELETE RESTRICT` currently makes SQLite refuse to delete a chunk that any
node cites. The corpus can no longer know who cites what — projects that use
it may not even be open.

Corpus chunks are **append-only, and a chunk id never changes its meaning.**
Chunk content is already documented as immutable; formalize it.
Re-ingesting a corrected transcript creates *new* chunk rows with *new* ids
and soft-deletes (`deleted_at`) the old ones — it never mutates or renumbers
an existing row. Old evidence keeps resolving to exactly the text it always
resolved to; the atlas ignores soft-deleted chunks on the next build; the
doctor reports "cites a superseded chunk".

The corollary matters for snapshots (§8): because ids are stable and rows are
never hard-deleted, an evidence chunk id that resolved when a snapshot was
taken still resolves — and to the same content — against any *later* corpus
state. A snapshot therefore does **not** need to pin a corpus version; the
chunk id already is the version. (Restoring a project against an *older*
corpus that predates a cited chunk is the only dangling case, and it is a
backup-hygiene problem, not a data-model one — §8.2.)

The rejected alternative — a `citations` table in the corpus that every
project writes into — would make every project a corpus writer, which breaks
§5.3.

### 5.3 Concurrency: many readers, one writer

SQLite WAL already gives many concurrent readers + one writer, and readers
never block on the writer. That covers the *mechanics*. It does not cover the
*logic*: two projects starting an atlas rebuild or an ingest simultaneously is
incoherent regardless of locking.

- **Advisory writer lock** — `corpora/<c>/corpus.lock`, taken at boot
  (`O_EXCL` + pid, or a `BEGIN IMMEDIATE` probe). First session in wins.
- Lock holder runs: ingest, the embedding `Backfiller`, the index
  `Snapshotter`, `reconcileEmbeddingModel`, atlas rebuilds.
- Non-holders open the corpus read-only, **load** the BM25/HNSW snapshots but
  never write them, and hide the Ingest drawer + the atlas "rebuild" button
  with "corpus is read-only — held by <project>".
- Set `busy_timeout` on both handles so any residual contention retries rather
  than errors.

This also solves a bug the current design would otherwise inherit: three
projects sharing a corpus would otherwise run three `Snapshotter`s racing on
the same `corpus.bm25` file.

### 5.4 Read-only sessions noticing a new build

`atlas.Service` caches the current build in memory. If the lock holder
finishes a rebuild, read-only sessions won't see it. Extend the mechanism
already added for in-process rebuilds: the `atlasBuiltAt` signal →
`window.atlasGraph.syncTo()`. Server-side, have read-only sessions re-check
`CurrentBuild()`'s id/timestamp on a slow poll (or on the existing
`/atlas/status` hit) and swap `s.current` when it changed.

### 5.5 Unbound / missing corpus

`project.json` absent, or names a corpus that isn't there. Do not fail to
boot. `Deps.Corpus == nil` →

- evidence pane, transcript reader, atlas tab, ingest drawer: hidden
- outline, threads, branches, snapshots, preview/export: fully functional
- a "bind a corpus" picker in the project bar, listing every corpus in
  `~/.shuttle/corpora/` by name

### 5.6 Per-project evidence customization

**Goal:** once a chunk is cited as evidence, the author can edit that
excerpt's text within the project without touching the corpus, and two
projects citing the same chunk can quote it differently.

The split gives this almost for free. An `evidence` row lives entirely in
`project.db` and already carries a denormalized copy of everything it renders
from — `text`, `source_file`, `char_start/end` — plus the `chunk_ref` node's
own `Body`/`Title`. Nothing in the render path consults the corpus. The
existing `Service.EditQuote` (trim / splice) already writes only
`evidence.text` + offsets and the node body. So:

- **Divergence of `evidence.text` from its source chunk is a supported state,
  not just a failure fallback.** Phase 4 adds a free-form "edit quote" op
  beside trim/splice — the author retypes the excerpt, the corpus is never
  written. Every such write sets the evidence row's `edited` flag (§5.1).
- **Consequence — corpus round-trips degrade to best-effort once text
  diverges.** "Read in transcript →" and span re-anchoring (`locateSelection`
  in `internal/outline`) rely on `evidence.text == chunk[char_start:char_end]`.
  After an edit that no longer holds. Because the divergence is now
  *expected*, the UI labels an edited excerpt explicitly ("edited — differs
  from source") and keeps the "jump to source" affordance pointed at the
  chunk (whole-chunk, not the stale span) rather than hiding it or flagging an
  error.
- **Snapshots already capture evidence text**, so per-project customizations
  are versioned inside the project like any other outline edit.
- **The atlas is unaffected** — it reads corpus chunks, which never changed.

This is the payoff of the denormalized-excerpt design noted in §1: the same
property that makes a dangling evidence row degrade softly also makes a
*deliberately* edited one work correctly.

---

## 6. Migrating existing projects

One-shot, offline, reversible-by-backup — `shuttle migrate split`:

1. For each `~/.shuttle/<name>/`, create `corpora/<name>/` and
   `projects/<name>/`.
2. Copy `shuttle.db` → `corpora/<name>/corpus.db`; drop the document tables
   from it. Copy `shuttle.db` → `projects/<name>/project.db`; drop the corpus
   tables from it; drop the `evidence.chunk_id` FK (SQLite: recreate the table
   without it).
3. Move `shuttle.bm25`, `shuttle.hnsw`, `uploads/` into the corpus dir;
   `outline.md`, `state.json`, `branches/` into the project dir.
4. Write `projects/<name>/project.json` = `{"corpus": "<name>"}`.
5. Leave the original directory in place as `~/.shuttle/<name>.pre-split/`
   until the user confirms.

Then the multi-audience flow the user wants is just: create N projects, point
all their `project.json` at the same corpus. No copying, no bundles, no
re-running the atlas.

---

## 7. Phasing

| Phase | Work | Ships |
|---|---|---|
| **1** | Split the store interfaces in-process — `corpus.Store` + `doc.Store`, both still backed by the *same* `*sql.DB`. Fix the 3 straddle sites, delete `node_chunks`. | No behaviour change, no migration. All the code churn, none of the risk. |
| **2** | Two DB files, project↔corpus binding, `project.json`, new on-disk layout, `shuttle migrate split`. Single process still. | The actual separation. |
| **3** | Writer lock + read-only corpus sessions + read-only UI affordances. Named-corpus picker + `~/.shuttle/config.json` corpus registry. | Multiple audience projects open at once. |
| **4** | Append-only chunks / soft delete, `evidence.edited` flag, free-form "edit quote" op (§5.6), `shuttle doctor` with its three evidence states (§5.1). | Robustness + per-project excerpt customization. |

All four phases land, in order, each usable on its own — the separation only
pays off once the corpus is genuinely shared (Phase 3) and citations survive
re-ingest and per-project edits (Phase 4). Phase 1 still ships first and
independently: it is the vocabulary discipline made mechanical, no
user-visible change, and it de-risks everything after it.

---

## 8. Tradeoffs

**What you give up**

1. **Referential integrity becomes an application invariant.** The database
   can no longer prove that every citation resolves. Mitigated by validation
   on write, soft-deleted chunks, and a doctor — but it is a genuine downgrade
   from "SQLite will not let this happen."
2. **Two files to back up together.** Append-only chunks with stable ids
   (§5.2) mean a project DB restored against a *newer* corpus is always fine —
   every cited id still resolves to the same text. The one dangling case is a
   project DB restored against an *older* corpus that predates a cited chunk.
   Backups should still snapshot the pair; the failure window is just much
   narrower than "any mismatch".
3. **One corpus writer at a time.** With three audience projects open, only
   one can ingest or rebuild the atlas; the others are read-only until it
   exits. For a single-user tool this is nearly free. A corpus daemon would
   lift the ceiling but is a much bigger build and out of scope; concurrent
   ingest-while-drafting is the reopening point if it ever becomes a real
   need. Until then the read-only UI affordances (§5.3) carry the explanation
   so it doesn't read as a bug.
4. **Cross-boundary reads become two round-trips** instead of one SQL join
   (`GetNodeChunks`). Irrelevant at this scale; a small amount of code.
5. **One atlas per corpus, shared by all audiences.** An audience can't have
   its own clustering parameters without an `atlas_build.scope` column. (Given
   regions/labels are audience-independent, this is almost certainly correct —
   but it is a decision, not an accident.)
6. **A migration you can't casually undo**, and more moving parts in `main.go`
   wiring / `Deps`.
7. **N projects : 1 corpus only.** A project drawing on *two* corpora would
   need `evidence.corpus_id` and a resolver. Not hard, but out of scope for v1
   and worth not painting into a corner.

**What you get**

1. **The corpus is built once.** Ingest, embeddings, and hours of LLM digest +
   label work are shared by every audience project rather than duplicated N
   times (~28 MB and ~2 h of gemma per audience today).
2. **The vocabulary confusion becomes impossible.** You cannot accidentally
   join `nodes` to `chunks` in SQL, because they aren't in the same database.
   Every existing warning comment in `internal/atlas` stops being a request
   for discipline and becomes a fact.
3. **Corrections propagate instantly.** Fix a transcript once; every audience
   project sees it on next read. No export/import sync step (the whole point
   of preferring this over `multi_project_corpus_plan.md` §3C).
4. **The writing tool degrades gracefully** and can run with no corpus at all —
   a clean, testable boundary rather than a monolith.
5. **Backups get cheaper in the common case:** the thing you edit constantly
   (a ~100 KB project DB) is separate from the thing that rarely changes (a
   28 MB corpus).
6. **Each audience can quote the shared corpus in its own words.** The same
   chunk cited by three projects can be trimmed or rewritten differently in
   each (§5.6), with no risk of one project's edit leaking into another or
   into the corpus.
