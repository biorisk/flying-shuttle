# Flying Shuttle — Installation & Deployment

Flying Shuttle is a structural writing tool that lets you build, branch, and
stitch narrative outlines from transcripts. It is a single Go binary: a
server-rendered UI (templ + Datastar, HTML fragments over SSE) at `/`, plus a
tiny JSON surface under `/api/v1/ingest` for the offline embedding pipeline.
No Node toolchain.

## Prerequisites

| Tool | Version | Purpose |
|------|---------|---------|
| Go   | 1.25+   | Build (templ requires 1.25; the toolchain auto-downloads) |

No external database or services are required — the backend uses an embedded
SQLite database file. Optional: a local Python venv for the Qwen3 embedder
(see `python/README.md`); without it, search runs BM25-only.

## Quick Start

```bash
git clone https://github.com/biorisk/flying-shuttle.git
cd flying-shuttle
make build && ./bin/shuttle
```

`make build` runs `templ generate` then compiles `bin/shuttle`. The server
listens on **http://localhost:8080** — the UI at `/`. All state lives under
**`~/.shuttle/`** (see *Projects & durability* below); the working directory is
never written to.

### Development loop

```bash
make templ-tools            # once: install the templ CLI
make dev                    # templ --watch + go run, with live reload proxy
```

## Environment Variables

| Variable                 | Default     | Description                              |
|--------------------------|-------------|------------------------------------------|
| `SHUTTLE_HOME`           | `~/.shuttle`| Root dir for all projects & config       |
| `SHUTTLE_ADDR`           | `:8080`     | Listen address                           |
| `SHUTTLE_EMBED_AUTOSTART`| `1`         | Set `0` to skip the Python embedder      |
| `SHUTTLE_RRF_K`          | (tuned)     | Reciprocal-rank-fusion constant          |
| `SHUTTLE_EMBED_*`, `SHUTTLE_PYTHON` | — | Python sidecar overrides (`python/README.md`) |

## Production Build & Deployment

```bash
make build
SHUTTLE_ADDR=:8080 ./bin/shuttle
```

The binary is self-contained (SQLite via `modernc.org/sqlite`, no CGO; the UI's
CSS/JS and the Datastar runtime are embedded). Deploy the binary alone — no
reverse proxy, no static file server, no `web/` directory.

## Projects & durability

Every project is a directory under `~/.shuttle/`:

```
~/.shuttle/
  config.json              {"current": "<project>"}
  <project>/
    shuttle.db  shuttle.db-wal  shuttle.db-shm
    shuttle.bm25  shuttle.hnsw
    uploads/
    outline.md               human-readable mirror, rewritten on every change
    state.json               lossless mirror — recovery source
```

- **Switching projects** — the top-left dropdown in the UI. It persists the
  choice and restarts the server (a brief reload); "+ new project" creates one.
- **`outline.md` + `state.json`** are regenerated (atomically) whenever the DAG
  changes, so your work is readable and recoverable even outside SQLite.
- **Recovery** — if `shuttle.db` is missing or empty on boot but `state.json`
  exists, the server imports it automatically and logs `recovery: restored N
  nodes`.

## Inputs & Data

### What you provide

- **Transcript files** (`.txt`, `.md`, `.markdown`, `.text`) — upload via the
  left drawer. The system parses, chunks, and indexes them. Audio is not
  supported.
- **Outline structure** — build it in the center pane (keyboard-first; drag to
  reorder).
- **Threads** — named reading paths; toggle bullets in, or use Brush mode.

### What the system creates

- **`~/.shuttle/<project>/shuttle.db`** — all project state (nodes, edges,
  evidence, threads, snapshots, branches).
- **`outline.md` / `state.json`** — human-readable + recovery mirrors.
- **`uploads/`** — the raw transcript files.

### Key concepts

| Concept    | Description |
|------------|-------------|
| **Node**   | A bullet in the outline (title + optional body) |
| **Edge**   | linear = parent/child, branch = CYOA fork, jump = goto |
| **Evidence** | A passage (a span of a transcript chunk) attached to a bullet as a locked sub-bullet |
| **Thread** | A named reading order through selected nodes |
| **Snapshot** | A point-in-time copy of the whole DAG |
| **Branch** | A named working copy for exploring alternatives |

## HTTP surface

- **`/`** — the app. The UI drives itself through HTML-fragment endpoints
  (`/outline`, `/evidence`, `/stitch`, `/snapshots`, `/branches`, `/threads`, …)
  that return Datastar SSE patches. Not a stable API.
- **`GET /export.md?thread=&glue=&title=`** — download the stitched manuscript.
- **`GET /healthz`** — liveness.
- **`POST /api/v1/ingest/{embed-file,embed-file-legacy,directory,directory-legacy}`**
  — the only JSON API. Body `{"path": "…"}` pointing at a `.fembed` / `.embed`
  file or a directory of them. Used by the offline embedding pipeline
  (`python/`) to bulk-load pre-computed vectors; response `{"data":{"imported":N}}`.

## Testing

```bash
make test        # go test ./... (runs templ generate first)
make lint        # go vet
```

## Project Structure

```
cmd/shuttle/            entry point (project resolution, workers, re-exec on switch)
internal/
  api/                  chi router (ingest JSON API + mounts web)
  web/                  server-rendered UI: templ components, Datastar handlers, static assets
  project/              ~/.shuttle home, per-project paths, config
  workingdocs/          outline.md + state.json mirror + recovery
  outline/              outline tree + structural ops + diff
  transcript/           scrubbable transcript windows
  pipeline/             transcript ingestion
  dag/ stitch/ export/  linearization, glue-stitching, markdown
  search/               hybrid BM25 + HNSW index
  indexer/              incremental index persistence + embedding backfill
  ingest/ ingest/embedfile/  parsing, embeddings, .fembed reader
  model/ store/         domain types, SQLite persistence + migrations
```
