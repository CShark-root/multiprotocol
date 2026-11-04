package store

import (
	"context"
	"database/sql"
	"time"
)

type Admin struct {
	ID           int64
	Username     string
	PasswordHash string
	TOTPSecret   string // "" = two-factor authentication off
	CreatedAt    time.Time
}

func (s *Store) CountAdmins(ctx context.Context) (int, error) {
	var n int
	err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM admins`).Scan(&n)
	return n, err
}

// CreateFirstAdmin inserts the admin only while the table is empty, so two
// concurrent setup requests cannot both succeed. It returns ErrConflict once
// an admin exists.
func (s *Store) CreateFirstAdmin(ctx context.Context, username, passwordHash string, now time.Time) (Admin, error) {
	res, err := s.db.ExecContext(ctx, `INSERT INTO admins (username, password_hash, created_at)
		SELECT ?, ?, ? WHERE NOT EXISTS (SELECT 1 FROM admins)`,
		username, passwordHash, now.Unix())
	if err != nil {
		return Admin{}, err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return Admin{}, err
	}
	if n == 0 {
		return Admin{}, ErrConflict
	}
	id, err := res.LastInsertId()
	if err != nil {
		return Admin{}, err
	}
	return Admin{ID: id, Username: username, PasswordHash: passwordHash, CreatedAt: fromUnix(now.Unix())}, nil
}

const adminColumns = `id, username, password_hash, COALESCE(totp_secret, ''), created_at`

func (s *Store) AdminByUsername(ctx context.Context, username string) (Admin, error) {
	return scanAdmin(s.db.QueryRowContext(ctx, `SELECT `+adminColumns+` FROM admins WHERE username = ?`, username))
}

func (s *Store) AdminByID(ctx context.Context, id int64) (Admin, error) {
	return scanAdmin(s.db.QueryRowContext(ctx, `SELECT `+adminColumns+` FROM admins WHERE id = ?`, id))
}

func (s *Store) SetAdminPassword(ctx context.Context, id int64, passwordHash string) error {
	_, err := s.db.ExecContext(ctx, `UPDATE admins SET password_hash = ? WHERE id = ?`, passwordHash, id)
	return err
}

// SetAdminTOTP stores the secret; "" turns two-factor authentication off.
func (s *Store) SetAdminTOTP(ctx context.Context, id int64, secret string) error {
	_, err := s.db.ExecContext(ctx, `UPDATE admins SET totp_secret = ? WHERE id = ?`,
		sql.NullString{String: secret, Valid: secret != ""}, id)
	return err
}

func scanAdmin(row *sql.Row) (Admin, error) {
	var a Admin
	var created int64
	if err := row.Scan(&a.ID, &a.Username, &a.PasswordHash, &a.TOTPSecret, &created); err != nil {
		return Admin{}, notFound(err)
	}
	a.CreatedAt = fromUnix(created)
	return a, nil
}

func (s *Store) AdminUsernames(ctx context.Context) ([]string, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT username FROM admins ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var u string
		if err := rows.Scan(&u); err != nil {
			return nil, err
		}
		out = append(out, u)
	}
	return out, rows.Err()
}
