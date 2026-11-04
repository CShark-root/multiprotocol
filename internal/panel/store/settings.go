package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
)

// GetSetting loads a JSON setting into v. It reports false when the key
// was never saved, leaving v untouched.
func (s *Store) GetSetting(ctx context.Context, key string, v any) (bool, error) {
	var raw string
	err := s.db.QueryRowContext(ctx, `SELECT value_json FROM settings WHERE key = ?`, key).Scan(&raw)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return true, json.Unmarshal([]byte(raw), v)
}

func (s *Store) SetSetting(ctx context.Context, key string, v any) error {
	raw, err := json.Marshal(v)
	if err != nil {
		return err
	}
	_, err = s.db.ExecContext(ctx, `INSERT INTO settings (key, value_json) VALUES (?, ?)
		ON CONFLICT (key) DO UPDATE SET value_json = excluded.value_json`, key, string(raw))
	return err
}
