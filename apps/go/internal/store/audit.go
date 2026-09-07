package store

import (
	"context"
	"encoding/json"
	"fmt"
	"time"
)

// AuditEntry is one administrative or security-relevant action.
type AuditEntry struct {
	UserID     *int
	Email      string
	Action     string
	Target     string
	Outcome    string
	RemoteAddr string
	Details    map[string]any
}

// AuditRecord is a stored audit row.
type AuditRecord struct {
	ID         int64     `json:"id" db:"id"`
	TS         time.Time `json:"ts" db:"ts"`
	UserID     *int      `json:"userId,omitempty" db:"user_id"`
	Email      *string   `json:"email,omitempty" db:"email"`
	Action     string    `json:"action" db:"action"`
	Target     *string   `json:"target,omitempty" db:"target"`
	Outcome    string    `json:"outcome" db:"outcome"`
	RemoteAddr *string   `json:"remoteAddr,omitempty" db:"remote_addr"`
	Details    *string   `json:"details,omitempty" db:"details"`
}

// RecordAudit appends an entry. It is best-effort by design: an audit failure
// is logged by the caller and never blocks the action it describes.
func (s *Store) RecordAudit(ctx context.Context, entry AuditEntry) error {
	var details *string
	if len(entry.Details) > 0 {
		encoded, err := json.Marshal(entry.Details)
		if err != nil {
			return fmt.Errorf("encode audit details: %w", err)
		}
		text := string(encoded)
		details = &text
	}
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO audit_log (ts, user_id, email, action, target, outcome, remote_addr, details)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
	`, time.Now().UTC(), entry.UserID, nullIfEmpty(entry.Email), entry.Action, nullIfEmpty(entry.Target),
		entry.Outcome, nullIfEmpty(entry.RemoteAddr), details)
	return err
}

// ListAudit returns the most recent entries, newest first.
func (s *Store) ListAudit(ctx context.Context, limit int) ([]AuditRecord, error) {
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	rows := []AuditRecord{}
	if err := s.db.SelectContext(ctx, &rows, `
		SELECT id, ts, user_id, email, action, target, outcome, remote_addr, details
		FROM audit_log
		ORDER BY ts DESC, id DESC
		LIMIT $1
	`, limit); err != nil {
		return nil, err
	}
	return rows, nil
}

func nullIfEmpty(value string) *string {
	if value == "" {
		return nil
	}
	return &value
}
