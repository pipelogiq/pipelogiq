package api

import (
	"context"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"

	"pipelogiq/internal/store"
)

// audit records a security-relevant action. It never fails the request: a lost
// audit row is logged, the action still happens.
func (s *Server) audit(r *http.Request, action, target, outcome string, details map[string]any) {
	entry := store.AuditEntry{
		Action:     action,
		Target:     target,
		Outcome:    outcome,
		RemoteAddr: r.RemoteAddr,
		Details:    details,
	}
	if userID := getUserIDFromContext(r.Context()); userID != 0 {
		entry.UserID = &userID
	}
	if email, ok := r.Context().Value(userEmailKey).(string); ok {
		entry.Email = email
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if err := s.store.RecordAudit(ctx, entry); err != nil {
		s.logger.Error("audit record failed", "action", action, "err", err)
	}
}

// auditMutations records every state-changing call on the administrative
// surface with its route pattern and outcome.
func (s *Server) auditMutations(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet || r.Method == http.MethodHead || r.Method == http.MethodOptions {
			next.ServeHTTP(w, r)
			return
		}
		ww := middleware.NewWrapResponseWriter(w, r.ProtoMajor)
		next.ServeHTTP(ww, r)

		outcome := "ok"
		if ww.Status() >= 400 {
			outcome = "failed"
		}
		pattern := chi.RouteContext(r.Context()).RoutePattern()
		s.audit(r, r.Method+" "+pattern, r.URL.Path, outcome, map[string]any{"status": ww.Status()})
	})
}

func (s *Server) handleListAudit(w http.ResponseWriter, r *http.Request) {
	limit := 100
	if raw := r.URL.Query().Get("limit"); raw != "" {
		if parsed, err := strconv.Atoi(raw); err == nil {
			limit = parsed
		}
	}
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()
	rows, err := s.store.ListAudit(ctx, limit)
	if err != nil {
		s.logger.Error("list audit failed", "err", err)
		writeJSONError(w, "failed to list audit log", http.StatusInternalServerError)
		return
	}
	writeJSON(w, rows, http.StatusOK)
}
