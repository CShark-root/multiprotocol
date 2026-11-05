-- Inbounds left out of a user's subscription (JSON array of ids). Empty means
-- all: new inbounds join every subscription unless the admin hides them.
ALTER TABLE users ADD COLUMN sub_hidden TEXT NOT NULL DEFAULT '';
