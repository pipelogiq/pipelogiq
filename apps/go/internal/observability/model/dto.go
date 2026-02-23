package model

type IntegrationConfigDTO struct {
	Type          IntegrationType `json:"type"`
	Status        string          `json:"status"`
	Config        map[string]any  `json:"config,omitempty"`
	LastTestedAt  *string         `json:"lastTestedAt,omitempty"`
	LastSuccessAt *string         `json:"lastSuccessAt,omitempty"`
	LastError     *string         `json:"lastError,omitempty"`
}

type ObservabilityConfigResponse struct {
	Integrations []IntegrationConfigDTO `json:"integrations"`
}

type OtelStatus struct {
	Configured          bool    `json:"configured"`
	Connected           bool    `json:"connected"`
	LastSuccessExportAt *string `json:"lastSuccessExportAt,omitempty"`
	ExportRatePerMin    float64 `json:"exportRatePerMin"`
	DropRate            float64 `json:"dropRate"`
	LastError           *string `json:"lastError,omitempty"`
}

type PrometheusStatus struct {
	Configured     bool    `json:"configured"`
	Connected      bool    `json:"connected"`
	ScrapeEndpoint *string `json:"scrapeEndpoint,omitempty"`
	LastScrapeAt   *string `json:"lastScrapeAt,omitempty"`
}

type LogsStatus struct {
	Configured             bool    `json:"configured"`
	Provider               *string `json:"provider,omitempty"`
	LinkTemplateConfigured bool    `json:"linkTemplateConfigured"`
}

type AlertingStatus struct {
	Configured bool     `json:"configured"`
	Channels   []string `json:"channels"`
	Events     []string `json:"events"`
}

type ObservabilityStatusResponse struct {
	Otel       OtelStatus       `json:"otel"`
	Prometheus PrometheusStatus `json:"prometheus"`
	Logs       LogsStatus       `json:"logs"`
	Alerting   AlertingStatus   `json:"alerting"`
}

type SaveConfigRequest struct {
	Type   string         `json:"type"`
	Config map[string]any `json:"config"`
}

type TestConnectionRequest struct {
	Type string `json:"type"`
}

type TestConnectionResult struct {
	Success   bool   `json:"success"`
	Message   string `json:"message"`
	LatencyMs *int   `json:"latencyMs,omitempty"`
}

type TraceEntry struct {
	TraceID      string `json:"traceId"`
	ExecutionID  *int   `json:"executionId,omitempty"`
	PipelineName string `json:"pipelineName"`
	Status       string `json:"status"`
	DurationMs   int    `json:"durationMs"`
	SpansCount   int    `json:"spansCount"`
	Timestamp    string `json:"timestamp"`
}

type SlowestStage struct {
	PipelineName string `json:"pipelineName"`
	StageName    string `json:"stageName"`
	P95Ms        int    `json:"p95Ms"`
}

type ErrorHotspot struct {
	PipelineName string  `json:"pipelineName"`
	StageName    string  `json:"stageName"`
	FailureRate  float64 `json:"failureRate"`
	AvgRetries   float64 `json:"avgRetries"`
}

type InsightsSummary struct {
	ExecutionsPerMin float64 `json:"executionsPerMin"`
	FailuresPerMin   float64 `json:"failuresPerMin"`
	SuccessRate      float64 `json:"successRate"`
	AvgStageMs       float64 `json:"avgStageMs"`
}

type InsightsResponse struct {
	SlowestStages []SlowestStage  `json:"slowestStages"`
	ErrorHotspots []ErrorHotspot  `json:"errorHotspots"`
	Summary       InsightsSummary `json:"summary"`
}

type ErrorEnvelope struct {
	Error APIError `json:"error"`
}

type APIError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
	Details any    `json:"details,omitempty"`
}
