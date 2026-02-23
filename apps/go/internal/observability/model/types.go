package model

import (
	"strings"
	"time"
)

type IntegrationType string

const (
	IntegrationTypeOpenTelemetry IntegrationType = "opentelemetry"
	IntegrationTypePrometheus    IntegrationType = "prometheus"
	IntegrationTypeAlerting      IntegrationType = "alerting"
	IntegrationTypeGrafana       IntegrationType = "grafana"
	IntegrationTypeSentry        IntegrationType = "sentry"
	IntegrationTypeDatadog       IntegrationType = "datadog"
	IntegrationTypeGraylog       IntegrationType = "graylog"
)

var SupportedIntegrationTypes = []IntegrationType{
	IntegrationTypeOpenTelemetry,
	IntegrationTypeAlerting,
	IntegrationTypeGrafana,
	IntegrationTypeSentry,
	IntegrationTypeDatadog,
	IntegrationTypeGraylog,
}

func ParseIntegrationType(raw string) (IntegrationType, bool) {
	candidate := IntegrationType(strings.ToLower(strings.TrimSpace(raw)))
	for _, integrationType := range SupportedIntegrationTypes {
		if integrationType == candidate {
			return integrationType, true
		}
	}
	return "", false
}

type IntegrationStatus string

const (
	IntegrationStatusNotConfigured IntegrationStatus = "not_configured"
	IntegrationStatusConfigured    IntegrationStatus = "configured"
	IntegrationStatusConnected     IntegrationStatus = "connected"
	IntegrationStatusDisconnected  IntegrationStatus = "disconnected"
	IntegrationStatusError         IntegrationStatus = "error"
)

type IntegrationHealth struct {
	Type             IntegrationType
	LastTestedAt     *time.Time
	LastSuccessAt    *time.Time
	LastError        *string
	ExportRatePerMin float64
	DropRate         float64
}

type Integration struct {
	Type      IntegrationType
	Config    map[string]any
	Status    IntegrationStatus
	CreatedAt time.Time
	UpdatedAt time.Time
	Health    IntegrationHealth
}

type TraceFilter struct {
	Search string
	Status string
	Since  *time.Time
	Limit  int
}

type TraceRecord struct {
	PipelineID   int
	PipelineName string
	TraceID      string
	Status       string
	CreatedAt    time.Time
	FinishedAt   *time.Time
	SpansCount   int
}

type StageMetricRecord struct {
	PipelineName string
	StageName    string
	Status       string
	StartedAt    *time.Time
	FinishedAt   *time.Time
}

type PipelineSummaryRecord struct {
	Status string
}
