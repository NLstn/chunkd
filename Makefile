.PHONY: dev install test build

install:
	go mod download
	npm --prefix web ci

# Linux/macOS development: the shell trap stops both children on exit.
dev:
	go build -o bin/chunkd ./cmd/chunkd
	@set -eu; \
	CHUNKD_DEV=true CHUNKD_ORIGIN=http://127.0.0.1:5173 ./bin/chunkd & backend=$$!; \
	trap 'kill $$backend 2>/dev/null || true' EXIT INT TERM; \
	npm --prefix web run dev

test:
	go test ./...
	go vet ./...
	npm --prefix web test

build:
	go build -o bin/chunkd ./cmd/chunkd
	npm --prefix web run build
