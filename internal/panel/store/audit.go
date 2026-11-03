package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"time"
)

type AuditEntry struct {
	AdminID    int64 // 0 = system
	Action     string
	TargetType string
	TargetID   string
	Diff       any // marshalled to JSON
}

func (s *Store) AddAudit(ctx context.Context, e AuditEntry, now time.Time) error {
	diff := []byte("{}")
	if e.Diff != nil {
		b, err := json.Marshal(e.Diff)
		if err != nil {
			return err
		}
		diff = b
	}
	_, err := s.db.ExecContext(ctx, `INSERT INTO audit_log
		(ts, admin_id, action, target_type, target_id, diff_json) VALUES (?, ?, ?, ?, ?, ?)`,
		now.Unix(), sql.NullInt64{Int64: e.AdminID, Valid: e.AdminID != 0},
		e.Action, e.TargetType, e.TargetID, string(diff))
	return err
}
