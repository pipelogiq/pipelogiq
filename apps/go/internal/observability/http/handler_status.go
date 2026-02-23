package observabilityhttp

import (
	"context"
	"net/http"
)

func (h *Handler) GetStatus(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), requestTimeout)
	defer cancel()

	response, err := h.service.GetStatus(ctx)
	if err != nil {
		h.writeError(w, err)
		return
	}

	writeJSON(w, response, http.StatusOK)
}
