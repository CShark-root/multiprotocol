package store

import (
	"context"
	"database/sql"
	"time"
)

type User struct {
	ID           int64
	Name         string
	Note         string
	Enabled      bool
	TrafficLimit int64 // bytes, 0 = unlimited
	TrafficUsed  int64
	ResetPeriod  string
	ExpireAt     *time.Time // nil = never
	SubToken     string
	UUID         string // VLESS credential
	CreatedAt    time.Time
	UpdatedAt    time.Time
}

// UserFields are the admin-editable columns.
type UserFields struct {
	Name         string
	Note         string
	Enabled      bool
	TrafficLimit int64
	ResetPeriod  string
	ExpireAt     *time.Time
}

const userColumns = `id, name, note, enabled, traffic_limit, traffic_used, reset_period,
	expire_at, sub_token, uuid, created_at, updated_at`

func (s *Store) ListUsers(ctx context.Context) ([]User, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT `+userColumns+` FROM users ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	users := []User{}
	for rows.Next() {
		u, err := scanUser(rows)
		if err != nil {
			return nil, err
		}
		users = append(users, u)
	}
	return users, rows.Err()
}

func (s *Store) GetUser(ctx context.Context, id int64) (User, error) {
	u, err := scanUser(s.db.QueryRowContext(ctx, `SELECT `+userColumns+` FROM users WHERE id = ?`, id))
	return u, notFound(err)
}

func (s *Store) GetUserByName(ctx context.Context, name string) (User, error) {
	u, err := scanUser(s.db.QueryRowContext(ctx, `SELECT `+userColumns+` FROM users WHERE name = ?`, name))
	return u, notFound(err)
}

func (s *Store) GetUserBySubToken(ctx context.Context, token string) (User, error) {
	u, err := scanUser(s.db.QueryRowContext(ctx, `SELECT `+userColumns+` FROM users WHERE sub_token = ?`, token))
	return u, notFound(err)
}

// AddTraffic adds bytes to each user's counter in one transaction.
func (s *Store) AddTraffic(ctx context.Context, byUser map[int64]int64, now time.Time) error {
	return s.inTx(ctx, func(tx *sql.Tx) error {
		for id, bytes := range byUser {
			if _, err := tx.ExecContext(ctx,
				`UPDATE users SET traffic_used = traffic_used + ?, updated_at = ? WHERE id = ?`,
				bytes, now.Unix(), id); err != nil {
				return err
			}
		}
		return nil
	})
}

func (s *Store) CreateUser(ctx context.Context, f UserFields, subToken, uuid string, now time.Time) (User, error) {
	res, err := s.db.ExecContext(ctx, `INSERT INTO users
		(name, note, enabled, traffic_limit, reset_period, expire_at, sub_token, uuid, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		f.Name, f.Note, f.Enabled, f.TrafficLimit, f.ResetPeriod, unixOrNull(f.ExpireAt), subToken, uuid,
		now.Unix(), now.Unix())
	if isUniqueViolation(err) {
		return User{}, ErrConflict
	}
	if err != nil {
		return User{}, err
	}
	id, err := res.LastInsertId()
	if err != nil {
		return User{}, err
	}
	return s.GetUser(ctx, id)
}

func (s *Store) UpdateUser(ctx context.Context, id int64, f UserFields, now time.Time) (User, error) {
	res, err := s.db.ExecContext(ctx, `UPDATE users SET
		name = ?, note = ?, enabled = ?, traffic_limit = ?, reset_period = ?, expire_at = ?, updated_at = ?
		WHERE id = ?`,
		f.Name, f.Note, f.Enabled, f.TrafficLimit, f.ResetPeriod, unixOrNull(f.ExpireAt), now.Unix(), id)
	if isUniqueViolation(err) {
		return User{}, ErrConflict
	}
	if err != nil {
		return User{}, err
	}
	if n, err := res.RowsAffected(); err != nil {
		return User{}, err
	} else if n == 0 {
		return User{}, ErrNotFound
	}
	return s.GetUser(ctx, id)
}

func (s *Store) DeleteUser(ctx context.Context, id int64) error {
	res, err := s.db.ExecContext(ctx, `DELETE FROM users WHERE id = ?`, id)
	if err != nil {
		return err
	}
	if n, err := res.RowsAffected(); err != nil {
		return err
	} else if n == 0 {
		return ErrNotFound
	}
	return nil
}

type scanner interface{ Scan(dest ...any) error }

func scanUser(row scanner) (User, error) {
	var u User
	var enabled, created, updated int64
	var expire sql.NullInt64
	err := row.Scan(&u.ID, &u.Name, &u.Note, &enabled, &u.TrafficLimit, &u.TrafficUsed, &u.ResetPeriod,
		&expire, &u.SubToken, &u.UUID, &created, &updated)
	if err != nil {
		return User{}, err
	}
	u.Enabled = enabled != 0
	if expire.Valid {
		t := fromUnix(expire.Int64)
		u.ExpireAt = &t
	}
	u.CreatedAt, u.UpdatedAt = fromUnix(created), fromUnix(updated)
	return u, nil
}

func unixOrNull(t *time.Time) sql.NullInt64 {
	if t == nil {
		return sql.NullInt64{}
	}
	return sql.NullInt64{Int64: t.Unix(), Valid: true}
}
