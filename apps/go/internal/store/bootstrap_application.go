package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"pipelogiq/internal/constants"
)

// ApplicationBootstrap describes the application and API key provisioned from
// environment so a fresh host can accept pipelines without dashboard clicks.
type ApplicationBootstrap struct {
	Name string
	// AppID, when set, asserts the queue prefix the application must end up
	// with. The prefix is derived from the numeric application id, so this is a
	// guard against a consumer's Compose file drifting from the server, not a
	// value that can be chosen.
	AppID string
	// APIKey is read once from the environment and stored the same way
	// GenerateApiKey stores a generated key.
	APIKey string
}

// ErrApplicationQueueIDMismatch reports that the bootstrapped application does not
// carry the queue prefix the operator asserted.
var ErrApplicationQueueIDMismatch = errors.New("bootstrap application queue id mismatch")

// BootstrapApplicationResult reports what a bootstrap run resolved.
type BootstrapApplicationResult struct {
	ApplicationID  int
	AppID          string
	CreatedApp     bool
	CreatedAPIKey  bool
	AlreadyExisted bool
}

// BootstrapApplication creates the configured application and its API key when the
// application does not exist yet. It is a no-op for an application that already
// exists, so rotating or revoking a key in the dashboard is never undone by a
// restart. Run it before BootstrapAdmin: a freshly created application has no
// members, which is exactly what the admin bootstrap adopts.
func (s *Store) BootstrapApplication(
	ctx context.Context,
	cfg ApplicationBootstrap,
) (*BootstrapApplicationResult, error) {
	name := strings.TrimSpace(cfg.Name)
	if name == "" {
		return nil, nil
	}

	apiKey := strings.TrimSpace(cfg.APIKey)
	expectedAppID := strings.TrimSpace(cfg.AppID)

	tx, err := s.db.BeginTxx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()

	result := &BootstrapApplicationResult{}

	err = tx.QueryRowContext(ctx, `
		SELECT id FROM application WHERE lower(name) = lower($1) ORDER BY id LIMIT 1
	`, name).Scan(&result.ApplicationID)
	switch {
	case err == nil:
		result.AlreadyExisted = true
	case errors.Is(err, sql.ErrNoRows):
		if err = tx.QueryRowContext(ctx, `
			INSERT INTO application (name, description)
			VALUES ($1, $2)
			RETURNING id
		`, name, "Created by BOOTSTRAP_APPLICATION_NAME").Scan(&result.ApplicationID); err != nil {
			return nil, fmt.Errorf("create bootstrap application: %w", err)
		}
		result.CreatedApp = true
	default:
		return nil, fmt.Errorf("lookup bootstrap application: %w", err)
	}

	result.AppID = constants.ApplicationQueueID(result.ApplicationID)
	if expectedAppID != "" && expectedAppID != result.AppID {
		return nil, fmt.Errorf(
			"%w: application %q resolves to %q, but BOOTSTRAP_APPLICATION_APP_ID is %q",
			ErrApplicationQueueIDMismatch, name, result.AppID, expectedAppID,
		)
	}

	if apiKey != "" && result.CreatedApp {
		if _, err = tx.ExecContext(ctx, `
			INSERT INTO api_key (application_id, name, key, created_at)
			VALUES ($1, $2, $3, $4)
		`, result.ApplicationID, "bootstrap", apiKey, time.Now().UTC()); err != nil {
			return nil, fmt.Errorf("create bootstrap api key: %w", err)
		}
		result.CreatedAPIKey = true
	}

	if err = tx.Commit(); err != nil {
		return nil, err
	}
	return result, nil
}
