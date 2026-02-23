package store

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/jmoiron/sqlx"

	"pipelogiq/internal/types"
)

func (s *Store) GetPipelines(ctx context.Context, req types.GetPipelinesRequest) (*types.PagedResult[types.PipelineResponse], error) {
	pageNumber := 1
	pageSize := 10

	if req.PageNumber != nil && *req.PageNumber > 0 {
		pageNumber = *req.PageNumber
	}
	if req.PageSize != nil && *req.PageSize > 0 {
		pageSize = *req.PageSize
	}

	offset := (pageNumber - 1) * pageSize

	// Build WHERE clause
	conditions := []string{"1=1"}
	args := []interface{}{}
	argNum := 1

	if req.ApplicationID != nil {
		conditions = append(conditions, fmt.Sprintf("p.application_id = $%d", argNum))
		args = append(args, *req.ApplicationID)
		argNum++
	}

	if len(req.Statuses) > 0 {
		placeholders := make([]string, len(req.Statuses))
		for i, st := range req.Statuses {
			placeholders[i] = fmt.Sprintf("$%d", argNum)
			args = append(args, st)
			argNum++
		}
		conditions = append(conditions, fmt.Sprintf("p.status IN (%s)", strings.Join(placeholders, ",")))
	}

	if req.PipelineStartFrom != nil {
		if t, err := time.Parse(time.RFC3339, *req.PipelineStartFrom); err == nil {
			conditions = append(conditions, fmt.Sprintf("p.created_at >= $%d", argNum))
			args = append(args, t)
			argNum++
		}
	}

	if req.PipelineStartTo != nil {
		if t, err := time.Parse(time.RFC3339, *req.PipelineStartTo); err == nil {
			conditions = append(conditions, fmt.Sprintf("p.created_at <= $%d", argNum))
			args = append(args, t)
			argNum++
		}
	}

	if req.PipelineEndFrom != nil {
		if t, err := time.Parse(time.RFC3339, *req.PipelineEndFrom); err == nil {
			conditions = append(conditions, fmt.Sprintf("p.finished_at >= $%d", argNum))
			args = append(args, t)
			argNum++
		}
	}

	if req.PipelineEndTo != nil {
		if t, err := time.Parse(time.RFC3339, *req.PipelineEndTo); err == nil {
			conditions = append(conditions, fmt.Sprintf("p.finished_at <= $%d", argNum))
			args = append(args, t)
			argNum++
		}
	}

	// Full-text search across keyword values, pipeline name, stage name/description
	if req.Search != nil && *req.Search != "" {
		searchPattern := "%" + *req.Search + "%"
		conditions = append(conditions, fmt.Sprintf(`(
			p.name ILIKE $%d
			OR EXISTS (
				SELECT 1 FROM stage s2 WHERE s2.pipeline_id = p.id
				AND (s2.name ILIKE $%d OR s2.description ILIKE $%d)
			)
			OR EXISTS (
				SELECT 1 FROM pipeline_keyword pk
				JOIN keyword k ON k.id = pk.keyword_id
				WHERE pk.pipeline_id = p.id AND k.value ILIKE $%d
			)
		)`, argNum, argNum, argNum, argNum))
		args = append(args, searchPattern)
		argNum++
	}

	// Keyword filter
	if len(req.Keywords) > 0 {
		keywordPlaceholders := make([]string, len(req.Keywords))
		for i, kw := range req.Keywords {
			keywordPlaceholders[i] = fmt.Sprintf("$%d", argNum)
			args = append(args, kw)
			argNum++
		}
		conditions = append(conditions, fmt.Sprintf(`
			EXISTS (
				SELECT 1 FROM pipeline_keyword pk
				JOIN keyword k ON k.id = pk.keyword_id
				WHERE pk.pipeline_id = p.id AND k.key IN (%s)
			)
		`, strings.Join(keywordPlaceholders, ",")))
	}

	whereClause := strings.Join(conditions, " AND ")

	// Count total
	var totalCount int
	countQuery := fmt.Sprintf(`SELECT COUNT(*) FROM pipeline p WHERE %s`, whereClause)
	err := s.db.QueryRowContext(ctx, countQuery, args...).Scan(&totalCount)
	if err != nil {
		return nil, fmt.Errorf("count pipelines: %w", err)
	}

	// Get pipelines
	args = append(args, pageSize, offset)
	query := fmt.Sprintf(`
		SELECT p.id, p.name, COALESCE(p.trace_id, '') AS trace_id, p.status, p.created_at, p.finished_at, p.application_id
		FROM pipeline p
		WHERE %s
		ORDER BY p.created_at DESC
		LIMIT $%d OFFSET $%d
	`, whereClause, argNum, argNum+1)

	rows, err := s.db.QueryxContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("query pipelines: %w", err)
	}
	defer rows.Close()

	pipelines := []types.PipelineResponse{}
	pipelineIDs := []int{}
	for rows.Next() {
		var p struct {
			ID            int        `db:"id"`
			Name          string     `db:"name"`
			TraceID       string     `db:"trace_id"`
			Status        *string    `db:"status"`
			CreatedAt     time.Time  `db:"created_at"`
			FinishedAt    *time.Time `db:"finished_at"`
			ApplicationID *int       `db:"application_id"`
		}
		if err := rows.StructScan(&p); err != nil {
			continue
		}

		status := types.PipelineStatusNotStarted
		if p.Status != nil {
			status = *p.Status
		}

		pipeline := types.PipelineResponse{
			ID:            p.ID,
			Name:          p.Name,
			TraceID:       p.TraceID,
			Status:        status,
			CreatedAt:     p.CreatedAt,
			FinishedAt:    p.FinishedAt,
			ApplicationID: p.ApplicationID,
		}

		pipelines = append(pipelines, pipeline)
		pipelineIDs = append(pipelineIDs, p.ID)
	}

	// Load all stages for all pipelines in one query
	if len(pipelineIDs) > 0 {
		stagesByPipeline, err := s.GetStagesForPipelines(ctx, pipelineIDs)
		if err != nil {
			return nil, fmt.Errorf("load stages: %w", err)
		}
		for i := range pipelines {
			stages := stagesByPipeline[pipelines[i].ID]
			if stages == nil {
				stages = []types.StageResponse{}
			}
			pipelines[i].Stages = stages
		}
	}

	return &types.PagedResult[types.PipelineResponse]{
		Items:      pipelines,
		TotalCount: totalCount,
		PageNumber: pageNumber,
		PageSize:   pageSize,
	}, nil
}

func (s *Store) RerunStage(ctx context.Context, stageID int, rerunAllNext bool) error {
	tx, err := s.db.BeginTxx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() {
		if err != nil {
			_ = tx.Rollback()
		}
	}()

	// Get pipeline ID
	var pipelineID int
	err = tx.QueryRowContext(ctx, `SELECT pipeline_id FROM stage WHERE id = $1`, stageID).Scan(&pipelineID)
	if err != nil {
		return fmt.Errorf("get pipeline id: %w", err)
	}

	affectedStages := make([]struct {
		ID     int    `db:"id"`
		Status string `db:"status"`
	}, 0)
	if rerunAllNext {
		if err = tx.SelectContext(ctx, &affectedStages, `
			SELECT id, status
			FROM stage
			WHERE pipeline_id = $1 AND id >= $2
			ORDER BY id
		`, pipelineID, stageID); err != nil {
			return fmt.Errorf("load stages to rerun: %w", err)
		}
	} else {
		if err = tx.SelectContext(ctx, &affectedStages, `
			SELECT id, status
			FROM stage
			WHERE id = $1
		`, stageID); err != nil {
			return fmt.Errorf("load stage to rerun: %w", err)
		}
	}

	// Reset the stage
	_, err = tx.ExecContext(ctx, `
		UPDATE stage
		SET status = $1, started_at = NULL, finished_at = NULL, is_skipped = false, retry_attempt = 0, next_retry_at = NULL
		WHERE id = $2
	`, types.StageStatusNotStarted, stageID)
	if err != nil {
		return fmt.Errorf("reset stage: %w", err)
	}

	// Clear output
	_, _ = tx.ExecContext(ctx, `UPDATE stage_io SET output = NULL WHERE stage_id = $1`, stageID)

	if rerunAllNext {
		// Reset all subsequent stages
		_, err = tx.ExecContext(ctx, `
			UPDATE stage
			SET status = $1, started_at = NULL, finished_at = NULL, is_skipped = false, retry_attempt = 0, next_retry_at = NULL
			WHERE pipeline_id = $2 AND id > $3
		`, types.StageStatusNotStarted, pipelineID, stageID)
		if err != nil {
			return fmt.Errorf("reset next stages: %w", err)
		}

		// Clear outputs of subsequent stages
		_, _ = tx.ExecContext(ctx, `
			UPDATE stage_io SET output = NULL
			WHERE stage_id IN (SELECT id FROM stage WHERE pipeline_id = $1 AND id > $2)
		`, pipelineID, stageID)
	}

	// Reset pipeline status
	_, err = tx.ExecContext(ctx, `
		UPDATE pipeline SET status = $1, is_completed = false, finished_at = NULL
		WHERE id = $2
	`, types.PipelineStatusRunning, pipelineID)
	if err != nil {
		return fmt.Errorf("reset pipeline: %w", err)
	}

	if err = tx.Commit(); err != nil {
		return err
	}

	for _, stage := range affectedStages {
		if stage.Status != types.StageStatusNotStarted {
			s.LogStageChange(ctx, pipelineID, stage.ID, stage.Status, types.StageStatusNotStarted, "rerun_stage")
		}
	}

	return nil
}

func (s *Store) SkipStage(ctx context.Context, stageID int) error {
	tx, err := s.db.BeginTxx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() {
		if err != nil {
			_ = tx.Rollback()
		}
	}()

	var oldStatus string
	var pipelineID int
	err = tx.QueryRowContext(ctx, `
		SELECT status, pipeline_id
		FROM stage
		WHERE id = $1
	`, stageID).Scan(&oldStatus, &pipelineID)
	if err != nil {
		return fmt.Errorf("load stage before skip: %w", err)
	}

	_, err = tx.ExecContext(ctx, `
		UPDATE stage SET status = $1, is_skipped = true, finished_at = NOW(), next_retry_at = NULL
		WHERE id = $2
	`, types.StageStatusSkipped, stageID)
	if err != nil {
		return fmt.Errorf("skip stage: %w", err)
	}

	// Recompute pipeline status after skip
	var stageStatuses []string
	if err = sqlx.SelectContext(ctx, tx, &stageStatuses, `SELECT status FROM stage WHERE pipeline_id=$1 ORDER BY id`, pipelineID); err != nil {
		return fmt.Errorf("load stage statuses after skip: %w", err)
	}
	newPipelineStatus := computePipelineStatus(stageStatuses)
	var lastStageID int
	if err = tx.GetContext(ctx, &lastStageID, `SELECT MAX(id) FROM stage WHERE pipeline_id=$1`, pipelineID); err != nil {
		return fmt.Errorf("get last stage: %w", err)
	}
	isLast := stageID == lastStageID
	if isLast {
		_, err = tx.ExecContext(ctx, `UPDATE pipeline SET status=$1, is_completed=true, finished_at=NOW() WHERE id=$2`, newPipelineStatus, pipelineID)
	} else {
		_, err = tx.ExecContext(ctx, `UPDATE pipeline SET status=$1, is_completed=false WHERE id=$2`, newPipelineStatus, pipelineID)
	}
	if err != nil {
		return fmt.Errorf("update pipeline status after skip: %w", err)
	}

	if err = tx.Commit(); err != nil {
		return err
	}

	if oldStatus != types.StageStatusSkipped {
		s.LogStageChange(ctx, pipelineID, stageID, oldStatus, types.StageStatusSkipped, "skip_stage")
	}

	return nil
}

func (s *Store) GetStagesForPipelines(ctx context.Context, pipelineIDs []int) (map[int][]types.StageResponse, error) {
	query, args, err := sqlx.In(`
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
		WHERE s.pipeline_id IN (?)
		ORDER BY s.pipeline_id, s.id
	`, pipelineIDs)
	if err != nil {
		return nil, fmt.Errorf("build stages query: %w", err)
	}

	query = s.db.Rebind(query)

	stages := []types.StageResponse{}
	if err := s.db.SelectContext(ctx, &stages, query, args...); err != nil {
		return nil, fmt.Errorf("query stages: %w", err)
	}

	result := make(map[int][]types.StageResponse, len(pipelineIDs))
	for i := range stages {
		pid := stages[i].PipelineID
		result[pid] = append(result[pid], stages[i])
	}

	// Set NextStageID for each pipeline's stages
	for pid := range result {
		pStages := result[pid]
		for i := range pStages {
			if i < len(pStages)-1 {
				next := pStages[i+1].ID
				pStages[i].NextStageID = &next
			}
		}
	}

	return result, nil
}

func parseQueryInt(value string) *int {
	if value == "" {
		return nil
	}
	if i, err := strconv.Atoi(value); err == nil {
		return &i
	}
	return nil
}
