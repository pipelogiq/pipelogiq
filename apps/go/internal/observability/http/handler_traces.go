package observabilityhttp

import (
	"context"
	"net/http"
	"strings"

	"pipelogiq/internal/observability/model"
)

func (h *Handler) GetTraces(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), requestTimeout)
	defer cancel()

	search := strings.TrimSpace(r.URL.Query().Get("search"))
	status := strings.TrimSpace(r.URL.Query().Get("status"))
	timeRange := resolveTimeRangeParam(r)

	response, err := h.service.GetTraces(ctx, search, status, timeRange)
	if err != nil {
		h.writeError(w, err)
		return
	}
	if response == nil {
		response = []model.TraceEntry{}
	}

	writeJSON(w, response, http.StatusOK)
}
