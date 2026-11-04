package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"strings"
	"time"
)

type LogEntry struct {
	ID     int64             `json:"id"`
	TS     time.Time         `json:"ts"`
	Level  string            `json:"level"`
	Source string            `json:"source"`
	Msg    string            `json:"msg"`
	Fields map[string]string `json:"fields,omitempty"`
}

type LogFilter struct {
	Source   string // "" = all
	MinLevel string // "" = all; info | warn | error
	Query    string // substring of the message
	BeforeID int64  // page backwards; 0 = newest
	Limit    int
}

var levelRank = map[string]int{"debug": 0, "info": 1, "warn": 2, "error": 3}

// LevelsAtLeast lists the levels at or above min.
func LevelsAtLeast(min string) []string {
	var out []string
	for l, r := range levelRank {
		if r >= levelRank[min] {
			out = append(out, l)
		}
	}
	return out
}

func (s *Store) InsertLogs(ctx context.Context, entries []LogEntry) error {
	return s.inTx(ctx, func(tx *sql.Tx) error {
		stmt, err := tx.PrepareContext(ctx, `INSERT INTO logs (ts, level, source, msg, fields_json) VALUES (?, ?, ?, ?, ?)`)
		if err != nil {
			return err
		}
		defer stmt.Close()
		for _, e := range entries {
			fields := []byte("{}")
			if len(e.Fields) > 0 {
				fields, _ = json.Marshal(e.Fields)
			}
			if _, err := stmt.ExecContext(ctx, e.TS.UnixMilli(), e.Level, e.Source, e.Msg, string(fields)); err != nil {
				return err
			}
		}
		return nil
	})
}

// QueryLogs returns matching entries, newest first.
func (s *Store) QueryLogs(ctx context.Context, f LogFilter) ([]LogEntry, error) {
	where := []string{"1=1"}
	args := []any{}
	if f.Source != "" {
		where = append(where, "source = ?")
		args = append(args, f.Source)
	}
	if f.MinLevel != "" && levelRank[f.MinLevel] > 0 {
		levels := LevelsAtLeast(f.MinLevel)
		where = append(where, "level IN ("+strings.TrimSuffix(strings.Repeat("?,", len(levels)), ",")+")")
		for _, l := range levels {
			args = append(args, l)
		}
	}
	if q := strings.TrimSpace(f.Query); q != "" {
		where = append(where, `msg LIKE ? ESCAPE '\'`)
		args = append(args, "%"+likeEscape(q)+"%")
	}
	if f.BeforeID > 0 {
		where = append(where, "id < ?")
		args = append(args, f.BeforeID)
	}
	limit := f.Limit
	if limit <= 0 || limit > 1000 {
		limit = 200
	}
	args = append(args, limit)
	rows, err := s.db.QueryContext(ctx, `SELECT id, ts, level, source, msg, fields_json FROM logs
		WHERE `+strings.Join(where, " AND ")+` ORDER BY id DESC LIMIT ?`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []LogEntry{}
	for rows.Next() {
		var e LogEntry
		var ts int64
		var fields string
		if err := rows.Scan(&e.ID, &ts, &e.Level, &e.Source, &e.Msg, &fields); err != nil {
			return nil, err
		}
		e.TS = time.UnixMilli(ts).UTC()
		if fields != "{}" {
			json.Unmarshal([]byte(fields), &e.Fields)
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

// PruneLogs keeps at most maxRows entries younger than maxAge.
func (s *Store) PruneLogs(ctx context.Context, maxAge time.Duration, maxRows int, now time.Time) error {
	if _, err := s.db.ExecContext(ctx, `DELETE FROM logs WHERE ts < ?`, now.Add(-maxAge).UnixMilli()); err != nil {
		return err
	}
	_, err := s.db.ExecContext(ctx, `DELETE FROM logs WHERE id <= (SELECT id FROM logs ORDER BY id DESC LIMIT 1 OFFSET ?)`, maxRows)
	return err
}

type Event struct {
	ID       int64          `json:"id"`
	TS       time.Time      `json:"ts"`
	Type     string         `json:"type"`
	Severity string         `json:"severity"`
	UserID   int64          `json:"userId,omitempty"`
	UserName string         `json:"userName,omitempty"`
	Message  string         `json:"message"`
	Data     map[string]any `json:"data,omitempty"`
}

func (s *Store) AddEvent(ctx context.Context, e Event) error {
	data := []byte("{}")
	if len(e.Data) > 0 {
		var err error
		if data, err = json.Marshal(e.Data); err != nil {
			return err
		}
	}
	_, err := s.db.ExecContext(ctx, `INSERT INTO events (ts, type, severity, user_id, message, data_json)
		VALUES (?, ?, ?, ?, ?, ?)`, e.TS.Unix(), e.Type, e.Severity,
		sql.NullInt64{Int64: e.UserID, Valid: e.UserID != 0}, e.Message, string(data))
	return err
}

func (s *Store) ListEvents(ctx context.Context, beforeID int64, limit int) ([]Event, error) {
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	if beforeID <= 0 {
		beforeID = 1 << 62
	}
	rows, err := s.db.QueryContext(ctx, `SELECT e.id, e.ts, e.type, e.severity, COALESCE(e.user_id, 0),
		COALESCE(u.name, ''), e.message, e.data_json
		FROM events e LEFT JOIN users u ON u.id = e.user_id
		WHERE e.id < ? ORDER BY e.id DESC LIMIT ?`, beforeID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Event{}
	for rows.Next() {
		var e Event
		var ts int64
		var data string
		if err := rows.Scan(&e.ID, &ts, &e.Type, &e.Severity, &e.UserID, &e.UserName, &e.Message, &data); err != nil {
			return nil, err
		}
		e.TS = fromUnix(ts)
		if data != "{}" {
			json.Unmarshal([]byte(data), &e.Data)
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

func (s *Store) PruneEvents(ctx context.Context, maxAge time.Duration, now time.Time) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM events WHERE ts < ?`, now.Add(-maxAge).Unix())
	return err
}

type AuditRecord struct {
	ID         int64           `json:"id"`
	TS         time.Time       `json:"ts"`
	Admin      string          `json:"admin"`
	Action     string          `json:"action"`
	TargetType string          `json:"targetType"`
	TargetID   string          `json:"targetId"`
	Diff       json.RawMessage `json:"diff"`
}

func (s *Store) ListAudit(ctx context.Context, beforeID int64, limit int) ([]AuditRecord, error) {
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	if beforeID <= 0 {
		beforeID = 1 << 62
	}
	rows, err := s.db.QueryContext(ctx, `SELECT a.id, a.ts, COALESCE(ad.username, ''), a.action, a.target_type,
		a.target_id, a.diff_json FROM audit_log a LEFT JOIN admins ad ON ad.id = a.admin_id
		WHERE a.id < ? ORDER BY a.id DESC LIMIT ?`, beforeID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []AuditRecord{}
	for rows.Next() {
		var r AuditRecord
		var ts int64
		var diff string
		if err := rows.Scan(&r.ID, &ts, &r.Admin, &r.Action, &r.TargetType, &r.TargetID, &diff); err != nil {
			return nil, err
		}
		r.TS, r.Diff = fromUnix(ts), json.RawMessage(diff)
		out = append(out, r)
	}
	return out, rows.Err()
}

func likeEscape(s string) string {
	r := strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`)
	return r.Replace(s)
}
