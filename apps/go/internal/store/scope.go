package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
)

// GetUserApplicationIDs returns the applications the user is a member of. Access to
// pipelines, stages, logs and workers is derived from this membership.
func (s *Store) GetUserApplicationIDs(ctx context.Context, userID int) ([]int, error) {
	ids := []int{}
	if err := s.db.SelectContext(ctx, &ids, `
		SELECT application_id
		FROM user_application
		WHERE user_id = $1
		ORDER BY application_id
	`, userID); err != nil {
		return nil, fmt.Errorf("load application membership: %w", err)
	}
	return ids, nil
}

// PipelineApplicationID returns the application owning the pipeline.
func (s *Store) PipelineApplicationID(ctx context.Context, pipelineID int) (int, error) {
	var appID int
	err := s.db.QueryRowxContext(ctx, `
		SELECT application_id FROM pipeline WHERE id = $1
	`, pipelineID).Scan(&appID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return 0, ErrPipelineNotFound
		}
		return 0, fmt.Errorf("resolve pipeline application: %w", err)
	}
	return appID, nil
}

// StageApplicationID returns the application owning the pipeline the stage belongs to.
func (s *Store) StageApplicationID(ctx context.Context, stageID int) (int, error) {
	var appID int
	err := s.db.QueryRowxContext(ctx, `
		SELECT p.application_id
		FROM stage s
		JOIN pipeline p ON p.id = s.pipeline_id
		WHERE s.id = $1
	`, stageID).Scan(&appID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return 0, ErrStageNotFound
		}
		return 0, fmt.Errorf("resolve stage application: %w", err)
	}
	return appID, nil
}

// ErrApiKeyNotFound is returned when an API key id does not resolve to a row.
var ErrApiKeyNotFound = errors.New("api key not found")

// ApiKeyApplicationID returns the application the API key belongs to.
func (s *Store) ApiKeyApplicationID(ctx context.Context, apiKeyID int) (int, error) {
	var appID int
	err := s.db.QueryRowxContext(ctx, `
		SELECT application_id FROM api_key WHERE id = $1
	`, apiKeyID).Scan(&appID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return 0, ErrApiKeyNotFound
		}
		return 0, fmt.Errorf("resolve api key application: %w", err)
	}
	return appID, nil
}
