-- Times are unix seconds (UTC). Booleans are 0/1.

CREATE TABLE admins (
    id            INTEGER PRIMARY KEY,
    username      TEXT    NOT NULL UNIQUE COLLATE NOCASE,
    password_hash TEXT    NOT NULL,
    totp_secret   TEXT,
    created_at    INTEGER NOT NULL
);

-- Only a SHA-256 of the session token is stored, so a leaked database
-- does not hand out live sessions.
CREATE TABLE sessions (
    token_hash TEXT    PRIMARY KEY,
    admin_id   INTEGER NOT NULL REFERENCES admins (id) ON DELETE CASCADE,
    created_at INTEGER NOT NULL,
    expires_at INTEGER NOT NULL,
    ip         TEXT    NOT NULL DEFAULT '',
    user_agent TEXT    NOT NULL DEFAULT ''
);
CREATE INDEX sessions_admin_idx ON sessions (admin_id);

-- Only "enabled" is stored; expired/limited are derived from the limits.
CREATE TABLE users (
    id            INTEGER PRIMARY KEY,
    name          TEXT    NOT NULL UNIQUE COLLATE NOCASE,
    note          TEXT    NOT NULL DEFAULT '',
    enabled       INTEGER NOT NULL DEFAULT 1,
    traffic_limit INTEGER NOT NULL DEFAULT 0, -- bytes, 0 = unlimited
    traffic_used  INTEGER NOT NULL DEFAULT 0,
    reset_period  TEXT    NOT NULL DEFAULT 'none',
    expire_at     INTEGER,                    -- NULL = never
    sub_token     TEXT    NOT NULL UNIQUE,
    created_at    INTEGER NOT NULL,
    updated_at    INTEGER NOT NULL
);

CREATE TABLE settings (
    key        TEXT PRIMARY KEY,
    value_json TEXT NOT NULL
);

CREATE TABLE audit_log (
    id          INTEGER PRIMARY KEY,
    ts          INTEGER NOT NULL,
    admin_id    INTEGER REFERENCES admins (id) ON DELETE SET NULL,
    action      TEXT    NOT NULL,
    target_type TEXT    NOT NULL DEFAULT '',
    target_id   TEXT    NOT NULL DEFAULT '',
    diff_json   TEXT    NOT NULL DEFAULT '{}'
);
CREATE INDEX audit_log_ts_idx ON audit_log (ts);
