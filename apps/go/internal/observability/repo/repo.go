package repo

import (
	"context"
	"time"

	"pipelogiq/internal/observability/model"
)

type Repository interface {
	EnsureIntegrations(ctx context.Context, types []model.IntegrationType) error
	ListIntegrations(ctx context.Context) ([]model.Integration, error)
	GetIntegration(ctx context.Context, integrationType model.IntegrationType) (*model.Integration, error)
	UpsertIntegrationConfig(ctx context.Context, integrationType model.IntegrationType, config map[string]any, status model.IntegrationStatus) error
	UpdateIntegrationStatus(ctx context.Context, integrationType model.IntegrationType, status model.IntegrationStatus) error
	RecordHealthSuccess(ctx context.Context, integrationType model.IntegrationType, testedAt time.Time) error
	RecordHealthFailure(ctx context.Context, integrationType model.IntegrationType, testedAt time.Time, message string) error

	ListTraces(ctx context.Context, filter model.TraceFilter) ([]model.TraceRecord, error)
	ListStageMetrics(ctx context.Context, since time.Time) ([]model.StageMetricRecord, error)
	ListPipelineSummaries(ctx context.Context, since time.Time) ([]model.PipelineSummaryRecord, error)
}
