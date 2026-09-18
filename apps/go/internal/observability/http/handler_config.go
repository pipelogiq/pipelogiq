package observabilityhttp

import (
	"context"
	"net/http"

	"github.com/go-chi/chi/v5"

	"pipelogiq/internal/observability/model"
	"pipelogiq/internal/observability/service"
)

func (h *Handler) GetConfig(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), requestTimeout)
	defer cancel()

	response, err := h.service.GetConfig(ctx)
	if err != nil {
		h.writeError(w, err)
		return
	}

	writeJSON(w, response, http.StatusOK)
}

func (h *Handler) SaveConfig(w http.ResponseWriter, r *http.Request) {
	var request model.SaveConfigRequest
	if err := decodeJSON(r, &request); err != nil {
		h.writeError(w, err)
		return
	}
	success := false
	defer func() { h.observeAudit(r, AuditActionConfigure, request.Type, success) }()

	ctx, cancel := context.WithTimeout(r.Context(), requestTimeout)
	defer cancel()

	response, err := h.service.SaveConfig(ctx, request)
	if err != nil {
		h.writeError(w, err)
		return
	}
	success = true

	writeJSON(w, response, http.StatusOK)
}

func (h *Handler) DeleteConfig(w http.ResponseWriter, r *http.Request) {
	integrationType := chi.URLParam(r, "type")
	if integrationType == "" {
		h.writeError(w, &service.AppError{Code: "invalid_payload", Message: "Integration type is required"})
		return
	}
	success := false
	defer func() { h.observeAudit(r, AuditActionDelete, integrationType, success) }()

	ctx, cancel := context.WithTimeout(r.Context(), requestTimeout)
	defer cancel()

	if err := h.service.DeleteConfig(ctx, integrationType); err != nil {
		h.writeError(w, err)
		return
	}
	success = true

	w.WriteHeader(http.StatusNoContent)
}
