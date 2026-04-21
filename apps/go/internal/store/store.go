package store

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/jmoiron/sqlx"

	"pipelogiq/internal/types"
)

type Store struct {
	db            *sqlx.DB
	logger        *slog.Logger
	alertSink     AlertSink
	policyRuntime StagePolicyRuntime
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
		  AND (expires_at IS NULL OR expires_at > CURRENT_TIMESTAMP)
		LIMIT 1
	`, key).Scan(&appID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return 0, errors.New("api key not found or disabled")
		}
		return 0, err
	}

	_, _ = s.db.ExecContext(ctx, `UPDATE api_key SET last_used=CURRENT_TIMESTAMP WHERE key=$1`, key)
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

	traceID := resolveTraceID(req.TraceID, req.PipelineContext)

	var pipelineID int
	var createdAt time.Time
	err = tx.QueryRowContext(ctx, `
		INSERT INTO pipeline (application_id, name, status, created_at, is_completed, trace_id)
		VALUES ($1, $2, $3, CURRENT_TIMESTAMP, false, $4)
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
		b := make([]byte, 8)
		_, _ = rand.Read(b)
		spanID := hex.EncodeToString(b)
		var stageID int
		var created time.Time
		err := tx.QueryRowContext(ctx, `
			INSERT INTO stage (name, stage_handler_name, description, status, pipeline_id, created_at, is_event, span_id)
			VALUES ($1,$2,$3,$4,$5,CURRENT_TIMESTAMP,$6,$7)
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

	status := resolvePipelineStatus(nullableStatus(row.Status), row.IsCompleted, states)
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
	if len(stageStatuses) == 0 {
		return types.PipelineStatusNotStarted
	}

	hasFailed := false
	hasRunning := false
	hasPending := false
	hasNotStarted := false
	hasProgress := false
	allFinished := true
	allNotStarted := true

	for _, st := range stageStatuses {
		switch st {
		case types.StageStatusFailed:
			hasFailed = true
			hasProgress = true
			allNotStarted = false
		case types.StageStatusRunning:
			hasRunning = true
			allFinished = false
			allNotStarted = false
		case types.StageStatusPending, types.StageStatusRetryScheduled, types.StageStatusThrottled, types.StageStatusWaitingApproval:
			hasPending = true
			allFinished = false
			allNotStarted = false
		case types.StageStatusCompleted, types.StageStatusSkipped:
			hasProgress = true
			allNotStarted = false
		case types.StageStatusNotStarted:
			hasNotStarted = true
			allFinished = false
		default:
			allFinished = false
			allNotStarted = false
		}
	}

	switch {
	case hasRunning:
		return types.PipelineStatusRunning
	case hasPending:
		return types.PipelineStatusPending
	case allNotStarted:
		return types.PipelineStatusNotStarted
	case hasFailed && !hasNotStarted:
		return types.PipelineStatusFailed
	case allFinished && !hasFailed:
		return types.PipelineStatusCompleted
	case hasFailed || hasNotStarted || hasProgress:
		return types.PipelineStatusPending
	default:
		return types.PipelineStatusNotStarted
	}
}

func nullableStatus(status *string) string {
	if status == nil {
		return ""
	}
	return strings.TrimSpace(*status)
}

func isPausedPipelineStatus(status string) bool {
	switch strings.ToLower(strings.TrimSpace(status)) {
	case strings.ToLower(types.PipelineStatusPaused),
		strings.ToLower(types.PipelineStatusCancelled),
		"canceled":
		return true
	default:
		return false
	}
}

func resolvePipelineStatus(rowStatus string, isCompleted bool, stageStatuses []string) string {
	if isPausedPipelineStatus(rowStatus) && !isCompleted {
		return types.PipelineStatusPaused
	}
	if isCompleted && strings.TrimSpace(rowStatus) != "" {
		return rowStatus
	}
	return computePipelineStatus(stageStatuses)
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
			LEFT JOIN stage_options so_self ON so_self.id = (
				SELECT MAX(so2.id) FROM stage_options so2 WHERE so2.stage_id = s.id
			)
				WHERE p.is_completed = false
				  AND COALESCE(p.status, '') NOT IN ($7, $8)
				  AND (
					s.status = $1
					OR (s.status = $2 AND s.next_retry_at IS NOT NULL AND s.next_retry_at <= CURRENT_TIMESTAMP)
					OR (s.status = $3 AND s.next_retry_at IS NOT NULL AND s.next_retry_at <= CURRENT_TIMESTAMP)
				  )
				  AND COALESCE(s.is_skipped,false) = false
				  AND COALESCE(s.is_event,false) = false
			  AND NOT EXISTS (
				SELECT 1 FROM stage sb
				WHERE sb.pipeline_id = p.id
				  AND COALESCE(sb.is_event,false) = false
				  AND (
					CASE
					  WHEN COALESCE(so_self.depends_on, '') != ''
					  THEN ',' || so_self.depends_on || ',' LIKE '%,' || sb.name || ',%'
					  ELSE sb.id < s.id
					END
					  )
					  AND NOT (
						sb.status IN ($4, $5)
						OR (
							sb.status = $6
							AND COALESCE(so_self.run_next_if_failed, false)
						)
					  )
			  )
			ORDER BY p.id, s.id
			LIMIT 1
		)
			SELECT id FROM candidate
		`, types.StageStatusNotStarted, types.StageStatusRetryScheduled, types.StageStatusThrottled,
		types.StageStatusCompleted, types.StageStatusSkipped, types.StageStatusFailed,
		types.PipelineStatusPaused, types.PipelineStatusCancelled).Scan(&stageID)

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

	err = tx.GetContext(ctx, &row, fmt.Sprintf(`
		SELECT s.id, s.pipeline_id, s.status AS stage_status, s.stage_handler_name, io.input, p.application_id,
			p.trace_id, s.span_id
		FROM stage s
		JOIN pipeline p ON p.id = s.pipeline_id
		LEFT JOIN stage_io io ON io.stage_id = s.id
		WHERE s.id = $1
	%s
	`, s.forUpdateOfClause("s")), stageID)
	if err != nil {
		return nil, err
	}

	// After acquiring the lock, verify the stage is still in a dispatchable state.
	// A concurrent transaction may have already transitioned it to Pending.
	switch row.StageStatus {
	case types.StageStatusNotStarted, types.StageStatusRetryScheduled, types.StageStatusThrottled:
	default:
		s.logger.Warn("stage status changed between candidate selection and lock acquisition",
			"stageId", row.StageID, "status", row.StageStatus)
		_ = tx.Commit()
		return nil, nil
	}

	if s.policyRuntime != nil {
		scope, err := s.loadPolicyRuntimeScopeTx(ctx, tx, row.StageID)
		if err != nil {
			return nil, err
		}
		evaluation, err := s.evaluateStagePoliciesTx(ctx, tx, scope)
		if err != nil {
			return nil, err
		}
		switch evaluation.Action {
		case types.PolicyRuntimeActionThrottle:
			throttleUntil := time.Now().UTC().Add(30 * time.Second)
			if evaluation.ThrottleUntil != nil {
				throttleUntil = evaluation.ThrottleUntil.UTC()
			}
			if _, err = tx.ExecContext(ctx, `
				UPDATE pipeline SET status=$1 WHERE id=$2
			`, types.PipelineStatusPending, row.PipelineID); err != nil {
				return nil, err
			}
			if _, err = tx.ExecContext(ctx, `
				UPDATE stage
				SET status=$1, started_at=NULL, finished_at=NULL, next_retry_at=$2
				WHERE id=$3
			`, types.StageStatusThrottled, throttleUntil, row.StageID); err != nil {
				return nil, err
			}
			if err = tx.Commit(); err != nil {
				return nil, err
			}
			s.LogStageChange(ctx, row.PipelineID, row.StageID, row.StageStatus, types.StageStatusThrottled, "policy_runtime")
			_ = s.policyRuntime.RecordRuntimeEvaluation(ctx, scope, evaluation, "publisher")
			return nil, nil
		case types.PolicyRuntimeActionBlock:
			if _, err = tx.ExecContext(ctx, `
				UPDATE stage
				SET status=$1, finished_at=CURRENT_TIMESTAMP, next_retry_at=NULL
				WHERE id=$2
			`, types.StageStatusFailed, row.StageID); err != nil {
				return nil, err
			}
			if _, err = tx.ExecContext(ctx, `
				UPDATE stage_io SET output=$1 WHERE stage_id=$2
			`, evaluation.Reason, row.StageID); err != nil {
				return nil, err
			}
			if _, err = tx.ExecContext(ctx, `
				UPDATE pipeline SET is_completed=true, finished_at=CURRENT_TIMESTAMP, status=$2 WHERE id=$1
			`, row.PipelineID, types.PipelineStatusFailed); err != nil {
				return nil, err
			}
			if err = tx.Commit(); err != nil {
				return nil, err
			}
			s.LogStageChange(ctx, row.PipelineID, row.StageID, row.StageStatus, types.StageStatusFailed, "policy_runtime")
			_ = s.policyRuntime.RecordRuntimeEvaluation(ctx, scope, evaluation, "publisher")
			return nil, nil
		default:
			_ = s.policyRuntime.RecordRuntimeEvaluation(ctx, scope, evaluation, "publisher")
		}
	}

	if _, err = tx.ExecContext(ctx, `
		UPDATE pipeline SET status=$1 WHERE id=$2
	`, types.PipelineStatusPending, row.PipelineID); err != nil {
		return nil, err
	}
	if _, err = tx.ExecContext(ctx, `
		UPDATE stage SET status=$1, started_at=CURRENT_TIMESTAMP, finished_at=NULL, next_retry_at=NULL WHERE id=$2
	`, types.StageStatusPending, row.StageID); err != nil {
		return nil, err
	}

	ctxItems, err := s.getContextItemsTx(ctx, tx, row.PipelineID)
	if err != nil {
		return nil, err
	}

	if err = s.insertStageLogTx(
		ctx,
		tx,
		row.StageID,
		"INFO",
		fmt.Sprintf(
			"Stage scheduled for execution [pipeline=%d, handler=%s, contextItems=%d, input=%s]",
			row.PipelineID,
			row.StageHandlerName.String,
			len(ctxItems),
			stageLogPreview(row.Input.String, 900),
		),
	); err != nil {
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
	return s.MarkActiveTooLong(ctx, olderThan)
}

func (s *Store) MarkActiveTooLong(ctx context.Context, olderThan time.Duration) (int64, error) {
	rows, err := s.db.QueryxContext(ctx, `
		SELECT s.id
		FROM stage s
		JOIN pipeline p ON p.id = s.pipeline_id
		WHERE p.is_completed = false
		  AND s.status IN ($1, $2)
	`, types.StageStatusPending, types.StageStatusRunning)
	if err != nil {
		return 0, err
	}
	defer rows.Close()

	stageIDs := make([]int, 0, 32)
	for rows.Next() {
		var stageID int
		if err := rows.Scan(&stageID); err != nil {
			return 0, err
		}
		stageIDs = append(stageIDs, stageID)
	}
	if err := rows.Err(); err != nil {
		return 0, err
	}

	var count int64
	for _, stageID := range stageIDs {
		timedOut, err := s.failActiveStageIfTimedOut(ctx, stageID, olderThan)
		if err != nil {
			return count, err
		}
		if timedOut {
			count++
		}
	}

	return count, nil
}

func (s *Store) failActiveStageIfTimedOut(ctx context.Context, stageID int, fallbackTimeout time.Duration) (bool, error) {
	if fallbackTimeout <= 0 {
		fallbackTimeout = 5 * time.Minute
	}

	tx, err := s.db.BeginTxx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return false, err
	}
	defer func() {
		if err != nil {
			_ = tx.Rollback()
		}
	}()

	var stage struct {
		PipelineID       int           `db:"pipeline_id"`
		Status           string        `db:"status"`
		CreatedAt        time.Time     `db:"created_at"`
		StartedAt        sql.NullTime  `db:"started_at"`
		TimeoutSeconds   sql.NullInt64 `db:"time_out"`
		PipelineComplete bool          `db:"is_completed"`
	}

	lockQuery := fmt.Sprintf(`
		SELECT
			s.pipeline_id,
			s.status,
			s.created_at,
			s.started_at,
			so.time_out,
			p.is_completed
		FROM stage s
		JOIN pipeline p ON p.id = s.pipeline_id
		LEFT JOIN stage_options so ON so.stage_id = s.id
		WHERE s.id = $1
		ORDER BY so.id DESC NULLS LAST
		LIMIT 1%s
	`, s.forUpdateOfClause("s"))
	if err = tx.GetContext(ctx, &stage, lockQuery, stageID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			err = tx.Commit()
			return false, err
		}
		return false, err
	}

	if stage.PipelineComplete || !isWatchedActiveStageStatus(stage.Status) {
		if err = tx.Commit(); err != nil {
			return false, err
		}
		return false, nil
	}

	startedAt := stage.CreatedAt.UTC()
	if stage.StartedAt.Valid {
		startedAt = stage.StartedAt.Time.UTC()
	}

	effectiveTimeout := fallbackTimeout
	if stage.TimeoutSeconds.Valid && stage.TimeoutSeconds.Int64 > 0 {
		effectiveTimeout = time.Duration(stage.TimeoutSeconds.Int64) * time.Second
	}
	if s.policyRuntime != nil {
		scope, scopeErr := s.loadPolicyRuntimeScopeTx(ctx, tx, stageID)
		if scopeErr != nil {
			return false, scopeErr
		}
		evaluation, evalErr := s.evaluateStagePoliciesTx(ctx, tx, scope)
		if evalErr != nil {
			return false, evalErr
		}
		if evaluation.EffectiveTimeoutMs != nil && *evaluation.EffectiveTimeoutMs > 0 {
			timeoutFromPolicy := time.Duration(*evaluation.EffectiveTimeoutMs) * time.Millisecond
			if timeoutFromPolicy < effectiveTimeout {
				effectiveTimeout = timeoutFromPolicy
			}
		}
		_ = s.policyRuntime.RecordRuntimeEvaluation(ctx, scope, evaluation, "stage_timeout_watcher")
	}

	age := time.Now().UTC().Sub(startedAt)
	if age < effectiveTimeout {
		if err = tx.Commit(); err != nil {
			return false, err
		}
		return false, nil
	}

	msg := fmt.Sprintf("Stage has been %s for too long - %.0f seconds", strings.ToLower(strings.TrimSpace(stage.Status)), age.Seconds())
	if _, err = tx.ExecContext(ctx, `
		UPDATE stage SET status=$1, finished_at=CURRENT_TIMESTAMP, next_retry_at=NULL WHERE id=$2
	`, types.StageStatusFailed, stageID); err != nil {
		return false, err
	}
	if _, err = tx.ExecContext(ctx, `
		UPDATE pipeline SET is_completed=true, finished_at=CURRENT_TIMESTAMP, status=$2 WHERE id=$1
	`, stage.PipelineID, types.PipelineStatusFailed); err != nil {
		return false, err
	}
	if _, err = tx.ExecContext(ctx, `
		UPDATE stage_io SET output=$1 WHERE stage_id=$2
	`, msg, stageID); err != nil {
		return false, err
	}

	if err = tx.Commit(); err != nil {
		return false, err
	}

	s.LogStageChange(ctx, stage.PipelineID, stageID, stage.Status, types.StageStatusFailed, "stage_timeout_watcher")
	return true, nil
}

// RecoverOrphanedStages finds stages stuck in Running longer than the recovery
// threshold and resets them to NotStarted so the publisher re-schedules them.
// Unlike MarkActiveTooLong (which fails stages after the full timeout), this
// uses a shorter threshold to give stages a second chance after a worker crash
// or restart.
func (s *Store) RecoverOrphanedStages(ctx context.Context, stuckThreshold time.Duration) (int64, error) {
	if stuckThreshold <= 0 {
		stuckThreshold = 60 * time.Second
	}

	cutoff := time.Now().UTC().Add(-stuckThreshold)

	rows, err := s.db.QueryxContext(ctx, `
		SELECT s.id, s.pipeline_id
		FROM stage s
		JOIN pipeline p ON p.id = s.pipeline_id
		WHERE p.is_completed = false
		  AND s.status = $1
		  AND (COALESCE(s.started_at, s.created_at)) < $2
	`, types.StageStatusRunning, cutoff)
	if err != nil {
		return 0, err
	}
	defer rows.Close()

	type orphan struct {
		StageID    int `db:"id"`
		PipelineID int `db:"pipeline_id"`
	}
	var orphans []orphan
	for rows.Next() {
		var o orphan
		if err := rows.Scan(&o.StageID, &o.PipelineID); err != nil {
			return 0, err
		}
		orphans = append(orphans, o)
	}
	if err := rows.Err(); err != nil {
		return 0, err
	}

	var count int64
	for _, o := range orphans {
		tx, txErr := s.db.BeginTxx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
		if txErr != nil {
			return count, txErr
		}

		var status string
		var startedAt sql.NullTime
		var createdAt time.Time
		lockErr := tx.QueryRowContext(ctx, fmt.Sprintf(`
			SELECT s.status, s.started_at, s.created_at
			FROM stage s
			WHERE s.id = $1%s
		`, s.forUpdateOfClause("s")), o.StageID).Scan(&status, &startedAt, &createdAt)

		if lockErr != nil || !isWatchedActiveStageStatus(status) {
			_ = tx.Rollback()
			continue
		}

		lastActiveAt := createdAt.UTC()
		if startedAt.Valid {
			lastActiveAt = startedAt.Time.UTC()
		}
		stuckFor := time.Since(lastActiveAt).Round(time.Second)

		_, execErr := tx.ExecContext(ctx, `
			UPDATE stage SET status = $1, started_at = NULL, finished_at = NULL
			WHERE id = $2
		`, types.StageStatusNotStarted, o.StageID)
		if execErr != nil {
			_ = tx.Rollback()
			return count, execErr
		}

		if commitErr := tx.Commit(); commitErr != nil {
			return count, commitErr
		}

		s.LogStageChange(ctx, o.PipelineID, o.StageID, status, types.StageStatusNotStarted, "orphan_recovery")
		s.logStageMessage(ctx, o.StageID, "WARNING", buildOrphanRecoveryReason(status, stuckFor, stuckThreshold, lastActiveAt))
		count++
	}

	return count, nil
}

func buildOrphanRecoveryReason(status string, stuckFor, threshold time.Duration, lastActiveAt time.Time) string {
	if stuckFor < 0 {
		stuckFor = 0
	}

	base := fmt.Sprintf(
		"Orphan recovery reset this stage after it remained %s for %s (threshold %s, last activity %s).",
		status,
		stuckFor,
		threshold.Round(time.Second),
		lastActiveAt.Format(time.RFC3339),
	)

	switch status {
	case types.StageStatusPending:
		return base + " The stage was scheduled but no worker pickup, Running transition, or stage result was recorded before the recovery window expired, so it was returned to NotStarted for re-scheduling."
	case types.StageStatusRunning:
		return base + " A worker had marked the stage Running, but no completion, failure, or follow-up status update was recorded before the recovery window expired, so it was returned to NotStarted for re-scheduling."
	default:
		return base + " No further progress was recorded before the recovery window expired, so it was returned to NotStarted for re-scheduling."
	}
}

func isWatchedActiveStageStatus(status string) bool {
	switch status {
	case types.StageStatusRunning:
		return true
	default:
		return false
	}
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

	err = tx.GetContext(ctx, &stage, fmt.Sprintf(`
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
	%s
	`, s.forUpdateOfClause("s")), msg.StageID)
	if err != nil {
		return nil, err
	}

	var pipeline struct {
		ID          int            `db:"id"`
		Status      sql.NullString `db:"status"`
		IsCompleted bool           `db:"is_completed"`
	}
	if err = tx.GetContext(ctx, &pipeline, fmt.Sprintf(`
		SELECT id, status, is_completed
		FROM pipeline
		WHERE id = $1%s
	`, s.forUpdateClause()), stage.PipelineID); err != nil {
		return nil, err
	}
	if isPipelineTerminalStatus(pipeline.Status.String, pipeline.IsCompleted) {
		return nil, ErrPipelineAppendNotAllowed
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
	var nextRetryDelay time.Duration

	if msg.IsWaitingForApproval {
		newStatus = types.StageStatusWaitingApproval
	} else if msg.IsSuccess {
		newStatus = types.StageStatusCompleted
	} else {
		skipAutomaticRetry := shouldSkipAutomaticRetry(msg.ErrorCode)

		// Policy-based retry takes precedence over stage_options retry.
		if s.policyRuntime != nil && !skipAutomaticRetry {
			allPolicies, _ := s.policyRuntime.RuntimePolicies(ctx)
			scope, scopeErr := s.loadPolicyRuntimeScopeTx(ctx, tx, msg.StageID)
			if scopeErr == nil {
				evaluation, evalErr := s.evaluateStagePoliciesTx(ctx, tx, scope)
				if evalErr == nil {
					if retryRule := effectiveRetryPolicy(evaluation, allPolicies); retryRule != nil {
						maxAttempts := 0
						if retryRule.MaxAttempts != nil {
							maxAttempts = *retryRule.MaxAttempts
						}
						if maxAttempts > 0 && stage.RetryAttempt < maxAttempts && retryOnMatches(*retryRule, msg.ErrorCode) {
							newStatus = types.StageStatusRetryScheduled
							nextRetryDelay = computeBackoffDelay(*retryRule, stage.RetryAttempt+1)
						}
					}
					_ = s.policyRuntime.RecordRuntimeEvaluation(ctx, scope, evaluation, "result_processor")
				}
			}
		}

		// Fall back to stage_options retry when no policy triggered a retry.
		if newStatus != types.StageStatusRetryScheduled && !skipAutomaticRetry {
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
				nextRetryDelay = time.Duration(retryIntervalSeconds) * time.Second
			}
		}
	}

	pausedPipeline := isPausedPipelineStatus(pipeline.Status.String)

	if newStatus == types.StageStatusRetryScheduled {
		if nextRetryDelay <= 0 {
			nextRetryDelay = 30 * time.Second
		}
		nextRetryAt := time.Now().UTC().Add(nextRetryDelay)
		if _, err = tx.ExecContext(ctx, `
			UPDATE stage
			SET status=$1, finished_at=CURRENT_TIMESTAMP, retry_attempt=retry_attempt + 1, next_retry_at=$2
			WHERE id=$3
		`, newStatus, nextRetryAt, msg.StageID); err != nil {
			return nil, err
		}
	} else {
		if _, err = tx.ExecContext(ctx, `
			UPDATE stage SET status=$1, finished_at=CURRENT_TIMESTAMP, next_retry_at=NULL WHERE id=$2
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

	appendedStages, err := mapStageResultAppendedStages(msg.AppendedStages)
	if err != nil {
		return nil, err
	}
	addedStages, err := s.appendStagesTx(
		ctx,
		tx,
		stage.PipelineID,
		appendedStages,
		fmt.Sprintf("stage_result:%d", msg.StageID),
		"stage result",
	)
	if err != nil {
		return nil, err
	}

	resultSummary := fmt.Sprintf(
		"Stage result processed [success=%t, waitingForApproval=%t, newStatus=%s, errorCode=%s, logs=%d, contextItems=%d, appendedStages=%d, result=%s]",
		msg.IsSuccess,
		msg.IsWaitingForApproval,
		newStatus,
		blankIfEmpty(msg.ErrorCode, "-"),
		len(msg.Logs),
		len(msg.ContextItems),
		len(addedStages),
		stageLogPreview(msg.Result, 900),
	)
	if err = s.insertStageLogTx(ctx, tx, msg.StageID, pickStageResultLogLevel(msg.IsSuccess, msg.IsWaitingForApproval), resultSummary); err != nil {
		return nil, err
	}

	if newStatus == types.StageStatusRetryScheduled {
		nextPipelineStatus := types.PipelineStatusPending
		if pausedPipeline {
			nextPipelineStatus = types.PipelineStatusPaused
		}
		if _, err = tx.ExecContext(ctx, `
			UPDATE pipeline SET is_completed=false, finished_at=NULL, status=$2 WHERE id=$1
		`, stage.PipelineID, nextPipelineStatus); err != nil {
			return nil, err
		}
	} else {
		// Mark pipeline completed when failed or when this is last stage.
		var lastStageID int
		if err = tx.GetContext(ctx, &lastStageID, `SELECT MAX(id) FROM stage WHERE pipeline_id=$1`, stage.PipelineID); err != nil {
			return nil, err
		}

		hasFailureContinuation := false
		if !msg.IsSuccess {
			hasFailureContinuation, err = s.hasFailureContinuationStageTx(ctx, tx, stage.PipelineID, msg.StageID)
			if err != nil {
				return nil, err
			}
		}

		hasEarlierFailure := false
		if msg.IsSuccess && msg.StageID == lastStageID {
			hasEarlierFailure, err = s.hasPriorFailedStageTx(ctx, tx, stage.PipelineID, msg.StageID)
			if err != nil {
				return nil, err
			}
		}

		completePipeline := ((!msg.IsSuccess && !hasFailureContinuation) || msg.StageID == lastStageID) && !msg.IsWaitingForApproval
		if completePipeline {
			pStatus := types.PipelineStatusCompleted
			if !msg.IsSuccess || hasEarlierFailure {
				pStatus = types.PipelineStatusFailed
			}
			if _, err = tx.ExecContext(ctx, `
				UPDATE pipeline SET is_completed=true, finished_at=CURRENT_TIMESTAMP, status=$2 WHERE id=$1
			`, stage.PipelineID, pStatus); err != nil {
				return nil, err
			}
		} else {
			nextPipelineStatus := types.PipelineStatusPending
			if pausedPipeline {
				nextPipelineStatus = types.PipelineStatusPaused
			} else {
				var stageStatuses []string
				if err = sqlx.SelectContext(ctx, tx, &stageStatuses, `SELECT status FROM stage WHERE pipeline_id=$1 ORDER BY id`, stage.PipelineID); err != nil {
					return nil, err
				}
				nextPipelineStatus = computePipelineStatus(stageStatuses)
				if nextPipelineStatus == types.PipelineStatusRunning {
					nextPipelineStatus = types.PipelineStatusPending
				}
			}
			if _, err = tx.ExecContext(ctx, `
				UPDATE pipeline SET is_completed=false, finished_at=NULL, status=$2 WHERE id=$1
			`, stage.PipelineID, nextPipelineStatus); err != nil {
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

func shouldSkipAutomaticRetry(errorCode string) bool {
	switch strings.ToUpper(strings.TrimSpace(errorCode)) {
	case "TOOL_LOOP", "BUDGET_EXCEEDED", "LLM_INVALID_REQUEST":
		return true
	default:
		return false
	}
}

func (s *Store) hasFailureContinuationStageTx(ctx context.Context, tx *sqlx.Tx, pipelineID int, failedStageID int) (bool, error) {
	var exists bool
	if err := tx.GetContext(ctx, &exists, `
		SELECT EXISTS (
			SELECT 1
			FROM stage s
			LEFT JOIN stage_options so ON so.stage_id = s.id
			WHERE s.pipeline_id = $1
			  AND s.id > $2
			  AND COALESCE(s.is_event, false) = false
			  AND COALESCE(s.is_skipped, false) = false
			  AND COALESCE(so.run_next_if_failed, false)
		)
	`, pipelineID, failedStageID); err != nil {
		return false, err
	}

	return exists, nil
}

func (s *Store) hasPriorFailedStageTx(ctx context.Context, tx *sqlx.Tx, pipelineID int, currentStageID int) (bool, error) {
	var exists bool
	if err := tx.GetContext(ctx, &exists, `
		SELECT EXISTS (
			SELECT 1
			FROM stage s
			WHERE s.pipeline_id = $1
			  AND s.id < $2
			  AND COALESCE(s.is_event, false) = false
			  AND COALESCE(s.is_skipped, false) = false
			  AND s.status = $3
		)
	`, pipelineID, currentStageID, types.StageStatusFailed); err != nil {
		return false, err
	}

	return exists, nil
}

func pickStageResultLogLevel(isSuccess bool, isWaitingForApproval bool) string {
	if isWaitingForApproval {
		return "WARN"
	}
	if isSuccess {
		return "INFO"
	}
	return "ERROR"
}

func blankIfEmpty(value string, fallback string) string {
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	return value
}

func valueTypeOrDefault(vt string) string {
	if vt == "" {
		return "string"
	}
	return vt
}

func mapStageResultAppendedStages(appended []types.AppendedStage) ([]types.StageCreate, error) {
	if len(appended) == 0 {
		return nil, nil
	}

	mapped := make([]types.StageCreate, 0, len(appended))
	for _, stage := range appended {
		runNextIfFailed := false
		if stage.Options != nil && stage.Options.RunNextIfFailed != nil {
			runNextIfFailed = *stage.Options.RunNextIfFailed
		}

		isEvent := false
		if stage.IsEvent != nil {
			isEvent = *stage.IsEvent
		}

		mapped = append(mapped, types.StageCreate{
			Name:            stage.StageName,
			StageHandler:    stage.StageHandlerName,
			Description:     stage.Description,
			Input:           strings.TrimSpace(stage.Input),
			Options:         stage.Options,
			IsEvent:         isEvent,
			RunNextIfFailed: runNextIfFailed,
		})
	}

	return mapped, nil
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
	err = tx.QueryRowContext(ctx, fmt.Sprintf(`
		SELECT status, pipeline_id FROM stage WHERE id = $1%s
	`, s.forUpdateClause()), msg.StageID).Scan(&oldStatus, &pipelineID)
	if err != nil {
		return nil, err
	}

	var pipeline struct {
		Status      sql.NullString `db:"status"`
		IsCompleted bool           `db:"is_completed"`
	}
	if err = tx.GetContext(ctx, &pipeline, fmt.Sprintf(`
		SELECT status, is_completed
		FROM pipeline
		WHERE id = $1%s
	`, s.forUpdateClause()), pipelineID); err != nil {
		return nil, err
	}

	if !isValidStageTransition(oldStatus, msg.Status) {
		s.logger.Warn("rejecting invalid stage status transition",
			"stageId", msg.StageID, "from", oldStatus, "to", msg.Status)
		_ = tx.Commit()
		return s.GetPipelineWithStages(ctx, pipelineID)
	}

	switch msg.Status {
	case types.StageStatusRunning:
		if _, err = tx.ExecContext(ctx, `
			UPDATE stage
			SET status=$1, started_at=CURRENT_TIMESTAMP
			WHERE id=$2
		`, msg.Status, msg.StageID); err != nil {
			return nil, err
		}
	default:
		if _, err = tx.ExecContext(ctx, `
			UPDATE stage SET status=$1 WHERE id=$2
		`, msg.Status, msg.StageID); err != nil {
			return nil, err
		}
	}

	if !isPipelineTerminalStatus(pipeline.Status.String, pipeline.IsCompleted) {
		nextPipelineStatus := nullableStatus(&msg.Status)
		switch msg.Status {
		case types.StageStatusRunning:
			nextPipelineStatus = types.PipelineStatusRunning
		case types.StageStatusPending, types.StageStatusRetryScheduled, types.StageStatusThrottled, types.StageStatusWaitingApproval:
			nextPipelineStatus = types.PipelineStatusPending
		default:
			var stageStatuses []string
			if err = sqlx.SelectContext(ctx, tx, &stageStatuses, `SELECT status FROM stage WHERE pipeline_id=$1 ORDER BY id`, pipelineID); err != nil {
				return nil, err
			}
			nextPipelineStatus = computePipelineStatus(stageStatuses)
		}

		if isPausedPipelineStatus(pipeline.Status.String) {
			nextPipelineStatus = types.PipelineStatusPaused
		}

		if _, err = tx.ExecContext(ctx, `
			UPDATE pipeline SET status=$1, is_completed=false, finished_at=NULL WHERE id=$2
		`, nextPipelineStatus, pipelineID); err != nil {
			return nil, err
		}
	}

	if err = tx.Commit(); err != nil {
		return nil, err
	}

	if oldStatus != msg.Status {
		s.LogStageChange(ctx, pipelineID, msg.StageID, oldStatus, msg.Status, "status_consumer")
	}

	return s.GetPipelineWithStages(ctx, pipelineID)
}

// resolveTraceID returns a valid W3C trace ID (32 lowercase hex chars).
// Priority: explicit traceId from request → extract from traceparent context item → generate new.
func resolveTraceID(explicit string, contextItems []types.ContextItem) string {
	if explicit != "" {
		return explicit
	}
	for _, item := range contextItems {
		if !strings.EqualFold(item.Key, "traceparent") {
			continue
		}
		// W3C traceparent: "00-{traceId}-{spanId}-{flags}"
		parts := strings.Split(item.Value, "-")
		if len(parts) == 4 && len(parts[1]) == 32 {
			return parts[1]
		}
	}
	// Generate a proper 16-byte (128-bit) random trace ID.
	b := make([]byte, 16)
	if _, err := rand.Read(b); err == nil {
		return hex.EncodeToString(b)
	}
	return hex.EncodeToString([]byte("pipelogiq_trace_"))
}
