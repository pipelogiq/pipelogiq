package observabilityhttp

import (
	"context"
	"net/http"
	"strconv"
	"strings"

	"pipelogiq/internal/observability/model"
)

func (h *Handler) GetTraces(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), requestTimeout)
	defer cancel()

	search := strings.TrimSpace(r.URL.Query().Get("search"))
	status := strings.TrimSpace(r.URL.Query().Get("status"))
	timeRange := resolveTimeRangeParam(r)

	page, _ := strconv.Atoi(r.URL.Query().Get("page"))
	pageSize, _ := strconv.Atoi(r.URL.Query().Get("pageSize"))
	if page < 1 {
		page = 1
	}
	if pageSize < 1 || pageSize > 100 {
		pageSize = 50
	}

	response, err := h.service.GetTraces(ctx, search, status, timeRange, page, pageSize)
	if err != nil {
		h.writeError(w, err)
		return
	}
	if response.Items == nil {
		response.Items = []model.TraceEntry{}
	}

	writeJSON(w, response, http.StatusOK)
}
