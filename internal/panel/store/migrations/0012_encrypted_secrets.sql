ALTER TABLE users ADD COLUMN sub_token_hash TEXT;
CREATE UNIQUE INDEX users_sub_token_hash_idx ON users(sub_token_hash) WHERE sub_token_hash IS NOT NULL;
