package store

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
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
	lockQuery := fmt.Sprintf(`
		SELECT id, status, is_completed
		FROM pipeline
		WHERE id = $1%s
	`, s.forUpdateClause())
	if err = tx.GetContext(ctx, &row, lockQuery, pipelineID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrPipelineNotFound
		}
		return nil, fmt.Errorf("load pipeline for append: %w", err)
	}

	if isPipelineTerminalStatus(row.Status.String, row.IsCompleted) {
		return nil, ErrPipelineAppendNotAllowed
	}

	added := make([]types.StageDTO, 0, len(req.Stages))
	for _, incoming := range req.Stages {
		stageName := strings.TrimSpace(incoming.Name)
		handlerName := strings.TrimSpace(incoming.StageHandler)
		runNextIfFailed := incoming.RunNextIfFailed
		if incoming.Options != nil && incoming.Options.RunNextIfFailed != nil {
			runNextIfFailed = *incoming.Options.RunNextIfFailed
		}

		createdAt := time.Now().UTC()
		spanID := randomHex(8)

		var stageID int
		if err = tx.QueryRowContext(ctx, `
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

		if _, err = tx.ExecContext(ctx, `
			INSERT INTO stage_io (input, stage_id) VALUES ($1, $2)
		`, nullableString(incoming.Input), stageID); err != nil {
			return nil, fmt.Errorf("insert stage io: %w", err)
		}

		if err = s.insertStageOptions(ctx, tx, stageID, incoming.Options); err != nil {
			return nil, fmt.Errorf("insert stage options: %w", err)
		}

		if err = s.insertStageAuditTx(
			ctx,
			tx,
			stageID,
			fmt.Sprintf("Stage appended via API [pipeline=%d, actor=%s]", pipelineID, auditActor(actor)),
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

	if err = tx.Commit(); err != nil {
		return nil, err
	}
	committed = true

	return &types.AppendStagesResponse{Stages: added}, nil
}

func (s *Store) ResumeStageApproval(
	ctx context.Context,
	stageID int,
	req types.ResumeStageRequest,
	actor string,
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
	lockQuery := fmt.Sprintf(`
		SELECT
			id,
			pipeline_id,
			status,
			approval_decision,
			approval_rejection_reason
		FROM stage
		WHERE id = $1%s
	`, s.forUpdateClause())
	if err = tx.GetContext(ctx, &stage, lockQuery, stageID); err != nil {
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

	if err = tx.Commit(); err != nil {
		return err
	}
	committed = true

	if oldStatus != newStatus {
		s.LogStageChange(ctx, stage.PipelineID, stageID, oldStatus, newStatus, "stage_resume")
	}

	return nil
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
	driver := strings.ToLower(strings.TrimSpace(s.db.DriverName()))
	if strings.Contains(driver, "sqlite") {
		return ""
	}
	return " FOR UPDATE"
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
