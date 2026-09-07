package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"pipelogiq/internal/types"
)

// SchedulePlan is what a caller decides for one due schedule while the row is
// locked. Returning an error leaves the row untouched and the transaction intact
// for the remaining schedules.
type SchedulePlan struct {
	// Ticks are the scheduled_for instants to fire, oldest first.
	Ticks []time.Time
	// SkippedTicks are ticks recorded as Skipped/Catchup rather than fired.
	SkippedTicks []time.Time
	// NextRunAt is where the schedule's cursor moves to. Nil parks the schedule.
	NextRunAt *time.Time
	// Status, when set, replaces the schedule's status — Once archives itself.
	Status string
}

// ClaimedSchedule is a due schedule whose cursor has already been advanced.
type ClaimedSchedule struct {
	Schedule ScheduleRow
	Plan     SchedulePlan
}

// ClaimDueSchedules advances every Active schedule whose next_run_at has passed,
// and returns what each one should fire.
//
// The claim and the cursor advance happen in one transaction with
// FOR UPDATE SKIP LOCKED, so a second scheduler instance walks straight past a
// row this one is holding. That is the whole multi-instance guard: no leader
// election, and no window in which two instances see the same due row. The
// unique index on schedule_run(schedule_id, scheduled_for) then catches anything
// that slips through, such as a restart mid-batch.
//
// plan runs inside the transaction and must not perform I/O of its own.
func (s *Store) ClaimDueSchedules(
	ctx context.Context,
	now time.Time,
	limit int,
	plan func(ScheduleRow) (SchedulePlan, error),
) ([]ClaimedSchedule, error) {
	var claimed []ClaimedSchedule
	// SQLite has no SKIP LOCKED: two schedulers on the local-dev database collide
	// on the write lock instead of stepping past each other. Retrying is enough,
	// because the loser sees the advanced cursor and finds nothing due.
	err := s.withSQLiteLockRetry(ctx, func() error {
		var opErr error
		claimed, opErr = s.claimDueSchedulesOnce(ctx, now, limit, plan)
		return opErr
	})
	if err != nil {
		return nil, err
	}
	return claimed, nil
}

func (s *Store) claimDueSchedulesOnce(
	ctx context.Context,
	now time.Time,
	limit int,
	plan func(ScheduleRow) (SchedulePlan, error),
) ([]ClaimedSchedule, error) {
	if limit <= 0 {
		limit = 100
	}

	tx, err := s.db.BeginTxx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()

	skipLocked := ""
	if !s.isSQLiteDriver() {
		skipLocked = " FOR UPDATE SKIP LOCKED"
	}

	rows := []ScheduleRow{}
	if err = tx.SelectContext(ctx, &rows, `
		SELECT `+scheduleColumns+`
		FROM schedule
		WHERE status = $1 AND next_run_at IS NOT NULL AND next_run_at <= $2
		ORDER BY next_run_at
		LIMIT $3`+skipLocked,
		types.ScheduleStatusActive, now.UTC(), limit,
	); err != nil {
		return nil, fmt.Errorf("claim due schedules: %w", err)
	}
	if len(rows) == 0 {
		return nil, tx.Commit()
	}

	claimed := make([]ClaimedSchedule, 0, len(rows))
	for _, row := range rows {
		decision, planErr := plan(row)
		if planErr != nil {
			// A schedule the planner cannot make sense of must not wedge the loop.
			// Park it and let the operator see the failure on its next run row.
			if _, err = tx.ExecContext(ctx, `
				UPDATE schedule
				SET next_run_at = NULL, last_run_status = $2, updated_at = CURRENT_TIMESTAMP
				WHERE id = $1
			`, row.ID, types.ScheduleRunOutcomeFailed); err != nil {
				return nil, fmt.Errorf("park unplannable schedule: %w", err)
			}
			claimed = append(claimed, ClaimedSchedule{
				Schedule: row,
				Plan:     SchedulePlan{SkippedTicks: nil, Ticks: nil},
			})
			s.logger.Error("schedule planning failed, parking the schedule",
				"scheduleId", row.ID, "name", row.Name, "err", planErr)
			continue
		}

		status := row.Status
		if decision.Status != "" {
			status = decision.Status
		}
		if _, err = tx.ExecContext(ctx, `
			UPDATE schedule
			SET next_run_at = $2, status = $3, updated_at = CURRENT_TIMESTAMP
			WHERE id = $1
		`, row.ID, nullTime(decision.NextRunAt), status); err != nil {
			return nil, fmt.Errorf("advance schedule cursor: %w", err)
		}

		claimed = append(claimed, ClaimedSchedule{Schedule: row, Plan: decision})
	}

	if err = tx.Commit(); err != nil {
		return nil, err
	}
	return claimed, nil
}

// ScheduleRunRecord is one row of the run history.
type ScheduleRunRecord struct {
	ScheduleID        int64
	ScheduledFor      time.Time
	Trigger           string
	Outcome           string
	SkipReason        string
	PipelineID        *int
	DefinitionVersion int
	Error             string
}

// RecordScheduleRun inserts one run and updates the schedule's last_run_*.
// A tick another instance already recorded returns ErrScheduleRunAlreadyRecorded:
// the unique index on (schedule_id, scheduled_for) is what makes double firing
// impossible, and hitting it is a normal race outcome, not a failure.
func (s *Store) RecordScheduleRun(ctx context.Context, record ScheduleRunRecord) (int64, error) {
	tx, err := s.db.BeginTxx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer func() { _ = tx.Rollback() }()

	var runID int64
	err = tx.QueryRowContext(ctx, `
		INSERT INTO schedule_run (
			schedule_id, scheduled_for, fired_at, trigger, outcome,
			skip_reason, pipeline_id, definition_version, error
		)
		VALUES ($1, $2, CURRENT_TIMESTAMP, $3, $4, $5, $6, $7, $8)
		RETURNING id
	`,
		record.ScheduleID, record.ScheduledFor.UTC(), record.Trigger, record.Outcome,
		nullString(record.SkipReason), nullIntPtr(record.PipelineID),
		record.DefinitionVersion, nullString(record.Error),
	).Scan(&runID)
	if err != nil {
		if isUniqueViolation(err) {
			return 0, ErrScheduleRunAlreadyRecorded
		}
		return 0, fmt.Errorf("insert schedule run: %w", err)
	}

	if _, err = tx.ExecContext(ctx, `
		UPDATE schedule
		SET last_run_at = CURRENT_TIMESTAMP, last_run_status = $2, updated_at = CURRENT_TIMESTAMP
		WHERE id = $1
	`, record.ScheduleID, record.Outcome); err != nil {
		return 0, fmt.Errorf("update schedule last run: %w", err)
	}

	if err = tx.Commit(); err != nil {
		return 0, err
	}
	return runID, nil
}

// LinkScheduleRunPipeline attaches the created pipeline to an already inserted run.
func (s *Store) LinkScheduleRunPipeline(ctx context.Context, runID int64, pipelineID int) error {
	if _, err := s.db.ExecContext(ctx, `
		UPDATE schedule_run SET pipeline_id = $2 WHERE id = $1
	`, runID, pipelineID); err != nil {
		return fmt.Errorf("link schedule run pipeline: %w", err)
	}
	return nil
}

// FailScheduleRun records the error on a run whose pipeline could not be created.
func (s *Store) FailScheduleRun(ctx context.Context, runID int64, cause string) error {
	if _, err := s.db.ExecContext(ctx, `
		UPDATE schedule_run SET outcome = $2, error = $3 WHERE id = $1
	`, runID, types.ScheduleRunOutcomeFailed, truncateError(cause)); err != nil {
		return fmt.Errorf("mark schedule run failed: %w", err)
	}
	return nil
}

// UnfinishedSchedulePipeline returns the newest unfinished pipeline of a schedule,
// which is what the overlap policies act on.
func (s *Store) UnfinishedSchedulePipeline(ctx context.Context, scheduleID int64) (*int, error) {
	var pipelineID int
	err := s.db.QueryRowxContext(ctx, `
		SELECT id
		FROM pipeline
		WHERE schedule_id = $1 AND is_completed = false
		ORDER BY id DESC
		LIMIT 1
	`, scheduleID).Scan(&pipelineID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, fmt.Errorf("find unfinished schedule pipeline: %w", err)
	}
	return &pipelineID, nil
}

// LastScheduledPipelineID returns the most recent pipeline this schedule created,
// which is what {{previousPipelineId}} resolves to.
func (s *Store) LastScheduledPipelineID(ctx context.Context, scheduleID int64) (*int, error) {
	var pipelineID int
	err := s.db.QueryRowxContext(ctx, `
		SELECT id
		FROM pipeline
		WHERE schedule_id = $1
		ORDER BY id DESC
		LIMIT 1
	`, scheduleID).Scan(&pipelineID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, fmt.Errorf("find last schedule pipeline: %w", err)
	}
	return &pipelineID, nil
}

// AttachPipelineToSchedule records which schedule and run created a pipeline.
func (s *Store) AttachPipelineToSchedule(
	ctx context.Context,
	pipelineID int,
	scheduleID int64,
	runID int64,
) error {
	if _, err := s.db.ExecContext(ctx, `
		UPDATE pipeline SET schedule_id = $2, schedule_run_id = $3 WHERE id = $1
	`, pipelineID, scheduleID, runID); err != nil {
		return fmt.Errorf("attach pipeline to schedule: %w", err)
	}
	return nil
}

func nullIntPtr(value *int) any {
	if value == nil {
		return nil
	}
	return *value
}

// truncateError keeps a run row readable when a driver returns a wall of text.
func truncateError(cause string) any {
	cause = strings.TrimSpace(cause)
	if cause == "" {
		return nil
	}
	const limit = 2000
	if len(cause) > limit {
		cause = cause[:limit] + "…"
	}
	return cause
}

// isUniqueViolation recognises a duplicate-key error from PostgreSQL and SQLite
// without depending on a driver-specific error type.
func isUniqueViolation(err error) bool {
	if err == nil {
		return false
	}
	message := strings.ToLower(err.Error())
	return strings.Contains(message, "duplicate key") ||
		strings.Contains(message, "unique constraint") ||
		strings.Contains(message, "unique violation") ||
		strings.Contains(message, "constraint failed: unique")
}

// ActiveScheduleNames returns the names of every schedule that is currently
// Active, so the scheduler can drop metric series for the rest.
func (s *Store) ActiveScheduleNames(ctx context.Context) (map[string]struct{}, error) {
	names := []string{}
	if err := s.db.SelectContext(ctx, &names, `
		SELECT name FROM schedule WHERE status = $1
	`, types.ScheduleStatusActive); err != nil {
		return nil, fmt.Errorf("list active schedule names: %w", err)
	}
	out := make(map[string]struct{}, len(names))
	for _, name := range names {
		out[name] = struct{}{}
	}
	return out, nil
}

// HasLiveWorker reports whether the application has a worker that could pick up
// a job right now.
//
// Draining and stopped workers do not count: draining means the worker is
// deliberately finishing up and taking nothing new. A stale heartbeat does not
// count either — a process that died without saying so leaves its last state
// behind, and only the clock reveals it.
func (s *Store) HasLiveWorker(ctx context.Context, appID int, offlineAfter time.Duration) (bool, error) {
	if offlineAfter <= 0 {
		offlineAfter = 45 * time.Second
	}
	cutoff := time.Now().UTC().Add(-offlineAfter)

	var live int
	if err := s.db.QueryRowxContext(ctx, `
		SELECT COUNT(*)
		FROM worker_client
		WHERE application_id = $1
		  AND state NOT IN ('stopped', 'error', 'offline', 'draining')
		  AND last_seen_at > $2
	`, appID, cutoff).Scan(&live); err != nil {
		return false, fmt.Errorf("count live workers: %w", err)
	}
	return live > 0, nil
}
