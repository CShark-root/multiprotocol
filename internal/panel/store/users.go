package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"slices"
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
	LastResetAt  time.Time
	AmneziaKey   string
	SubHidden    []int64 // inbounds left out of the subscription
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
	AmneziaKey   string // vpn://... shared from the Amnezia app; shown only
	SubHidden    []int64
}

const userColumns = `id, name, note, enabled, traffic_limit, traffic_used, reset_period,
	expire_at, sub_token, uuid, last_reset_at, created_at, updated_at, amnezia_key, sub_hidden`

// idList stores a list of ids as JSON ("" when empty).
func idList(ids []int64) string {
	if len(ids) == 0 {
		return ""
	}
	b, _ := json.Marshal(ids)
	return string(b)
}

// InSubscription reports whether an inbound belongs in the user's
// subscription. Direct links work for every inbound regardless.
func (u User) InSubscription(inboundID int64) bool {
	return !slices.Contains(u.SubHidden, inboundID)
}

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

// AddTraffic adds bytes to each user's counter and to today's daily row
// (server local date) in one transaction.
func (s *Store) AddTraffic(ctx context.Context, byUser map[int64]int64, now time.Time) error {
	day := now.Local().Format(time.DateOnly)
	return s.inTx(ctx, func(tx *sql.Tx) error {
		for id, bytes := range byUser {
			res, err := tx.ExecContext(ctx,
				`UPDATE users SET traffic_used = traffic_used + ?, updated_at = ? WHERE id = ?`,
				bytes, now.Unix(), id)
			if err != nil {
				return err
			}
			if n, _ := res.RowsAffected(); n == 0 {
				continue // user deleted meanwhile
			}
			if _, err := tx.ExecContext(ctx, `INSERT INTO traffic_daily (user_id, day, bytes) VALUES (?, ?, ?)
				ON CONFLICT (user_id, day) DO UPDATE SET bytes = bytes + excluded.bytes`, id, day, bytes); err != nil {
				return err
			}
		}
		return nil
	})
}

// ResetTraffic zeroes a user's counter (daily history stays).
func (s *Store) ResetTraffic(ctx context.Context, id int64, now time.Time) error {
	res, err := s.db.ExecContext(ctx, `UPDATE users SET traffic_used = 0, last_reset_at = ?, updated_at = ? WHERE id = ?`,
		now.Unix(), now.Unix(), id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

// SetExpire changes only the expiry of a user.
func (s *Store) SetExpire(ctx context.Context, id int64, expire *time.Time, now time.Time) error {
	res, err := s.db.ExecContext(ctx, `UPDATE users SET expire_at = ?, updated_at = ? WHERE id = ?`,
		unixOrNull(expire), now.Unix(), id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

type DayTraffic struct {
	Day   string `json:"day"` // YYYY-MM-DD
	Bytes int64  `json:"bytes"`
}

// TrafficByDay sums traffic per day from `from` (YYYY-MM-DD) on; userID 0
// means all users.
func (s *Store) TrafficByDay(ctx context.Context, userID int64, from string) ([]DayTraffic, error) {
	q := `SELECT day, SUM(bytes) FROM traffic_daily WHERE day >= ?`
	args := []any{from}
	if userID != 0 {
		q += ` AND user_id = ?`
		args = append(args, userID)
	}
	rows, err := s.db.QueryContext(ctx, q+` GROUP BY day ORDER BY day`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []DayTraffic{}
	for rows.Next() {
		var d DayTraffic
		if err := rows.Scan(&d.Day, &d.Bytes); err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	return out, rows.Err()
}

// PruneTrafficDaily drops daily rows older than `before` (YYYY-MM-DD).
func (s *Store) PruneTrafficDaily(ctx context.Context, before string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM traffic_daily WHERE day < ?`, before)
	return err
}

func (s *Store) CreateUser(ctx context.Context, f UserFields, subToken, uuid string, now time.Time) (User, error) {
	res, err := s.db.ExecContext(ctx, `INSERT INTO users
		(name, note, enabled, traffic_limit, reset_period, expire_at, sub_token, uuid, last_reset_at, created_at, updated_at, amnezia_key, sub_hidden)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		f.Name, f.Note, f.Enabled, f.TrafficLimit, f.ResetPeriod, unixOrNull(f.ExpireAt), subToken, uuid,
		now.Unix(), now.Unix(), now.Unix(), f.AmneziaKey, idList(f.SubHidden))
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
		name = ?, note = ?, enabled = ?, traffic_limit = ?, reset_period = ?, expire_at = ?, amnezia_key = ?, sub_hidden = ?, updated_at = ?
		WHERE id = ?`,
		f.Name, f.Note, f.Enabled, f.TrafficLimit, f.ResetPeriod, unixOrNull(f.ExpireAt), f.AmneziaKey, idList(f.SubHidden), now.Unix(), id)
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

// DeleteUser removes a user. Their daily traffic moves into the server
// total (user_id 0): ids can be reused, and a new user must not inherit it.
func (s *Store) DeleteUser(ctx context.Context, id int64) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	res, err := tx.ExecContext(ctx, `DELETE FROM users WHERE id = ?`, id)
	if err != nil {
		return err
	}
	if n, err := res.RowsAffected(); err != nil {
		return err
	} else if n == 0 {
		return ErrNotFound
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO traffic_daily (user_id, day, bytes)
		SELECT 0, day, bytes FROM traffic_daily WHERE user_id = ? AND true
		ON CONFLICT (user_id, day) DO UPDATE SET bytes = bytes + excluded.bytes`, id); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM traffic_daily WHERE user_id = ?`, id); err != nil {
		return err
	}
	return tx.Commit()
}

type scanner interface{ Scan(dest ...any) error }

func scanUser(row scanner) (User, error) {
	var u User
	var enabled, lastReset, created, updated int64
	var expire sql.NullInt64
	var hidden string
	err := row.Scan(&u.ID, &u.Name, &u.Note, &enabled, &u.TrafficLimit, &u.TrafficUsed, &u.ResetPeriod,
		&expire, &u.SubToken, &u.UUID, &lastReset, &created, &updated, &u.AmneziaKey, &hidden)
	if err != nil {
		return User{}, err
	}
	u.Enabled = enabled != 0
	if hidden != "" {
		json.Unmarshal([]byte(hidden), &u.SubHidden)
	}
	if expire.Valid {
		t := fromUnix(expire.Int64)
		u.ExpireAt = &t
	}
	u.LastResetAt, u.CreatedAt, u.UpdatedAt = fromUnix(lastReset), fromUnix(created), fromUnix(updated)
	return u, nil
}

func unixOrNull(t *time.Time) sql.NullInt64 {
	if t == nil {
		return sql.NullInt64{}
	}
	return sql.NullInt64{Int64: t.Unix(), Valid: true}
}

// AddServerTraffic добавляет трафик, у которого нет пользователя панели
// (клиенты Amnezia без привязки), в общий трафик сервера (user_id 0).
func (s *Store) AddServerTraffic(ctx context.Context, bytes int64, now time.Time) error {
	day := now.Local().Format(time.DateOnly)
	_, err := s.db.ExecContext(ctx, `INSERT INTO traffic_daily (user_id, day, bytes) VALUES (0, ?, ?)
		ON CONFLICT (user_id, day) DO UPDATE SET bytes = bytes + excluded.bytes`, day, bytes)
	return err
}

// UserTraffic — трафик одного пользователя за период (UserID 0 — без
// пользователя: удалённые и клиенты Amnezia без привязки).
type UserTraffic struct {
	UserID int64
	Bytes  int64
}

// TrafficTop — трафик по пользователям с `from` (YYYY-MM-DD), больше всех первым.
func (s *Store) TrafficTop(ctx context.Context, from string) ([]UserTraffic, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT user_id, SUM(bytes) FROM traffic_daily WHERE day >= ?
		GROUP BY user_id ORDER BY 2 DESC`, from)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []UserTraffic
	for rows.Next() {
		var t UserTraffic
		if err := rows.Scan(&t.UserID, &t.Bytes); err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}
