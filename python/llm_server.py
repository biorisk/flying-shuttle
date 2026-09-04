"""
Local instruct-LLM server for Flying Shuttle.

Holds one MLX chat model in memory and answers a tiny JSON protocol:

    GET  /health                                  -> 200 {"status":"ok"} (503 loading)
    POST /complete {"system":"...","user":"..."}  -> 200 {"text":"..."}

The Go supervisor (ingest.PythonCompleter) starts this on demand. To keep the
model from sitting resident on an 8 GB machine, the server exits itself after
SHUTTLE_LLM_IDLE seconds with no request; the supervisor restarts it on the
next call.

All MLX work runs on ONE dedicated worker thread — Metal streams are
thread-local, so loading the model on one thread and generating on another
raises "no Stream(gpu, 1) in current thread".

    python llm_server.py --addr 127.0.0.1:8072
"""

import argparse
import gc
import json
import os
import queue
import sys
import threading
import time
import traceback
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer

import mlx.core as mx

MODEL = os.environ.get("SHUTTLE_LLM_MODEL", "mlx-community/gemma-4-e2b-it-4bit")
IDLE_SECONDS = float(os.environ.get("SHUTTLE_LLM_IDLE", "180"))
MAX_TOKENS = int(os.environ.get("SHUTTLE_LLM_MAX_TOKENS", "320"))

try:
    mx.set_cache_limit(512 * 1024 * 1024)
except Exception:  # noqa: BLE001
    pass

_ready = threading.Event()
_last_req = time.time()
_jobs: "queue.Queue" = queue.Queue()


def _worker():
    """Owns the model and every MLX call."""
    global _last_req
    try:
        from mlx_lm import load, generate
        from mlx_lm.sample_utils import make_sampler

        model, tok = load(MODEL)
    except Exception as e:  # noqa: BLE001
        print(f"llm_server: FATAL: could not load {MODEL}: {e}", file=sys.stderr, flush=True)
        os._exit(3)

    _ready.set()
    print(f"llm_server: {MODEL} loaded", flush=True)
    sampler = make_sampler(temp=0.0)

    while True:
        system, user, out = _jobs.get()
        _last_req = time.time()
        try:
            msgs = []
            if system:
                msgs.append({"role": "system", "content": system})
            msgs.append({"role": "user", "content": user})
            try:
                prompt = tok.apply_chat_template(msgs, add_generation_prompt=True, enable_thinking=False)
            except TypeError:
                prompt = tok.apply_chat_template(msgs, add_generation_prompt=True)
            text = generate(model, tok, prompt=prompt, max_tokens=MAX_TOKENS, sampler=sampler, verbose=False)
            if hasattr(mx, "clear_cache"):
                mx.clear_cache()
            gc.collect()
            out.put(("ok", text.strip()))
        except Exception as e:  # noqa: BLE001
            traceback.print_exc()
            out.put(("err", str(e)))
        _last_req = time.time()


def _idle_watch():
    while True:
        time.sleep(5)
        if _ready.is_set() and time.time() - _last_req > IDLE_SECONDS:
            print(f"llm_server: idle {IDLE_SECONDS:.0f}s, exiting to free memory", flush=True)
            os._exit(0)


def _complete(system, user):
    reply: "queue.Queue" = queue.Queue(maxsize=1)
    _jobs.put((system, user, reply))
    status, payload = reply.get()
    if status == "err":
        raise RuntimeError(payload)
    return payload


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
        sys.stderr.write("llm_server: " + (fmt % args) + "\n")

    def do_GET(self):
        if self.path.rstrip("/") == "/health":
            self._send(200 if _ready.is_set() else 503,
                       {"status": "ok"} if _ready.is_set() else {"status": "loading"})
            return
        self._send(404, {"error": "not found"})

    def do_POST(self):
        if self.path.rstrip("/") != "/complete":
            self._send(404, {"error": "not found"})
            return
        if not _ready.is_set():
            self._send(503, {"error": "model still loading"})
            return
        try:
            length = int(self.headers.get("Content-Length", "0"))
            raw = self.rfile.read(length) if length else b"{}"
            p = json.loads(raw)
            self._send(200, {"text": _complete(str(p.get("system", "")), str(p.get("user", "")))})
        except Exception as e:  # noqa: BLE001
            traceback.print_exc()
            self._send(500, {"error": str(e)})


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("--addr", default="127.0.0.1:8072")
    args = ap.parse_args()
    host, _, port = args.addr.rpartition(":")
    server = ThreadingHTTPServer((host or "127.0.0.1", int(port)), Handler)
    threading.Thread(target=_worker, daemon=True).start()
    threading.Thread(target=_idle_watch, daemon=True).start()
    print(f"llm_server: listening on {args.addr}", flush=True)
    try:
        server.serve_forever()
    except KeyboardInterrupt:
        pass
    finally:
        server.shutdown()


if __name__ == "__main__":
    main()
