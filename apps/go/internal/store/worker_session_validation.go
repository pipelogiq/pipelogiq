package store

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"time"
)

// ValidateWorkerSessionForApplication verifies a bootstrap session against the
// authenticated application's identity. Execution fencing remains in the result
// consumer so duplicate or late result submissions can still be acknowledged.
func (s *Store) ValidateWorkerSessionForApplication(ctx context.Context, applicationID int, sessionToken string) error {
	sessionToken = strings.TrimSpace(sessionToken)
	if sessionToken == "" || applicationID <= 0 {
		return errWorkerSessionInvalid
	}
	var expiresAt time.Time
	err := s.db.GetContext(ctx, &expiresAt, `
		SELECT session_expires_at FROM worker_client
		WHERE session_token=$1 AND application_id=$2 LIMIT 1
	`, sessionToken, applicationID)
	if errors.Is(err, sql.ErrNoRows) {
		return errWorkerSessionInvalid
	}
	if err != nil {
		return err
	}
	if !expiresAt.After(time.Now().UTC()) {
		return errWorkerSessionInvalid
	}
	return nil
}
