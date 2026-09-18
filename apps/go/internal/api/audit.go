package api

import (
	"context"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"

	"pipelogiq/internal/store"
)

const requestAuditKey contextKey = "requestAudit"

type requestAudit struct {
	action  string
	target  string
	outcome string
	details map[string]any
}

// annotateAudit adds only explicitly selected operation metadata. Request and
// response bodies must never be copied into the audit log: they can hold keys,
// passwords, pipeline inputs, and other sensitive application data.
func annotateAudit(r *http.Request, action, target string, details map[string]any) {
	entry, _ := r.Context().Value(requestAuditKey).(*requestAudit)
	if entry == nil {
		return
	}
	if action != "" {
		entry.action = action
	}
	if target != "" {
		entry.target = target
	}
	for key, value := range details {
		entry.details[key] = value
	}
}

func annotateAuditOutcome(r *http.Request, outcome string) {
	if entry, _ := r.Context().Value(requestAuditKey).(*requestAudit); entry != nil {
		entry.outcome = outcome
	}
}

// audit combines an explicit handler event with the request event, so a user
// mutation is recorded once. Public authentication handlers record directly.
func (s *Server) audit(r *http.Request, action, target, outcome string, details map[string]any) {
	if entry, _ := r.Context().Value(requestAuditKey).(*requestAudit); entry != nil {
		annotateAudit(r, action, target, details)
		annotateAuditOutcome(r, outcome)
		return
	}
	s.recordAudit(r, action, target, outcome, details)
}

func (s *Server) recordAudit(r *http.Request, action, target, outcome string, details map[string]any) {
	entry := store.AuditEntry{
		Action: action, Target: target, Outcome: outcome,
		RemoteAddr: r.RemoteAddr, Details: details,
	}
	if userID := getUserIDFromContext(r.Context()); userID > 0 {
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

// auditMutations covers the entire authenticated mutation surface, before role
// gates, so both operators' actions and denied attempts are recorded. Normal
// reads and automatic refreshes do not create events; exporting is deliberate.
func (s *Server) auditMutations(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if (r.Method == http.MethodGet || r.Method == http.MethodHead || r.Method == http.MethodOptions) &&
			(r.Method != http.MethodGet || strings.TrimRight(r.URL.Path, "/") != "/audit/export") {
			next.ServeHTTP(w, r)
			return
		}
		entry := &requestAudit{details: make(map[string]any)}
		r = r.WithContext(context.WithValue(r.Context(), requestAuditKey, entry))
		ww := middleware.NewWrapResponseWriter(w, r.ProtoMajor)
		defer func() {
			recovered := recover()
			status := ww.Status()
			if status == 0 {
				status = http.StatusOK
			}
			if recovered != nil {
				status = http.StatusInternalServerError
			}
			pattern := strings.TrimRight(chi.RouteContext(r.Context()).RoutePattern(), "/")
			pattern = auditRoutePattern(r, pattern)
			action, target := defaultAuditAction(r, pattern)
			if entry.action != "" {
				action = entry.action
			}
			if entry.target != "" {
				target = entry.target
			}
			outcome := entry.outcome
			if outcome == "" || status >= 400 && outcome == "ok" || recovered != nil {
				switch {
				case status == http.StatusForbidden || status == http.StatusUnauthorized:
					outcome = "denied"
				case status == http.StatusTooManyRequests:
					outcome = "rate_limited"
				case status >= 400:
					outcome = "failed"
				default:
					outcome = "ok"
				}
			}
			entry.details["method"] = r.Method
			entry.details["route"] = pattern
			entry.details["status"] = status
			s.recordAudit(r, action, target, outcome, entry.details)
			if recovered != nil {
				panic(recovered)
			}
		}()
		next.ServeHTTP(ww, r)
	})
}

var auditRouteActions = map[string]string{
	"POST /auth/password":                   "auth.password",
	"POST /pipelines/{id}/pause":            "pipeline.pause",
	"POST /pipelines/{id}/resume":           "pipeline.resume",
	"POST /pipelines/rerunStage":            "stage.rerun",
	"POST /pipelines/skipStage":             "stage.skip",
	"POST /pipelines/bulkAction":            "pipeline.bulk_action",
	"POST /schedules/{id}/pause":            "schedule.pause",
	"POST /schedules/{id}/resume":           "schedule.resume",
	"POST /schedules/{id}/archive":          "schedule.archive",
	"POST /schedules/{id}/trigger":          "schedule.trigger",
	"PATCH /schedules/{id}/policies":        "schedule.update_policies",
	"POST /users":                           "user.create",
	"PUT /users/{id}":                       "user.update",
	"POST /users/{id}/password":             "user.password_reset",
	"POST /applications":                    "application.save",
	"POST /apiKeys":                         "api_key.create",
	"PUT /apiKeys/disable":                  "api_key.disable",
	"POST /policies":                        "policy.create",
	"PUT /policies/{id}":                    "policy.update",
	"DELETE /policies/{id}":                 "policy.delete",
	"POST /policies/{id}/duplicate":         "policy.duplicate",
	"POST /policies/{id}/promote":           "policy.promote",
	"POST /policies/{id}/enable":            "policy.enable",
	"POST /policies/{id}/disable":           "policy.disable",
	"POST /policies/{id}/pause":             "policy.pause",
	"POST /policies/{id}/resume":            "policy.resume",
	"POST /policies/preview":                "policy.preview",
	"POST /observability/config":            "observability.configure",
	"DELETE /observability/config/{type}":   "observability.delete",
	"POST /observability/test":              "observability.test",
	"PUT /retention/{applicationId}":        "retention.update",
	"POST /retention/{applicationId}/purge": "retention.purge",
	"GET /audit/export":                     "audit.export",
}

func defaultAuditAction(r *http.Request, pattern string) (string, string) {
	action := auditRouteActions[r.Method+" "+pattern]
	if action == "" {
		// Keep newly added mutation routes auditable even before a friendly
		// action name is added. Query strings and payloads are never recorded.
		action = r.Method + " " + pattern
	}
	target := r.URL.Path
	params := auditPathParams(r.URL.Path, pattern)
	if id := params["id"]; id != "" {
		for prefix, kind := range map[string]string{"/pipelines/": "pipeline", "/schedules/": "schedule", "/users/": "user", "/policies/": "policy"} {
			if strings.HasPrefix(pattern, prefix) {
				target = kind + ":" + id
				break
			}
		}
	}
	if id := params["applicationId"]; id != "" {
		target = "application:" + id
	}
	if kind := params["type"]; kind != "" && strings.HasPrefix(pattern, "/observability/") {
		target = "integration:" + kind
	}
	if action == "auth.password" {
		target = "user:" + strconv.Itoa(getUserIDFromContext(r.Context()))
	}
	return action, target
}

// A parent authorization gate may reject a nested request before chi resolves
// its leaf route. Match only the known route shapes so denied and successful
// attempts still have the same action name without inspecting request bodies.
func auditRoutePattern(r *http.Request, pattern string) string {
	if _, known := auditRouteActions[r.Method+" "+pattern]; known {
		return pattern
	}
	for route := range auditRouteActions {
		method, candidate, _ := strings.Cut(route, " ")
		if method == r.Method && auditPathParams(r.URL.Path, candidate) != nil {
			return candidate
		}
	}
	return pattern
}

func auditPathParams(path, pattern string) map[string]string {
	parts := strings.Split(strings.Trim(path, "/"), "/")
	shape := strings.Split(strings.Trim(pattern, "/"), "/")
	if len(parts) != len(shape) {
		return nil
	}
	params := make(map[string]string)
	for i, part := range shape {
		if strings.HasPrefix(part, "{") && strings.HasSuffix(part, "}") && parts[i] != "" {
			params[strings.Trim(part, "{}")] = parts[i]
		} else if part != parts[i] {
			return nil
		}
	}
	return params
}

func (s *Server) handleListAudit(w http.ResponseWriter, r *http.Request) {
	filter := store.AuditFilter{Page: 1, PageSize: 50}
	query := r.URL.Query()
	filter.Action = strings.TrimSpace(query.Get("action"))
	if len(filter.Action) > 100 {
		writeJSONError(w, "action is too long", http.StatusBadRequest)
		return
	}
	filter.Outcome = strings.TrimSpace(query.Get("outcome"))
	switch filter.Outcome {
	case "", "ok", "failed", "partial", "denied", "rate_limited":
	default:
		writeJSONError(w, "invalid outcome", http.StatusBadRequest)
		return
	}
	for name, target := range map[string]*int{"page": &filter.Page, "pageSize": &filter.PageSize} {
		if raw, exists := query[name]; exists {
			value, err := strconv.Atoi(raw[0])
			if err != nil || value < 1 || name == "pageSize" && value > 100 || name == "page" && value > 10000000 {
				writeJSONError(w, "invalid "+name, http.StatusBadRequest)
				return
			}
			*target = value
		}
	}
	if raw, exists := query["userId"]; exists {
		value, err := strconv.Atoi(raw[0])
		if err != nil || value < 0 {
			writeJSONError(w, "invalid userId", http.StatusBadRequest)
			return
		}
		filter.UserID = &value
	}
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()
	page, err := s.store.ListAuditFiltered(ctx, filter)
	if err != nil {
		s.logger.Error("list audit failed", "err", err)
		writeJSONError(w, "failed to list audit log", http.StatusInternalServerError)
		return
	}
	writeJSON(w, page, http.StatusOK)
}

func (s *Server) handleAuditOptions(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()
	options, err := s.store.AuditFilterOptions(ctx)
	if err != nil {
		s.logger.Error("list audit filter options failed", "err", err)
		writeJSONError(w, "failed to load audit filters", http.StatusInternalServerError)
		return
	}
	writeJSON(w, options, http.StatusOK)
}
