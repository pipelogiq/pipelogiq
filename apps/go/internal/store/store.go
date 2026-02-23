package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jmoiron/sqlx"

	"pipelogiq/internal/types"
)

type Store struct {
	db        *sqlx.DB
	logger    *slog.Logger
	alertSink AlertSink
}

func New(db *sqlx.DB, logger *slog.Logger) *Store {
	return &Store{db: db, logger: logger}
}

type AlertSink interface {
	NotifyStageChange(ctx context.Context, event StageAlertEvent)
	NotifyWorkerEvent(ctx context.Context, event WorkerAlertEvent)
}

type StageAlertEvent struct {
	PipelineID   int
	PipelineName string
	StageID      int
	StageName    string
	OldStatus    string
	NewStatus    string
	Source       string
	TS           time.Time
}

type WorkerAlertEvent struct {
	WorkerID  string
	TS        time.Time
	Level     string
	EventType string
	Message   string
	Details   map[string]any
}

func (s *Store) SetAlertSink(sink AlertSink) {
	s.alertSink = sink
}

// DB returns the underlying sqlx.DB for direct queries.
func (s *Store) DB() *sqlx.DB {
	return s.db
}

func (s *Store) emitStageAlert(event StageAlertEvent) {
	if s.alertSink == nil {
		return
	}
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		s.alertSink.NotifyStageChange(ctx, event)
	}()
}

func (s *Store) emitWorkerAlert(event WorkerAlertEvent) {
	if s.alertSink == nil {
		return
	}
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		s.alertSink.NotifyWorkerEvent(ctx, event)
	}()
}

func cloneAlertDetailsMap(input map[string]any) map[string]any {
	if len(input) == 0 {
		return nil
	}
	cloned := make(map[string]any, len(input))
	for key, value := range input {
		cloned[key] = value
	}
	return cloned
}

// ValidateAPIKey returns application id for a valid API key.
func (s *Store) ValidateAPIKey(ctx context.Context, key string) (int, error) {
	if strings.TrimSpace(key) == "" {
		return 0, errors.New("api key required")
	}
	var appID int
	err := s.db.QueryRowContext(ctx, `
		SELECT application_id
		FROM api_key
		WHERE key=$1
		  AND disabled_at IS NULL
		  AND (expires_at IS NULL OR expires_at > NOW())
		LIMIT 1
	`, key).Scan(&appID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return 0, errors.New("api key not found or disabled")
		}
		return 0, err
	}

	_, _ = s.db.ExecContext(ctx, `UPDATE api_key SET last_used=NOW() WHERE key=$1`, key)
	return appID, nil
}

// CreatePipeline inserts pipeline, stages, keywords and context items in a single transaction.
func (s *Store) CreatePipeline(ctx context.Context, req types.PipelineCreateRequest, appID int) (*types.PipelineResponse, error) {
	tx, err := s.db.BeginTxx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer func() {
		if err != nil {
			_ = tx.Rollback()
		}
	}()

	traceID := req.TraceID
	if traceID == "" {
		traceID = uuid.NewString()
	}

	var pipelineID int
	var createdAt time.Time
	err = tx.QueryRowContext(ctx, `
		INSERT INTO pipeline (application_id, name, status, created_at, is_completed, trace_id)
		VALUES ($1, $2, $3, NOW(), false, $4)
		RETURNING id, created_at
	`, appID, req.Name, types.PipelineStatusNotStarted, traceID).Scan(&pipelineID, &createdAt)
	if err != nil {
		return nil, fmt.Errorf("insert pipeline: %w", err)
	}

	if err = s.insertKeywords(ctx, tx, pipelineID, req.PipelineKeywords); err != nil {
		return nil, err
	}
	if err = s.insertContextItems(ctx, tx, pipelineID, req.PipelineContext); err != nil {
		return nil, err
	}
	if err = s.insertStages(ctx, tx, pipelineID, req.Stages); err != nil {
		return nil, err
	}

	if err = tx.Commit(); err != nil {
		return nil, err
	}

	return s.GetPipelineWithStages(ctx, pipelineID)
}

func (s *Store) insertKeywords(ctx context.Context, tx *sqlx.Tx, pipelineID int, keywords []types.PipelineKeyword) error {
	for _, kw := range keywords {
		var keywordID int
		err := tx.QueryRowContext(ctx, `
			SELECT id FROM keyword WHERE key=$1 AND value=$2 LIMIT 1
		`, kw.Key, kw.Value).Scan(&keywordID)

		if errors.Is(err, sql.ErrNoRows) {
			err = tx.QueryRowContext(ctx, `
				INSERT INTO keyword (key, value) VALUES ($1, $2) RETURNING id
			`, kw.Key, kw.Value).Scan(&keywordID)
		}
		if err != nil {
			return fmt.Errorf("keyword %s:%s: %w", kw.Key, kw.Value, err)
		}

		if _, err = tx.ExecContext(ctx, `
			INSERT INTO pipeline_keyword (pipeline_id, keyword_id)
			VALUES ($1, $2)
		`, pipelineID, keywordID); err != nil {
			return fmt.Errorf("link keyword: %w", err)
		}
	}
	return nil
}

func (s *Store) insertContextItems(ctx context.Context, tx *sqlx.Tx, pipelineID int, contextItems []types.ContextItem) error {
	for _, item := range contextItems {
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO pipeline_context_item (key, value, value_type, pipeline_id)
			VALUES ($1, $2, $3, $4)
		`, item.Key, item.Value, valueTypeOrDefault(item.ValueType), pipelineID); err != nil {
			return fmt.Errorf("insert context item %s: %w", item.Key, err)
		}
	}
	return nil
}

func (s *Store) insertStages(ctx context.Context, tx *sqlx.Tx, pipelineID int, stages []types.StageCreate) error {
	for _, st := range stages {
		spanID := uuid.NewString()
		var stageID int
		var created time.Time
		err := tx.QueryRowContext(ctx, `
			INSERT INTO stage (name, stage_handler_name, description, status, pipeline_id, created_at, is_event, span_id)
			VALUES ($1,$2,$3,$4,$5,NOW(),$6,$7)
			RETURNING id, created_at
		`, st.Name, st.StageHandler, st.Description, types.StageStatusNotStarted, pipelineID, st.IsEvent, spanID).Scan(&stageID, &created)
		if err != nil {
			return fmt.Errorf("insert stage %s: %w", st.Name, err)
		}

		if _, err = tx.ExecContext(ctx, `
			INSERT INTO stage_io (input, stage_id) VALUES ($1, $2)
		`, nullableString(st.Input), stageID); err != nil {
			return fmt.Errorf("insert stage io: %w", err)
		}

		if err = s.insertStageOptions(ctx, tx, stageID, st.Options); err != nil {
			return err
		}
	}
	return nil
}

func (s *Store) insertStageOptions(ctx context.Context, tx *sqlx.Tx, stageID int, opt *types.StageOptions) error {
	if opt == nil {
		return nil
	}

	if allNilStageOptions(opt) {
		return nil
	}

	_, err := tx.ExecContext(ctx, `
		INSERT INTO stage_options
			(run_next_if_failed, retry_interval, time_out, max_retries, depends_on, run_in_parallel_with, fail_if_output_empty, notify_on_failure, run_as_user, stage_id)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)
	`, opt.RunNextIfFailed, opt.RetryInterval, opt.TimeOut, opt.MaxRetries,
		joinList(opt.DependsOn), joinList(opt.RunInParallelWith),
		opt.FailIfOutputEmpty, opt.NotifyOnFailure, opt.RunAsUser, stageID)
	return err
}

func allNilStageOptions(opt *types.StageOptions) bool {
	return opt.RunNextIfFailed == nil &&
		opt.RetryInterval == nil &&
		opt.TimeOut == nil &&
		opt.MaxRetries == nil &&
		len(opt.DependsOn) == 0 &&
		len(opt.RunInParallelWith) == 0 &&
		opt.FailIfOutputEmpty == nil &&
		opt.NotifyOnFailure == nil &&
		opt.RunAsUser == nil
}

func joinList(list []string) *string {
	if len(list) == 0 {
		return nil
	}
	joined := strings.Join(list, ",")
	return &joined
}

func nullableString(val string) *string {
	if val == "" {
		return nil
	}
	return &val
}

// GetPipeline returns pipeline with status and stage statuses.
func (s *Store) GetPipeline(ctx context.Context, pipelineID int) (*types.PipelineResponse, error) {
	var row struct {
		ID            int        `db:"id"`
		Name          string     `db:"name"`
		TraceID       string     `db:"trace_id"`
		Status        *string    `db:"status"`
		CreatedAt     time.Time  `db:"created_at"`
		FinishedAt    *time.Time `db:"finished_at"`
		IsCompleted   bool       `db:"is_completed"`
		ApplicationID *int       `db:"application_id"`
	}

	if err := s.db.GetContext(ctx, &row, `
		SELECT id, name, COALESCE(trace_id, '') AS trace_id, status, created_at, finished_at, is_completed, application_id
		FROM pipeline WHERE id=$1
	`, pipelineID); err != nil {
		return nil, err
	}

	if row.FinishedAt == nil {
		var lastFinished *time.Time
		_ = s.db.GetContext(ctx, &lastFinished, `SELECT MAX(finished_at) FROM stage WHERE pipeline_id=$1`, pipelineID)
		if lastFinished != nil {
			row.FinishedAt = lastFinished
		}
	}

	states := []string{}
	if err := s.db.SelectContext(ctx, &states, `SELECT status FROM stage WHERE pipeline_id=$1 ORDER BY id`, pipelineID); err != nil {
		return nil, err
	}

	status := computePipelineStatus(states)
	isEvent := s.getPipelineIsEvent(ctx, pipelineID)

	return &types.PipelineResponse{
		ID:            row.ID,
		Name:          row.Name,
		TraceID:       row.TraceID,
		Status:        status,
		CreatedAt:     row.CreatedAt,
		FinishedAt:    row.FinishedAt,
		ApplicationID: row.ApplicationID,
		StageStatuses: states,
		IsEvent:       isEvent,
	}, nil
}

// GetPipelineWithStages returns pipeline including stages and context items.
func (s *Store) GetPipelineWithStages(ctx context.Context, pipelineID int) (*types.PipelineResponse, error) {
	pipeline, err := s.GetPipeline(ctx, pipelineID)
	if err != nil {
		return nil, err
	}
	stages, err := s.GetPipelineStages(ctx, pipelineID)
	if err != nil {
		s.logger.Error("get pipeline stages failed", "pipelineId", pipelineID, "err", err)
	} else {
		pipeline.Stages = stages
	}
	ctxItems, err := s.GetPipelineContext(ctx, pipelineID)
	if err != nil {
		s.logger.Error("get pipeline context failed", "pipelineId", pipelineID, "err", err)
	} else {
		pipeline.PipelineContext = ctxItems
	}
	return pipeline, nil
}

// GetPipelineKeywords returns keywords associated with a pipeline.
func (s *Store) GetPipelineKeywords(ctx context.Context, pipelineID int) ([]types.PipelineKeyword, error) {
	keywords := []types.PipelineKeyword{}
	err := s.db.SelectContext(ctx, &keywords, `
		SELECT k.key, k.value
		FROM keyword k
		JOIN pipeline_keyword pk ON pk.keyword_id = k.id
		WHERE pk.pipeline_id = $1
		ORDER BY k.id
	`, pipelineID)
	return keywords, err
}

// GetPipelineFullDetail returns pipeline with stages (including logs), context, and keywords.
func (s *Store) GetPipelineFullDetail(ctx context.Context, pipelineID int) (*types.PipelineResponse, error) {
	pipeline, err := s.GetPipelineWithStages(ctx, pipelineID)
	if err != nil {
		return nil, err
	}

	// Load logs for each stage
	for i := range pipeline.Stages {
		stageID := pipeline.Stages[i].ID
		logs, err := s.GetStageLogs(ctx, pipelineID, &stageID)
		if err != nil {
			s.logger.Error("get stage logs failed", "pipelineId", pipelineID, "stageId", stageID, "err", err)
		} else {
			pipeline.Stages[i].Logs = logs
		}
	}

	// Load keywords
	keywords, err := s.GetPipelineKeywords(ctx, pipelineID)
	if err != nil {
		s.logger.Error("get pipeline keywords failed", "pipelineId", pipelineID, "err", err)
	} else {
		pipeline.PipelineKeywords = keywords
	}

	return pipeline, nil
}

func (s *Store) getPipelineIsEvent(ctx context.Context, pipelineID int) *bool {
	var isEvent *bool
	_ = s.db.GetContext(ctx, &isEvent, `SELECT is_event FROM stage WHERE pipeline_id=$1 ORDER BY id LIMIT 1`, pipelineID)
	return isEvent
}

func computePipelineStatus(stageStatuses []string) string {
	hasFailed := false
	hasRunning := false
	allFinished := len(stageStatuses) > 0
	allNotStarted := len(stageStatuses) > 0

	for _, st := range stageStatuses {
		switch st {
		case types.StageStatusFailed:
			hasFailed = true
			allNotStarted = false
		case types.StageStatusRunning, types.StageStatusPending, types.StageStatusRetryScheduled:
			hasRunning = true
			allNotStarted = false
			allFinished = false
		case types.StageStatusCompleted, types.StageStatusSkipped:
			allNotStarted = false
		case types.StageStatusNotStarted:
			allFinished = false
		default:
			allFinished = false
			allNotStarted = false
		}
	}

	switch {
	case hasFailed && !hasRunning:
		return types.PipelineStatusFailed
	case allFinished && !hasFailed:
		return types.PipelineStatusCompleted
	case allNotStarted:
		return types.PipelineStatusNotStarted
	case hasRunning || hasFailed:
		return types.PipelineStatusRunning
	default:
		return types.PipelineStatusNotStarted
	}
}

func (s *Store) GetPipelineStages(ctx context.Context, pipelineID int) ([]types.StageResponse, error) {
	rows := []types.StageResponse{}
	if err := s.db.SelectContext(ctx, &rows, `
		SELECT
			s.id AS id,
			s.pipeline_id AS pipeline_id,
			COALESCE(s.span_id, '') AS span_id,
			COALESCE(s.name, '') AS name,
			COALESCE(s.stage_handler_name, '') AS stage_handler_name,
			COALESCE(s.description, '') AS description,
			COALESCE(s.status, '') AS status,
			s.created_at AS created_at,
			s.finished_at AS finished_at,
			s.started_at AS started_at,
			s.is_skipped AS is_skipped,
			s.is_event AS is_event,
			io.input AS input,
			io.output AS output
		FROM stage s
		LEFT JOIN stage_io io ON io.stage_id = s.id
		WHERE s.pipeline_id=$1
		ORDER BY s.id
	`, pipelineID); err != nil {
		return nil, err
	}

	for i := range rows {
		if i < len(rows)-1 {
			next := rows[i+1].ID
			rows[i].NextStageID = &next
		}
	}

	return rows, nil
}

func (s *Store) GetPipelineContext(ctx context.Context, pipelineID int) ([]types.ContextItem, error) {
	items := []types.ContextItem{}
	if err := s.db.SelectContext(ctx, &items, `
		SELECT key, value, COALESCE(value_type, '') AS value_type FROM pipeline_context_item WHERE pipeline_id=$1 ORDER BY id
	`, pipelineID); err != nil {
		return nil, err
	}
	return items, nil
}

// GetStageToExecute picks the next stage atomically and marks it Pending.
func (s *Store) GetStageToExecute(ctx context.Context) (*types.StageNextMessage, error) {
	tx, err := s.db.BeginTxx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return nil, err
	}
	defer func() {
		if err != nil {
			_ = tx.Rollback()
		}
	}()

	var stageID int
	err = tx.QueryRowContext(ctx, `
		WITH candidate AS (
			SELECT s.id
			FROM stage s
			JOIN pipeline p ON p.id = s.pipeline_id
			WHERE p.is_completed = false
			  AND (
				s.status = $1
				OR (s.status = $3 AND s.next_retry_at IS NOT NULL AND s.next_retry_at <= NOW())
			  )
			  AND COALESCE(s.is_skipped,false) = false
			  AND COALESCE(s.is_event,false) = false
			  AND NOT EXISTS (
				SELECT 1 FROM stage sp WHERE sp.pipeline_id = p.id AND sp.status = $2
			  )
			  AND NOT EXISTS (
				SELECT 1 FROM stage sb
				WHERE sb.pipeline_id = p.id
				  AND sb.id < s.id
				  AND COALESCE(sb.is_event,false) = false
				  AND sb.status NOT IN ($4, $5)
			  )
			ORDER BY p.id, s.id
			LIMIT 1
		)
		SELECT id FROM candidate
	`, types.StageStatusNotStarted, types.StageStatusPending, types.StageStatusRetryScheduled,
		types.StageStatusCompleted, types.StageStatusSkipped).Scan(&stageID)

	if errors.Is(err, sql.ErrNoRows) {
		_ = tx.Commit()
		return nil, nil
	}
	if err != nil {
		return nil, err
	}

	var row struct {
		StageID          int            `db:"id"`
		PipelineID       int            `db:"pipeline_id"`
		StageStatus      string         `db:"stage_status"`
		StageHandlerName sql.NullString `db:"stage_handler_name"`
		Input            sql.NullString `db:"input"`
		ApplicationID    sql.NullInt64  `db:"application_id"`
		TraceID          sql.NullString `db:"trace_id"`
		SpanID           sql.NullString `db:"span_id"`
	}

	err = tx.GetContext(ctx, &row, `
		SELECT s.id, s.pipeline_id, s.status AS stage_status, s.stage_handler_name, io.input, p.application_id,
			p.trace_id, s.span_id
		FROM stage s
		JOIN pipeline p ON p.id = s.pipeline_id
		LEFT JOIN stage_io io ON io.stage_id = s.id
		WHERE s.id = $1
		FOR UPDATE OF s
	`, stageID)
	if err != nil {
		return nil, err
	}

	if _, err = tx.ExecContext(ctx, `
		UPDATE pipeline SET status=$1 WHERE id=$2
	`, types.PipelineStatusRunning, row.PipelineID); err != nil {
		return nil, err
	}
	if _, err = tx.ExecContext(ctx, `
		UPDATE stage SET status=$1, started_at=NOW(), finished_at=NULL, next_retry_at=NULL WHERE id=$2
	`, types.StageStatusPending, row.StageID); err != nil {
		return nil, err
	}

	ctxItems, err := s.getContextItemsTx(ctx, tx, row.PipelineID)
	if err != nil {
		return nil, err
	}

	if err = tx.Commit(); err != nil {
		return nil, err
	}

	s.LogStageChange(ctx, row.PipelineID, row.StageID, row.StageStatus, types.StageStatusPending, "publisher")

	appID := int(row.ApplicationID.Int64)
	msg := &types.StageNextMessage{
		AppID:            appID,
		StageID:          row.StageID,
		PipelineID:       &row.PipelineID,
		TraceID:          row.TraceID.String,
		SpanID:           row.SpanID.String,
		StageHandlerName: row.StageHandlerName.String,
		Input:            row.Input.String,
		ContextItems:     ctxItems,
	}
	return msg, nil
}

func (s *Store) getContextItemsTx(ctx context.Context, tx *sqlx.Tx, pipelineID int) ([]types.ContextItem, error) {
	items := []types.ContextItem{}
	if err := tx.SelectContext(ctx, &items, `
		SELECT key, value, value_type FROM pipeline_context_item WHERE pipeline_id=$1
	`, pipelineID); err != nil {
		return nil, err
	}
	return items, nil
}

func (s *Store) MarkPendingTooLong(ctx context.Context, olderThan time.Duration) (int64, error) {
	rows, err := s.db.QueryxContext(ctx, `
		SELECT s.id, s.pipeline_id, EXTRACT(EPOCH FROM (NOW() - COALESCE(s.started_at, s.created_at))) AS age_seconds
		FROM stage s
		JOIN pipeline p ON p.id = s.pipeline_id
		WHERE p.is_completed = false
		  AND s.status = $1
		  AND (NOW() - COALESCE(s.started_at, s.created_at)) >= $2::interval
	`, types.StageStatusPending, olderThan.String())
	if err != nil {
		return 0, err
	}
	defer rows.Close()

	var count int64
	for rows.Next() {
		var stageID, pipelineID int
		var ageSeconds float64
		if err := rows.Scan(&stageID, &pipelineID, &ageSeconds); err != nil {
			return count, err
		}
		msg := fmt.Sprintf("Stage has been pending for too long - %.0f seconds", ageSeconds)
		tx, errTx := s.db.BeginTxx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
		if errTx != nil {
			return count, errTx
		}
		_, errTx = tx.ExecContext(ctx, `
				UPDATE stage SET status=$1, finished_at=NOW(), next_retry_at=NULL WHERE id=$2
			`, types.StageStatusFailed, stageID)
		if errTx == nil {
			_, errTx = tx.ExecContext(ctx, `UPDATE pipeline SET is_completed=true, status=$2 WHERE id=$1`, pipelineID, types.PipelineStatusFailed)
		}
		if errTx == nil {
			_, errTx = tx.ExecContext(ctx, `
				UPDATE stage_io SET output=$1 WHERE stage_id=$2
			`, msg, stageID)
		}
		if errTx != nil {
			_ = tx.Rollback()
			return count, errTx
		}
		if errTx = tx.Commit(); errTx != nil {
			return count, errTx
		}
		s.LogStageChange(ctx, pipelineID, stageID, types.StageStatusPending, types.StageStatusFailed, "pending_watcher")
		count++
	}

	return count, nil
}

// UpdateStageResult persists stage result and returns updated pipeline snapshot.
func (s *Store) UpdateStageResult(ctx context.Context, msg types.StageResultMessage) (*types.PipelineResponse, error) {
	tx, err := s.db.BeginTxx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return nil, err
	}
	defer func() {
		if err != nil {
			_ = tx.Rollback()
		}
	}()

	var stage struct {
		ID            int            `db:"id"`
		PipelineID    int            `db:"pipeline_id"`
		Status        string         `db:"status"`
		StagePayload  sql.NullString `db:"input"`
		ExistingOut   sql.NullString `db:"output"`
		RetryAttempt  int            `db:"retry_attempt"`
		RetryInterval sql.NullInt64  `db:"retry_interval"`
		MaxRetries    sql.NullInt64  `db:"max_retries"`
	}

	err = tx.GetContext(ctx, &stage, `
		SELECT
			s.id,
			s.pipeline_id,
			s.status,
			io.input,
			io.output,
			COALESCE(s.retry_attempt, 0) AS retry_attempt,
			so.retry_interval,
			so.max_retries
		FROM stage s
		LEFT JOIN stage_io io ON io.stage_id = s.id
		LEFT JOIN stage_options so ON so.stage_id = s.id
		WHERE s.id = $1
		ORDER BY so.id DESC NULLS LAST
		LIMIT 1
		FOR UPDATE OF s
	`, msg.StageID)
	if err != nil {
		return nil, err
	}

	// idempotency: process only active stage executions.
	if stage.Status != types.StageStatusPending && stage.Status != types.StageStatusRunning {
		err = tx.Commit()
		if err != nil {
			return nil, err
		}
		return s.GetPipeline(ctx, stage.PipelineID)
	}

	newStatus := types.StageStatusFailed
	if msg.IsSuccess {
		newStatus = types.StageStatusCompleted
	} else {
		maxRetries := 0
		if stage.MaxRetries.Valid {
			maxRetries = int(stage.MaxRetries.Int64)
		}
		retryIntervalSeconds := 0
		if stage.RetryInterval.Valid {
			retryIntervalSeconds = int(stage.RetryInterval.Int64)
		}

		if maxRetries > 0 && retryIntervalSeconds > 0 && stage.RetryAttempt < maxRetries {
			newStatus = types.StageStatusRetryScheduled
		}
	}

	if newStatus == types.StageStatusRetryScheduled {
		retryAfter := int(stage.RetryInterval.Int64)
		nextRetryAt := time.Now().UTC().Add(time.Duration(retryAfter) * time.Second)
		if _, err = tx.ExecContext(ctx, `
			UPDATE stage
			SET status=$1, finished_at=NOW(), retry_attempt=retry_attempt + 1, next_retry_at=$2
			WHERE id=$3
		`, newStatus, nextRetryAt, msg.StageID); err != nil {
			return nil, err
		}
	} else {
		if _, err = tx.ExecContext(ctx, `
			UPDATE stage SET status=$1, finished_at=NOW(), next_retry_at=NULL WHERE id=$2
		`, newStatus, msg.StageID); err != nil {
			return nil, err
		}
	}

	if _, err = tx.ExecContext(ctx, `
		UPDATE stage_io SET output=$1 WHERE stage_id=$2
	`, msg.Result, msg.StageID); err != nil {
		return nil, err
	}

	for _, log := range msg.Logs {
		if _, err = tx.ExecContext(ctx, `
			INSERT INTO stage_log (log, log_level, created_at, stage_id)
			VALUES ($1,$2,$3,$4)
		`, log.Message, log.LogLevel, log.Created, msg.StageID); err != nil {
			return nil, err
		}
	}

	for _, item := range msg.ContextItems {
		valueType := valueTypeOrDefault(item.ValueType)
		res, errExec := tx.ExecContext(ctx, `
			UPDATE pipeline_context_item SET value=$1, value_type=$2
			WHERE pipeline_id=$3 AND key=$4
		`, item.Value, valueType, stage.PipelineID, item.Key)
		if errExec != nil {
			return nil, errExec
		}
		affected, _ := res.RowsAffected()
		if affected == 0 {
			if _, errExec = tx.ExecContext(ctx, `
				INSERT INTO pipeline_context_item (key, value, value_type, pipeline_id)
				VALUES ($1,$2,$3,$4)
			`, item.Key, item.Value, valueType, stage.PipelineID); errExec != nil {
				return nil, errExec
			}
		}
	}

	if newStatus == types.StageStatusRetryScheduled {
		if _, err = tx.ExecContext(ctx, `
			UPDATE pipeline SET is_completed=false, finished_at=NULL, status=$2 WHERE id=$1
		`, stage.PipelineID, types.PipelineStatusRunning); err != nil {
			return nil, err
		}
	} else {
		// Mark pipeline completed when failed or when this is last stage.
		var lastStageID int
		if err = tx.GetContext(ctx, &lastStageID, `SELECT MAX(id) FROM stage WHERE pipeline_id=$1`, stage.PipelineID); err != nil {
			return nil, err
		}

		completePipeline := !msg.IsSuccess || msg.StageID == lastStageID
		if completePipeline {
			pStatus := types.PipelineStatusCompleted
			if !msg.IsSuccess {
				pStatus = types.PipelineStatusFailed
			}
			if _, err = tx.ExecContext(ctx, `
				UPDATE pipeline SET is_completed=true, finished_at=NOW(), status=$2 WHERE id=$1
			`, stage.PipelineID, pStatus); err != nil {
				return nil, err
			}
		}
	}

	if err = tx.Commit(); err != nil {
		return nil, err
	}

	s.LogStageChange(ctx, stage.PipelineID, msg.StageID, stage.Status, newStatus, "result_consumer")

	return s.GetPipelineWithStages(ctx, stage.PipelineID)
}

func valueTypeOrDefault(vt string) string {
	if vt == "" {
		return "string"
	}
	return vt
}

// UpdateStageStatus updates status and returns pipeline snapshot.
func (s *Store) UpdateStageStatus(ctx context.Context, msg types.SetStageStatusMessage) (*types.PipelineResponse, error) {
	tx, err := s.db.BeginTxx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return nil, err
	}
	defer func() {
		if err != nil {
			_ = tx.Rollback()
		}
	}()

	var oldStatus string
	var pipelineID int
	err = tx.QueryRowContext(ctx, `
		SELECT status, pipeline_id FROM stage WHERE id = $1 FOR UPDATE
	`, msg.StageID).Scan(&oldStatus, &pipelineID)
	if err != nil {
		return nil, err
	}

	if _, err = tx.ExecContext(ctx, `
		UPDATE stage SET status=$1 WHERE id=$2
	`, msg.Status, msg.StageID); err != nil {
		return nil, err
	}

	if err = tx.Commit(); err != nil {
		return nil, err
	}

	if oldStatus != msg.Status {
		s.LogStageChange(ctx, pipelineID, msg.StageID, oldStatus, msg.Status, "status_consumer")
	}

	return s.GetPipelineWithStages(ctx, pipelineID)
}
