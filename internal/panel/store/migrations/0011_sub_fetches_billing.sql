-- Когда и каким приложением пользователь обновлял подписку (последние 20).
CREATE TABLE sub_fetches (
  id      INTEGER PRIMARY KEY,
  user_id INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  at      INTEGER NOT NULL,
  app     TEXT NOT NULL,
  ip      TEXT NOT NULL
);
CREATE INDEX sub_fetches_user ON sub_fetches(user_id, at);

-- Оплата VPS: до какого дня оплачен (unix, 0 — не задано) и сколько стоит.
ALTER TABLE nodes ADD COLUMN paid_until INTEGER NOT NULL DEFAULT 0;
ALTER TABLE nodes ADD COLUMN price TEXT NOT NULL DEFAULT '';
