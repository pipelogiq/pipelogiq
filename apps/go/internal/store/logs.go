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

func (s *Store) GetStageLogs(ctx context.Context, pipelineID int, stageID *int) ([]types.StageLog, error) {
	logs := []types.StageLog{}

	var query string
	var args []interface{}

	if stageID != nil {
		query = `
			SELECT sl.id, sl.stage_id, COALESCE(sl.log, '') AS log, COALESCE(sl.log_level, '') AS log_level, sl.created_at
			FROM stage_log sl
			JOIN stage s ON s.id = sl.stage_id
			WHERE s.pipeline_id = $1 AND sl.stage_id = $2
			ORDER BY sl.created_at
		`
		args = []interface{}{pipelineID, *stageID}
	} else {
		query = `
			SELECT sl.id, sl.stage_id, COALESCE(sl.log, '') AS log, COALESCE(sl.log_level, '') AS log_level, sl.created_at
			FROM stage_log sl
			JOIN stage s ON s.id = sl.stage_id
			WHERE s.pipeline_id = $1
			ORDER BY sl.created_at
		`
		args = []interface{}{pipelineID}
	}

	err := s.db.SelectContext(ctx, &logs, query, args...)
	if err != nil {
		return nil, err
	}

	items, err := s.GetPipelineContext(ctx, pipelineID)
	if err != nil {
		return nil, err
	}
	history, err := s.getRedactionHistory(ctx, []int{pipelineID})
	if err != nil {
		return nil, err
	}
	return types.RedactStageLogs(logs, append(items, history[pipelineID]...)), nil
}

func (s *Store) SaveLog(ctx context.Context, req types.LogRequest) (*types.LogResponse, error) {
	var appID *int

	// Get application ID from API key if provided
	if req.ApiKey != nil && *req.ApiKey != "" {
		var id int
		err := s.db.QueryRowContext(ctx, `
			SELECT application_id FROM api_key WHERE key_hash = $1 AND disabled_at IS NULL
		`, HashAPIKey(*req.ApiKey)).Scan(&id)
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return nil, fmt.Errorf("validate api key: %w", err)
		}
		if err == nil {
			appID = &id
		}
	}

	created := req.Created
	if created == nil {
		now := time.Now()
		created = &now
	}

	var logID int
	err := s.db.QueryRowContext(ctx, `
		INSERT INTO log (log, log_level, created_at, application_id)
		VALUES ($1, $2, $3, $4)
		RETURNING id
	`, req.Message, req.LogLevel, created, appID).Scan(&logID)

	if err != nil {
		return nil, fmt.Errorf("insert log: %w", err)
	}

	// Insert keywords
	for _, kw := range req.Keywords {
		var keywordID int
		err := s.db.QueryRowContext(ctx, `
			SELECT id FROM keyword WHERE key = $1 AND value = $2 LIMIT 1
		`, kw.Key, kw.Value).Scan(&keywordID)

		if errors.Is(err, sql.ErrNoRows) {
			err = s.db.QueryRowContext(ctx, `
				INSERT INTO keyword (key, value) VALUES ($1, $2) RETURNING id
			`, kw.Key, kw.Value).Scan(&keywordID)
		}

		if err != nil {
			continue
		}

		_, _ = s.db.ExecContext(ctx, `
			INSERT INTO log_keyword (log_id, keyword_id) VALUES ($1, $2)
		`, logID, keywordID)
	}

	return &types.LogResponse{
		ID:            logID,
		ApplicationID: appID,
		Message:       req.Message,
		LogLevel:      req.LogLevel,
		CreatedAt:     created,
		Keywords:      req.Keywords,
	}, nil
}

func (s *Store) insertStageLogTx(ctx context.Context, tx *sqlx.Tx, stageID int, logLevel string, message string) error {
	if strings.TrimSpace(message) == "" {
		return nil
	}

	_, err := tx.ExecContext(ctx, `
		INSERT INTO stage_log (log, log_level, created_at, stage_id)
		VALUES ($1, $2, $3, $4)
	`, message, strings.ToUpper(strings.TrimSpace(logLevel)), time.Now().UTC(), stageID)
	return err
}

func (s *Store) logStageMessage(ctx context.Context, stageID int, logLevel string, message string) {
	if strings.TrimSpace(message) == "" {
		return
	}

	_, err := s.db.ExecContext(ctx, `
		INSERT INTO stage_log (log, log_level, created_at, stage_id)
		VALUES ($1, $2, $3, $4)
	`, message, strings.ToUpper(strings.TrimSpace(logLevel)), time.Now().UTC(), stageID)
	if err != nil {
		s.logger.Error("failed to insert stage log message", "stage_id", stageID, "err", err)
	}
}

func stageLogPreview(value string, maxLen int) string {
	trimmed := strings.TrimSpace(strings.ReplaceAll(strings.ReplaceAll(value, "\r", " "), "\n", " "))
	if trimmed == "" {
		return ""
	}
	if maxLen <= 0 || len(trimmed) <= maxLen {
		return trimmed
	}
	return trimmed[:maxLen] + "..."
}

// LogStageChange inserts a stage status change entry into stage_log.
// Best-effort: errors are logged but do not propagate.
func (s *Store) LogStageChange(ctx context.Context, pipelineID, stageID int, oldStatus, newStatus, source string) {
	// Fetch stage name for human-readable message.
	var stageName string
	var pipelineName string
	_ = s.db.QueryRowContext(ctx, `
		SELECT s.name, COALESCE(p.name, '')
		FROM stage s
		LEFT JOIN pipeline p ON p.id = s.pipeline_id
		WHERE s.id = $1
	`, stageID).Scan(&stageName, &pipelineName)

	msg := fmt.Sprintf("Stage '%s' (id=%d) status changed: %s → %s [pipeline=%d, source=%s]",
		stageName, stageID, oldStatus, newStatus, pipelineID, source)
	logLevel := "INFO"
	now := time.Now()

	_, err := s.db.ExecContext(ctx, `
		INSERT INTO stage_log (log, log_level, created_at, stage_id)
		VALUES ($1, $2, $3, $4)
	`, msg, logLevel, now, stageID)
	if err != nil {
		s.logger.Error("failed to log stage change", "err", err)
	}

	s.emitStageAlert(StageAlertEvent{
		PipelineID:   pipelineID,
		PipelineName: pipelineName,
		StageID:      stageID,
		StageName:    stageName,
		OldStatus:    oldStatus,
		NewStatus:    newStatus,
		Source:       source,
		TS:           now.UTC(),
	})
}

func (s *Store) GetLogsByAppID(ctx context.Context, appID int) ([]types.LogResponse, error) {
	logs := []types.LogResponse{}

	err := s.db.SelectContext(ctx, &logs, `
		SELECT id, application_id, log, log_level, created_at
		FROM log
		WHERE application_id = $1
		ORDER BY created_at DESC
	`, appID)

	if err != nil {
		return nil, err
	}

	return logs, nil
}

func (s *Store) GetKeywords(ctx context.Context, applicationIDs []int, search *string) ([]string, error) {
	keywords := []string{}
	if len(applicationIDs) == 0 {
		return keywords, nil
	}
	// Keyword rows are shared globally; only suggest keys actually linked to
	// a pipeline or application log within the caller's membership.
	query := `SELECT DISTINCT k.key FROM keyword k WHERE (
		EXISTS (SELECT 1 FROM pipeline_keyword pk JOIN pipeline p ON p.id=pk.pipeline_id
			WHERE pk.keyword_id=k.id AND p.application_id IN (?))
		OR EXISTS (SELECT 1 FROM log_keyword lk JOIN log l ON l.id=lk.log_id
			WHERE lk.keyword_id=k.id AND l.application_id IN (?))
	)`
	args := []any{applicationIDs, applicationIDs}
	if search != nil && *search != "" {
		query += ` AND LOWER(k.key) LIKE LOWER(?)`
		args = append(args, "%"+*search+"%")
	}
	query += ` ORDER BY k.key LIMIT 100`
	query, args, err := sqlx.In(query, args...)
	if err != nil {
		return nil, err
	}
	err = s.db.SelectContext(ctx, &keywords, s.db.Rebind(query), args...)
	if err != nil {
		return nil, err
	}

	return keywords, nil
}
