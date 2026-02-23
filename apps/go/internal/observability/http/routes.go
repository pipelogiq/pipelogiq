package observabilityhttp

import (
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"pipelogiq/internal/observability/model"
	"pipelogiq/internal/observability/service"
)

const (
	requestTimeout  = 10 * time.Second
	maxRequestBytes = 128 * 1024
)

type Handler struct {
	service service.Interface
	logger  *slog.Logger
}

func NewHandler(observabilityService service.Interface, logger *slog.Logger) *Handler {
	if logger == nil {
		logger = slog.Default()
	}

	return &Handler{
		service: observabilityService,
		logger:  logger,
	}
}

func RegisterRoutes(r chi.Router, handler *Handler) {
	r.Get("/config", handler.GetConfig)
	r.Post("/config", handler.SaveConfig)
	r.Get("/status", handler.GetStatus)
	r.Post("/test", handler.TestConnection)
	r.Get("/traces", handler.GetTraces)
	r.Get("/insights", handler.GetInsights)
}

func decodeJSON(r *http.Request, target any) error {
	limited := io.LimitReader(r.Body, maxRequestBytes)
	decoder := json.NewDecoder(limited)
	decoder.DisallowUnknownFields()

	if err := decoder.Decode(target); err != nil {
		return &service.AppError{
			Code:    "invalid_payload",
			Message: "Invalid request payload",
			Details: err.Error(),
		}
	}
	if decoder.Decode(&struct{}{}) != io.EOF {
		return &service.AppError{
			Code:    "invalid_payload",
			Message: "Invalid request payload",
			Details: "request body must contain a single JSON object",
		}
	}

	return nil
}

func writeJSON(w http.ResponseWriter, payload any, status int) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}

func (h *Handler) writeError(w http.ResponseWriter, err error) {
	if err == nil {
		writeJSON(w, model.ErrorEnvelope{
			Error: model.APIError{
				Code:    "internal_error",
				Message: "Internal server error",
			},
		}, http.StatusInternalServerError)
		return
	}

	var appErr *service.AppError
	if errors.As(err, &appErr) {
		writeJSON(w, model.ErrorEnvelope{
			Error: model.APIError{
				Code:    appErr.Code,
				Message: appErr.Message,
				Details: appErr.Details,
			},
		}, statusForCode(appErr.Code))
		return
	}

	h.logger.Error("observability request failed", "err", err)
	writeJSON(w, model.ErrorEnvelope{
		Error: model.APIError{
			Code:    "internal_error",
			Message: "Internal server error",
		},
	}, http.StatusInternalServerError)
}

func statusForCode(code string) int {
	switch strings.TrimSpace(code) {
	case "invalid_payload", "invalid_integration_type", "invalid_config", "config_too_large":
		return http.StatusBadRequest
	case "integration_not_found":
		return http.StatusNotFound
	case "integration_not_configured":
		return http.StatusUnprocessableEntity
	default:
		return http.StatusInternalServerError
	}
}

func resolveTimeRangeParam(r *http.Request) string {
	timeRange := strings.TrimSpace(r.URL.Query().Get("range"))
	if timeRange != "" {
		return timeRange
	}
	return strings.TrimSpace(r.URL.Query().Get("timeRange"))
}
