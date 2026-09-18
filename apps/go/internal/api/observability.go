package api

import (
	"net/http"

	"github.com/go-chi/chi/v5"

	observabilityhttp "pipelogiq/internal/observability/http"
)

func (s *Server) registerObservabilityRoutes(r chi.Router) {
	if s.observabilityHandler != nil {
		s.observabilityHandler.SetAuditObserver(func(r *http.Request, event observabilityhttp.AuditEvent) {
			annotateAudit(r, string(event.Action), "integration:"+string(event.IntegrationType), map[string]any{
				"integrationType": string(event.IntegrationType),
			})
			outcome := "failed"
			if event.Success {
				outcome = "ok"
			}
			annotateAuditOutcome(r, outcome)
		})
	}
	observabilityhttp.RegisterRoutes(r, s.observabilityHandler)
}
