package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"pipelogiq/internal/types"
)

var ErrPipelineNotCancellable = errors.New("pipeline is not cancellable")

// CancelPipelineForApplication atomically moves an application-owned pipeline
// and every unfinished stage to a terminal cancelled state.
//
// Cancellation is cooperative for a handler that is already executing. The
// persisted execution token is fenced immediately, so a late result cannot
// advance the cancelled pipeline, but Pipelogiq cannot undo an external side
// effect that the handler has already started.
func (s *Store) CancelPipelineForApplication(
	ctx context.Context,
	pipelineID int,
	applicationID int,
) (*types.PipelineResponse, error) {
	tx, err := s.db.BeginTxx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return nil, err
	}
	// Rollback is harmless after Commit and guarantees that every early return
	// releases any stage/pipeline row locks.
	defer func() { _ = tx.Rollback() }()

	var pipeline struct {
		ApplicationID sql.NullInt64  `db:"application_id"`
		Status        sql.NullString `db:"status"`
		IsCompleted   bool           `db:"is_completed"`
	}
	if err = tx.GetContext(ctx, &pipeline, `
		SELECT application_id, status, is_completed
		FROM pipeline
		WHERE id = $1
	`, pipelineID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrPipelineNotFound
		}
		return nil, err
	}

	if !pipeline.ApplicationID.Valid || int(pipeline.ApplicationID.Int64) != applicationID {
		return nil, ErrPipelineNotFound
	}

	// Result processing locks stage before pipeline. Lock stages in the same
	// order before taking the pipeline row lock to avoid a cancel/result
	// deadlock. Re-read the pipeline after waiting because it may have become
	// terminal while a result transaction held the stage lock.
	var stageIDs []int
	if err = tx.SelectContext(ctx, &stageIDs, fmt.Sprintf(`
		SELECT id
		FROM stage
		WHERE pipeline_id = $1
		ORDER BY id%s
	`, s.forUpdateClause()), pipelineID); err != nil {
		return nil, err
	}
	if err = tx.GetContext(ctx, &pipeline, fmt.Sprintf(`
		SELECT application_id, status, is_completed
		FROM pipeline
		WHERE id = $1%s
	`, s.forUpdateClause()), pipelineID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrPipelineNotFound
		}
		return nil, err
	}
	if !pipeline.ApplicationID.Valid || int(pipeline.ApplicationID.Int64) != applicationID {
		return nil, ErrPipelineNotFound
	}

	if pipeline.Status.Valid && pipeline.Status.String == types.PipelineStatusCancelled {
		if err = tx.Commit(); err != nil {
			return nil, err
		}
		return s.GetPipelineWithStages(ctx, pipelineID)
	}
	if pipeline.IsCompleted || types.IsTerminalPipelineStatus(pipeline.Status.String) {
		return nil, ErrPipelineNotCancellable
	}

	if _, err = tx.ExecContext(ctx, `
		UPDATE stage
		SET status = $1,
			finished_at = CURRENT_TIMESTAMP,
			next_retry_at = NULL,
			execution_id = NULL,
			dispatched_at = NULL,
			lease_owner = NULL,
			lease_expires_at = NULL
		WHERE pipeline_id = $2
		  AND status NOT IN ($3, $4, $5, $6)
	`, types.StageStatusCancelled, pipelineID,
		types.StageStatusCompleted,
		types.StageStatusFailed,
		types.StageStatusSkipped,
		types.StageStatusCancelled,
	); err != nil {
		return nil, fmt.Errorf("cancel pipeline stages: %w", err)
	}

	if _, err = tx.ExecContext(ctx, `
		UPDATE pipeline
		SET status = $1,
			is_completed = true,
			finished_at = CURRENT_TIMESTAMP
		WHERE id = $2
	`, types.PipelineStatusCancelled, pipelineID); err != nil {
		return nil, fmt.Errorf("cancel pipeline: %w", err)
	}

	if err = tx.Commit(); err != nil {
		return nil, err
	}

	return s.GetPipelineWithStages(ctx, pipelineID)
}
