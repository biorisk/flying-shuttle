# Flying Shuttle — Installation & Deployment

Flying Shuttle is a structural writing tool that lets you build, branch, and stitch narrative outlines from audio transcripts. It has a Go backend (SQLite, REST API) and a React frontend (Vite, TypeScript, Zustand).

## Prerequisites

| Tool   | Version        | Purpose           |
|--------|----------------|-------------------|
| Go     | 1.24+          | Backend build     |
| Node   | 20+            | Frontend build    |
| npm    | 10+            | Frontend packages |

No external database or services are required — the backend uses an embedded SQLite database file.

## Quick Start (Development)

### 1. Clone and install dependencies

```bash
git clone https://github.com/biorisk/flying-shuttle.git
cd flying-shuttle
```

Go and frontend dependencies are fetched automatically during the build.

### 2. Build and run

```bash
make build && ./bin/shuttle
```

This builds both frontend and backend, then starts the server on **http://localhost:8080**. The app (UI + API) is served from a single port. On first run it creates `shuttle.db` in the working directory and runs all migrations automatically.

### 3. Frontend development (with hot reload)

For frontend development, use the Vite dev server for hot module replacement:

```bash
# Terminal 1: start the backend
make run

# Terminal 2: start the Vite dev server
cd web && npm run dev
```

The Vite dev server starts on **http://localhost:5173** and proxies `/api/*` requests to the backend. Use this workflow when actively developing frontend code.

## Environment Variables

All configuration is via environment variables with sensible defaults:

| Variable             | Default      | Description                              |
|----------------------|--------------|------------------------------------------|
| `SHUTTLE_DB`         | `shuttle.db` | Path to the SQLite database file         |
| `SHUTTLE_ADDR`       | `:8080`      | Address/port for the API server          |
| `SHUTTLE_UPLOAD_DIR` | `uploads`    | Directory for uploaded audio files       |
| `SHUTTLE_STATIC_DIR` | `web/dist`   | Path to built frontend files (set to empty to disable) |

Example:

```bash
SHUTTLE_DB=/data/my-project.db SHUTTLE_ADDR=:9090 make run
```

## Production Build & Deployment

```bash
make build
# Produces: bin/shuttle (with frontend in web/dist/)
```

The backend binary is self-contained (SQLite is embedded via `modernc.org/sqlite`, no CGO required). Deploy the binary alongside the `web/dist/` directory:

```bash
SHUTTLE_STATIC_DIR=/path/to/web/dist SHUTTLE_ADDR=:8080 ./bin/shuttle
```

The backend serves both the API and the frontend — no reverse proxy or separate static file server needed. SPA routing is handled automatically (unknown paths fall back to `index.html`).

To serve the API without the frontend (e.g. headless or with a separate frontend deployment):

```bash
SHUTTLE_STATIC_DIR="" ./bin/shuttle
```

## Inputs & Data

### What You Provide

- **Audio files** — Upload audio (interview recordings, dictation, etc.) via the Source Vault panel. The system transcribes, chunks, and indexes the content for search and stitching. Supported formats depend on the configured transcriber.
- **Outline structure** — Build your narrative outline interactively in the center panel. Bullets can be nested, reordered via drag-and-drop, and linked to source chunks.
- **Threads** — Name reading paths through the outline (e.g., "Chapter 1", "Alternate ending"). Paint nodes into threads using the brush tool.

### What the System Creates

- **SQLite database** (`shuttle.db`) — All project state: nodes, edges, threads, chunks, snapshots, and branches. This single file is your entire project.
- **Upload directory** (`uploads/`) — Raw uploaded audio files.

### Key Concepts

| Concept      | Description |
|--------------|-------------|
| **Node**     | A bullet in the outline (title + optional body + source chunks) |
| **Edge**     | A connection between nodes (linear = parent/child, branch = CYOA fork) |
| **Thread**   | A named reading order through selected nodes |
| **Chunk**    | A segment of transcribed audio, linked to nodes as evidence |
| **Snapshot** | A saved point-in-time copy of the entire outline (nodes, edges, threads) |
| **Branch**   | A named working copy of the outline for exploring alternate directions |

## API Endpoints

All endpoints are under `/api/v1/`. The API returns JSON envelopes: `{"data": ...}` on success, `{"error": "..."}` on failure.

| Method | Path                            | Description                    |
|--------|---------------------------------|--------------------------------|
| GET    | `/healthz`                      | Health check                   |
| GET    | `/api/v1/nodes`                 | List all nodes                 |
| POST   | `/api/v1/nodes`                 | Create a node                  |
| GET    | `/api/v1/nodes/{id}`            | Get a node                     |
| PUT    | `/api/v1/nodes/{id}`            | Update a node                  |
| DELETE | `/api/v1/nodes/{id}`            | Delete a node                  |
| POST   | `/api/v1/nodes/{id}/move`       | Reparent/reorder a node        |
| GET    | `/api/v1/edges`                 | List all edges                 |
| POST   | `/api/v1/edges`                 | Create an edge                 |
| DELETE | `/api/v1/edges/{id}`            | Delete an edge                 |
| GET    | `/api/v1/threads`               | List threads                   |
| POST   | `/api/v1/threads`               | Create a thread                |
| PUT    | `/api/v1/threads/{id}`          | Update a thread                |
| DELETE | `/api/v1/threads/{id}`          | Delete a thread                |
| GET    | `/api/v1/threads/{id}/nodes`    | Get thread node ordering       |
| PUT    | `/api/v1/threads/{id}/nodes`    | Set thread node ordering       |
| GET    | `/api/v1/uploads`               | List uploads                   |
| POST   | `/api/v1/uploads`               | Upload an audio file           |
| GET    | `/api/v1/search?q=...`          | Search chunks                  |
| POST   | `/api/v1/stitch`                | Stitch chunks into prose       |
| POST   | `/api/v1/export/markdown`       | Export outline as Markdown     |
| GET    | `/api/v1/snapshots`             | List snapshots                 |
| POST   | `/api/v1/snapshots`             | Save a snapshot                |
| GET    | `/api/v1/snapshots/{id}`        | Get snapshot (with full data)  |
| POST   | `/api/v1/snapshots/{id}/restore`| Restore a snapshot             |
| GET    | `/api/v1/branches`              | List branches                  |
| POST   | `/api/v1/branches`              | Create (split) a branch        |
| GET    | `/api/v1/branches/active`       | Get active branch              |
| GET    | `/api/v1/branches/{id}`         | Get branch (with full data)    |
| PUT    | `/api/v1/branches/{id}`         | Rename a branch                |
| DELETE | `/api/v1/branches/{id}`         | Delete a branch                |
| POST   | `/api/v1/branches/{id}/switch`  | Switch to a branch             |

## Testing

```bash
# Backend tests
make test
# or: go test ./...

# Frontend type check
cd web && npm run typecheck

# Frontend lint
cd web && npm run lint
```

## Project Structure

```
cmd/shuttle/            Go entry point
internal/
  api/                  HTTP handlers and routing
  dag/                  DAG validation and traversal
  export/               Markdown export
  ingest/               Audio transcription and chunking
  model/                Domain types
  search/               Hybrid search index
  stitch/               Prose stitching
  store/                SQLite persistence + migrations
web/
  src/
    components/         React components
    pages/              Route pages
    services/           API client
    stores/             Zustand state stores
    types/              TypeScript type definitions
```
