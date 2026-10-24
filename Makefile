.PHONY: dev install test build

install:
	go mod download
	pnpm --dir web install --frozen-lockfile

# Linux/macOS development: the shell trap stops both children on exit.
dev:
	@set -eu; \
	CHUNKD_DEV=true CHUNKD_ORIGIN=http://127.0.0.1:5173 go run ./cmd/chunkd & backend=$$!; \
	trap 'kill $$backend 2>/dev/null || true' EXIT INT TERM; \
	pnpm --dir web dev

test:
	go test ./...
	go vet ./...
	pnpm --dir web test

build:
	go build -o bin/chunkd ./cmd/chunkd
	pnpm --dir web build
