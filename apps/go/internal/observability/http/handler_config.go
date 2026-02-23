package observabilityhttp

import (
	"context"
	"net/http"

	"pipelogiq/internal/observability/model"
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

	ctx, cancel := context.WithTimeout(r.Context(), requestTimeout)
	defer cancel()

	response, err := h.service.SaveConfig(ctx, request)
	if err != nil {
		h.writeError(w, err)
		return
	}

	writeJSON(w, response, http.StatusOK)
}
