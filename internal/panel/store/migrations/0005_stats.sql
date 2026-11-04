-- When the periodic traffic counter was last reset. Existing users start
-- from "now" so the upgrade does not wipe their current period.
ALTER TABLE users ADD COLUMN last_reset_at INTEGER NOT NULL DEFAULT 0;
UPDATE users SET last_reset_at = CAST(strftime('%s', 'now') AS INTEGER);

-- Traffic per user per day (server local date), for charts.
CREATE TABLE traffic_daily (
    user_id INTEGER NOT NULL,
    day     TEXT    NOT NULL, -- YYYY-MM-DD
    bytes   INTEGER NOT NULL DEFAULT 0,
    PRIMARY KEY (user_id, day)
);
CREATE INDEX traffic_daily_day_idx ON traffic_daily (day);
