.PHONY: build build-frontend test run lint clean embed-setup embed-server

build: build-frontend
	go build -o bin/shuttle ./cmd/shuttle

build-frontend:
	. "$$HOME/.nvm/nvm.sh" && nvm use --delete-prefix node && cd web && npm install && npm run build

test:
	go test ./...

run: build
	./bin/shuttle

lint:
	go vet ./...

clean:
	rm -rf bin/ web/dist/

# One-time setup for automatic embeddings: create a venv and install deps.
# Download the model into python/Qwen3-Embedding-4B-4bit-DWQ separately.
embed-setup:
	cd python && python3 -m venv .venv && \
		.venv/bin/pip install --upgrade pip && \
		.venv/bin/pip install -r requirements.txt

# Run the embedding server by hand (normally the shuttle process spawns it).
embed-server:
	cd python && ../python/.venv/bin/python embed_server.py --addr 127.0.0.1:8071
