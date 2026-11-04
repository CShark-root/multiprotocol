package store

import (
	"context"
	"time"
)

type Session struct {
	TokenHash string
	AdminID   int64
	CreatedAt time.Time
	ExpiresAt time.Time
	IP        string
	UserAgent string
}

func (s *Store) CreateSession(ctx context.Context, sess Session) error {
	_, err := s.db.ExecContext(ctx, `INSERT INTO sessions
		(token_hash, admin_id, created_at, expires_at, ip, user_agent) VALUES (?, ?, ?, ?, ?, ?)`,
		sess.TokenHash, sess.AdminID, sess.CreatedAt.Unix(), sess.ExpiresAt.Unix(), sess.IP, sess.UserAgent)
	return err
}

func (s *Store) SessionByHash(ctx context.Context, tokenHash string) (Session, error) {
	var sess Session
	var created, expires int64
	err := s.db.QueryRowContext(ctx, `SELECT token_hash, admin_id, created_at, expires_at, ip, user_agent
		FROM sessions WHERE token_hash = ?`, tokenHash).
		Scan(&sess.TokenHash, &sess.AdminID, &created, &expires, &sess.IP, &sess.UserAgent)
	if err != nil {
		return Session{}, notFound(err)
	}
	sess.CreatedAt, sess.ExpiresAt = fromUnix(created), fromUnix(expires)
	return sess, nil
}

func (s *Store) DeleteSession(ctx context.Context, tokenHash string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM sessions WHERE token_hash = ?`, tokenHash)
	return err
}

func (s *Store) ListSessions(ctx context.Context, adminID int64, now time.Time) ([]Session, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT token_hash, admin_id, created_at, expires_at, ip, user_agent
		FROM sessions WHERE admin_id = ? AND expires_at > ? ORDER BY created_at DESC`, adminID, now.Unix())
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Session
	for rows.Next() {
		var sess Session
		var created, expires int64
		if err := rows.Scan(&sess.TokenHash, &sess.AdminID, &created, &expires, &sess.IP, &sess.UserAgent); err != nil {
			return nil, err
		}
		sess.CreatedAt, sess.ExpiresAt = fromUnix(created), fromUnix(expires)
		out = append(out, sess)
	}
	return out, rows.Err()
}

// DeleteOtherSessions signs an admin out everywhere except keepHash.
func (s *Store) DeleteOtherSessions(ctx context.Context, adminID int64, keepHash string) (int64, error) {
	res, err := s.db.ExecContext(ctx, `DELETE FROM sessions WHERE admin_id = ? AND token_hash <> ?`, adminID, keepHash)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

// DeleteSessionOf removes one of the admin's sessions by its hash.
func (s *Store) DeleteSessionOf(ctx context.Context, adminID int64, tokenHash string) error {
	res, err := s.db.ExecContext(ctx, `DELETE FROM sessions WHERE admin_id = ? AND token_hash = ?`, adminID, tokenHash)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *Store) DeleteExpiredSessions(ctx context.Context, now time.Time) (int64, error) {
	res, err := s.db.ExecContext(ctx, `DELETE FROM sessions WHERE expires_at <= ?`, now.Unix())
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}
