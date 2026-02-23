package api

import (
	"github.com/go-chi/chi/v5"

	observabilityhttp "pipelogiq/internal/observability/http"
)

func (s *Server) registerObservabilityRoutes(r chi.Router) {
	observabilityhttp.RegisterRoutes(r, s.observabilityHandler)
}
