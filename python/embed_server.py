"""
Local embedding server for Flying Shuttle.

The Go backend spawns and supervises this process. It holds
`mlx-community/embeddinggemma-300m` (768-dim) in memory and answers a tiny
JSON protocol:

    GET  /health                                 -> 200 {"status":"ok","dim":768}
    POST /embed  {"texts":[…], "prompt":"query"}  -> 200 {"embeddings":[[…]], "dim":768}

`prompt` is optional: "document" (default) or "query" — EmbeddingGemma is an
asymmetric retrieval model and applies a different task prefix for each side.

Run standalone for debugging:

    python embed_server.py --addr 127.0.0.1:8071
"""

import argparse
import gc
import json
import os
import sys
import threading
import traceback
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer

import mlx.core as mx
import numpy as np

MODEL = os.environ.get("SHUTTLE_EMBED_MODEL", "mlx-community/embeddinggemma-300m-bf16")
BATCH_SIZE = int(os.environ.get("SHUTTLE_EMBED_BATCH", "16"))

# EmbeddingGemma task prefixes (from the model's config_sentence_transformers).
PROMPTS = {
    "document": "title: none | text: ",
    "query": "task: search result | query: ",
}

# Cap MLX's buffer cache so a batch run does not balloon RAM on an 8GB machine.
try:
    mx.set_cache_limit(256 * 1024 * 1024)
except Exception:  # noqa: BLE001 — older mlx
    pass

_model = None
_tok = None
_model_lock = threading.Lock()
_ready = threading.Event()
_dim = 0


def _load():
    global _model, _tok
    from mlx_embeddings import load

    _model, _tok = load(MODEL)


def _embed_batch(texts, prefix):
    enc = _tok._tokenizer(
        [prefix + t for t in texts], return_tensors="mlx", padding=True, truncation=True, max_length=2048
    )
    out = _model(enc["input_ids"], attention_mask=enc["attention_mask"])
    v = np.array(out.text_embeds, dtype=np.float32)
    norms = np.linalg.norm(v, axis=1, keepdims=True)
    norms[norms == 0] = 1.0
    return v / norms


def _warm_up():
    global _dim
    try:
        _load()
        vec = _embed_batch(["warm up"], PROMPTS["document"])
        _dim = int(vec.shape[1])
    except Exception as e:  # noqa: BLE001
        print(
            f"embed_server: FATAL: could not load {MODEL}: {e}\n"
            "embed_server: `pip install mlx-embeddings` into the venv and make "
            "sure the model is cached, then restart.",
            file=sys.stderr,
            flush=True,
        )
        os._exit(3)
    _ready.set()
    print(f"embed_server: {MODEL} loaded, dim={_dim}", flush=True)


def _embed_all(texts, prompt):
    prefix = PROMPTS.get(prompt, PROMPTS["document"])
    out = []
    with _model_lock:
        for i in range(0, len(texts), BATCH_SIZE):
            rows = _embed_batch(texts[i:i + BATCH_SIZE], prefix)
            out.extend([[float(x) for x in row] for row in rows])
        mx.clear_cache() if hasattr(mx, "clear_cache") else None
        gc.collect()
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

    def log_message(self, fmt, *args):
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
            payload = json.loads(raw)
            texts = payload.get("texts", [])
            prompt = payload.get("prompt", "document")
            if not isinstance(texts, list) or not all(isinstance(t, str) for t in texts):
                self._send(400, {"error": "texts must be a list of strings"})
                return
            if not texts:
                self._send(200, {"embeddings": [], "dim": _dim})
                return
            self._send(200, {"embeddings": _embed_all(texts, prompt), "dim": _dim})
        except Exception as e:  # noqa: BLE001
            traceback.print_exc()
            self._send(500, {"error": str(e)})


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("--addr", default="127.0.0.1:8071")
    args = ap.parse_args()
    host, _, port = args.addr.rpartition(":")
    server = ThreadingHTTPServer((host or "127.0.0.1", int(port)), Handler)
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
