# Chunkd

Minecraft Java Edition server administration for your own Linux host.

Chunkd uses a Go daemon, SQLite/GORM persistence, and a React + TypeScript + Vite
interface. **Every application API is provided by go-odata**, including admin
setup, login and logout. Future Minecraft servers run as native managed Java
processes, with no container runtime required.

## Current status

M1 foundation: versioned SQLite migrations, Servers/Jobs OData contracts, one
local administrator, session authentication, dashboard shell and CI. Server
installation, Java process control, console, files, backups and distribution
packaging are planned in M2–M5. This foundation cannot launch Minecraft yet.

There is no user management: no user list, roles, invitations or account screens.

## Develop locally

Requirements: Go 1.26+, Node.js 24, pnpm 11.25.0; `make` for the combined command.
SQLite is embedded using a pure Go GORM driver; no SQLite installation or C
compiler is required for normal builds. Linux is the deployment target.

```sh
make install
make dev
```

Open http://127.0.0.1:5173. Read `data/setup-token` on the host and paste its
contents into the first-run form. Set a username and a 12–72 byte password. Setup
is allowed once; the token file is removed afterward. Only a hash of the password
and of each session token is persisted. Setup tokens rotate on daemon restart.

Alternatively, run the backend and frontend in separate terminals:

```sh
CHUNKD_DEV=true CHUNKD_ORIGIN=http://127.0.0.1:5173 go run ./cmd/chunkd
pnpm --dir web dev
```

On PowerShell, set `$env:CHUNKD_DEV='true'` and
`$env:CHUNKD_ORIGIN='http://127.0.0.1:5173'`, then run
`go run ./cmd/chunkd`. Run `pnpm --dir web dev` in another terminal. Install with
`go mod download` and `pnpm --dir web install --frozen-lockfile`.

Use the exact browser address above: CSRF checks compare Origin exactly and
cookies use the browser's host. Vite proxies `/odata` to `127.0.0.1:8080` without
rewriting Origin. All other application endpoints require a valid session.

## Check and build

```sh
make test
make build
# Linux, with a C compiler for Go's race detector:
go test -race ./...
```

Build outputs are `bin/chunkd` and `web/dist`. M1 deliberately keeps them separate;
frontend embedding and release packaging are tracked in C-19. CI runs Go tests
with the race detector, vet and migration checks, frontend tests/type-check/build,
and builds a Linux daemon with CGO disabled.

## Configuration

| Variable | Default | Purpose |
| --- | --- | --- |
| `CHUNKD_LISTEN` | `127.0.0.1:8080` | Backend HTTP listener |
| `CHUNKD_DATA_DIR` | `./data` | SQLite, setup token; future server files/backups |
| `CHUNKD_ORIGIN` | `https://localhost` | Exact public browser origin, without trailing slash |
| `CHUNKD_DEV` | `false` | Explicitly allow HTTP cookies for loopback-only development |

The secure default assumes an HTTPS reverse proxy serving the frontend and
forwarding `/odata` to the loopback backend. It does not start a TLS listener.
Use `CHUNKD_ORIGIN` matching the proxy's external origin and keep production
`CHUNKD_DEV=false`. See [security](docs/security.md) before exposing the host.

## Project map

```text
cmd/chunkd/                     daemon startup and shutdown
internal/api/                   go-odata registration and single-admin auth
internal/config/                environment configuration and validation
internal/domain/                persisted application contracts
internal/storage/migrations/    immutable, versioned SQL migrations
web/src/                       React shell and typed OData client
docs/                          architecture, security and agreed roadmap
```

See [architecture](docs/architecture.md), [security](docs/security.md), and the
[MVP roadmap](docs/roadmap.json). GitHub milestones and issues C-01–C-20 track the
implementation. M1 issues close when their pull request is merged.
