package store

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
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

// AuditFilter selects audit rows before counting and pagination. A zero UserID
// selects unauthenticated actors; nil selects every actor.
type AuditFilter struct {
	Action   string
	UserID   *int
	Outcome  string
	Page     int
	PageSize int
}

type AuditPage struct {
	Items      []AuditRecord `json:"items"`
	TotalCount int           `json:"totalCount"`
	Page       int           `json:"page"`
	PageSize   int           `json:"pageSize"`
	TotalPages int           `json:"totalPages"`
}

type AuditUserOption struct {
	ID    int    `json:"id" db:"id"`
	Email string `json:"email" db:"email"`
}

type AuditOptions struct {
	Actions []string          `json:"actions"`
	Users   []AuditUserOption `json:"users"`
}

// RecordAudit appends an entry. It is best-effort by design: an audit failure
// is logged by the caller and never blocks the action it describes.
func (s *Store) RecordAudit(ctx context.Context, entry AuditEntry) error {
	// Route parameters and attempted identities are untrusted. PostgreSQL
	// rejects oversized VARCHAR values, invalid UTF-8 and NUL characters;
	// malformed requests must not be able to discard their own audit records.
	entry.Email = auditText(entry.Email, 255)
	entry.Action = auditText(entry.Action, 100)
	entry.Target = auditText(entry.Target, 255)
	entry.Outcome = auditText(entry.Outcome, 32)
	entry.RemoteAddr = auditText(entry.RemoteAddr, 64)
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

func auditText(value string, limit int) string {
	var result strings.Builder
	count := 0
	for _, character := range value {
		if count == limit {
			break
		}
		if character == 0 {
			character = '\uFFFD'
		}
		result.WriteRune(character)
		count++
	}
	return result.String()
}

// ListAudit returns the most recent entries, newest first.
func (s *Store) ListAudit(ctx context.Context, limit int) ([]AuditRecord, error) {
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	page, err := s.listAuditFiltered(ctx, AuditFilter{Page: 1, PageSize: limit}, 500)
	if err != nil {
		return nil, err
	}
	return page.Items, nil
}

// ListAuditFiltered returns a page of matching audit history, newest first.
func (s *Store) ListAuditFiltered(ctx context.Context, filter AuditFilter) (*AuditPage, error) {
	return s.listAuditFiltered(ctx, filter, 100)
}

func (s *Store) listAuditFiltered(ctx context.Context, filter AuditFilter, maxPageSize int) (*AuditPage, error) {
	if filter.Page <= 0 {
		filter.Page = 1
	}
	if filter.PageSize <= 0 {
		filter.PageSize = 100
	}
	if filter.PageSize > maxPageSize {
		filter.PageSize = maxPageSize
	}

	conditions := []string{}
	args := []any{}
	if filter.Action != "" {
		args = append(args, filter.Action)
		conditions = append(conditions, fmt.Sprintf("action = $%d", len(args)))
	}
	if filter.UserID != nil {
		if *filter.UserID == 0 {
			conditions = append(conditions, "user_id IS NULL")
		} else {
			args = append(args, *filter.UserID)
			conditions = append(conditions, fmt.Sprintf("user_id = $%d", len(args)))
		}
	}
	if filter.Outcome != "" {
		args = append(args, filter.Outcome)
		conditions = append(conditions, fmt.Sprintf("outcome = $%d", len(args)))
	}
	where := ""
	if len(conditions) > 0 {
		where = " WHERE " + strings.Join(conditions, " AND ")
	}

	page := &AuditPage{Items: []AuditRecord{}, Page: filter.Page, PageSize: filter.PageSize}
	if err := s.db.GetContext(ctx, &page.TotalCount, "SELECT COUNT(*) FROM audit_log"+where, args...); err != nil {
		return nil, err
	}
	if page.TotalCount == 0 {
		return page, nil
	}
	page.TotalPages = (page.TotalCount-1)/page.PageSize + 1
	if page.Page > page.TotalPages {
		return page, nil
	}

	args = append(args, page.PageSize, (page.Page-1)*page.PageSize)
	query := `
		SELECT id, ts, user_id, email, action, target, outcome, remote_addr, details
		FROM audit_log` + where + fmt.Sprintf(`
		ORDER BY ts DESC, id DESC
		LIMIT $%d OFFSET $%d
	`, len(args)-1, len(args))
	if err := s.db.SelectContext(ctx, &page.Items, query, args...); err != nil {
		return nil, err
	}
	return page, nil
}

// AuditFilterOptions reads the whole history so filtering remains possible for
// older actions and deleted users that are absent from the current page.
func (s *Store) AuditFilterOptions(ctx context.Context) (*AuditOptions, error) {
	options := &AuditOptions{Actions: []string{}, Users: []AuditUserOption{}}
	if err := s.db.SelectContext(ctx, &options.Actions, `
		SELECT DISTINCT action FROM audit_log ORDER BY action
	`); err != nil {
		return nil, err
	}
	if err := s.db.SelectContext(ctx, &options.Users, `
		SELECT actors.user_id AS id,
			COALESCE(NULLIF(u.email, ''), (
				SELECT history.email FROM audit_log history
				WHERE history.user_id = actors.user_id
					AND history.email IS NOT NULL AND history.email <> ''
				ORDER BY history.ts DESC, history.id DESC
				LIMIT 1
			), '') AS email
		FROM (SELECT DISTINCT user_id FROM audit_log WHERE user_id IS NOT NULL) actors
		LEFT JOIN "user" u ON u.id = actors.user_id
		ORDER BY email, actors.user_id
	`); err != nil {
		return nil, err
	}
	return options, nil
}

func nullIfEmpty(value string) *string {
	if value == "" {
		return nil
	}
	return &value
}
