-- A key shared from the Amnezia app (vpn://...), shown next to the panel's
-- own links. The panel only displays it: Amnezia manages that access.
ALTER TABLE users ADD COLUMN amnezia_key TEXT NOT NULL DEFAULT '';
