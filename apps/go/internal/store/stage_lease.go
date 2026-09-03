package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jmoiron/sqlx"

	"pipelogiq/internal/types"
)

const DefaultStageLeaseDuration = time.Minute

// AcquireStageLease fences a dispatched execution to one active worker.
// A lease prevents concurrent execution while it is valid; it does not make an
// external side effect exactly-once after the lease expires.
func (s *Store) AcquireStageLease(
	ctx context.Context,
	stageID int,
	req types.StageLeaseRequest,
	sessionToken string,
	leaseDuration time.Duration,
) (*types.StageLeaseResponse, error) {
	if stageID <= 0 ||
		strings.TrimSpace(req.ExecutionID) == "" ||
		strings.TrimSpace(req.WorkerID) == "" ||
		strings.TrimSpace(sessionToken) == "" {
		return nil, errWorkerSessionInvalid
	}
	if leaseDuration <= 0 {
		leaseDuration = DefaultStageLeaseDuration
	}

	tx, err := s.db.BeginTxx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()

	workerAppID, err := validateWorkerSessionForLease(ctx, tx, req.WorkerID, sessionToken)
	if err != nil {
		return nil, err
	}

	var row struct {
		PipelineID     int            `db:"pipeline_id"`
		ApplicationID  sql.NullInt64  `db:"application_id"`
		PipelineStatus sql.NullString `db:"pipeline_status"`
		PipelineDone   bool           `db:"pipeline_done"`
		Status         string         `db:"status"`
		ExecutionID    sql.NullString `db:"execution_id"`
		Attempt        int            `db:"execution_attempt"`
		LeaseOwner     sql.NullString `db:"lease_owner"`
		LeaseExpiresAt sql.NullTime   `db:"lease_expires_at"`
	}
	if err = tx.GetContext(ctx, &row, fmt.Sprintf(`
		SELECT
			s.pipeline_id,
			p.application_id,
			p.status AS pipeline_status,
			p.is_completed AS pipeline_done,
			s.status,
			s.execution_id,
			COALESCE(s.execution_attempt, 0) AS execution_attempt,
			s.lease_owner,
			s.lease_expires_at
		FROM stage s
		JOIN pipeline p ON p.id = s.pipeline_id
		WHERE s.id = $1%s
	`, s.forUpdateOfClause("s")), stageID); err != nil {
		return nil, err
	}

	response := &types.StageLeaseResponse{Attempt: row.Attempt}
	if !row.ApplicationID.Valid || int(row.ApplicationID.Int64) != workerAppID {
		response.Reason = "application_mismatch"
		if err = tx.Commit(); err != nil {
			return nil, err
		}
		return response, nil
	}
	if isPipelineTerminalStatus(row.PipelineStatus.String, row.PipelineDone) {
		response.Reason = "pipeline_terminal"
		if err = tx.Commit(); err != nil {
			return nil, err
		}
		return response, nil
	}
	if !row.ExecutionID.Valid || row.ExecutionID.String != strings.TrimSpace(req.ExecutionID) {
		response.Reason = "stale_execution"
		if err = tx.Commit(); err != nil {
			return nil, err
		}
		return response, nil
	}
	if row.Status != types.StageStatusPending && row.Status != types.StageStatusRunning {
		response.Reason = "stage_not_active"
		if err = tx.Commit(); err != nil {
			return nil, err
		}
		return response, nil
	}

	now := time.Now().UTC()
	if row.Status == types.StageStatusRunning {
		response.Reason = "lease_expired"
		if row.LeaseExpiresAt.Valid {
			expiry := row.LeaseExpiresAt.Time.UTC()
			response.LeaseExpiresAt = &expiry
			if expiry.After(now) {
				response.Reason = "lease_held"
			}
		}
		if err = tx.Commit(); err != nil {
			return nil, err
		}
		return response, nil
	}

	expiresAt := now.Add(leaseDuration)
	result, err := tx.ExecContext(ctx, `
		UPDATE stage
		SET
			status = $1,
			started_at = $2,
			lease_owner = $3,
			lease_expires_at = $4
		WHERE id = $5
		  AND execution_id = $6
		  AND status IN ($7, $8)
	`, types.StageStatusRunning, now, req.WorkerID, expiresAt, stageID,
		req.ExecutionID, types.StageStatusPending, types.StageStatusRunning)
	if err != nil {
		return nil, err
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return nil, err
	}
	if affected != 1 {
		response.Reason = "lease_not_acquired"
		if err = tx.Commit(); err != nil {
			return nil, err
		}
		return response, nil
	}

	if _, err = tx.ExecContext(ctx, `
		UPDATE pipeline
		SET status = $1
		WHERE id = $2 AND is_completed = false AND status <> $3
	`, types.PipelineStatusRunning, row.PipelineID, types.PipelineStatusPaused); err != nil {
		return nil, err
	}
	if err = tx.Commit(); err != nil {
		return nil, err
	}

	response.Acquired = true
	response.LeaseExpiresAt = &expiresAt
	return response, nil
}

// RenewStageLease extends only the current worker's execution lease.
func (s *Store) RenewStageLease(
	ctx context.Context,
	stageID int,
	req types.StageLeaseRequest,
	sessionToken string,
	leaseDuration time.Duration,
) (*types.StageLeaseResponse, error) {
	if leaseDuration <= 0 {
		leaseDuration = DefaultStageLeaseDuration
	}
	if stageID <= 0 ||
		strings.TrimSpace(req.ExecutionID) == "" ||
		strings.TrimSpace(req.WorkerID) == "" ||
		strings.TrimSpace(sessionToken) == "" {
		return nil, errWorkerSessionInvalid
	}

	tx, err := s.db.BeginTxx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()

	workerAppID, err := validateWorkerSessionForLease(ctx, tx, req.WorkerID, sessionToken)
	if err != nil {
		return nil, err
	}

	var row struct {
		Attempt       int            `db:"execution_attempt"`
		ApplicationID sql.NullInt64  `db:"application_id"`
		PipelineState sql.NullString `db:"pipeline_status"`
		PipelineDone  bool           `db:"pipeline_done"`
	}
	if err = tx.GetContext(ctx, &row, fmt.Sprintf(`
		SELECT
			COALESCE(s.execution_attempt, 0) AS execution_attempt,
			p.application_id,
			p.status AS pipeline_status,
			p.is_completed AS pipeline_done
		FROM stage s
		JOIN pipeline p ON p.id = s.pipeline_id
		WHERE s.id = $1%s
	`, s.forUpdateOfClause("s")), stageID); err != nil {
		return nil, err
	}

	response := &types.StageLeaseResponse{Attempt: row.Attempt}
	if !row.ApplicationID.Valid || int(row.ApplicationID.Int64) != workerAppID {
		response.Reason = "application_mismatch"
		_ = tx.Commit()
		return response, nil
	}
	if isPipelineTerminalStatus(row.PipelineState.String, row.PipelineDone) {
		response.Reason = "pipeline_terminal"
		_ = tx.Commit()
		return response, nil
	}

	expiresAt := time.Now().UTC().Add(leaseDuration)
	result, err := tx.ExecContext(ctx, `
		UPDATE stage
		SET lease_expires_at = $1
		WHERE id = $2
		  AND status = $3
		  AND execution_id = $4
		  AND lease_owner = $5
		  AND lease_expires_at > CURRENT_TIMESTAMP
	`, expiresAt, stageID, types.StageStatusRunning, req.ExecutionID, req.WorkerID)
	if err != nil {
		return nil, err
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return nil, err
	}
	if affected != 1 {
		response.Reason = "lease_not_owned"
		if err = tx.Commit(); err != nil {
			return nil, err
		}
		return response, nil
	}
	if err = tx.Commit(); err != nil {
		return nil, err
	}

	response.Acquired = true
	response.LeaseExpiresAt = &expiresAt
	return response, nil
}

func validateWorkerSessionForLease(
	ctx context.Context,
	tx *sqlx.Tx,
	workerID string,
	sessionToken string,
) (int, error) {
	var row struct {
		ApplicationID int       `db:"application_id"`
		ExpiresAt     time.Time `db:"session_expires_at"`
	}
	if err := tx.GetContext(ctx, &row, `
		SELECT application_id, session_expires_at
		FROM worker_client
		WHERE id = $1 AND session_token = $2
		LIMIT 1
	`, workerID, sessionToken); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return 0, errWorkerSessionInvalid
		}
		return 0, err
	}
	if row.ExpiresAt.Before(time.Now().UTC()) {
		return 0, errWorkerSessionInvalid
	}
	return row.ApplicationID, nil
}

// MarkStageDispatched records that RabbitMQ confirmed the current execution.
func (s *Store) MarkStageDispatched(ctx context.Context, stageID int, executionID string) (bool, error) {
	result, err := s.db.ExecContext(ctx, `
		UPDATE stage
		SET dispatched_at = CURRENT_TIMESTAMP
		WHERE id = $1
		  AND status IN ($2, $3)
		  AND execution_id = $4
	`, stageID, types.StageStatusPending, types.StageStatusRunning, executionID)
	if err != nil {
		return false, err
	}
	affected, err := result.RowsAffected()
	return affected == 1, err
}

// RecoverUndispatchedStages closes the DB-commit-before-broker-publish gap. A
// stage may still be delivered twice when publish was confirmed but recording
// dispatched_at failed; execution fencing makes the older delivery stale.
func (s *Store) RecoverUndispatchedStages(ctx context.Context, olderThan time.Duration) (int64, error) {
	if olderThan <= 0 {
		olderThan = 30 * time.Second
	}
	cutoff := time.Now().UTC().Add(-olderThan)
	result, err := s.db.ExecContext(ctx, `
		UPDATE stage
		SET
			status = $1,
			started_at = NULL,
			finished_at = NULL,
			execution_id = NULL,
			dispatched_at = NULL,
			lease_owner = NULL,
			lease_expires_at = NULL
		WHERE status = $2
		  AND execution_id IS NOT NULL
		  AND dispatched_at IS NULL
		  AND started_at < $3
	`, types.StageStatusNotStarted, types.StageStatusPending, cutoff)
	if err != nil {
		return 0, err
	}
	return result.RowsAffected()
}

// RecoverExpiredStageLeases makes a crashed worker's execution schedulable
// again. The next execution receives a new token and incremented attempt.
func (s *Store) RecoverExpiredStageLeases(ctx context.Context) (int64, error) {
	result, err := s.db.ExecContext(ctx, `
		UPDATE stage
		SET
			status = $1,
			started_at = NULL,
			finished_at = NULL,
			execution_id = NULL,
			dispatched_at = NULL,
			lease_owner = NULL,
			lease_expires_at = NULL
		WHERE status = $2
		  AND lease_expires_at IS NOT NULL
		  AND lease_expires_at <= CURRENT_TIMESTAMP
	`, types.StageStatusNotStarted, types.StageStatusRunning)
	if err != nil {
		return 0, err
	}
	return result.RowsAffected()
}
