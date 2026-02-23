package repo

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/jmoiron/sqlx"

	"pipelogiq/internal/observability/model"
)

type SQLRepository struct {
	db *sqlx.DB
}

func NewSQLRepository(db *sqlx.DB) *SQLRepository {
	return &SQLRepository{db: db}
}

func (r *SQLRepository) EnsureIntegrations(ctx context.Context, types []model.IntegrationType) error {
	now := time.Now().UTC()

	configQuery := r.db.Rebind(`
		INSERT INTO observability_integration_config (type, config_json, status, created_at, updated_at)
		VALUES (?, '{}', 'not_configured', ?, ?)
		ON CONFLICT(type) DO NOTHING
	`)

	healthQuery := r.db.Rebind(`
		INSERT INTO observability_integration_health (type)
		VALUES (?)
		ON CONFLICT(type) DO NOTHING
	`)

	for _, integrationType := range types {
		if _, err := r.db.ExecContext(ctx, configQuery, string(integrationType), now, now); err != nil {
			return fmt.Errorf("ensure integration config row (%s): %w", integrationType, err)
		}
		if _, err := r.db.ExecContext(ctx, healthQuery, string(integrationType)); err != nil {
			return fmt.Errorf("ensure integration health row (%s): %w", integrationType, err)
		}
	}

	return nil
}

func (r *SQLRepository) ListIntegrations(ctx context.Context) ([]model.Integration, error) {
	rows := []integrationRow{}
	query := `
		SELECT
			c.type,
			c.config_json,
			c.status,
			c.created_at,
			c.updated_at,
			h.last_tested_at,
			h.last_success_at,
			h.last_error,
			COALESCE(h.export_rate_per_min, 0) AS export_rate_per_min,
			COALESCE(h.drop_rate, 0) AS drop_rate
		FROM observability_integration_config c
		LEFT JOIN observability_integration_health h ON h.type = c.type
	`

	if err := r.db.SelectContext(ctx, &rows, query); err != nil {
		return nil, err
	}

	integrations := make([]model.Integration, 0, len(rows))
	for _, row := range rows {
		integration, err := toIntegration(row)
		if err != nil {
			return nil, err
		}
		integrations = append(integrations, integration)
	}

	return integrations, nil
}

func (r *SQLRepository) GetIntegration(ctx context.Context, integrationType model.IntegrationType) (*model.Integration, error) {
	var row integrationRow
	query := r.db.Rebind(`
		SELECT
			c.type,
			c.config_json,
			c.status,
			c.created_at,
			c.updated_at,
			h.last_tested_at,
			h.last_success_at,
			h.last_error,
			COALESCE(h.export_rate_per_min, 0) AS export_rate_per_min,
			COALESCE(h.drop_rate, 0) AS drop_rate
		FROM observability_integration_config c
		LEFT JOIN observability_integration_health h ON h.type = c.type
		WHERE c.type = ?
		LIMIT 1
	`)

	if err := r.db.GetContext(ctx, &row, query, string(integrationType)); err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, err
	}

	integration, err := toIntegration(row)
	if err != nil {
		return nil, err
	}

	return &integration, nil
}

func (r *SQLRepository) UpsertIntegrationConfig(
	ctx context.Context,
	integrationType model.IntegrationType,
	config map[string]any,
	status model.IntegrationStatus,
) error {
	configJSON, err := json.Marshal(config)
	if err != nil {
		return fmt.Errorf("marshal config json: %w", err)
	}

	now := time.Now().UTC()
	query := r.db.Rebind(`
		INSERT INTO observability_integration_config (type, config_json, status, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?)
		ON CONFLICT(type) DO UPDATE SET
			config_json = excluded.config_json,
			status = excluded.status,
			updated_at = excluded.updated_at
	`)

	if _, err := r.db.ExecContext(ctx, query, string(integrationType), string(configJSON), string(status), now, now); err != nil {
		return err
	}

	return r.ensureHealthRow(ctx, integrationType)
}

func (r *SQLRepository) UpdateIntegrationStatus(ctx context.Context, integrationType model.IntegrationType, status model.IntegrationStatus) error {
	query := r.db.Rebind(`
		UPDATE observability_integration_config
		SET status = ?, updated_at = ?
		WHERE type = ?
	`)
	_, err := r.db.ExecContext(ctx, query, string(status), time.Now().UTC(), string(integrationType))
	return err
}

func (r *SQLRepository) RecordHealthSuccess(ctx context.Context, integrationType model.IntegrationType, testedAt time.Time) error {
	if err := r.ensureHealthRow(ctx, integrationType); err != nil {
		return err
	}

	query := r.db.Rebind(`
		UPDATE observability_integration_health
		SET
			last_tested_at = ?,
			last_success_at = ?,
			last_error = NULL
		WHERE type = ?
	`)

	_, err := r.db.ExecContext(ctx, query, testedAt.UTC(), testedAt.UTC(), string(integrationType))
	return err
}

func (r *SQLRepository) RecordHealthFailure(
	ctx context.Context,
	integrationType model.IntegrationType,
	testedAt time.Time,
	message string,
) error {
	if err := r.ensureHealthRow(ctx, integrationType); err != nil {
		return err
	}

	query := r.db.Rebind(`
		UPDATE observability_integration_health
		SET
			last_tested_at = ?,
			last_error = ?
		WHERE type = ?
	`)

	_, err := r.db.ExecContext(ctx, query, testedAt.UTC(), strings.TrimSpace(message), string(integrationType))
	return err
}

func (r *SQLRepository) ListTraces(ctx context.Context, filter model.TraceFilter) ([]model.TraceRecord, error) {
	limit := filter.Limit
	if limit <= 0 {
		limit = 50
	}

	builder := strings.Builder{}
	builder.WriteString(`
		SELECT
			p.id AS pipeline_id,
			COALESCE(p.name, '') AS pipeline_name,
			COALESCE(p.trace_id, '') AS trace_id,
			COALESCE(p.status, 'NotStarted') AS status,
			p.created_at AS created_at,
			p.finished_at AS finished_at,
			COUNT(s.id) AS spans_count
		FROM pipeline p
		LEFT JOIN stage s ON s.pipeline_id = p.id
		WHERE 1=1
	`)

	args := make([]any, 0)
	if filter.Search != "" {
		builder.WriteString(`
			AND (
				LOWER(COALESCE(p.name, '')) LIKE ?
				OR CAST(p.id AS TEXT) = ?
				OR LOWER(COALESCE(p.trace_id, '')) LIKE ?
			)
		`)
		searchPattern := "%" + strings.ToLower(strings.TrimSpace(filter.Search)) + "%"
		exactID := strings.TrimSpace(filter.Search)
		args = append(args, searchPattern, exactID, searchPattern)
	}

	if filter.Status != "" && filter.Status != "all" {
		if dbStatus, ok := mapTraceStatusFilter(filter.Status); ok {
			builder.WriteString(` AND p.status = ? `)
			args = append(args, dbStatus)
		}
	}

	if filter.Since != nil {
		builder.WriteString(` AND p.created_at >= ? `)
		args = append(args, filter.Since.UTC())
	}

	builder.WriteString(`
		GROUP BY p.id, p.name, p.trace_id, p.status, p.created_at, p.finished_at
		ORDER BY p.created_at DESC
		LIMIT ?
	`)
	args = append(args, limit)

	query := r.db.Rebind(builder.String())
	rows := []traceRow{}
	if err := r.db.SelectContext(ctx, &rows, query, args...); err != nil {
		return nil, err
	}

	result := make([]model.TraceRecord, 0, len(rows))
	for _, row := range rows {
		result = append(result, model.TraceRecord{
			PipelineID:   row.PipelineID,
			PipelineName: row.PipelineName,
			TraceID:      row.TraceID,
			Status:       row.Status,
			CreatedAt:    row.CreatedAt,
			FinishedAt:   nullTimeToPtr(row.FinishedAt),
			SpansCount:   row.SpansCount,
		})
	}

	return result, nil
}

func (r *SQLRepository) ListStageMetrics(ctx context.Context, since time.Time) ([]model.StageMetricRecord, error) {
	query := r.db.Rebind(`
		SELECT
			COALESCE(p.name, '') AS pipeline_name,
			COALESCE(s.name, '') AS stage_name,
			COALESCE(s.status, '') AS status,
			s.started_at,
			s.finished_at
		FROM stage s
		JOIN pipeline p ON p.id = s.pipeline_id
		WHERE s.started_at IS NOT NULL
		  AND s.started_at >= ?
	`)

	rows := []stageMetricRow{}
	if err := r.db.SelectContext(ctx, &rows, query, since.UTC()); err != nil {
		return nil, err
	}

	result := make([]model.StageMetricRecord, 0, len(rows))
	for _, row := range rows {
		result = append(result, model.StageMetricRecord{
			PipelineName: row.PipelineName,
			StageName:    row.StageName,
			Status:       row.Status,
			StartedAt:    nullTimeToPtr(row.StartedAt),
			FinishedAt:   nullTimeToPtr(row.FinishedAt),
		})
	}

	return result, nil
}

func (r *SQLRepository) ListPipelineSummaries(ctx context.Context, since time.Time) ([]model.PipelineSummaryRecord, error) {
	query := r.db.Rebind(`
		SELECT COALESCE(status, '') AS status
		FROM pipeline
		WHERE created_at >= ?
	`)

	rows := []pipelineSummaryRow{}
	if err := r.db.SelectContext(ctx, &rows, query, since.UTC()); err != nil {
		return nil, err
	}

	result := make([]model.PipelineSummaryRecord, 0, len(rows))
	for _, row := range rows {
		result = append(result, model.PipelineSummaryRecord{Status: row.Status})
	}
	return result, nil
}

func (r *SQLRepository) ensureHealthRow(ctx context.Context, integrationType model.IntegrationType) error {
	query := r.db.Rebind(`
		INSERT INTO observability_integration_health (type)
		VALUES (?)
		ON CONFLICT(type) DO NOTHING
	`)
	_, err := r.db.ExecContext(ctx, query, string(integrationType))
	return err
}

func mapTraceStatusFilter(filter string) (string, bool) {
	switch strings.ToLower(strings.TrimSpace(filter)) {
	case "success":
		return "Completed", true
	case "error":
		return "Failed", true
	case "running":
		return "Running", true
	default:
		return "", false
	}
}

type integrationRow struct {
	Type             string         `db:"type"`
	ConfigJSON       string         `db:"config_json"`
	Status           string         `db:"status"`
	CreatedAt        time.Time      `db:"created_at"`
	UpdatedAt        time.Time      `db:"updated_at"`
	LastTestedAt     sql.NullTime   `db:"last_tested_at"`
	LastSuccessAt    sql.NullTime   `db:"last_success_at"`
	LastError        sql.NullString `db:"last_error"`
	ExportRatePerMin float64        `db:"export_rate_per_min"`
	DropRate         float64        `db:"drop_rate"`
}

func toIntegration(row integrationRow) (model.Integration, error) {
	config := map[string]any{}
	if strings.TrimSpace(row.ConfigJSON) != "" {
		if err := json.Unmarshal([]byte(row.ConfigJSON), &config); err != nil {
			return model.Integration{}, fmt.Errorf("unmarshal config for %s: %w", row.Type, err)
		}
	}

	integrationType, _ := model.ParseIntegrationType(row.Type)
	status := model.IntegrationStatus(row.Status)

	return model.Integration{
		Type:      integrationType,
		Config:    config,
		Status:    status,
		CreatedAt: row.CreatedAt,
		UpdatedAt: row.UpdatedAt,
		Health: model.IntegrationHealth{
			Type:             integrationType,
			LastTestedAt:     nullTimeToPtr(row.LastTestedAt),
			LastSuccessAt:    nullTimeToPtr(row.LastSuccessAt),
			LastError:        nullStringToPtr(row.LastError),
			ExportRatePerMin: row.ExportRatePerMin,
			DropRate:         row.DropRate,
		},
	}, nil
}

type traceRow struct {
	PipelineID   int          `db:"pipeline_id"`
	PipelineName string       `db:"pipeline_name"`
	TraceID      string       `db:"trace_id"`
	Status       string       `db:"status"`
	CreatedAt    time.Time    `db:"created_at"`
	FinishedAt   sql.NullTime `db:"finished_at"`
	SpansCount   int          `db:"spans_count"`
}

type stageMetricRow struct {
	PipelineName string       `db:"pipeline_name"`
	StageName    string       `db:"stage_name"`
	Status       string       `db:"status"`
	StartedAt    sql.NullTime `db:"started_at"`
	FinishedAt   sql.NullTime `db:"finished_at"`
}

type pipelineSummaryRow struct {
	Status string `db:"status"`
}

func nullTimeToPtr(value sql.NullTime) *time.Time {
	if !value.Valid {
		return nil
	}
	copy := value.Time.UTC()
	return &copy
}

func nullStringToPtr(value sql.NullString) *string {
	if !value.Valid {
		return nil
	}
	trimmed := strings.TrimSpace(value.String)
	if trimmed == "" {
		return nil
	}
	return &trimmed
}
