package observabilityhttp

import (
	"context"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"

	"pipelogiq/internal/observability/model"
)

func TestHandlersHappyPath(t *testing.T) {
	latency := 42
	mock := &mockService{
		configResponse: model.ObservabilityConfigResponse{
			Integrations: []model.IntegrationConfigDTO{
				{
					Type:   model.IntegrationTypeOpenTelemetry,
					Status: "configured",
					Config: map[string]any{"endpoint": "http://collector:4318", "protocol": "http"},
				},
			},
		},
		statusResponse: model.ObservabilityStatusResponse{
			Otel: model.OtelStatus{
				Configured: true,
				Connected:  true,
			},
		},
		testResponse: model.TestConnectionResult{
			Success:   true,
			Message:   "Connection established successfully",
			LatencyMs: &latency,
		},
		tracesResponse: []model.TraceEntry{
			{
				TraceID:      "trace-1",
				PipelineName: "pipeline-a",
				Status:       "success",
				DurationMs:   123,
				SpansCount:   2,
				Timestamp:    "2026-02-16T12:00:00Z",
			},
		},
		insightsResponse: model.InsightsResponse{
			SlowestStages: []model.SlowestStage{
				{PipelineName: "pipeline-a", StageName: "stage-1", P95Ms: 1100},
			},
			ErrorHotspots: []model.ErrorHotspot{
				{PipelineName: "pipeline-a", StageName: "stage-2", FailureRate: 12.3, AvgRetries: 0},
			},
			Summary: model.InsightsSummary{
				ExecutionsPerMin: 4.5,
				FailuresPerMin:   0.2,
				SuccessRate:      95,
				AvgStageMs:       410,
			},
		},
	}

	handler := NewHandler(mock, slog.Default())
	router := chi.NewRouter()
	RegisterRoutes(router, handler)

	tests := []struct {
		name         string
		method       string
		path         string
		body         string
		wantContains string
	}{
		{
			name:         "get config",
			method:       http.MethodGet,
			path:         "/config",
			wantContains: `"integrations"`,
		},
		{
			name:         "save config",
			method:       http.MethodPost,
			path:         "/config",
			body:         `{"type":"opentelemetry","config":{"endpoint":"http://collector:4318","protocol":"http"}}`,
			wantContains: `"integrations"`,
		},
		{
			name:         "get status",
			method:       http.MethodGet,
			path:         "/status",
			wantContains: `"otel"`,
		},
		{
			name:         "test connection",
			method:       http.MethodPost,
			path:         "/test",
			body:         `{"type":"opentelemetry"}`,
			wantContains: `"success":true`,
		},
		{
			name:         "get traces",
			method:       http.MethodGet,
			path:         "/traces?search=pipeline&status=success&range=1h",
			wantContains: `"traceId":"trace-1"`,
		},
		{
			name:         "get insights",
			method:       http.MethodGet,
			path:         "/insights?range=1h",
			wantContains: `"slowestStages"`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(tt.method, tt.path, strings.NewReader(tt.body))
			if tt.body != "" {
				req.Header.Set("Content-Type", "application/json")
			}

			recorder := httptest.NewRecorder()
			router.ServeHTTP(recorder, req)

			if recorder.Code != http.StatusOK {
				t.Fatalf("status = %d, want %d", recorder.Code, http.StatusOK)
			}
			if !strings.Contains(recorder.Body.String(), tt.wantContains) {
				t.Fatalf("response body %q does not contain %q", recorder.Body.String(), tt.wantContains)
			}
		})
	}
}

type mockService struct {
	configResponse   model.ObservabilityConfigResponse
	statusResponse   model.ObservabilityStatusResponse
	testResponse     model.TestConnectionResult
	tracesResponse   []model.TraceEntry
	insightsResponse model.InsightsResponse
}

func (m *mockService) GetConfig(context.Context) (model.ObservabilityConfigResponse, error) {
	return m.configResponse, nil
}

func (m *mockService) SaveConfig(context.Context, model.SaveConfigRequest) (model.ObservabilityConfigResponse, error) {
	return m.configResponse, nil
}

func (m *mockService) GetStatus(context.Context) (model.ObservabilityStatusResponse, error) {
	return m.statusResponse, nil
}

func (m *mockService) TestConnection(context.Context, model.TestConnectionRequest) (model.TestConnectionResult, error) {
	return m.testResponse, nil
}

func (m *mockService) GetTraces(context.Context, string, string, string) ([]model.TraceEntry, error) {
	return m.tracesResponse, nil
}

func (m *mockService) GetInsights(context.Context, string) (model.InsightsResponse, error) {
	return m.insightsResponse, nil
}
