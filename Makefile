.PHONY: build generate templ-tools test run lint clean dev embed-setup embed-server

# templ CLI version — keep in lockstep with the github.com/a-h/templ module in go.mod.
TEMPL_VERSION := v0.3.1020

build: generate
	go build -o bin/shuttle ./cmd/shuttle

# Regenerate *_templ.go from *.templ using the module-pinned templ tool
# (go.mod `tool` directive) — no separately installed binary required.
generate:
	go tool templ generate

# Install the standalone templ CLI (optional — for `templ generate --watch` / LSP).
templ-tools:
	go install github.com/a-h/templ/cmd/templ@$(TEMPL_VERSION)

test: generate
	go test ./...

run: build
	./bin/shuttle

# Dev loop: regenerate templ on change and restart the server. Requires
# `make templ-tools`.
dev:
	templ generate --watch --proxy=http://localhost:8080 --cmd="go run ./cmd/shuttle"

lint: generate
	go vet ./...

clean:
	rm -rf bin/
	find . -name '*_templ.go' -delete

# One-time setup for automatic embeddings: create a venv and install deps.
# Download the model into python/Qwen3-Embedding-4B-4bit-DWQ separately.
embed-setup:
	cd python && python3 -m venv .venv && \
		.venv/bin/pip install --upgrade pip && \
		.venv/bin/pip install -r requirements.txt

# Run the embedding server by hand (normally the shuttle process spawns it).
embed-server:
	cd python && ../python/.venv/bin/python embed_server.py --addr 127.0.0.1:8071
