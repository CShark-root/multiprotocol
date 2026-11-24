-- Optional fine-grained history. Minute is a Unix timestamp rounded down to
-- a minute; rows older than seven days are removed by the traffic sampler.
CREATE TABLE traffic_minutes (
    user_id INTEGER NOT NULL,
    minute  INTEGER NOT NULL,
    up      INTEGER NOT NULL DEFAULT 0,
    down    INTEGER NOT NULL DEFAULT 0,
    PRIMARY KEY (user_id, minute)
);
CREATE INDEX traffic_minutes_minute_idx ON traffic_minutes (minute);
