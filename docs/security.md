# Foundation security

## One local administrator

Chunkd has one administrator for the host, with a database constraint enforcing
that invariant. User creation, invitations, roles and account administration are
outside the MVP. First-run setup requires a random 256-bit token in
`CHUNKD_DATA_DIR/setup-token`. Read it through local host access, then submit it
with the initial credentials. A daemon restart rotates an unused token. The
database prevents re-running setup even if token-file deletion fails.

Passwords are bcrypt hashes (12–72 byte input). Sessions use random 256-bit
tokens, store SHA-256 token hashes, expire after 12 hours, survive daemon restart
and are revoked by logout. Login rotates the current browser session. Expired
rows are pruned on successful login. Cookies are HttpOnly, SameSite=Strict,
scoped to `/odata`, and Secure by default. They are never stored in localStorage.

Setup/login share a bounded global limit of ten attempts per minute. This is
intentionally simple for one administrator; repeated failures can temporarily
block local sign-in. Do not trust forwarded client-IP headers without a defined
proxy trust boundary. A proxy can add stricter rate limits before public access.

## Browser and transport boundary

Every write requires an exact configured Origin and `X-Chunkd-Request: 1` header.
No CORS access is enabled. Login, setup and logout use the same protection.
The request body is limited to 16 KiB; the HTTP server uses bounded header,
read, write and idle timeouts. Batch is disabled in M1 so nested requests cannot
skip session or mutation checks. API replies use no-store and nosniff headers.

Only GetSession, Setup and Login are anonymous. Metadata and all entity endpoints
require a valid, unexpired session. The OData service owns API routing and
serialization; middleware enforces transport/authentication policy. Private
credential/session models are not registered with the service.

## Run the daemon

Use an unprivileged dedicated Linux account and keep the data directory private.
The daemon creates/chmods it to 0700 and creates the setup token as 0600. Windows
file modes do not provide the same guarantees; Linux is the supported deployment
target. Do not use a world-accessible parent or a symlink for the data directory.
Database contents, WAL files and backups are sensitive.

Secure mode expects HTTPS at a reverse proxy, with the backend bound to loopback.
Serve `web/dist` on the same browser origin, proxy `/odata` unchanged, preserve
the browser Origin header, and set `CHUNKD_ORIGIN` to that exact HTTPS origin.
Plain HTTP development requires explicit `CHUNKD_DEV=true`, a loopback HTTP
origin and a loopback listener. Vite is a development server, not a deployment
server. Single-binary hosting/systemd packaging remains C-19.

## Recovery and limitations

M1 has no password-reset API. An operator with host access can perform offline
recovery using SQLite: stop Chunkd, back up the entire data directory, then in a
transaction delete all sessions and the administrator row. Restart and use the
new setup token to configure the replacement administrator. Server/Job tables
must be preserved. Never delete the whole database merely to reset a password.

MFA, account management, fine-grained permissions, remote-agent trust, audit
logging and an automatic backup scheduler are outside this foundation. Future
process and filesystem capabilities require the M4 security/lifecycle tests
before release.
