package service

import (
	"testing"
	"time"

	"pipelogiq/internal/observability/model"
)

func TestComputeIntegrationStatusMatrix(t *testing.T) {
	now := time.Date(2026, 2, 16, 12, 0, 0, 0, time.UTC)
	freshness := 10 * time.Minute
	errorMessage := "connection refused"

	tests := []struct {
		name            string
		config          map[string]any
		health          model.IntegrationHealth
		expectedStatus  model.IntegrationStatus
		integrationType model.IntegrationType
	}{
		{
			name:            "not configured when required keys are missing",
			config:          map[string]any{"endpoint": "http://collector:4318"},
			health:          model.IntegrationHealth{},
			expectedStatus:  model.IntegrationStatusNotConfigured,
			integrationType: model.IntegrationTypeOpenTelemetry,
		},
		{
			name: "configured when keys are present and no successful test",
			config: map[string]any{
				"endpoint": "http://collector:4318",
				"protocol": "http",
			},
			health:          model.IntegrationHealth{},
			expectedStatus:  model.IntegrationStatusConfigured,
			integrationType: model.IntegrationTypeOpenTelemetry,
		},
		{
			name: "connected when last success is fresh",
			config: map[string]any{
				"endpoint": "http://collector:4318",
				"protocol": "http",
			},
			health: model.IntegrationHealth{
				LastSuccessAt: timePtr(now.Add(-5 * time.Minute)),
				LastTestedAt:  timePtr(now.Add(-5 * time.Minute)),
			},
			expectedStatus:  model.IntegrationStatusConnected,
			integrationType: model.IntegrationTypeOpenTelemetry,
		},
		{
			name: "connected when last test succeeded even if stale",
			config: map[string]any{
				"endpoint": "collector:4317",
				"protocol": "grpc",
			},
			health: model.IntegrationHealth{
				LastSuccessAt: timePtr(now.Add(-24 * time.Hour)),
				LastTestedAt:  timePtr(now.Add(-24 * time.Hour)),
			},
			expectedStatus:  model.IntegrationStatusConnected,
			integrationType: model.IntegrationTypeOpenTelemetry,
		},
		{
			name: "disconnected when last test failed",
			config: map[string]any{
				"endpoint": "collector:4317",
				"protocol": "grpc",
			},
			health: model.IntegrationHealth{
				LastSuccessAt: timePtr(now.Add(-30 * time.Minute)),
				LastTestedAt:  timePtr(now.Add(-5 * time.Minute)),
				LastError:     &errorMessage,
			},
			expectedStatus:  model.IntegrationStatusDisconnected,
			integrationType: model.IntegrationTypeOpenTelemetry,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := computeIntegrationStatus(tt.integrationType, tt.config, tt.health, freshness, now)
			if got != tt.expectedStatus {
				t.Fatalf("computeIntegrationStatus() = %s, want %s", got, tt.expectedStatus)
			}
		})
	}
}

func timePtr(value time.Time) *time.Time {
	copy := value
	return &copy
}
