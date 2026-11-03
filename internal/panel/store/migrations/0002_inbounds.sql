-- One credential per user, shared by all VLESS inbounds. Existing users get
-- a random version-4 UUID.
ALTER TABLE users ADD COLUMN uuid TEXT NOT NULL DEFAULT '';
UPDATE users SET uuid = lower(
    hex(randomblob(4)) || '-' || hex(randomblob(2)) || '-4' || substr(hex(randomblob(2)), 2) || '-' ||
    substr('89ab', 1 + (abs(random()) % 4), 1) || substr(hex(randomblob(2)), 2) || '-' || hex(randomblob(6))
) WHERE uuid = '';
CREATE UNIQUE INDEX users_uuid_idx ON users (uuid);

CREATE TABLE inbounds (
    id            INTEGER PRIMARY KEY,
    remark        TEXT    NOT NULL,
    protocol      TEXT    NOT NULL,               -- vless
    transport     TEXT    NOT NULL,               -- xhttp | tcp | grpc
    security      TEXT    NOT NULL,               -- reality
    port          INTEGER NOT NULL,
    address       TEXT    NOT NULL DEFAULT '',    -- host for client links; '' = panel default
    settings_json TEXT    NOT NULL,               -- keys, SNI, transport options
    enabled       INTEGER NOT NULL DEFAULT 1,
    created_at    INTEGER NOT NULL,
    updated_at    INTEGER NOT NULL
);
