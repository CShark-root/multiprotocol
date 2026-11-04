-- System log lines from the panel and the cores; pruned to a few days.
CREATE TABLE logs (
    id          INTEGER PRIMARY KEY,
    ts          INTEGER NOT NULL, -- unix milliseconds
    level       TEXT    NOT NULL, -- debug | info | warn | error
    source      TEXT    NOT NULL, -- panel | xray | hysteria | sing-box
    msg         TEXT    NOT NULL,
    fields_json TEXT    NOT NULL DEFAULT '{}'
);
CREATE INDEX logs_ts_idx ON logs (ts);

-- Things an admin wants to notice: limits hit, cores failing, logins.
CREATE TABLE events (
    id        INTEGER PRIMARY KEY,
    ts        INTEGER NOT NULL, -- unix seconds
    type      TEXT    NOT NULL, -- e.g. user.limited, core.down, auth.failed
    severity  TEXT    NOT NULL, -- info | warning | danger
    user_id   INTEGER,
    message   TEXT    NOT NULL DEFAULT '',
    data_json TEXT    NOT NULL DEFAULT '{}'
);
CREATE INDEX events_ts_idx ON events (ts);
