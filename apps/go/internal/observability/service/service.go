package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"math"
	"net"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"

	"pipelogiq/internal/alerts"
	"pipelogiq/internal/observability/model"
	"pipelogiq/internal/observability/repo"
)

const (
	defaultFreshnessWindow = 10 * time.Minute
	defaultTestTimeout     = 5 * time.Second
	maxConfigPayloadBytes  = 64 * 1024
)

type Interface interface {
	GetConfig(ctx context.Context) (model.ObservabilityConfigResponse, error)
	SaveConfig(ctx context.Context, req model.SaveConfigRequest) (model.ObservabilityConfigResponse, error)
	GetStatus(ctx context.Context) (model.ObservabilityStatusResponse, error)
	TestConnection(ctx context.Context, req model.TestConnectionRequest) (model.TestConnectionResult, error)
	GetTraces(ctx context.Context, search, status, timeRange string) ([]model.TraceEntry, error)
	GetInsights(ctx context.Context, timeRange string) (model.InsightsResponse, error)
}

type Service struct {
	repo            repo.Repository
	logger          *slog.Logger
	httpClient      *http.Client
	freshnessWindow time.Duration
	testTimeout     time.Duration
}

type AppError struct {
	Code    string
	Message string
	Details any
}

func (e *AppError) Error() string {
	return e.Message
}

func New(repo repo.Repository, logger *slog.Logger) *Service {
	if logger == nil {
		logger = slog.Default()
	}

	return &Service{
		repo:   repo,
		logger: logger,
		httpClient: &http.Client{
			Timeout: defaultTestTimeout,
		},
		freshnessWindow: defaultFreshnessWindow,
		testTimeout:     defaultTestTimeout,
	}
}

func (s *Service) GetConfig(ctx context.Context) (model.ObservabilityConfigResponse, error) {
	integrations, err := s.listOrderedIntegrations(ctx)
	if err != nil {
		return model.ObservabilityConfigResponse{}, err
	}

	result := model.ObservabilityConfigResponse{Integrations: make([]model.IntegrationConfigDTO, 0, len(integrations))}
	for _, integration := range integrations {
		status := computeIntegrationStatus(integration.Type, integration.Config, integration.Health, s.freshnessWindow, time.Now().UTC())
		config := copyMap(integration.Config)
		if len(config) == 0 {
			config = nil
		}

		result.Integrations = append(result.Integrations, model.IntegrationConfigDTO{
			Type:          integration.Type,
			Status:        string(status),
			Config:        config,
			LastTestedAt:  formatTimePtr(integration.Health.LastTestedAt),
			LastSuccessAt: formatTimePtr(integration.Health.LastSuccessAt),
			LastError:     integration.Health.LastError,
		})
	}

	return result, nil
}

func (s *Service) SaveConfig(ctx context.Context, req model.SaveConfigRequest) (model.ObservabilityConfigResponse, error) {
	integrationType, ok := model.ParseIntegrationType(strings.TrimSpace(req.Type))
	if !ok {
		return model.ObservabilityConfigResponse{}, &AppError{
			Code:    "invalid_integration_type",
			Message: "Unknown integration type",
			Details: map[string]any{"type": req.Type},
		}
	}

	config := copyMap(req.Config)
	if config == nil {
		config = map[string]any{}
	}

	if err := validateConfigSize(config); err != nil {
		return model.ObservabilityConfigResponse{}, err
	}

	if err := validateConfigByType(integrationType, config, false); err != nil {
		return model.ObservabilityConfigResponse{}, err
	}

	if err := s.repo.EnsureIntegrations(ctx, model.SupportedIntegrationTypes); err != nil {
		return model.ObservabilityConfigResponse{}, err
	}

	existing, err := s.repo.GetIntegration(ctx, integrationType)
	if err != nil {
		return model.ObservabilityConfigResponse{}, err
	}

	health := model.IntegrationHealth{Type: integrationType}
	if existing != nil {
		health = existing.Health
	}

	nextStatus := computeIntegrationStatus(integrationType, config, health, s.freshnessWindow, time.Now().UTC())
	if err := s.repo.UpsertIntegrationConfig(ctx, integrationType, config, nextStatus); err != nil {
		return model.ObservabilityConfigResponse{}, err
	}

	// TODO: avoid storing secrets in plain JSON config; integrate secret storage/env indirection.
	return s.GetConfig(ctx)
}

func (s *Service) GetStatus(ctx context.Context) (model.ObservabilityStatusResponse, error) {
	integrations, err := s.listOrderedIntegrations(ctx)
	if err != nil {
		return model.ObservabilityStatusResponse{}, err
	}

	index := make(map[model.IntegrationType]model.Integration, len(integrations))
	for _, integration := range integrations {
		index[integration.Type] = integration
	}

	otel := index[model.IntegrationTypeOpenTelemetry]
	otelStatus := computeIntegrationStatus(otel.Type, otel.Config, otel.Health, s.freshnessWindow, time.Now().UTC())

	prom := index[model.IntegrationTypePrometheus]
	promStatus := computeIntegrationStatus(prom.Type, prom.Config, prom.Health, s.freshnessWindow, time.Now().UTC())

	alerting := index[model.IntegrationTypeAlerting]
	alertingStatus := computeIntegrationStatus(alerting.Type, alerting.Config, alerting.Health, s.freshnessWindow, time.Now().UTC())

	logs := index[model.IntegrationTypeGraylog]
	logsStatus := computeIntegrationStatus(logs.Type, logs.Config, logs.Health, s.freshnessWindow, time.Now().UTC())

	scrapeEndpoint := optionalString(prom.Config, "scrapeEndpoint")
	if scrapeEndpoint == nil {
		defaultEndpoint := "/metrics"
		scrapeEndpoint = &defaultEndpoint
	}

	provider := optionalString(logs.Config, "provider")
	lastScrapeAt := formatTimePtr(prom.Health.LastSuccessAt)
	alertChannels, _, _ := optionalStringList(alerting.Config, "channels")
	alertEvents, _, _ := optionalStringList(alerting.Config, "enabledEvents")
	if alertChannels == nil {
		alertChannels = []string{}
	}
	if alertEvents == nil {
		alertEvents = []string{}
	}

	return model.ObservabilityStatusResponse{
		Otel: model.OtelStatus{
			Configured:          otelStatus != model.IntegrationStatusNotConfigured,
			Connected:           otelStatus == model.IntegrationStatusConnected,
			LastSuccessExportAt: formatTimePtr(otel.Health.LastSuccessAt),
			ExportRatePerMin:    otel.Health.ExportRatePerMin,
			DropRate:            otel.Health.DropRate,
			LastError:           otel.Health.LastError,
		},
		Prometheus: model.PrometheusStatus{
			Configured:     promStatus != model.IntegrationStatusNotConfigured,
			Connected:      promStatus == model.IntegrationStatusConnected,
			ScrapeEndpoint: scrapeEndpoint,
			LastScrapeAt:   lastScrapeAt,
		},
		Logs: model.LogsStatus{
			Configured:             logsStatus != model.IntegrationStatusNotConfigured,
			Provider:               provider,
			LinkTemplateConfigured: hasNonEmptyString(logs.Config, "searchUrlTemplate"),
		},
		Alerting: model.AlertingStatus{
			Configured: alertingStatus != model.IntegrationStatusNotConfigured,
			Channels:   alertChannels,
			Events:     alertEvents,
		},
	}, nil
}

func (s *Service) TestConnection(ctx context.Context, req model.TestConnectionRequest) (model.TestConnectionResult, error) {
	integrationType, ok := model.ParseIntegrationType(strings.TrimSpace(req.Type))
	if !ok {
		return model.TestConnectionResult{}, &AppError{
			Code:    "invalid_integration_type",
			Message: "Unknown integration type",
			Details: map[string]any{"type": req.Type},
		}
	}

	if err := s.repo.EnsureIntegrations(ctx, model.SupportedIntegrationTypes); err != nil {
		return model.TestConnectionResult{}, err
	}

	integration, err := s.repo.GetIntegration(ctx, integrationType)
	if err != nil {
		return model.TestConnectionResult{}, err
	}
	if integration == nil {
		return model.TestConnectionResult{}, &AppError{
			Code:    "integration_not_found",
			Message: "Integration is not configured",
		}
	}

	if err := validateConfigByType(integrationType, integration.Config, true); err != nil {
		message := "Integration not configured"
		now := time.Now().UTC()
		_ = s.repo.RecordHealthFailure(ctx, integrationType, now, message)
		_ = s.repo.UpdateIntegrationStatus(ctx, integrationType, model.IntegrationStatusNotConfigured)
		return model.TestConnectionResult{
			Success: false,
			Message: message,
		}, nil
	}

	started := time.Now()
	err = s.runConnectivityCheck(ctx, integrationType, integration.Config)
	if err == nil && integrationType == model.IntegrationTypeAlerting {
		err = alerts.New(s.repo, s.logger).SendTestAlert(ctx)
	}
	latencyMs := int(time.Since(started).Milliseconds())
	now := time.Now().UTC()

	if err != nil {
		errMsg := err.Error()
		if updateErr := s.repo.RecordHealthFailure(ctx, integrationType, now, errMsg); updateErr != nil {
			s.logger.Error("record health failure failed", "err", updateErr, "type", integrationType)
		}
		_ = s.repo.UpdateIntegrationStatus(ctx, integrationType, model.IntegrationStatusDisconnected)
		return model.TestConnectionResult{
			Success: false,
			Message: errMsg,
		}, nil
	}

	if updateErr := s.repo.RecordHealthSuccess(ctx, integrationType, now); updateErr != nil {
		s.logger.Error("record health success failed", "err", updateErr, "type", integrationType)
	}
	_ = s.repo.UpdateIntegrationStatus(ctx, integrationType, model.IntegrationStatusConnected)

	successMessage := "Connection established successfully"
	if integrationType == model.IntegrationTypeAlerting {
		successMessage = "Test alert sent successfully"
	}

	return model.TestConnectionResult{
		Success:   true,
		Message:   successMessage,
		LatencyMs: &latencyMs,
	}, nil
}

func (s *Service) GetTraces(ctx context.Context, search, status, timeRange string) ([]model.TraceEntry, error) {
	filter := model.TraceFilter{
		Search: strings.TrimSpace(search),
		Status: strings.TrimSpace(status),
		Limit:  50,
	}
	if since := parseTimeRangeStart(timeRange); since != nil {
		filter.Since = since
	}

	rows, err := s.repo.ListTraces(ctx, filter)
	if err != nil {
		if isMissingTableError(err) {
			return []model.TraceEntry{}, nil
		}
		return nil, err
	}

	entries := make([]model.TraceEntry, 0, len(rows))
	now := time.Now().UTC()
	for _, row := range rows {
		durationEnd := now
		if row.FinishedAt != nil {
			durationEnd = row.FinishedAt.UTC()
		}
		durationMs := int(durationEnd.Sub(row.CreatedAt.UTC()).Milliseconds())
		if durationMs < 0 {
			durationMs = 0
		}

		execID := row.PipelineID
		entries = append(entries, model.TraceEntry{
			TraceID:      row.TraceID,
			ExecutionID:  &execID,
			PipelineName: row.PipelineName,
			Status:       mapPipelineStatusToTraceStatus(row.Status),
			DurationMs:   durationMs,
			SpansCount:   row.SpansCount,
			Timestamp:    row.CreatedAt.UTC().Format(time.RFC3339),
		})
	}

	return entries, nil
}

func (s *Service) GetInsights(ctx context.Context, timeRange string) (model.InsightsResponse, error) {
	rangeDuration := parseTimeRangeDuration(timeRange)
	if rangeDuration <= 0 {
		rangeDuration = time.Hour
	}
	since := time.Now().UTC().Add(-rangeDuration)

	stageMetrics, err := s.repo.ListStageMetrics(ctx, since)
	if err != nil {
		if isMissingTableError(err) {
			return emptyInsights(), nil
		}
		return model.InsightsResponse{}, err
	}

	pipelineSummaries, err := s.repo.ListPipelineSummaries(ctx, since)
	if err != nil {
		if isMissingTableError(err) {
			return emptyInsights(), nil
		}
		return model.InsightsResponse{}, err
	}

	slowestStages, hotspots, avgStageMs := computeStageInsights(stageMetrics)
	summary := computeSummaryInsights(pipelineSummaries, avgStageMs, rangeDuration)

	return model.InsightsResponse{
		SlowestStages: slowestStages,
		ErrorHotspots: hotspots,
		Summary:       summary,
	}, nil
}

func (s *Service) listOrderedIntegrations(ctx context.Context) ([]model.Integration, error) {
	if err := s.repo.EnsureIntegrations(ctx, model.SupportedIntegrationTypes); err != nil {
		return nil, err
	}

	records, err := s.repo.ListIntegrations(ctx)
	if err != nil {
		return nil, err
	}

	byType := make(map[model.IntegrationType]model.Integration, len(records))
	for _, record := range records {
		byType[record.Type] = record
	}

	ordered := make([]model.Integration, 0, len(model.SupportedIntegrationTypes))
	for _, integrationType := range model.SupportedIntegrationTypes {
		record, ok := byType[integrationType]
		if !ok {
			record = model.Integration{Type: integrationType, Config: map[string]any{}, Status: model.IntegrationStatusNotConfigured}
		}
		ordered = append(ordered, record)
	}

	return ordered, nil
}

func (s *Service) runConnectivityCheck(ctx context.Context, integrationType model.IntegrationType, config map[string]any) error {
	switch integrationType {
	case model.IntegrationTypeOpenTelemetry:
		return s.testOpenTelemetry(ctx, config)
	case model.IntegrationTypePrometheus:
		endpoint := requiredString(config, "healthEndpoint")
		if endpoint == "" {
			endpoint = requiredString(config, "scrapeEndpoint")
		}
		if endpoint == "" {
			return errors.New("prometheus scrapeEndpoint is required")
		}
		return s.testHTTPReachability(ctx, endpoint, http.MethodGet, nil)
	case model.IntegrationTypeGraylog:
		endpoint := requiredString(config, "healthEndpoint")
		if endpoint == "" {
			endpoint = requiredString(config, "baseUrl")
		}
		if endpoint == "" {
			return errors.New("graylog baseUrl is required")
		}
		return s.testHTTPReachability(ctx, endpoint, http.MethodGet, nil)
	case model.IntegrationTypeAlerting:
		if endpoint := requiredString(config, "healthEndpoint"); endpoint != "" {
			return s.testHTTPReachability(ctx, endpoint, http.MethodGet, nil)
		}
		for _, field := range []string{"webhookUrl", "slackWebhookUrl", "whatsappWebhookUrl", "teamsWebhookUrl"} {
			if endpoint := requiredString(config, field); endpoint != "" {
				return s.testHTTPReachability(ctx, endpoint, http.MethodPost, nil)
			}
		}
		// Token-based channels (Telegram/PagerDuty/etc.) may not expose a safe generic probe endpoint here.
		// Treat config validation as the test fallback unless an explicit HTTP endpoint is provided.
		return nil
	default:
		return nil
	}
}

func (s *Service) testOpenTelemetry(ctx context.Context, config map[string]any) error {
	endpoint := requiredString(config, "endpoint")
	protocol := strings.ToLower(requiredString(config, "protocol"))

	if endpoint == "" {
		return errors.New("opentelemetry endpoint is required")
	}
	if protocol != "grpc" && protocol != "http" {
		return errors.New("opentelemetry protocol must be grpc or http")
	}

	if protocol == "grpc" {
		hostPort, err := normalizeEndpointHostPort(endpoint, "4317")
		if err != nil {
			return err
		}
		dialer := net.Dialer{Timeout: s.testTimeout}
		conn, err := dialer.DialContext(ctx, "tcp", hostPort)
		if err != nil {
			return fmt.Errorf("grpc reachability failed: %w", err)
		}
		_ = conn.Close()
		return nil
	}

	headers, err := extractHeaders(config)
	if err != nil {
		return err
	}

	return s.testHTTPReachability(ctx, endpoint, http.MethodPost, headers)
}

func (s *Service) testHTTPReachability(ctx context.Context, rawURL string, method string, headers map[string]string) error {
	parsedURL, err := parseHTTPURL(rawURL)
	if err != nil {
		return err
	}

	body := strings.NewReader("")
	if method == http.MethodPost || method == http.MethodPut {
		body = strings.NewReader("{}")
	}

	req, err := http.NewRequestWithContext(ctx, method, parsedURL, body)
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	for key, value := range headers {
		if strings.TrimSpace(key) == "" || strings.TrimSpace(value) == "" {
			continue
		}
		req.Header.Set(key, value)
	}

	resp, err := s.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("http reachability failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 500 {
		return fmt.Errorf("endpoint returned status %d", resp.StatusCode)
	}

	return nil
}

func computeIntegrationStatus(
	integrationType model.IntegrationType,
	config map[string]any,
	health model.IntegrationHealth,
	freshnessWindow time.Duration,
	now time.Time,
) model.IntegrationStatus {
	if integrationType == "" {
		return model.IntegrationStatusNotConfigured
	}

	if err := validateConfigByType(integrationType, config, true); err != nil {
		return model.IntegrationStatusNotConfigured
	}

	if health.LastTestedAt == nil && health.LastSuccessAt == nil {
		return model.IntegrationStatusConfigured
	}

	if health.LastError != nil && strings.TrimSpace(*health.LastError) != "" {
		if health.LastSuccessAt == nil {
			return model.IntegrationStatusDisconnected
		}
		if health.LastTestedAt != nil && health.LastTestedAt.After(*health.LastSuccessAt) {
			return model.IntegrationStatusDisconnected
		}
	}

	if health.LastSuccessAt != nil {
		if health.LastTestedAt == nil || !health.LastTestedAt.After(*health.LastSuccessAt) {
			return model.IntegrationStatusConnected
		}
		if now.Sub(health.LastSuccessAt.UTC()) <= freshnessWindow {
			return model.IntegrationStatusConnected
		}
	}

	return model.IntegrationStatusConfigured
}

func validateConfigByType(integrationType model.IntegrationType, config map[string]any, strict bool) error {
	requiredKeys := map[model.IntegrationType][]string{
		model.IntegrationTypeOpenTelemetry: {"endpoint", "protocol"},
		model.IntegrationTypePrometheus:    {"scrapeEndpoint"},
		model.IntegrationTypeGrafana:       {"dashboardUrl"},
		model.IntegrationTypeSentry:        {"dsn", "environment"},
		model.IntegrationTypeDatadog:       {"site", "apiKey"},
		model.IntegrationTypeGraylog:       {"baseUrl", "provider", "searchUrlTemplate"},
	}

	for _, key := range requiredKeys[integrationType] {
		if !hasNonEmptyString(config, key) {
			if strict {
				return &AppError{
					Code:    "integration_not_configured",
					Message: "Integration is not configured",
					Details: map[string]any{"missingKey": key, "type": integrationType},
				}
			}
		}
	}

	if integrationType == model.IntegrationTypeOpenTelemetry {
		protocol := strings.ToLower(strings.TrimSpace(requiredString(config, "protocol")))
		if protocol != "" && protocol != "grpc" && protocol != "http" {
			return &AppError{
				Code:    "invalid_config",
				Message: "OpenTelemetry protocol must be grpc or http",
				Details: map[string]any{"type": integrationType},
			}
		}

		if _, err := extractHeaders(config); err != nil {
			return err
		}
		if _, ok := optionalBool(config, "tlsInsecure"); !ok && config != nil {
			if _, exists := config["tlsInsecure"]; exists {
				return &AppError{
					Code:    "invalid_config",
					Message: "OpenTelemetry tlsInsecure must be a boolean",
					Details: map[string]any{"type": integrationType, "field": "tlsInsecure"},
				}
			}
		}
	}

	if integrationType == model.IntegrationTypePrometheus {
		if interval, ok := optionalFloat(config, "scrapeIntervalSeconds"); ok && interval <= 0 {
			return &AppError{
				Code:    "invalid_config",
				Message: "Prometheus scrapeIntervalSeconds must be greater than 0",
				Details: map[string]any{"type": integrationType},
			}
		}
	}

	if integrationType == model.IntegrationTypeAlerting {
		if err := validateAlertingConfig(config, strict); err != nil {
			return err
		}
	}

	return nil
}

func validateAlertingConfig(config map[string]any, strict bool) error {
	channels, _, err := optionalStringList(config, "channels")
	if err != nil {
		return &AppError{
			Code:    "invalid_config",
			Message: "Alerting channels must be a string array or comma-separated string",
			Details: map[string]any{"type": model.IntegrationTypeAlerting, "field": "channels"},
		}
	}

	events, _, err := optionalStringList(config, "enabledEvents")
	if err != nil {
		return &AppError{
			Code:    "invalid_config",
			Message: "Alerting enabledEvents must be a string array or comma-separated string",
			Details: map[string]any{"type": model.IntegrationTypeAlerting, "field": "enabledEvents"},
		}
	}

	if strict && len(channels) == 0 {
		return &AppError{
			Code:    "integration_not_configured",
			Message: "Integration is not configured",
			Details: map[string]any{"missingKey": "channels", "type": model.IntegrationTypeAlerting},
		}
	}
	if strict && len(events) == 0 {
		return &AppError{
			Code:    "integration_not_configured",
			Message: "Integration is not configured",
			Details: map[string]any{"missingKey": "enabledEvents", "type": model.IntegrationTypeAlerting},
		}
	}

	allowedChannels := map[string]struct{}{
		"telegram":  {},
		"whatsapp":  {},
		"slack":     {},
		"email":     {},
		"webhook":   {},
		"pagerduty": {},
		"teams":     {},
	}
	for _, channel := range channels {
		if _, ok := allowedChannels[channel]; !ok {
			return &AppError{
				Code:    "invalid_config",
				Message: "Unknown alerting channel",
				Details: map[string]any{"type": model.IntegrationTypeAlerting, "field": "channels", "value": channel},
			}
		}
		if strict && !alertingChannelHasConfig(config, channel) {
			return &AppError{
				Code:    "integration_not_configured",
				Message: "Integration is not configured",
				Details: map[string]any{
					"missingKey": alertingChannelRequiredField(channel),
					"type":       model.IntegrationTypeAlerting,
					"channel":    channel,
				},
			}
		}
	}

	allowedEvents := map[string]struct{}{
		"stage_failed":          {},
		"stage_rerun_manual":    {},
		"stage_skipped_manual":  {},
		"pipeline_failed":       {},
		"pipeline_stuck":        {},
		"worker_started":        {},
		"worker_failed":         {},
		"worker_stopped":        {},
		"worker_heartbeat_lost": {},
		"policy_triggered":      {},
		"policy_changed":        {},
		"queue_backlog_high":    {},
		"dlq_message_detected":  {},
	}
	for _, event := range events {
		if _, ok := allowedEvents[event]; !ok {
			return &AppError{
				Code:    "invalid_config",
				Message: "Unknown alerting event",
				Details: map[string]any{"type": model.IntegrationTypeAlerting, "field": "enabledEvents", "value": event},
			}
		}
	}

	if window, ok := optionalFloat(config, "dedupeWindowSeconds"); ok && window <= 0 {
		return &AppError{
			Code:    "invalid_config",
			Message: "Alerting dedupeWindowSeconds must be greater than 0",
			Details: map[string]any{"type": model.IntegrationTypeAlerting, "field": "dedupeWindowSeconds"},
		}
	}

	if _, ok := optionalBool(config, "sendResolved"); !ok && config != nil {
		if _, exists := config["sendResolved"]; exists {
			return &AppError{
				Code:    "invalid_config",
				Message: "Alerting sendResolved must be a boolean",
				Details: map[string]any{"type": model.IntegrationTypeAlerting, "field": "sendResolved"},
			}
		}
	}

	return nil
}

func alertingChannelHasConfig(config map[string]any, channel string) bool {
	switch channel {
	case "telegram":
		return hasNonEmptyString(config, "telegramBotToken") && hasNonEmptyString(config, "telegramChatId")
	case "whatsapp":
		return hasNonEmptyString(config, "whatsappWebhookUrl")
	case "slack":
		return hasNonEmptyString(config, "slackWebhookUrl")
	case "email":
		return hasNonEmptyString(config, "emailRecipients")
	case "webhook":
		return hasNonEmptyString(config, "webhookUrl")
	case "pagerduty":
		return hasNonEmptyString(config, "pagerdutyRoutingKey")
	case "teams":
		return hasNonEmptyString(config, "teamsWebhookUrl")
	default:
		return false
	}
}

func alertingChannelRequiredField(channel string) string {
	switch channel {
	case "telegram":
		return "telegramBotToken,telegramChatId"
	case "whatsapp":
		return "whatsappWebhookUrl"
	case "slack":
		return "slackWebhookUrl"
	case "email":
		return "emailRecipients"
	case "webhook":
		return "webhookUrl"
	case "pagerduty":
		return "pagerdutyRoutingKey"
	case "teams":
		return "teamsWebhookUrl"
	default:
		return "channelConfig"
	}
}

func validateConfigSize(config map[string]any) error {
	serialized, err := json.Marshal(config)
	if err != nil {
		return &AppError{Code: "invalid_config", Message: "Invalid config payload", Details: err.Error()}
	}

	if len(serialized) > maxConfigPayloadBytes {
		return &AppError{
			Code:    "config_too_large",
			Message: "Config payload is too large",
			Details: map[string]any{"maxBytes": maxConfigPayloadBytes},
		}
	}

	return nil
}

func parseTimeRangeDuration(raw string) time.Duration {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "15m":
		return 15 * time.Minute
	case "1h":
		return time.Hour
	case "6h":
		return 6 * time.Hour
	case "24h", "1d":
		return 24 * time.Hour
	case "7d":
		return 7 * 24 * time.Hour
	default:
		return 0
	}
}

func parseTimeRangeStart(raw string) *time.Time {
	duration := parseTimeRangeDuration(raw)
	if duration <= 0 {
		return nil
	}
	since := time.Now().UTC().Add(-duration)
	return &since
}

func mapPipelineStatusToTraceStatus(status string) string {
	switch strings.ToLower(strings.TrimSpace(status)) {
	case "completed":
		return "success"
	case "failed":
		return "error"
	default:
		return "running"
	}
}

func computeStageInsights(stageMetrics []model.StageMetricRecord) ([]model.SlowestStage, []model.ErrorHotspot, float64) {
	type bucket struct {
		PipelineName string
		StageName    string
		DurationsMs  []int
		Total        int
		Failed       int
	}

	buckets := make(map[string]*bucket)
	totalDuration := 0
	totalCount := 0
	now := time.Now().UTC()

	for _, metric := range stageMetrics {
		if metric.StartedAt == nil {
			continue
		}
		end := now
		if metric.FinishedAt != nil {
			end = metric.FinishedAt.UTC()
		}
		durationMs := int(end.Sub(metric.StartedAt.UTC()).Milliseconds())
		if durationMs < 0 {
			durationMs = 0
		}

		key := metric.PipelineName + "::" + metric.StageName
		if _, ok := buckets[key]; !ok {
			buckets[key] = &bucket{PipelineName: metric.PipelineName, StageName: metric.StageName}
		}
		buckets[key].DurationsMs = append(buckets[key].DurationsMs, durationMs)
		buckets[key].Total++
		if strings.EqualFold(metric.Status, "Failed") {
			buckets[key].Failed++
		}

		totalDuration += durationMs
		totalCount++
	}

	slowest := make([]model.SlowestStage, 0, len(buckets))
	hotspots := make([]model.ErrorHotspot, 0, len(buckets))

	for _, bucket := range buckets {
		if len(bucket.DurationsMs) == 0 {
			continue
		}

		sort.Ints(bucket.DurationsMs)
		p95Index := int(math.Ceil(float64(len(bucket.DurationsMs))*0.95)) - 1
		if p95Index < 0 {
			p95Index = 0
		}
		if p95Index >= len(bucket.DurationsMs) {
			p95Index = len(bucket.DurationsMs) - 1
		}

		slowest = append(slowest, model.SlowestStage{
			PipelineName: bucket.PipelineName,
			StageName:    bucket.StageName,
			P95Ms:        bucket.DurationsMs[p95Index],
		})

		if bucket.Failed > 0 && bucket.Total > 0 {
			hotspots = append(hotspots, model.ErrorHotspot{
				PipelineName: bucket.PipelineName,
				StageName:    bucket.StageName,
				FailureRate:  float64(bucket.Failed) / float64(bucket.Total) * 100,
				AvgRetries:   0,
			})
		}
	}

	sort.Slice(slowest, func(i, j int) bool {
		return slowest[i].P95Ms > slowest[j].P95Ms
	})
	if len(slowest) > 10 {
		slowest = slowest[:10]
	}

	sort.Slice(hotspots, func(i, j int) bool {
		return hotspots[i].FailureRate > hotspots[j].FailureRate
	})
	if len(hotspots) > 10 {
		hotspots = hotspots[:10]
	}

	avgStageMs := 0.0
	if totalCount > 0 {
		avgStageMs = float64(totalDuration) / float64(totalCount)
	}

	return slowest, hotspots, avgStageMs
}

func computeSummaryInsights(pipelines []model.PipelineSummaryRecord, avgStageMs float64, rangeDuration time.Duration) model.InsightsSummary {
	total := len(pipelines)
	failed := 0

	for _, pipeline := range pipelines {
		if strings.EqualFold(pipeline.Status, "Failed") {
			failed++
		}
	}

	summary := model.InsightsSummary{AvgStageMs: avgStageMs}
	if total == 0 {
		return summary
	}

	minutes := rangeDuration.Minutes()
	if minutes <= 0 {
		minutes = 60
	}

	summary.ExecutionsPerMin = float64(total) / minutes
	summary.FailuresPerMin = float64(failed) / minutes
	summary.SuccessRate = float64(total-failed) / float64(total) * 100

	return summary
}

func emptyInsights() model.InsightsResponse {
	return model.InsightsResponse{
		SlowestStages: []model.SlowestStage{},
		ErrorHotspots: []model.ErrorHotspot{},
		Summary:       model.InsightsSummary{},
	}
}

func parseHTTPURL(raw string) (string, error) {
	candidate := strings.TrimSpace(raw)
	if candidate == "" {
		return "", errors.New("endpoint is required")
	}
	if !strings.Contains(candidate, "://") {
		candidate = "http://" + candidate
	}

	parsed, err := url.Parse(candidate)
	if err != nil {
		return "", fmt.Errorf("invalid url: %w", err)
	}
	if parsed.Host == "" {
		return "", errors.New("invalid url: host is required")
	}

	return parsed.String(), nil
}

func normalizeEndpointHostPort(endpoint string, defaultPort string) (string, error) {
	candidate := strings.TrimSpace(endpoint)
	if candidate == "" {
		return "", errors.New("endpoint is required")
	}

	if strings.Contains(candidate, "://") {
		parsed, err := url.Parse(candidate)
		if err != nil {
			return "", fmt.Errorf("invalid endpoint: %w", err)
		}
		candidate = parsed.Host
	}

	if !strings.Contains(candidate, ":") {
		candidate = net.JoinHostPort(candidate, defaultPort)
	}

	return candidate, nil
}

func isMissingTableError(err error) bool {
	if err == nil {
		return false
	}
	message := strings.ToLower(err.Error())
	return strings.Contains(message, "no such table") ||
		strings.Contains(message, "does not exist") ||
		strings.Contains(message, "relation \"pipeline\" does not exist")
}

func formatTimePtr(ts *time.Time) *string {
	if ts == nil {
		return nil
	}
	formatted := ts.UTC().Format(time.RFC3339)
	return &formatted
}

func optionalStringList(values map[string]any, key string) ([]string, bool, error) {
	if values == nil {
		return []string{}, false, nil
	}

	raw, ok := values[key]
	if !ok || raw == nil {
		return []string{}, false, nil
	}

	items := make([]string, 0)
	switch value := raw.(type) {
	case string:
		items = splitStringList(value)
	case []string:
		items = append(items, value...)
	case []any:
		items = make([]string, 0, len(value))
		for _, item := range value {
			str, ok := item.(string)
			if !ok {
				return nil, true, fmt.Errorf("%s contains a non-string value", key)
			}
			items = append(items, str)
		}
	default:
		return nil, true, fmt.Errorf("%s must be a string array or comma-separated string", key)
	}

	normalized := make([]string, 0, len(items))
	seen := make(map[string]struct{}, len(items))
	for _, item := range items {
		trimmed := strings.ToLower(strings.TrimSpace(item))
		if trimmed == "" {
			continue
		}
		if _, exists := seen[trimmed]; exists {
			continue
		}
		seen[trimmed] = struct{}{}
		normalized = append(normalized, trimmed)
	}

	return normalized, true, nil
}

func splitStringList(raw string) []string {
	return strings.FieldsFunc(raw, func(r rune) bool {
		return r == ',' || r == '\n' || r == ';'
	})
}

func optionalString(values map[string]any, key string) *string {
	if values == nil {
		return nil
	}
	raw, ok := values[key]
	if !ok {
		return nil
	}

	switch value := raw.(type) {
	case string:
		trimmed := strings.TrimSpace(value)
		if trimmed == "" {
			return nil
		}
		return &trimmed
	case fmt.Stringer:
		trimmed := strings.TrimSpace(value.String())
		if trimmed == "" {
			return nil
		}
		return &trimmed
	default:
		return nil
	}
}

func hasNonEmptyString(values map[string]any, key string) bool {
	return optionalString(values, key) != nil
}

func requiredString(values map[string]any, key string) string {
	if values == nil {
		return ""
	}
	raw, ok := values[key]
	if !ok {
		return ""
	}

	switch value := raw.(type) {
	case string:
		return strings.TrimSpace(value)
	case float64:
		return strings.TrimSpace(strconv.FormatFloat(value, 'f', -1, 64))
	default:
		return ""
	}
}

func optionalFloat(values map[string]any, key string) (float64, bool) {
	if values == nil {
		return 0, false
	}
	raw, ok := values[key]
	if !ok {
		return 0, false
	}

	switch value := raw.(type) {
	case float64:
		return value, true
	case int:
		return float64(value), true
	case string:
		parsed, err := strconv.ParseFloat(strings.TrimSpace(value), 64)
		if err != nil {
			return 0, false
		}
		return parsed, true
	default:
		return 0, false
	}
}

func optionalBool(values map[string]any, key string) (bool, bool) {
	if values == nil {
		return false, false
	}
	raw, ok := values[key]
	if !ok {
		return false, false
	}

	switch value := raw.(type) {
	case bool:
		return value, true
	case string:
		parsed, err := strconv.ParseBool(strings.TrimSpace(value))
		if err != nil {
			return false, false
		}
		return parsed, true
	default:
		return false, false
	}
}

func copyMap(input map[string]any) map[string]any {
	if input == nil {
		return nil
	}
	copy := make(map[string]any, len(input))
	for key, value := range input {
		copy[key] = value
	}
	return copy
}

func extractHeaders(config map[string]any) (map[string]string, error) {
	if config == nil {
		return map[string]string{}, nil
	}
	raw, ok := config["headers"]
	if !ok || raw == nil {
		return map[string]string{}, nil
	}

	switch headers := raw.(type) {
	case map[string]string:
		return headers, nil
	case map[string]any:
		result := make(map[string]string, len(headers))
		for key, value := range headers {
			trimmedKey := strings.TrimSpace(key)
			if trimmedKey == "" {
				continue
			}
			stringValue, ok := value.(string)
			if !ok {
				return nil, &AppError{
					Code:    "invalid_config",
					Message: "OpenTelemetry headers values must be strings",
					Details: map[string]any{"field": "headers"},
				}
			}
			result[trimmedKey] = strings.TrimSpace(stringValue)
		}
		return result, nil
	default:
		return nil, &AppError{
			Code:    "invalid_config",
			Message: "OpenTelemetry headers must be an object of key/value strings",
			Details: map[string]any{"field": "headers"},
		}
	}
}
