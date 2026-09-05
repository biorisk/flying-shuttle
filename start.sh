#!/usr/bin/env bash
# Build and run the Flying Shuttle server with the local MLX Python environment.
#
# The embedding + LLM sidecars need mlx / mlx_lm, which live in ~/.venv here
# (not the system python3). detectPython() honours $SHUTTLE_PYTHON first, so we
# point it there and run fully offline against the already-cached models.
#
# Usage:  ./start.sh            # build, then run in the foreground
#         ./start.sh --no-build # skip the build, just run ./bin/shuttle
#         SHUTTLE_ADDR=:8090 ./start.sh
set -euo pipefail

cd "$(dirname "$0")"

# --- Python / MLX environment -------------------------------------------------
VENV_PY="${SHUTTLE_PYTHON:-$HOME/.venv/bin/python}"
if [[ ! -x "$VENV_PY" ]]; then
	echo "start.sh: python not found at $VENV_PY" >&2
	echo "  set SHUTTLE_PYTHON to a python with mlx / mlx_lm installed." >&2
	exit 1
fi
if ! "$VENV_PY" -c 'import mlx.core, mlx_lm' 2>/dev/null; then
	echo "start.sh: $VENV_PY is missing mlx / mlx_lm — vector search + digests will be disabled." >&2
fi
export SHUTTLE_PYTHON="$VENV_PY"
export HF_HUB_OFFLINE="${HF_HUB_OFFLINE:-1}"   # models are cached; don't hit the network

# --- build -------------------------------------------------------------------
if [[ "${1:-}" != "--no-build" ]]; then
	echo "start.sh: building…" >&2
	go tool templ generate >/dev/null
	go build -o bin/shuttle ./cmd/shuttle
else
	shift || true
fi

# --- run --------------------------------------------------------------------
echo "start.sh: SHUTTLE_PYTHON=$SHUTTLE_PYTHON  HF_HUB_OFFLINE=$HF_HUB_OFFLINE" >&2
exec ./bin/shuttle "$@"
