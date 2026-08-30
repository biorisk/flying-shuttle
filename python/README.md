# Embeddings

Flying Shuttle stores chunk embeddings so it can do semantic (vector) search in
addition to keyword (BM25) search. Embeddings are produced by a local
**Qwen3-Embedding-4B (4-bit)** model running on MLX.

## How it fits together

- `embed_server.py` — a tiny HTTP service that holds the model in memory:
  - `GET  /health` → `200` once the model is loaded (`503` while loading)
  - `POST /embed` `{"texts": [...]}` → `{"embeddings": [[...]], "dim": N}`
- The **shuttle** process spawns and supervises `embed_server.py` automatically
  (set `SHUTTLE_EMBED_AUTOSTART=0` to disable). It restarts it on crash and
  gives up after a few fast failures, logging why.
- A background **backfiller** in shuttle embeds any chunk that has no vector
  yet — on startup, every 30s, and immediately after each upload. So you just
  upload audio or text; vectors fill in on their own once the model is up.
- `embed.py` — the original offline batch tool (`--text-dir`, `--convert`);
  still useful for bulk-embedding a directory into a `.fembed` file.

## Setup (one time)

```sh
make embed-setup            # creates python/.venv, installs mlx-lm + numpy
# then download the model into python/Qwen3-Embedding-4B-4bit-DWQ
```

`detectPython()` in the shuttle binary prefers `python/.venv/bin/python`, so
after `make embed-setup` the next `./bin/shuttle` will use it.

## Environment variables

| var | default | meaning |
|-----|---------|---------|
| `SHUTTLE_EMBED_AUTOSTART` | `1` | spawn/supervise the embed server |
| `SHUTTLE_EMBED_ADDR` | `127.0.0.1:8071` | address the server listens on |
| `SHUTTLE_EMBED_SCRIPT` | `python/embed_server.py` | server entrypoint |
| `SHUTTLE_EMBED_DIR` | dir of the script | child process working dir (model lives here) |
| `SHUTTLE_PYTHON` | auto-detected | interpreter to run the server with |
| `SHUTTLE_BM25_PATH` | `shuttle.bm25` | BM25 index snapshot |
| `SHUTTLE_HNSW_PATH` | `shuttle.hnsw` | vector index snapshot |
