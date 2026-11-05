-- Другие наши серверы (узлы). На каждом работает агент: он сам приходит к
-- главной панели за конфигом и приносит трафик и состояние. Токен хранится
-- только хэшем. host — адрес узла для ссылок клиентов.
CREATE TABLE nodes (
  id         INTEGER PRIMARY KEY,
  name       TEXT NOT NULL,
  host       TEXT NOT NULL DEFAULT '',
  token_hash TEXT NOT NULL UNIQUE,
  created_at INTEGER NOT NULL,
  last_seen  INTEGER NOT NULL DEFAULT 0
);
-- 0 — этот сервер (главная панель).
ALTER TABLE inbounds ADD COLUMN node_id INTEGER NOT NULL DEFAULT 0;
