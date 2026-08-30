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
listens on **http://localhost:8080** — the UI at `/`. On first run it creates
`shuttle.db` and runs all migrations.

### Development loop

```bash
make templ-tools            # once: install the templ CLI
make dev                    # templ --watch + go run, with live reload proxy
```

## Environment Variables

| Variable             | Default      | Description                         |
|----------------------|--------------|-------------------------------------|
| `SHUTTLE_DB`         | `shuttle.db` | SQLite database file                |
| `SHUTTLE_ADDR`       | `:8080`      | Listen address                      |
| `SHUTTLE_UPLOAD_DIR` | `uploads`    | Directory for uploaded transcripts  |
| `SHUTTLE_BM25_PATH`  | `shuttle.bm25` | BM25 index snapshot               |
| `SHUTTLE_HNSW_PATH`  | `shuttle.hnsw` | Vector index snapshot             |
| `SHUTTLE_EMBED_AUTOSTART` | `1`     | Set `0` to skip the Python embedder |

## Production Build & Deployment

```bash
make build
SHUTTLE_ADDR=:8080 ./bin/shuttle
```

The binary is self-contained (SQLite via `modernc.org/sqlite`, no CGO; the UI's
CSS/JS and the Datastar runtime are embedded). Deploy the binary alone — no
reverse proxy, no static file server, no `web/` directory.

## Inputs & Data

### What you provide

- **Transcript files** (`.txt`, `.md`, `.markdown`, `.text`) — upload via the
  left drawer. The system parses, chunks, and indexes them. Audio is not
  supported.
- **Outline structure** — build it in the center pane (keyboard-first; drag to
  reorder).
- **Threads** — named reading paths; toggle bullets in, or use Brush mode.

### What the system creates

- **`shuttle.db`** — all project state (nodes, edges, evidence, threads,
  snapshots, branches). This one file is your project.
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
cmd/shuttle/            entry point
internal/
  api/                  chi router + JSON handlers
  web/                  server-rendered UI: templ components, Datastar handlers, static assets
  outline/              outline tree + structural ops + diff
  transcript/           scrubbable transcript windows
  pipeline/             transcript ingestion
  dag/ stitch/ export/  linearization, glue-stitching, markdown
  search/               hybrid BM25 + HNSW index
  indexer/              incremental index persistence + embedding backfill
  ingest/ ingest/embedfile/  parsing, embeddings, .fembed reader
  model/ store/         domain types, SQLite persistence + migrations
```
