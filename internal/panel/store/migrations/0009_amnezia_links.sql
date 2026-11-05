-- Клиенты AmneziaWG, привязанные к пользователям панели: срок и лимит
-- трафика пользователя действуют и на них. rx/tx — последние увиденные
-- счётчики интерфейса (-1: ещё не видели), чтобы считать прирост.
CREATE TABLE amnezia_links (
  container TEXT NOT NULL,
  key       TEXT NOT NULL,
  user_id   INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  rx        INTEGER NOT NULL DEFAULT -1,
  tx        INTEGER NOT NULL DEFAULT -1,
  PRIMARY KEY (container, key)
);
CREATE INDEX amnezia_links_user ON amnezia_links(user_id);
