# Plan — project rename + sharing one corpus across audience projects

**Goal:** build the transcript corpus (chunks, embeddings, atlas regions,
digests, chunk labels) **once**, then spin up several projects that each author
a different manuscript for a different audience against that same corpus —
without re-ingesting or re-running the (hours-long, LLM-bound) atlas build per
project.

Nothing here is implemented yet. This is the design.

---

## 1. How projects work today

`internal/project` — a project is just a directory under `~/.shuttle/`
(`$SHUTTLE_HOME`):

```
~/.shuttle/
  config.json            {"current": "<name>"}
  <name>/
    shuttle.db  -wal  -shm     one SQLite DB — everything
    shuttle.bm25  shuttle.hnsw  search index snapshots (derived, rebuildable)
    uploads/                    raw uploaded files (used only at ingest time)
    outline.md  state.json      human-readable mirror of the DAG
    branches/<b>.md
```

- **One SQLite DB per project.** Migrations 001–010, all tables in the one file.
- **The project name is not stored in the DB.** It's the directory name, the
  `config.json` `current` value, and `state.json`'s `Project` field (used only
  for the `outline.md` `# <title>` heading and the recovery display). The only
  `meta` key in use is `embed_model`.
- **Switching projects re-execs the binary** (`syscall.Exec`) — the process
  only ever has one project's DB open. `POST /project/switch` /
  `POST /project/new` drive it; the browser polls `/healthz` and reloads.
- The `uploads/` directory is **only touched during ingest**. After chunking,
  every view (transcript reader included) reads the `chunks` table, never disk.
- The atlas is entirely DB-backed and **global per DB** — `atlas.Service` keeps
  one `Current()` ("ready") build; `LoadCurrent()` reads it at boot.

### What's in a project DB, split by "who owns it"

| Group | Tables | Notes |
|---|---|---|
| **Corpus** (audience-independent; expensive) | `chunks` (+ `embedding_vec`), `uploads`, `transcript_segments` | embeddings need MLX; `transcript_segments` ≈ 24k rows for the current corpus (audio output) |
| **Atlas** (audience-independent; *very* expensive — LLM) | `atlas_build`, `atlas_region`, `atlas_region_chunk`, `atlas_region_link`, `atlas_transcript` (per-build), `atlas_digest`, `atlas_chunk_label` (build-independent, content-addressed) | current corpus: 575 digests + 2122 chunk labels — hours of gemma calls |
| **Document** (audience-specific) | `nodes`, `edges`, `threads`, `thread_nodes`, `evidence`, `node_chunks` (legacy), `snapshots`, `branches` | tiny — 21 nodes / 16 evidence rows today |
| Derived / rebuildable | `shuttle.bm25`, `shuttle.hnsw` | reconciled incrementally from `chunks` on boot |
| Config | `meta` (`embed_model`) | must match wherever the corpus is shared |

### The one hard coupling

```sql
evidence.chunk_id  TEXT NOT NULL REFERENCES chunks(id) ON DELETE RESTRICT
node_chunks.chunk_id  … ON DELETE RESTRICT
atlas_chunk_label.chunk_id  … ON DELETE CASCADE
atlas_region_chunk.chunk_id … ON DELETE CASCADE
```

A "document-only" project is impossible: the moment an audience attaches
evidence, that project's DB needs the referenced `chunks` row. So every
audience project must carry (a copy of, or a shared handle to) the corpus's
`chunks`. Chunk **ids are stable** (generated at chunk time, never reassigned),
which is what makes copying safe — evidence keeps pointing at the right text
across projects.

---

## 2. Renaming a project

There is **no rename** today. But the name lives in very few places, so it's
easy.

### 2a. Do it by hand right now

```bash
# server must NOT be running on that project
mv ~/.shuttle/oldname ~/.shuttle/newname
# if it was the current project:
#   edit ~/.shuttle/config.json  ->  {"current": "newname"}
# start the server; the working-doc Flusher rewrites state.json + outline.md
# with the new "# newname" heading on the next edit.
```

Constraints: new name must match `^[a-z0-9][a-z0-9_-]{0,63}$`; target dir must
not already exist; `state.json`'s `Project` field stays stale until the next
flush (cosmetic — recovery only shows it).

### 2b. As a feature (small)

`POST /project/rename` (form: `from`, `to`), plus a rename item in the project
bar.

- Validate `to` with `project.ValidName`; refuse if `PathsFor(home, to).Dir`
  exists.
- **Renaming the current project:** close the store → `os.Rename(oldDir,
  newDir)` → `project.SetCurrent(home, to)` → re-exec. Reuse the existing
  switch machinery (`doSwitch` already does close + `SetCurrent` + re-exec; add
  a rename step before `SetCurrent`).
- **Renaming a non-current project:** just `os.Rename`; touch `config.json`
  only if it named that project. No restart.
- Optionally rewrite `state.json`'s `Project` field in place; or leave it for
  the Flusher.

Effort: ~1 handler + 1 `project.Rename(home, from, to)` helper + a project-bar
button. No schema change.

---

## 3. Sharing the corpus across audience projects

Three approaches, cheapest-to-build first.

### Approach A — Clone the project directory

"New audience project" = copy an existing project's whole directory, then wipe
its DAG.

```
cp -r ~/.shuttle/canon ~/.shuttle/audience-teens
# then, in the new project: DELETE FROM evidence, node_chunks, thread_nodes,
#   edges, threads, nodes;  (clearDAGTables already exists)
```

- **Pros:** zero new infrastructure. Corpus + embeddings + atlas + indexes come
  along for free. The atlas build (the expensive thing) is *copied*, not rerun.
- **Cons:** full ~28 MB duplication per audience (fine for a handful).
  **After the clone the projects are independent** — a corrected transcript or
  a fresh atlas build has to be propagated by hand, or you re-clone and lose
  the audience's outline.
- **Feature form:** `POST /project/new?from=<source>` — `project.Create`, copy
  the DB file (+ index snapshots + `uploads/`), open it, run `clearDAGTables`,
  switch. ~1 handler + a file-copy helper.
- **Good when:** the corpus is basically frozen once the audiences start, and
  you have ≤ ~10 audiences.

### Approach B — One shared corpus DB, per-project document DB (SQLite `ATTACH`)

```
~/.shuttle/
  _corpus/corpus.db          chunks, uploads, transcript_segments, atlas_*, meta
  _corpus/corpus.bm25/.hnsw   shared indexes
  audience-teens/shuttle.db   nodes, edges, threads, evidence, snapshots, branches
  audience-clergy/shuttle.db  "
```

Each project's store `ATTACH`es `corpus.db`. One ingest, one atlas build,
truly shared.

- **Pros:** no duplication; a corpus change is instantly visible to every
  audience project; the atlas is genuinely built once.
- **Cons — this is a real refactor:**
  - **Cross-DB foreign keys don't exist in SQLite.** `evidence.chunk_id` →
    `corpus.chunks(id)` can't be a real FK. Drop the constraint; the app
    already mediates every write, so integrity is an app-code invariant (add a
    check in the evidence-create path).
  - The store has to route each table to the right database (main vs
    `corpus`), split the migration set, and teach `reconcileEmbeddingModel`,
    the backfiller, the atlas service, and the index snapshotter that the
    corpus lives elsewhere.
  - **Write concurrency.** SQLite is single-writer. Two `shuttle` processes
    (one per open project) both ingesting or both rebuilding the atlas into
    `corpus.db` will conflict. Mitigation: corpus writes (ingest, atlas
    rebuild, backfill) happen **only from a designated "corpus admin"
    session** — a project (or a `--corpus-admin` flag) that opens `corpus.db`
    read-write; all audience sessions open it read-only. In practice you'd
    ingest + build the atlas in the admin project, then just author in the
    audience projects.
  - Backups get more interesting (two files that must stay consistent).
- **Good when:** the corpus keeps evolving and you want every audience to
  track it live, and you're willing to spend the refactor.

### Approach C — Portable corpus bundle: export / import (recommended)

A `shuttle corpus` subcommand pair (or `/corpus/export` + `/corpus/import`
endpoints) that moves the corpus between independent project DBs on demand.

```
# in the canonical corpus project:
shuttle corpus export  ~/corpus.bundle
# in each audience project (new or existing):
shuttle corpus import  ~/corpus.bundle      # idempotent; re-run after corpus changes
```

**Bundle contents** (a single SQLite file, or a tar of CSV/JSONL + blobs):

| Include | Why |
|---|---|
| `chunks` (full rows incl. `embedding_vec`) | required by `evidence` FK; embeddings are expensive |
| `atlas_digest`, `atlas_chunk_label` | the LLM output — the whole point |
| current `atlas_build` + its `atlas_region` / `atlas_region_chunk` / `atlas_region_link` / `atlas_transcript` | the ready graph/regions |
| `meta['embed_model']` | so `reconcileEmbeddingModel` doesn't wipe the imported vectors on boot |
| *(optional)* `uploads`, `transcript_segments`, upload files | provenance / ability to re-chunk; **not** needed to view or attach evidence |

**Import semantics (idempotent upsert):**

- `chunks`: `INSERT … ON CONFLICT(id) DO UPDATE` — new chunks appear, existing
  ones refresh (content is immutable in practice, so this is a no-op for
  unchanged rows). Never deletes chunks an audience's evidence points at.
- `atlas_digest` / `atlas_chunk_label`: upsert by their content-addressed keys
  — exactly how a rebuild already treats them. A re-import after the corpus
  changed brings only the new/changed rows.
- `atlas_build` + regions/links/transcripts: replace wholesale for the
  imported build id, then let the target's `PruneExcept` keep one. The target
  `atlas.Service.LoadCurrent()` picks it up on next boot / a lightweight
  "reload" endpoint.
- BM25 / HNSW: nothing to import — `indexer.LoadAndReconcile` already adds any
  chunk ids the local index is missing on boot.

**Pros:**
- Matches the mental model the user asked about ("import?").
- Projects stay independent single files — simple backups, no shared-writer
  concurrency, no cross-DB FK problem.
- The atlas is built **once** (in the corpus project) and *imported*, never
  rebuilt per audience.
- "Sync after a transcript fix" = re-export + re-import; each audience's
  outline is untouched (evidence chunk ids are stable).

**Cons:**
- Still duplicates corpus storage per project (~7 MB embeddings + ~7 MB atlas
  per audience — acceptable).
- Sync is a manual step, not live (a feature, not a bug, for authored work —
  you decide when the ground shifts under a draft).

**Effort:** one `internal/corpus` package (export = `SELECT` the tables to a
bundle; import = upsert them in a transaction) + a `shuttle corpus
export|import` CLI path (the binary is currently server-only; add a tiny
`os.Args` switch in `main`) or two admin endpoints. Store gets
`ExportCorpus(w io.Writer)` / `ImportCorpus(r io.Reader)` methods. No migration
change. ~1 focused PR.

---

## 4. Recommendation

**Approach C (bundle export/import), with a "clone" shortcut for first
creation.**

Workflow it enables:

1. Keep one project — call it `corpus` — where you ingest transcripts and run
   the atlas. Never write an outline there.
2. `shuttle corpus export ~/corpus.bundle`.
3. For each audience: `POST /project/new?from=corpus` (Approach A clone — fast
   first copy) **or** `project/new` + `shuttle corpus import`. Author the
   audience's manuscript.
4. Corrected a transcript / rebuilt the atlas in `corpus`? Re-export, then
   `shuttle corpus import ~/corpus.bundle` in each audience project. Outlines
   survive; new chunks + atlas rows fold in.

Phasing:

- **Phase 0 (now, no code):** rename by hand (§2a); create audiences by
  `cp -r` + manual DAG wipe.
- **Phase 1:** `project.Rename` + `POST /project/rename` (§2b); `POST
  /project/new?from=<src>` clone (§3A). Removes the manual steps.
- **Phase 2:** `internal/corpus` + `shuttle corpus export|import` (§3C) +
  an atlas "reload from DB" endpoint so a re-import shows up without a restart.
- **Phase 3 (only if Phase 2's manual sync chafes):** Approach B shared
  `corpus.db` with a corpus-admin write lock.

---

## 5. Open decisions for you

1. **Do audiences ever need to see each other's outlines / share threads?**
   (Assumed no — fully independent manuscripts.)
2. **Is the corpus frozen once authoring starts, or does it keep changing?**
   Frozen → Approach A alone is enough. Changing → need C's re-import (or B).
3. **Should the bundle carry `uploads/` + `transcript_segments`** (re-chunking
   possible in any project, bigger bundle) **or just `chunks`** (view + attach
   only, ~half the size)?
4. **One atlas for all audiences, or per-audience atlas tuning?** (Assumed one
   — regions/labels are audience-independent. If an audience wanted different
   `ClusterParams`, that's a per-project atlas build on the shared chunks,
   which all approaches still allow.)
5. **CLI vs endpoints for export/import?** The binary is server-only today;
   adding `shuttle corpus …` means a small `os.Args` dispatch in `main`.
