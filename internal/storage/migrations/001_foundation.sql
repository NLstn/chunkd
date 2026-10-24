CREATE TABLE admins (
    id INTEGER PRIMARY KEY CHECK (id = 1),
    username TEXT NOT NULL UNIQUE,
    password_hash TEXT NOT NULL
);
CREATE TABLE sessions (
    token_hash TEXT PRIMARY KEY,
    expires_at DATETIME NOT NULL
);
CREATE INDEX idx_sessions_expires_at ON sessions(expires_at);
CREATE TABLE Servers (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    name TEXT NOT NULL,
    state TEXT NOT NULL,
    created_at DATETIME NOT NULL
);
CREATE TABLE Jobs (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    kind TEXT NOT NULL,
    state TEXT NOT NULL,
    created_at DATETIME NOT NULL
);
