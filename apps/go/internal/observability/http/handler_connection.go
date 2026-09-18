package observabilityhttp

import (
	"context"
	"net/http"

	"pipelogiq/internal/observability/model"
)

func (h *Handler) TestConnection(w http.ResponseWriter, r *http.Request) {
	var request model.TestConnectionRequest
	if err := decodeJSON(r, &request); err != nil {
		h.writeError(w, err)
		return
	}
	success := false
	defer func() { h.observeAudit(r, AuditActionTest, request.Type, success) }()

	ctx, cancel := context.WithTimeout(r.Context(), requestTimeout)
	defer cancel()

	response, err := h.service.TestConnection(ctx, request)
	if err != nil {
		h.writeError(w, err)
		return
	}
	success = response.Success

	writeJSON(w, response, http.StatusOK)
}
