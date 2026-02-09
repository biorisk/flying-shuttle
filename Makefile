.PHONY: build test run lint clean

build:
	go build -o bin/shuttle ./cmd/shuttle

test:
	go test ./...

run: build
	./bin/shuttle

lint:
	go vet ./...

clean:
	rm -rf bin/
