// Marks web/ as its own (empty) module so `go test ./...` and `go vet ./...` at the
// repo root skip the Next.js app and its node_modules.
module github.com/hsraktu17/doorkey/web

go 1.25
