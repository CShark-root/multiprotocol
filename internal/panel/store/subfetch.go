package store

import (
	"context"
	"database/sql"
	"strings"
	"time"
)

// SubFetch — одно обновление подписки.
type SubFetch struct {
	At  time.Time `json:"at"`
	App string    `json:"app"` // User-Agent приложения
	IP  string    `json:"ip"`
}

// subFetchKeep — сколько обновлений помнить на пользователя.
const subFetchKeep = 20

// LogSubFetch запоминает обновление подписки. Ошибки не мешают выдаче.
func (s *Store) LogSubFetch(ctx context.Context, userID int64, app, ip string, now time.Time) {
	app = strings.Map(func(r rune) rune {
		if r < 0x20 || r == 0x7f {
			return -1
		}
		return r
	}, app)
	if len(app) > 160 {
		app = app[:160]
	}
	s.inTx(ctx, func(tx *sql.Tx) error {
		if _, err := tx.ExecContext(ctx, `INSERT INTO sub_fetches (user_id, at, app, ip) VALUES (?, ?, ?, ?)`, userID, now.Unix(), app, ip); err != nil {
			return err
		}
		_, err := tx.ExecContext(ctx, `DELETE FROM sub_fetches WHERE user_id = ? AND id NOT IN
			(SELECT id FROM sub_fetches WHERE user_id = ? ORDER BY id DESC LIMIT ?)`, userID, userID, subFetchKeep)
		return err
	})
}

func (s *Store) ListSubFetches(ctx context.Context, userID int64) ([]SubFetch, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT at, app, ip FROM sub_fetches WHERE user_id = ? ORDER BY id DESC`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []SubFetch{}
	for rows.Next() {
		var f SubFetch
		var at int64
		if err := rows.Scan(&at, &f.App, &f.IP); err != nil {
			return nil, err
		}
		f.At = fromUnix(at)
		out = append(out, f)
	}
	return out, rows.Err()
}

// LastSubFetches — последнее обновление подписки каждого пользователя.
func (s *Store) LastSubFetches(ctx context.Context) (map[int64]SubFetch, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT f.user_id, f.at, f.app, f.ip FROM sub_fetches f
		JOIN (SELECT user_id, MAX(id) id FROM sub_fetches GROUP BY user_id) m ON m.id = f.id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[int64]SubFetch{}
	for rows.Next() {
		var id, at int64
		var f SubFetch
		if err := rows.Scan(&id, &at, &f.App, &f.IP); err != nil {
			return nil, err
		}
		f.At = fromUnix(at)
		out[id] = f
	}
	return out, rows.Err()
}
