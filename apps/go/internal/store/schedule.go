package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/jmoiron/sqlx"

	"pipelogiq/internal/types"
)

var (
	// ErrScheduleNotFound reports a schedule that does not exist for the application.
	ErrScheduleNotFound = errors.New("schedule not found")
	// ErrScheduleArchived reports an operation that is not valid on an archived schedule.
	ErrScheduleArchived = errors.New("schedule is archived")
	// ErrScheduleRunAlreadyRecorded reports a tick another instance already fired.
	ErrScheduleRunAlreadyRecorded = errors.New("schedule run already recorded")
)

// IsScheduleNotFoundError reports whether err is ErrScheduleNotFound.
func IsScheduleNotFoundError(err error) bool { return errors.Is(err, ErrScheduleNotFound) }

// ScheduleRow is the persisted shape of one schedule.
type ScheduleRow struct {
	ID                int64          `db:"id"`
	ApplicationID     int            `db:"application_id"`
	Name              string         `db:"name"`
	Status            string         `db:"status"`
	Kind              string         `db:"kind"`
	CronExpression    sql.NullString `db:"cron_expression"`
	TimeZone          string         `db:"time_zone"`
	IntervalSeconds   sql.NullInt64  `db:"interval_seconds"`
	RunAt             sql.NullTime   `db:"run_at"`
	DefinitionJSON    string         `db:"definition_json"`
	DefinitionVersion int            `db:"definition_version"`
	OverlapPolicy     string         `db:"overlap_policy"`
	CatchupPolicy     string         `db:"catchup_policy"`
	CatchupMax        int            `db:"catchup_max"`
	JitterSeconds     int            `db:"jitter_seconds"`
	NextRunAt         sql.NullTime   `db:"next_run_at"`
	LastRunAt         sql.NullTime   `db:"last_run_at"`
	LastRunStatus     sql.NullString `db:"last_run_status"`
	CreatedBy         sql.NullString `db:"created_by"`
	CreatedAt         time.Time      `db:"created_at"`
	UpdatedAt         time.Time      `db:"updated_at"`
}

const scheduleColumns = `
	id, application_id, name, status, kind, cron_expression, time_zone,
	interval_seconds, run_at, definition_json, definition_version,
	overlap_policy, catchup_policy, catchup_max, jitter_seconds,
	next_run_at, last_run_at, last_run_status, created_by, created_at, updated_at`

// Response converts a row into the API projection.
func (r ScheduleRow) Response() types.ScheduleResponse {
	out := types.ScheduleResponse{
		ID:                r.ID,
		ApplicationID:     r.ApplicationID,
		Name:              r.Name,
		Status:            r.Status,
		Kind:              r.Kind,
		CronExpression:    r.CronExpression.String,
		TimeZone:          r.TimeZone,
		Definition:        json.RawMessage(r.DefinitionJSON),
		DefinitionVersion: r.DefinitionVersion,
		OverlapPolicy:     r.OverlapPolicy,
		CatchupPolicy:     r.CatchupPolicy,
		CatchupMax:        r.CatchupMax,
		JitterSeconds:     r.JitterSeconds,
		LastRunStatus:     r.LastRunStatus.String,
		CreatedBy:         r.CreatedBy.String,
		CreatedAt:         r.CreatedAt.UTC(),
		UpdatedAt:         r.UpdatedAt.UTC(),
	}
	if r.IntervalSeconds.Valid {
		seconds := int(r.IntervalSeconds.Int64)
		out.IntervalSeconds = &seconds
	}
	if r.RunAt.Valid {
		runAt := r.RunAt.Time.UTC()
		out.RunAt = &runAt
	}
	if r.NextRunAt.Valid {
		next := r.NextRunAt.Time.UTC()
		out.NextRunAt = &next
	}
	if r.LastRunAt.Valid {
		last := r.LastRunAt.Time.UTC()
		out.LastRunAt = &last
	}
	return out
}

// ScheduleUpsert is the persisted intent of PUT /schedules/{name}. Cron, interval
// and runAt are already validated by the caller.
type ScheduleUpsert struct {
	Name            string
	Kind            string
	CronExpression  string
	TimeZone        string
	IntervalSeconds *int
	RunAt           *time.Time
	OverlapPolicy   string
	CatchupPolicy   string
	CatchupMax      int
	JitterSeconds   int
	DefinitionJSON  string
	NextRunAt       *time.Time
	CreatedBy       string
}

// definitionFingerprint is what decides whether an upsert bumps the version.
// It covers everything a run would behave differently on; created_by and the
// computed next_run_at are deliberately excluded.
func (u ScheduleUpsert) definitionFingerprint() string {
	interval := ""
	if u.IntervalSeconds != nil {
		interval = strconv.Itoa(*u.IntervalSeconds)
	}
	runAt := ""
	if u.RunAt != nil {
		runAt = u.RunAt.UTC().Format(time.RFC3339Nano)
	}
	return strings.Join([]string{
		u.Kind, u.CronExpression, u.TimeZone, interval, runAt,
		u.OverlapPolicy, u.CatchupPolicy,
		strconv.Itoa(u.CatchupMax), strconv.Itoa(u.JitterSeconds),
		canonicalJSON(u.DefinitionJSON),
	}, "\x1f")
}

func (r ScheduleRow) definitionFingerprint() string {
	interval := ""
	if r.IntervalSeconds.Valid {
		interval = strconv.FormatInt(r.IntervalSeconds.Int64, 10)
	}
	runAt := ""
	if r.RunAt.Valid {
		runAt = r.RunAt.Time.UTC().Format(time.RFC3339Nano)
	}
	return strings.Join([]string{
		r.Kind, r.CronExpression.String, r.TimeZone, interval, runAt,
		r.OverlapPolicy, r.CatchupPolicy,
		strconv.Itoa(r.CatchupMax), strconv.Itoa(r.JitterSeconds),
		canonicalJSON(r.DefinitionJSON),
	}, "\x1f")
}

// UpsertSchedule creates the schedule or updates it in place. The definition
// version is bumped only when something that changes behaviour changed, so a
// client that re-applies the same definition on every deploy does not churn it.
// A resurrected archived schedule comes back Active.
func (s *Store) UpsertSchedule(
	ctx context.Context,
	appID int,
	upsert ScheduleUpsert,
) (*types.ScheduleResponse, bool, error) {
	tx, err := s.db.BeginTxx(ctx, nil)
	if err != nil {
		return nil, false, err
	}
	defer func() { _ = tx.Rollback() }()

	existing, err := loadScheduleForUpdate(ctx, tx, s.forUpdateClause(), appID, upsert.Name)
	if err != nil && !errors.Is(err, ErrScheduleNotFound) {
		return nil, false, err
	}

	now := time.Now().UTC()
	created := errors.Is(err, ErrScheduleNotFound)

	var id int64
	if created {
		if err = tx.QueryRowContext(ctx, `
			INSERT INTO schedule (
				application_id, name, status, kind, cron_expression, time_zone,
				interval_seconds, run_at, definition_json, definition_version,
				overlap_policy, catchup_policy, catchup_max, jitter_seconds,
				next_run_at, created_by, created_at, updated_at
			)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, 1, $10, $11, $12, $13, $14, $15, $16, $16)
			RETURNING id
		`,
			appID, upsert.Name, types.ScheduleStatusActive, upsert.Kind,
			nullString(upsert.CronExpression), upsert.TimeZone,
			nullInt(upsert.IntervalSeconds), nullTime(upsert.RunAt),
			upsert.DefinitionJSON,
			upsert.OverlapPolicy, upsert.CatchupPolicy, upsert.CatchupMax, upsert.JitterSeconds,
			nullTime(upsert.NextRunAt), nullString(upsert.CreatedBy), now,
		).Scan(&id); err != nil {
			return nil, false, fmt.Errorf("create schedule: %w", err)
		}
	} else {
		id = existing.ID
		version := existing.DefinitionVersion
		if existing.definitionFingerprint() != upsert.definitionFingerprint() {
			version++
		}

		// An unchanged definition must not move next_run_at: re-applying the same
		// schedule on every deploy would otherwise keep pushing the next tick out.
		nextRunAt := existing.NextRunAt
		status := existing.Status
		if version != existing.DefinitionVersion || existing.Status == types.ScheduleStatusArchived {
			nextRunAt = toNullTime(upsert.NextRunAt)
		}
		if existing.Status == types.ScheduleStatusArchived {
			status = types.ScheduleStatusActive
		}

		if _, err = tx.ExecContext(ctx, `
			UPDATE schedule SET
				status = $2, kind = $3, cron_expression = $4, time_zone = $5,
				interval_seconds = $6, run_at = $7, definition_json = $8,
				definition_version = $9, overlap_policy = $10, catchup_policy = $11,
				catchup_max = $12, jitter_seconds = $13, next_run_at = $14, updated_at = $15
			WHERE id = $1
		`,
			id, status, upsert.Kind, nullString(upsert.CronExpression), upsert.TimeZone,
			nullInt(upsert.IntervalSeconds), nullTime(upsert.RunAt), upsert.DefinitionJSON,
			version, upsert.OverlapPolicy, upsert.CatchupPolicy, upsert.CatchupMax,
			upsert.JitterSeconds, nextRunAt, now,
		); err != nil {
			return nil, false, fmt.Errorf("update schedule: %w", err)
		}
	}

	if err = tx.Commit(); err != nil {
		return nil, false, err
	}

	stored, err := s.GetScheduleByID(ctx, id)
	if err != nil {
		return nil, false, err
	}
	return stored, created, nil
}

// GetSchedule loads one schedule by application-scoped name.
func (s *Store) GetSchedule(ctx context.Context, appID int, name string) (*types.ScheduleResponse, error) {
	var row ScheduleRow
	err := s.db.GetContext(ctx, &row, `
		SELECT `+scheduleColumns+`
		FROM schedule
		WHERE application_id = $1 AND lower(name) = lower($2)
	`, appID, name)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrScheduleNotFound
		}
		return nil, fmt.Errorf("load schedule: %w", err)
	}
	response := row.Response()
	return &response, nil
}

// GetScheduleByID loads one schedule without an application filter. Callers must
// authorize on ApplicationID themselves.
func (s *Store) GetScheduleByID(ctx context.Context, id int64) (*types.ScheduleResponse, error) {
	var row ScheduleRow
	err := s.db.GetContext(ctx, &row, `
		SELECT `+scheduleColumns+`
		FROM schedule
		WHERE id = $1
	`, id)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrScheduleNotFound
		}
		return nil, fmt.Errorf("load schedule: %w", err)
	}
	response := row.Response()
	return &response, nil
}

// ListSchedules pages an application's schedules by descending id.
func (s *Store) ListSchedules(
	ctx context.Context,
	appID int,
	status string,
	cursor string,
	limit int,
) (*types.ScheduleListResponse, error) {
	if limit <= 0 || limit > 200 {
		limit = 50
	}

	query := `
		SELECT ` + scheduleColumns + `
		FROM schedule
		WHERE application_id = $1`
	args := []any{appID}

	if status != "" {
		args = append(args, status)
		query += fmt.Sprintf(" AND status = $%d", len(args))
	}
	if cursor != "" {
		after, err := strconv.ParseInt(cursor, 10, 64)
		if err != nil {
			return nil, fmt.Errorf("invalid cursor: %w", err)
		}
		args = append(args, after)
		query += fmt.Sprintf(" AND id < $%d", len(args))
	}
	args = append(args, limit+1)
	query += fmt.Sprintf(" ORDER BY id DESC LIMIT $%d", len(args))

	rows := []ScheduleRow{}
	if err := s.db.SelectContext(ctx, &rows, query, args...); err != nil {
		return nil, fmt.Errorf("list schedules: %w", err)
	}

	out := &types.ScheduleListResponse{Schedules: []types.ScheduleResponse{}}
	if len(rows) > limit {
		out.NextCursor = strconv.FormatInt(rows[limit-1].ID, 10)
		rows = rows[:limit]
	}
	for _, row := range rows {
		out.Schedules = append(out.Schedules, row.Response())
	}
	return out, nil
}

// AttachScheduleHealth fills the health summary from the last 20 runs of each
// schedule, in one query rather than one per row.
func (s *Store) AttachScheduleHealth(ctx context.Context, schedules []types.ScheduleResponse) error {
	if len(schedules) == 0 {
		return nil
	}

	ids := make([]int64, 0, len(schedules))
	for _, schedule := range schedules {
		ids = append(ids, schedule.ID)
	}

	query, args, err := sqlx.In(`
		SELECT schedule_id, outcome, scheduled_for, fired_at
		FROM schedule_run
		WHERE schedule_id IN (?)
		ORDER BY schedule_id, id DESC
	`, ids)
	if err != nil {
		return fmt.Errorf("build schedule health query: %w", err)
	}

	type healthRow struct {
		ScheduleID   int64     `db:"schedule_id"`
		Outcome      string    `db:"outcome"`
		ScheduledFor time.Time `db:"scheduled_for"`
		FiredAt      time.Time `db:"fired_at"`
	}
	rows := []healthRow{}
	if err = s.db.SelectContext(ctx, &rows, s.db.Rebind(query), args...); err != nil {
		return fmt.Errorf("load schedule health: %w", err)
	}

	health := make(map[int64]*types.ScheduleHealth, len(schedules))
	for _, row := range rows {
		entry, ok := health[row.ScheduleID]
		if !ok {
			entry = &types.ScheduleHealth{Outcomes: []string{}}
			health[row.ScheduleID] = entry
		}
		if len(entry.Outcomes) >= 20 {
			continue
		}
		entry.Outcomes = append(entry.Outcomes, row.Outcome)
		entry.RecentRuns++
		switch row.Outcome {
		case types.ScheduleRunOutcomeFailed:
			entry.RecentFailed++
		case types.ScheduleRunOutcomeSkipped:
			entry.RecentSkipped++
		}
		if entry.LagSeconds == nil {
			lag := row.FiredAt.Sub(row.ScheduledFor).Seconds()
			entry.LagSeconds = &lag
		}
	}

	for i := range schedules {
		if entry, ok := health[schedules[i].ID]; ok {
			schedules[i].Health = entry
		} else {
			schedules[i].Health = &types.ScheduleHealth{Outcomes: []string{}}
		}
	}
	return nil
}

// SetScheduleStatus moves a schedule between Active, Paused and Archived.
// Resuming recomputes next_run_at from now, so a schedule paused for a week does
// not wake up owing a week of ticks.
func (s *Store) SetScheduleStatus(
	ctx context.Context,
	appID int,
	name string,
	status string,
	nextRunAt *time.Time,
) (*types.ScheduleResponse, error) {
	// Only a resume re-plans the cursor; pausing and archiving leave it alone so
	// the stored plan survives. This is two statements rather than one with a
	// CASE because PostgreSQL cannot deduce a single type for a parameter used
	// both as a varchar column value and as a comparison operand.
	var (
		result sql.Result
		err    error
	)
	if status == types.ScheduleStatusActive {
		result, err = s.db.ExecContext(ctx, `
			UPDATE schedule
			SET status = $3, next_run_at = $4, updated_at = CURRENT_TIMESTAMP
			WHERE application_id = $1 AND lower(name) = lower($2)
		`, appID, name, status, nullTime(nextRunAt))
	} else {
		result, err = s.db.ExecContext(ctx, `
			UPDATE schedule
			SET status = $3, updated_at = CURRENT_TIMESTAMP
			WHERE application_id = $1 AND lower(name) = lower($2)
		`, appID, name, status)
	}
	if err != nil {
		return nil, fmt.Errorf("update schedule status: %w", err)
	}
	affected, err := result.RowsAffected()
	if err == nil && affected == 0 {
		return nil, ErrScheduleNotFound
	}
	return s.GetSchedule(ctx, appID, name)
}

// ListScheduleRuns pages a schedule's run history by descending id, joining the
// pipeline so the dashboard can show an outcome without a second round trip.
func (s *Store) ListScheduleRuns(
	ctx context.Context,
	scheduleID int64,
	cursor string,
	limit int,
) (*types.ScheduleRunListResponse, error) {
	if limit <= 0 || limit > 200 {
		limit = 50
	}

	query := `
		SELECT
			r.id, r.schedule_id, r.scheduled_for, r.fired_at, r.trigger, r.outcome,
			r.skip_reason, r.pipeline_id, r.definition_version, r.error,
			p.status AS pipeline_status, p.finished_at AS pipeline_finished_at
		FROM schedule_run r
		LEFT JOIN pipeline p ON p.id = r.pipeline_id
		WHERE r.schedule_id = $1`
	args := []any{scheduleID}

	if cursor != "" {
		after, err := strconv.ParseInt(cursor, 10, 64)
		if err != nil {
			return nil, fmt.Errorf("invalid cursor: %w", err)
		}
		args = append(args, after)
		query += fmt.Sprintf(" AND r.id < $%d", len(args))
	}
	args = append(args, limit+1)
	query += fmt.Sprintf(" ORDER BY r.id DESC LIMIT $%d", len(args))

	type runRow struct {
		ID                 int64          `db:"id"`
		ScheduleID         int64          `db:"schedule_id"`
		ScheduledFor       time.Time      `db:"scheduled_for"`
		FiredAt            time.Time      `db:"fired_at"`
		Trigger            string         `db:"trigger"`
		Outcome            string         `db:"outcome"`
		SkipReason         sql.NullString `db:"skip_reason"`
		PipelineID         sql.NullInt64  `db:"pipeline_id"`
		DefinitionVersion  int            `db:"definition_version"`
		Error              sql.NullString `db:"error"`
		PipelineStatus     sql.NullString `db:"pipeline_status"`
		PipelineFinishedAt sql.NullTime   `db:"pipeline_finished_at"`
	}
	rows := []runRow{}
	if err := s.db.SelectContext(ctx, &rows, query, args...); err != nil {
		return nil, fmt.Errorf("list schedule runs: %w", err)
	}

	out := &types.ScheduleRunListResponse{Runs: []types.ScheduleRunResponse{}}
	if len(rows) > limit {
		out.NextCursor = strconv.FormatInt(rows[limit-1].ID, 10)
		rows = rows[:limit]
	}
	for _, row := range rows {
		run := types.ScheduleRunResponse{
			ID:                row.ID,
			ScheduleID:        row.ScheduleID,
			ScheduledFor:      row.ScheduledFor.UTC(),
			FiredAt:           row.FiredAt.UTC(),
			Trigger:           row.Trigger,
			Outcome:           row.Outcome,
			SkipReason:        row.SkipReason.String,
			DefinitionVersion: row.DefinitionVersion,
			Error:             row.Error.String,
			LagSeconds:        row.FiredAt.Sub(row.ScheduledFor).Seconds(),
			PipelineStatus:    row.PipelineStatus.String,
		}
		if row.PipelineID.Valid {
			pipelineID := int(row.PipelineID.Int64)
			run.PipelineID = &pipelineID
		}
		if row.PipelineFinishedAt.Valid {
			finished := row.PipelineFinishedAt.Time.UTC()
			run.PipelineFinished = &finished
		}
		out.Runs = append(out.Runs, run)
	}
	return out, nil
}

func loadScheduleForUpdate(
	ctx context.Context,
	tx *sqlx.Tx,
	forUpdate string,
	appID int,
	name string,
) (ScheduleRow, error) {
	var row ScheduleRow
	err := tx.GetContext(ctx, &row, `
		SELECT `+scheduleColumns+`
		FROM schedule
		WHERE application_id = $1 AND lower(name) = lower($2)`+forUpdate,
		appID, name)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return ScheduleRow{}, ErrScheduleNotFound
		}
		return ScheduleRow{}, fmt.Errorf("load schedule: %w", err)
	}
	return row, nil
}

// canonicalJSON re-renders a JSON document with sorted keys and no incidental
// whitespace.
//
// This is what makes "an unchanged definition does not bump the version" true on
// PostgreSQL: definition_json is jsonb, so the value read back has been
// normalised by the database — keys reordered, whitespace dropped — and no
// longer matches the bytes that were sent. Comparing the raw strings would bump
// definition_version on every re-apply, which a client that applies its
// schedules on every deploy would do forever.
//
// A document that cannot be parsed is returned unchanged; the comparison then
// degrades to a byte comparison rather than failing.
func canonicalJSON(raw string) string {
	if strings.TrimSpace(raw) == "" {
		return raw
	}
	var parsed any
	if err := json.Unmarshal([]byte(raw), &parsed); err != nil {
		return raw
	}
	// encoding/json sorts map keys, so this is stable for any input ordering.
	rendered, err := json.Marshal(parsed)
	if err != nil {
		return raw
	}
	return string(rendered)
}

func nullString(value string) any {
	if strings.TrimSpace(value) == "" {
		return nil
	}
	return value
}

func nullInt(value *int) any {
	if value == nil {
		return nil
	}
	return *value
}

func nullTime(value *time.Time) any {
	if value == nil {
		return nil
	}
	return value.UTC()
}

func toNullTime(value *time.Time) sql.NullTime {
	if value == nil {
		return sql.NullTime{}
	}
	return sql.NullTime{Time: value.UTC(), Valid: true}
}
