.PHONY: build build-frontend test run lint clean

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
