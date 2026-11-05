package store

import (
	"context"
	"database/sql"
)

// AmneziaLink — клиент AmneziaWG, привязанный к пользователю панели.
type AmneziaLink struct {
	Container string `json:"container"`
	Key       string `json:"key"`
	UserID    int64  `json:"userId"`
	Rx        int64  `json:"-"`
	Tx        int64  `json:"-"`
}

func (s *Store) ListAmneziaLinks(ctx context.Context) ([]AmneziaLink, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT container, key, user_id, rx, tx FROM amnezia_links ORDER BY container, key`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []AmneziaLink{}
	for rows.Next() {
		var l AmneziaLink
		if err := rows.Scan(&l.Container, &l.Key, &l.UserID, &l.Rx, &l.Tx); err != nil {
			return nil, err
		}
		out = append(out, l)
	}
	return out, rows.Err()
}

// LinkAmnezia привязывает клиента к пользователю (userID 0 — отвязывает).
// Счётчики начинаются заново: прошлый трафик клиента пользователю не засчитывается.
func (s *Store) LinkAmnezia(ctx context.Context, container, key string, userID int64) error {
	if userID == 0 {
		_, err := s.db.ExecContext(ctx, `DELETE FROM amnezia_links WHERE container = ? AND key = ?`, container, key)
		return err
	}
	_, err := s.db.ExecContext(ctx, `INSERT INTO amnezia_links (container, key, user_id) VALUES (?, ?, ?)
		ON CONFLICT (container, key) DO UPDATE SET user_id = excluded.user_id, rx = -1, tx = -1`, container, key, userID)
	return err
}

// SetAmneziaCounters запоминает последние увиденные счётчики клиентов.
func (s *Store) SetAmneziaCounters(ctx context.Context, links []AmneziaLink) error {
	return s.inTx(ctx, func(tx *sql.Tx) error {
		for _, l := range links {
			if _, err := tx.ExecContext(ctx, `UPDATE amnezia_links SET rx = ?, tx = ? WHERE container = ? AND key = ?`, l.Rx, l.Tx, l.Container, l.Key); err != nil {
				return err
			}
		}
		return nil
	})
}
