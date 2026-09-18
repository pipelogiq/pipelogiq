package observabilityhttp

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"

	"pipelogiq/internal/observability/model"
	"pipelogiq/internal/observability/service"
)

type auditMockService struct {
	mockService
	saveErr   error
	deleteErr error
	testErr   error
}

func (m *auditMockService) SaveConfig(context.Context, model.SaveConfigRequest) (model.ObservabilityConfigResponse, error) {
	return m.configResponse, m.saveErr
}

func (m *auditMockService) DeleteConfig(context.Context, string) error {
	return m.deleteErr
}

func (m *auditMockService) TestConnection(context.Context, model.TestConnectionRequest) (model.TestConnectionResult, error) {
	return m.testResponse, m.testErr
}

func TestAuditObserverCapturesOperationOutcomesWithoutSecrets(t *testing.T) {
	for _, tc := range []struct {
		name        string
		method      string
		path        string
		body        string
		action      AuditAction
		serviceErr  error
		testSuccess bool
		wantSuccess bool
		wantStatus  int
	}{
		{"configure", http.MethodPost, "/config", `{"type":" GrAfAnA ","config":{"password":"request-secret","endpoint":"https://user:request-secret@example.com"}}`, AuditActionConfigure, nil, false, true, http.StatusOK},
		{"configure failed", http.MethodPost, "/config", `{"type":"grafana","config":{"password":"request-secret"}}`, AuditActionConfigure, errors.New("backend-secret"), false, false, http.StatusInternalServerError},
		{"delete", http.MethodDelete, "/config/GrAfAnA", "", AuditActionDelete, nil, false, true, http.StatusNoContent},
		{"delete failed", http.MethodDelete, "/config/grafana", "", AuditActionDelete, errors.New("backend-secret"), false, false, http.StatusInternalServerError},
		{"connection succeeds", http.MethodPost, "/test", `{"type":" GrAfAnA "}`, AuditActionTest, nil, true, true, http.StatusOK},
		{"connection fails with HTTP 200", http.MethodPost, "/test", `{"type":"grafana"}`, AuditActionTest, nil, false, false, http.StatusOK},
		{"connection service fails", http.MethodPost, "/test", `{"type":"grafana"}`, AuditActionTest, errors.New("backend-secret"), false, false, http.StatusInternalServerError},
	} {
		t.Run(tc.name, func(t *testing.T) {
			mock := &auditMockService{
				mockService: mockService{
					configResponse: model.ObservabilityConfigResponse{Integrations: []model.IntegrationConfigDTO{{
						Type: model.IntegrationTypeGrafana, Config: map[string]any{"password": "response-secret"},
					}}},
					testResponse: model.TestConnectionResult{Success: tc.testSuccess, Message: "connection failed at https://user:response-secret@example.com"},
				},
				saveErr: tc.serviceErr, deleteErr: tc.serviceErr, testErr: tc.serviceErr,
			}
			handler := NewHandler(mock, slog.New(slog.NewTextHandler(io.Discard, nil)))
			var events []AuditEvent
			handler.SetAuditObserver(func(r *http.Request, event AuditEvent) {
				if r.URL.Path != tc.path {
					t.Errorf("observer received an unrelated request: %s", r.URL.Path)
				}
				events = append(events, event)
			})
			router := chi.NewRouter()
			RegisterRoutes(router, handler)
			rec := httptest.NewRecorder()
			router.ServeHTTP(rec, httptest.NewRequest(tc.method, tc.path, strings.NewReader(tc.body)))
			if rec.Code != tc.wantStatus {
				t.Fatalf("HTTP status = %d, want %d", rec.Code, tc.wantStatus)
			}
			want := []AuditEvent{{Action: tc.action, IntegrationType: model.IntegrationTypeGrafana, Success: tc.wantSuccess}}
			if !reflect.DeepEqual(events, want) {
				t.Fatalf("audit events = %+v, want %+v", events, want)
			}
			encoded, err := json.Marshal(events)
			if err != nil {
				t.Fatal(err)
			}
			for _, secret := range []string{"request-secret", "response-secret", "backend-secret", "password", "endpoint", "connection failed"} {
				if strings.Contains(string(encoded), secret) {
					t.Fatalf("audit event leaked %q: %s", secret, encoded)
				}
			}
		})
	}
}

func TestAuditObserverDoesNotPassUnvalidatedTypesOrReads(t *testing.T) {
	invalidType := &service.AppError{Code: "invalid_integration_type", Message: "Invalid request-secret"}
	for _, tc := range []struct{ method, path, body string }{
		{http.MethodPost, "/config", `{"type":"request-secret","config":{"password":"request-secret"}}`},
		{http.MethodDelete, "/config/request-secret", ""},
		{http.MethodPost, "/test", `{"type":"request-secret"}`},
		{http.MethodPost, "/test", `{"type":"grafana","extra":"request-secret"}`},
		{http.MethodGet, "/config", ""},
	} {
		t.Run(tc.method+" "+tc.path, func(t *testing.T) {
			handler := NewHandler(&auditMockService{saveErr: invalidType, deleteErr: invalidType, testErr: invalidType}, nil)
			handler.SetAuditObserver(func(_ *http.Request, event AuditEvent) {
				t.Errorf("unexpected audit annotation: %+v", event)
			})
			router := chi.NewRouter()
			RegisterRoutes(router, handler)
			router.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(tc.method, tc.path, strings.NewReader(tc.body)))
		})
	}
}
