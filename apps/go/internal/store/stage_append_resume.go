package store

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jmoiron/sqlx"

	"pipelogiq/internal/types"
)

var (
	ErrPipelineNotFound         = errors.New("pipeline not found")
	ErrPipelineAppendNotAllowed = errors.New("append stages not allowed for pipeline status")
	ErrStageNotFound            = errors.New("stage not found")
	ErrStageNotWaitingApproval  = errors.New("stage is not waiting for approval")
	ErrStageResumeConflict      = errors.New("stage resume conflict")
)

const appendedStageIDsContextKey = "pipelogiq:appendedStageIds"

func IsPipelineNotFoundError(err error) bool {
	return errors.Is(err, ErrPipelineNotFound)
}

func IsPipelineAppendNotAllowedError(err error) bool {
	return errors.Is(err, ErrPipelineAppendNotAllowed)
}

func IsStageNotFoundError(err error) bool {
	return errors.Is(err, ErrStageNotFound)
}

func IsStageNotWaitingApprovalError(err error) bool {
	return errors.Is(err, ErrStageNotWaitingApproval)
}

func IsStageResumeConflictError(err error) bool {
	return errors.Is(err, ErrStageResumeConflict)
}

func (s *Store) AppendStages(
	ctx context.Context,
	pipelineID int,
	req types.AppendStagesRequest,
	actor string,
	appID ...int,
) (*types.AppendStagesResponse, error) {
	tx, err := s.db.BeginTxx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return nil, err
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback()
		}
	}()

	var row struct {
		ID          int            `db:"id"`
		Status      sql.NullString `db:"status"`
		IsCompleted bool           `db:"is_completed"`
	}

	// When appID is provided, enforce ownership — the pipeline must belong
	// to the caller's application. This prevents cross-app data leakage.
	var lockQuery string
	var lockArgs []interface{}
	if len(appID) > 0 && appID[0] > 0 {
		lockQuery = fmt.Sprintf(`
			SELECT id, status, is_completed
			FROM pipeline
			WHERE id = $1 AND application_id = $2%s
		`, s.forUpdateClause())
		lockArgs = []interface{}{pipelineID, appID[0]}
	} else {
		lockQuery = fmt.Sprintf(`
			SELECT id, status, is_completed
			FROM pipeline
			WHERE id = $1%s
		`, s.forUpdateClause())
		lockArgs = []interface{}{pipelineID}
	}
	if err = tx.GetContext(ctx, &row, lockQuery, lockArgs...); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrPipelineNotFound
		}
		return nil, fmt.Errorf("load pipeline for append: %w", err)
	}

	if isPipelineTerminalStatus(row.Status.String, row.IsCompleted) {
		return nil, ErrPipelineAppendNotAllowed
	}

	added, err := s.appendStagesTx(ctx, tx, pipelineID, req.Stages, actor, "API")
	if err != nil {
		return nil, err
	}

	if err = tx.Commit(); err != nil {
		return nil, err
	}
	committed = true

	return &types.AppendStagesResponse{Stages: added}, nil
}

func (s *Store) appendStagesTx(
	ctx context.Context,
	tx *sqlx.Tx,
	pipelineID int,
	stages []types.StageCreate,
	actor string,
	source string,
) ([]types.StageDTO, error) {
	added := make([]types.StageDTO, 0, len(stages))
	for _, incoming := range stages {
		stageName := strings.TrimSpace(incoming.Name)
		handlerName := strings.TrimSpace(incoming.StageHandler)
		runNextIfFailed := incoming.RunNextIfFailed
		if incoming.Options != nil && incoming.Options.RunNextIfFailed != nil {
			runNextIfFailed = *incoming.Options.RunNextIfFailed
		}

		createdAt := time.Now().UTC()
		spanID := randomHex(8)

		var stageID int
		if err := tx.QueryRowContext(ctx, `
			INSERT INTO stage
				(name, stage_handler_name, description, status, pipeline_id, created_at, is_event, span_id)
			VALUES
				($1, $2, $3, $4, $5, $6, $7, $8)
			RETURNING id
		`,
			stageName,
			handlerName,
			incoming.Description,
			types.StageStatusNotStarted,
			pipelineID,
			createdAt,
			incoming.IsEvent,
			spanID,
		).Scan(&stageID); err != nil {
			return nil, fmt.Errorf("insert stage: %w", err)
		}

		if _, err := tx.ExecContext(ctx, `
			INSERT INTO stage_io (input, stage_id) VALUES ($1, $2)
		`, nullableString(incoming.Input), stageID); err != nil {
			return nil, fmt.Errorf("insert stage io: %w", err)
		}

		if err := s.insertStageOptions(ctx, tx, stageID, incoming.Options); err != nil {
			return nil, fmt.Errorf("insert stage options: %w", err)
		}

		if err := s.insertStageAuditTx(
			ctx,
			tx,
			stageID,
			buildAppendStageAuditMessage(pipelineID, actor, incoming, handlerName, runNextIfFailed, source),
		); err != nil {
			return nil, fmt.Errorf("insert append audit log: %w", err)
		}

		added = append(added, types.StageDTO{
			ID:                     stageID,
			PipelineID:             pipelineID,
			Name:                   stageName,
			StageHandlerName:       handlerName,
			Status:                 types.StageStatusNotStarted,
			CreatedAt:              createdAt,
			Input:                  incoming.Input,
			IsSkipped:              false,
			IsEvent:                incoming.IsEvent,
			RunNextIfCurrentFailed: runNextIfFailed,
		})
	}

	for i := 0; i < len(added)-1; i++ {
		next := added[i+1].ID
		added[i].NextStageID = &next
	}

	if err := s.upsertAppendedStageIDsTx(ctx, tx, pipelineID, added); err != nil {
		return nil, fmt.Errorf("upsert appended stage id map: %w", err)
	}

	return added, nil
}

func (s *Store) upsertAppendedStageIDsTx(
	ctx context.Context,
	tx *sqlx.Tx,
	pipelineID int,
	added []types.StageDTO,
) error {
	if len(added) == 0 {
		return nil
	}

	stageIDs := make(map[string]int, len(added))
	var existingRaw sql.NullString
	err := tx.GetContext(ctx, &existingRaw, `
		SELECT value
		FROM pipeline_context_item
		WHERE pipeline_id = $1 AND key = $2
		ORDER BY id DESC
		LIMIT 1
	`, pipelineID, appendedStageIDsContextKey)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	if existingRaw.Valid && strings.TrimSpace(existingRaw.String) != "" {
		_ = json.Unmarshal([]byte(existingRaw.String), &stageIDs)
	}

	for _, stage := range added {
		stageName := strings.TrimSpace(stage.Name)
		if stageName == "" {
			continue
		}
		stageIDs[stageName] = stage.ID
	}

	encoded, err := json.Marshal(stageIDs)
	if err != nil {
		return err
	}

	res, err := tx.ExecContext(ctx, `
		UPDATE pipeline_context_item
		SET value = $1, value_type = $2
		WHERE pipeline_id = $3 AND key = $4
	`, string(encoded), "", pipelineID, appendedStageIDsContextKey)
	if err != nil {
		return err
	}
	affected, _ := res.RowsAffected()
	if affected > 0 {
		return nil
	}

	_, err = tx.ExecContext(ctx, `
		INSERT INTO pipeline_context_item (key, value, value_type, pipeline_id)
		VALUES ($1, $2, $3, $4)
	`, appendedStageIDsContextKey, string(encoded), "", pipelineID)
	return err
}

func (s *Store) ResumeStageApproval(
	ctx context.Context,
	stageID int,
	req types.ResumeStageRequest,
	actor string,
	appID ...int,
) error {
	return s.withSQLiteLockRetry(ctx, func() error {
		return s.resumeStageApprovalOnce(ctx, stageID, req, actor, appID...)
	})
}

func (s *Store) resumeStageApprovalOnce(
	ctx context.Context,
	stageID int,
	req types.ResumeStageRequest,
	actor string,
	appID ...int,
) error {
	tx, err := s.db.BeginTxx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return err
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback()
		}
	}()

	var stage struct {
		ID                      int            `db:"id"`
		PipelineID              int            `db:"pipeline_id"`
		Status                  sql.NullString `db:"status"`
		ApprovalDecision        sql.NullBool   `db:"approval_decision"`
		ApprovalRejectionReason sql.NullString `db:"approval_rejection_reason"`
	}

	// When appID is provided, join with pipeline to enforce ownership.
	var lockQuery string
	var lockArgs []interface{}
	if len(appID) > 0 && appID[0] > 0 {
		lockQuery = fmt.Sprintf(`
			SELECT
				s.id,
				s.pipeline_id,
				s.status,
				s.approval_decision,
				s.approval_rejection_reason
			FROM stage s
			JOIN pipeline p ON p.id = s.pipeline_id
			WHERE s.id = $1 AND p.application_id = $2%s
		`, s.forUpdateOfClause("s"))
		lockArgs = []interface{}{stageID, appID[0]}
	} else {
		lockQuery = fmt.Sprintf(`
			SELECT
				id,
				pipeline_id,
				status,
				approval_decision,
				approval_rejection_reason
			FROM stage
			WHERE id = $1%s
		`, s.forUpdateClause())
		lockArgs = []interface{}{stageID}
	}
	if err = tx.GetContext(ctx, &stage, lockQuery, lockArgs...); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return ErrStageNotFound
		}
		return fmt.Errorf("load stage for resume: %w", err)
	}

	requestReason := normalizeRejectionReason(req.RejectionReason)
	if stage.ApprovalDecision.Valid {
		existingReason := strings.TrimSpace(stage.ApprovalRejectionReason.String)
		if stage.ApprovalDecision.Bool == req.Approved && existingReason == requestReason {
			if err = tx.Commit(); err != nil {
				return err
			}
			committed = true
			return nil
		}
		return ErrStageResumeConflict
	}

	oldStatus := stage.Status.String
	if !isWaitingApprovalStatus(oldStatus) {
		return ErrStageNotWaitingApproval
	}

	now := time.Now().UTC()
	newStatus := types.StageStatusCompleted
	if !req.Approved {
		newStatus = types.StageStatusFailed
	}

	var rejectionReason *string
	if requestReason != "" {
		rejectionReason = &requestReason
	}

	if _, err = tx.ExecContext(ctx, `
		UPDATE stage
		SET
			status = $1,
			finished_at = $2,
			next_retry_at = NULL,
			approval_decision = $3,
			approval_rejection_reason = $4,
			approval_resumed_at = $5,
			approval_resumed_by = $6
		WHERE id = $7
	`,
		newStatus,
		now,
		req.Approved,
		rejectionReason,
		now,
		nullableString(strings.TrimSpace(actor)),
		stageID,
	); err != nil {
		return fmt.Errorf("update stage on resume: %w", err)
	}

	if !req.Approved && rejectionReason != nil {
		res, errExec := tx.ExecContext(ctx, `UPDATE stage_io SET output = $1 WHERE stage_id = $2`, *rejectionReason, stageID)
		if errExec != nil {
			return fmt.Errorf("update stage output on rejection: %w", errExec)
		}
		affected, _ := res.RowsAffected()
		if affected == 0 {
			if _, errExec = tx.ExecContext(ctx, `INSERT INTO stage_io (output, stage_id) VALUES ($1, $2)`, *rejectionReason, stageID); errExec != nil {
				return fmt.Errorf("insert stage output on rejection: %w", errExec)
			}
		}
	}

	statuses := []string{}
	if err = sqlx.SelectContext(ctx, tx, &statuses, `SELECT status FROM stage WHERE pipeline_id = $1 ORDER BY id`, stage.PipelineID); err != nil {
		return fmt.Errorf("load pipeline stage statuses: %w", err)
	}

	pipelineStatus := computePipelineStatus(statuses)
	if isPipelineTerminalStatus(pipelineStatus, false) {
		if _, err = tx.ExecContext(ctx, `
			UPDATE pipeline
			SET status = $1, is_completed = true, finished_at = $2
			WHERE id = $3
		`, pipelineStatus, now, stage.PipelineID); err != nil {
			return fmt.Errorf("update pipeline terminal status: %w", err)
		}
	} else {
		if _, err = tx.ExecContext(ctx, `
			UPDATE pipeline
			SET status = $1, is_completed = false, finished_at = NULL
			WHERE id = $2
		`, pipelineStatus, stage.PipelineID); err != nil {
			return fmt.Errorf("update pipeline running status: %w", err)
		}
	}

	if err = s.insertStageAuditTx(
		ctx,
		tx,
		stageID,
		fmt.Sprintf(
			"Stage approval resumed via API [pipeline=%d, approved=%t, actor=%s, reasonProvided=%t]",
			stage.PipelineID,
			req.Approved,
			auditActor(actor),
			requestReason != "",
		),
	); err != nil {
		return fmt.Errorf("insert resume audit log: %w", err)
	}

	// Inject approval decision into pipeline context so the next stage (agent:think)
	// can read it via AgentConstants.ApprovalDecision ("agent:approved").
	approvedValue := "false"
	if req.Approved {
		approvedValue = "true"
	}
	if err = upsertPipelineContextItem(ctx, tx, stage.PipelineID, "agent:approved", approvedValue); err != nil {
		return fmt.Errorf("inject agent:approved context: %w", err)
	}
	if !req.Approved && requestReason != "" {
		reasonJSON, _ := json.Marshal(requestReason)
		if err = upsertPipelineContextItem(ctx, tx, stage.PipelineID, "agent:rejectionReason", string(reasonJSON)); err != nil {
			return fmt.Errorf("inject agent:rejectionReason context: %w", err)
		}
	}

	if err = tx.Commit(); err != nil {
		return err
	}
	committed = true

	if oldStatus != newStatus {
		s.LogStageChange(ctx, stage.PipelineID, stageID, oldStatus, newStatus, "stage_resume")
	}

	return nil
}

func (s *Store) withSQLiteLockRetry(ctx context.Context, operation func() error) error {
	if !s.isSQLiteDriver() {
		return operation()
	}

	var err error
	backoff := 10 * time.Millisecond
	for attempt := 0; attempt < 5; attempt++ {
		err = operation()
		if !isSQLiteLockError(err) {
			return err
		}
		if ctx.Err() != nil {
			return ctx.Err()
		}

		timer := time.NewTimer(backoff)
		select {
		case <-ctx.Done():
			timer.Stop()
			return ctx.Err()
		case <-timer.C:
		}

		if backoff < 100*time.Millisecond {
			backoff *= 2
		}
	}

	return err
}

func (s *Store) insertStageAuditTx(ctx context.Context, tx *sqlx.Tx, stageID int, message string) error {
	if strings.TrimSpace(message) == "" {
		return nil
	}

	_, err := tx.ExecContext(ctx, `
		INSERT INTO stage_log (log, log_level, created_at, stage_id)
		VALUES ($1, $2, $3, $4)
	`, message, "INFO", time.Now().UTC(), stageID)
	return err
}

func (s *Store) forUpdateClause() string {
	if s.isSQLiteDriver() {
		return ""
	}
	return " FOR UPDATE"
}

// forUpdateOfClause returns a FOR UPDATE OF <alias> clause for queries that
// use LEFT JOINs. PostgreSQL does not allow FOR UPDATE on the nullable side of
// an outer join, so we must lock only the specific table alias.
func (s *Store) forUpdateOfClause(tableAlias string) string {
	if s.isSQLiteDriver() {
		return ""
	}
	return " FOR UPDATE OF " + tableAlias
}

func (s *Store) isSQLiteDriver() bool {
	driver := strings.ToLower(strings.TrimSpace(s.db.DriverName()))
	return strings.Contains(driver, "sqlite")
}

func isSQLiteLockError(err error) bool {
	if err == nil {
		return false
	}

	message := strings.ToLower(err.Error())
	return strings.Contains(message, "database table is locked") ||
		strings.Contains(message, "database is locked") ||
		strings.Contains(message, "database is deadlocked") ||
		strings.Contains(message, "sqlite_busy") ||
		strings.Contains(message, "sqlite_locked")
}

// isValidStageTransition enforces a strict state machine for stage status
// transitions. Prevents regressions like Completed→Running on message
// redelivery.
func isValidStageTransition(from, to string) bool {
	if from == to {
		return false
	}
	switch from {
	case types.StageStatusNotStarted:
		return to == types.StageStatusPending || to == types.StageStatusSkipped
	case types.StageStatusPending:
		return to == types.StageStatusRunning || to == types.StageStatusFailed
	case types.StageStatusRunning:
		return to == types.StageStatusCompleted ||
			to == types.StageStatusFailed ||
			to == types.StageStatusWaitingApproval
	case types.StageStatusRetryScheduled:
		return to == types.StageStatusNotStarted || to == types.StageStatusPending
	case types.StageStatusThrottled:
		return to == types.StageStatusNotStarted || to == types.StageStatusPending
	case types.StageStatusWaitingApproval:
		return to == types.StageStatusCompleted || to == types.StageStatusFailed
	case types.StageStatusCompleted, types.StageStatusFailed, types.StageStatusSkipped:
		return false
	default:
		return true
	}
}

func isPipelineTerminalStatus(status string, isCompleted bool) bool {
	if isCompleted {
		return true
	}

	switch strings.ToLower(strings.TrimSpace(status)) {
	case strings.ToLower(types.PipelineStatusCompleted),
		strings.ToLower(types.PipelineStatusFailed),
		strings.ToLower(types.PipelineStatusCancelled),
		"canceled":
		return true
	default:
		return false
	}
}

func upsertPipelineContextItem(ctx context.Context, tx *sqlx.Tx, pipelineID int, key, value string) error {
	res, err := tx.ExecContext(ctx, `
		UPDATE pipeline_context_item SET value=$1, value_type='string'
		WHERE pipeline_id=$2 AND key=$3
	`, value, pipelineID, key)
	if err != nil {
		return err
	}
	affected, _ := res.RowsAffected()
	if affected == 0 {
		_, err = tx.ExecContext(ctx, `
			INSERT INTO pipeline_context_item (key, value, value_type, pipeline_id)
			VALUES ($1, $2, 'string', $3)
		`, key, value, pipelineID)
	}
	return err
}

func isWaitingApprovalStatus(status string) bool {
	switch strings.ToLower(strings.TrimSpace(status)) {
	case strings.ToLower(types.StageStatusWaitingApproval), "waiting_for_approval", "awaiting_approval":
		return true
	default:
		return false
	}
}

func normalizeRejectionReason(reason *string) string {
	if reason == nil {
		return ""
	}
	return strings.TrimSpace(*reason)
}

func auditActor(actor string) string {
	trimmed := strings.TrimSpace(actor)
	if trimmed == "" {
		return "unknown"
	}
	return trimmed
}

func buildAppendStageAuditMessage(
	pipelineID int,
	actor string,
	incoming types.StageCreate,
	handlerName string,
	runNextIfFailed bool,
	source string,
) string {
	parts := []string{
		fmt.Sprintf("pipeline=%d", pipelineID),
		fmt.Sprintf("actor=%s", auditActor(actor)),
		fmt.Sprintf("stage=%s", strings.TrimSpace(incoming.Name)),
		fmt.Sprintf("handler=%s", strings.TrimSpace(handlerName)),
		fmt.Sprintf("runNextIfFailed=%t", runNextIfFailed),
		fmt.Sprintf("isEvent=%t", incoming.IsEvent),
	}

	if incoming.Options != nil {
		if incoming.Options.MaxRetries != nil {
			parts = append(parts, fmt.Sprintf("maxRetries=%d", *incoming.Options.MaxRetries))
		}
		if incoming.Options.RetryInterval != nil {
			parts = append(parts, fmt.Sprintf("retryIntervalSec=%d", *incoming.Options.RetryInterval))
		}
		if incoming.Options.TimeOut != nil {
			parts = append(parts, fmt.Sprintf("timeoutSec=%d", *incoming.Options.TimeOut))
		}
		if len(incoming.Options.DependsOn) > 0 {
			parts = append(parts, fmt.Sprintf("dependsOn=%s", strings.Join(incoming.Options.DependsOn, ",")))
		}
		if len(incoming.Options.RunInParallelWith) > 0 {
			parts = append(parts, fmt.Sprintf("parallelWith=%s", strings.Join(incoming.Options.RunInParallelWith, ",")))
		}
	}

	if preview := stageLogPreview(incoming.Input, 900); preview != "" {
		parts = append(parts, fmt.Sprintf("input=%s", preview))
	}

	sourceLabel := strings.TrimSpace(source)
	if sourceLabel == "" {
		sourceLabel = "unknown source"
	}
	return fmt.Sprintf("Stage appended via %s [%s]", sourceLabel, strings.Join(parts, ", "))
}

func randomHex(size int) string {
	if size <= 0 {
		return ""
	}
	buf := make([]byte, size)
	if _, err := rand.Read(buf); err == nil {
		return hex.EncodeToString(buf)
	}
	return hex.EncodeToString([]byte(time.Now().UTC().Format(time.RFC3339Nano)))
}
