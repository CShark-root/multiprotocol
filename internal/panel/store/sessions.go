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

func (s *Store) DeleteExpiredSessions(ctx context.Context, now time.Time) (int64, error) {
	res, err := s.db.ExecContext(ctx, `DELETE FROM sessions WHERE expires_at <= ?`, now.Unix())
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}
