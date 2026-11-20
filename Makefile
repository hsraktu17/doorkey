.PHONY: run build test lint fmt clean

# Load provider keys from .env when it exists.
-include .env
export

run:
	go run ./cmd/doorkey

build:
	go build -o bin/doorkey ./cmd/doorkey

test:
	go test -race ./...

lint:
	go vet ./...

fmt:
	gofmt -w .

clean:
	rm -rf bin
