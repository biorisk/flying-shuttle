"""
Local embedding server for Flying Shuttle.

The Go backend spawns and supervises this process. It holds the Qwen3 model in
memory and answers a tiny JSON protocol:

    GET  /health              -> 200 {"status":"ok","dim":N}   (503 until loaded)
    POST /embed  {"texts":[…]} -> 200 {"embeddings":[[…]], "dim":N}

Run standalone for debugging:

    python embed_server.py --addr 127.0.0.1:8071
"""

import argparse
import json
import sys
import threading
import traceback
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer

from embed import BATCH_SIZE, get_embeddings, _load_model

# Model calls are serialized: mlx generation is not reentrant and RAM is tight.
_model_lock = threading.Lock()
_ready = threading.Event()
_dim = 0


def _warm_up():
    """Load the model and record the embedding dimension."""
    global _dim
    try:
        _load_model()
        vec = get_embeddings(["warm up"])
        _dim = int(vec.shape[1])
    except Exception as e:  # noqa: BLE001
        print(
            f"embed_server: FATAL: could not load model: {e}\n"
            "embed_server: install deps and the model, then restart "
            "(see python/README or `make embed-setup`).",
            file=sys.stderr,
            flush=True,
        )
        # Exit so the supervisor sees a failure rather than a silent hang.
        import os
        os._exit(3)
    _ready.set()
    print(f"embed_server: model loaded, dim={_dim}", flush=True)


def _embed_all(texts):
    """Embed texts in small batches, returning a list of float lists."""
    out = []
    with _model_lock:
        for i in range(0, len(texts), BATCH_SIZE):
            batch = texts[i:i + BATCH_SIZE]
            vecs = get_embeddings(batch)
            out.extend([[float(x) for x in row] for row in vecs])
    return out


class Handler(BaseHTTPRequestHandler):
    protocol_version = "HTTP/1.1"

    def _send(self, code, payload):
        body = json.dumps(payload).encode("utf-8")
        self.send_response(code)
        self.send_header("Content-Type", "application/json")
        self.send_header("Content-Length", str(len(body)))
        self.end_headers()
        self.wfile.write(body)

    def log_message(self, fmt, *args):  # keep stderr readable
        sys.stderr.write("embed_server: " + (fmt % args) + "\n")

    def do_GET(self):
        if self.path.rstrip("/") == "/health":
            if _ready.is_set():
                self._send(200, {"status": "ok", "dim": _dim})
            else:
                self._send(503, {"status": "loading"})
            return
        self._send(404, {"error": "not found"})

    def do_POST(self):
        if self.path.rstrip("/") != "/embed":
            self._send(404, {"error": "not found"})
            return
        if not _ready.is_set():
            self._send(503, {"error": "model still loading"})
            return

        try:
            length = int(self.headers.get("Content-Length", "0"))
            raw = self.rfile.read(length) if length else b"{}"
            texts = json.loads(raw).get("texts", [])
            if not isinstance(texts, list) or not all(isinstance(t, str) for t in texts):
                self._send(400, {"error": "texts must be a list of strings"})
                return
            if not texts:
                self._send(200, {"embeddings": [], "dim": _dim})
                return

            embeddings = _embed_all(texts)
            self._send(200, {"embeddings": embeddings, "dim": _dim})
        except Exception as e:  # noqa: BLE001 — report everything to the supervisor
            traceback.print_exc()
            self._send(500, {"error": str(e)})


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("--addr", default="127.0.0.1:8071", help="host:port to listen on")
    args = ap.parse_args()

    host, _, port = args.addr.rpartition(":")
    server = ThreadingHTTPServer((host or "127.0.0.1", int(port)), Handler)

    # Load the model in the background so /health is answerable immediately.
    threading.Thread(target=_warm_up, daemon=True).start()

    print(f"embed_server: listening on {args.addr}", flush=True)
    try:
        server.serve_forever()
    except KeyboardInterrupt:
        pass
    finally:
        server.shutdown()


if __name__ == "__main__":
    main()
