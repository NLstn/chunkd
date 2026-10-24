# Architecture and MVP boundaries

## Decisions

- One host, Linux first, Minecraft Java Edition only. One local administrator;
  no user-management subsystem or multi-tenant account model.
- One Go daemon owns SQLite and (from M2) supervised Java processes. Each
  Minecraft server gets an isolated directory and validated ports/runtime
  configuration. Containers, remote agents and orchestration are outside v0.1.0.
- All application APIs use `github.com/nlstn/go-odata`, including operations that
  are modeled as OData actions/functions. No parallel REST, GraphQL or WebSocket
  application API. Logs/metrics use bounded OData polling in M3.
- GORM provides persistence using SQLite in WAL mode. A single database
  connection serializes writes; busy waits are bounded. Migrations are embedded,
  numbered SQL files applied once inside a transaction; schema is not inferred
  with AutoMigrate. Add a new migration instead of editing an applied one.
- React + TypeScript + Vite uses a same-origin typed OData client. M1 serves the
  development UI through Vite and proxies OData to Go. M5 embeds the built UI in
  the Linux binary and supplies systemd/release packaging.

## M1 contracts

| OData resource | Method | Authentication | Purpose |
| --- | --- | --- | --- |
| `/odata/GetSession()` | GET | Public | Setup-required and session flags |
| `/odata/Setup` | POST | Host setup token | Create the one local administrator |
| `/odata/Login` | POST | Credentials | Issue a session cookie |
| `/odata/Logout` | POST | Session | Revoke current session |
| `/odata/$metadata` | GET | Session | OData CSDL contract |
| `/odata/Servers` | GET | Session | Read-only server configuration contract |
| `/odata/Jobs` | GET | Session | Read-only durable job contract |

Setup/Login/Logout are go-odata unbound actions. GetSession is an unbound
function. Servers have ID, Name, State, CreatedAt; Jobs have ID, Kind, State,
CreatedAt. Neither collection is populated automatically. Authentication tables
are private and never registered as OData entities. Entity mutation and batch
are unavailable in M1; authenticated metadata and query options come from the
library. Collections default to 100 results; clients request explicit limits.

## Native process model (M2 onward)

The supervisor will execute a validated Java binary directly using explicit
argument arrays, never a shell command assembled from user input. It will own
stdin/stdout/stderr, capture bounded ordered logs and serialize lifecycle changes
per server. State transitions and asynchronous Jobs will be persisted; client
polling will not own process lifetime.

Start/Stop/Restart will be OData actions with authorization. Stop will first
request Minecraft's graceful shutdown, then enforce a timeout. Unique ports,
isolated working directories, verified Vanilla/Paper artifacts and explicit EULA
acceptance must precede a joinable-server workflow. Java compatibility must be
checked against the selected Minecraft version.

M4 adds reconciliation after daemon failure. A PID alone is not sufficient proof
of ownership; recovery must validate process identity and refuse to kill an
ambiguous process. Backup restore must be consistent, confirm intent and protect
live data; file operations must reject traversal and symlink escapes. These
features are design constraints, not claimed as implemented in M1.

## Milestones

| Milestone | Issues | Outcome |
| --- | --- | --- |
| M1 Foundation | C-01–C-05 | Buildable daemon/UI, persistence, OData and local login |
| M2 First Minecraft Server | C-06–C-10 | Native Java supervision and first joinable server |
| M3 Everyday Administration | C-11–C-14 | Console, metrics, properties and multiple servers |
| M4 Files/Backups/Resilience | C-15–C-18 | Safe file access, backup/restore and failure recovery |
| M5 v0.1.0 | C-19–C-20 | Embedded frontend, Linux packaging and verified deployment |

The machine-readable [roadmap](roadmap.json) preserves titles, priorities,
dependencies and acceptance criteria. GitHub issues also have native blocked-by
relationships and linked dependencies in their descriptions.
