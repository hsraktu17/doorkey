.PHONY: run build test lint fmt clean demo-gateway web-install web-dev web-test web-e2e traffic

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
	gofmt -w cmd internal

clean:
	rm -rf bin

# --- Dashboard (web/) ---

# Gateway with mock providers only: no API keys, no quota. Port 18089.
demo-gateway:
	DOORKEY_API_KEY= go run ./cmd/doorkey -config web/e2e/doorkey.e2e.json

web-install:
	cd web && npm ci && npx playwright install chromium

web-dev:
	cd web && npm run dev

web-test:
	cd web && npm run lint && npx tsc --noEmit && npm test

web-e2e:
	cd web && npm run e2e

# Steady mock traffic against demo-gateway.
traffic:
	cd web && DOORKEY_URL=http://127.0.0.1:18089 DOORKEY_API_KEY= npm run traffic
