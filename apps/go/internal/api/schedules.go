package api

import (
	"context"
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"pipelogiq/internal/store"
	"pipelogiq/internal/types"
)

// registerScheduleRoutes mounts the dashboard's schedule surface. Every route is
// membership-scoped: a schedule outside the caller's applications answers 404 so
// schedule ids stay unenumerable, exactly as pipelines do.
func (s *Server) registerScheduleRoutes(r chi.Router) {
	r.Get("/", s.handleGetSchedules)
	r.Get("/{id}", s.handleGetSchedule)
	r.Get("/{id}/runs", s.handleGetScheduleRuns)
	r.Get("/{id}/runs/{runId}", s.handleGetScheduleRunPipeline)
	r.With(s.requireOperatorMiddleware).Post("/{id}/pause", s.handlePauseScheduleInternal)
	r.With(s.requireOperatorMiddleware).Post("/{id}/resume", s.handleResumeScheduleInternal)
	r.With(s.requireOperatorMiddleware).Post("/{id}/archive", s.handleArchiveScheduleInternal)
	r.With(s.requireOperatorMiddleware).Post("/{id}/trigger", s.handleTriggerScheduleInternal)
	r.With(s.requireOperatorMiddleware).Patch("/{id}/policies", s.handleUpdateSchedulePoliciesInternal)
}

// requireScheduleAccess loads the schedule and checks membership in one step.
func (s *Server) requireScheduleAccess(
	w http.ResponseWriter,
	r *http.Request,
	ctx context.Context,
) (*types.ScheduleResponse, bool) {
	id, err := strconv.ParseInt(strings.TrimSpace(chi.URLParam(r, "id")), 10, 64)
	if err != nil || id <= 0 {
		writeJSONError(w, "not found", http.StatusNotFound)
		return nil, false
	}
	annotateAudit(r, "", "schedule:"+strconv.FormatInt(id, 10), map[string]any{"scheduleId": id})

	stored, err := s.store.GetScheduleByID(ctx, id)
	if err != nil {
		if !store.IsScheduleNotFoundError(err) {
			s.logger.Error("load schedule failed", "scheduleId", id, "err", err)
		}
		writeJSONError(w, "not found", http.StatusNotFound)
		return nil, false
	}
	if !s.requireApplicationAccess(w, r, stored.ApplicationID) {
		return nil, false
	}
	annotateAudit(r, "", "", map[string]any{"applicationId": stored.ApplicationID})
	return stored, true
}

func (s *Server) handleGetSchedules(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()

	scope := scopeFromContext(ctx)
	out := types.ScheduleListResponse{Schedules: []types.ScheduleResponse{}}
	if scope.isEmpty() {
		writeJSON(w, out, http.StatusOK)
		return
	}

	status := ""
	if raw := strings.TrimSpace(r.URL.Query().Get("status")); raw != "" {
		if status = types.NormalizeScheduleStatus(raw); status == "" {
			writeJSONError(w, "status must be Active, Paused, or Archived", http.StatusBadRequest)
			return
		}
	}

	// One query per application keeps the store's cursor contract simple; a user
	// belongs to a handful of applications, not thousands.
	for _, appID := range scope.applicationIDs() {
		list, err := s.store.ListSchedules(ctx, appID, status, "", 200)
		if err != nil {
			s.logger.Error("list schedules failed", "applicationId", appID, "err", err)
			writeJSONError(w, "failed to list schedules", http.StatusInternalServerError)
			return
		}
		out.Schedules = append(out.Schedules, list.Schedules...)
	}

	if err := s.store.AttachScheduleHealth(ctx, out.Schedules); err != nil {
		s.logger.Warn("attach schedule health failed", "err", err)
	}
	writeJSON(w, out, http.StatusOK)
}

func (s *Server) handleGetSchedule(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()

	stored, ok := s.requireScheduleAccess(w, r, ctx)
	if !ok {
		return
	}
	single := []types.ScheduleResponse{*stored}
	if err := s.store.AttachScheduleHealth(ctx, single); err == nil {
		stored = &single[0]
	}
	writeJSON(w, stored, http.StatusOK)
}

func (s *Server) handleGetScheduleRuns(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()

	stored, ok := s.requireScheduleAccess(w, r, ctx)
	if !ok {
		return
	}

	limit, err := parseScheduleLimit(r.URL.Query().Get("limit"))
	if err != nil {
		writeJSONError(w, err.Error(), http.StatusBadRequest)
		return
	}

	runs, err := s.store.ListScheduleRuns(ctx, stored.ID, strings.TrimSpace(r.URL.Query().Get("cursor")), limit)
	if err != nil {
		s.logger.Error("list schedule runs failed", "scheduleId", stored.ID, "err", err)
		writeJSONError(w, "failed to list schedule runs", http.StatusInternalServerError)
		return
	}
	writeJSON(w, runs, http.StatusOK)
}

// handleGetScheduleRunPipeline resolves one run to its pipeline detail, so the
// dashboard can open a run in the existing pipeline view with its stages and logs.
func (s *Server) handleGetScheduleRunPipeline(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()

	stored, ok := s.requireScheduleAccess(w, r, ctx)
	if !ok {
		return
	}

	runID, err := strconv.ParseInt(strings.TrimSpace(chi.URLParam(r, "runId")), 10, 64)
	if err != nil || runID <= 0 {
		writeJSONError(w, "not found", http.StatusNotFound)
		return
	}

	runs, err := s.store.ListScheduleRuns(ctx, stored.ID, "", 200)
	if err != nil {
		s.logger.Error("load schedule run failed", "scheduleId", stored.ID, "runId", runID, "err", err)
		writeJSONError(w, "failed to load the run", http.StatusInternalServerError)
		return
	}

	for _, run := range runs.Runs {
		if run.ID != runID {
			continue
		}
		if run.PipelineID == nil {
			writeJSON(w, map[string]any{"run": run, "pipeline": nil}, http.StatusOK)
			return
		}
		pipeline, pipelineErr := s.store.GetPipelineFullDetail(ctx, *run.PipelineID)
		if pipelineErr != nil {
			s.logger.Error("load run pipeline failed", "pipelineId", *run.PipelineID, "err", pipelineErr)
			writeJSONError(w, "failed to load the pipeline", http.StatusInternalServerError)
			return
		}
		writeJSON(w, map[string]any{
			"run":      run,
			"pipeline": types.RedactPipelineResponse(pipeline),
		}, http.StatusOK)
		return
	}

	writeJSONError(w, "not found", http.StatusNotFound)
}

func (s *Server) handlePauseScheduleInternal(w http.ResponseWriter, r *http.Request) {
	s.setScheduleStatusInternal(w, r, types.ScheduleStatusPaused)
}

func (s *Server) handleArchiveScheduleInternal(w http.ResponseWriter, r *http.Request) {
	s.setScheduleStatusInternal(w, r, types.ScheduleStatusArchived)
}

func (s *Server) handleResumeScheduleInternal(w http.ResponseWriter, r *http.Request) {
	s.setScheduleStatusInternal(w, r, types.ScheduleStatusActive)
}

func (s *Server) setScheduleStatusInternal(w http.ResponseWriter, r *http.Request, status string) {
	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()

	stored, ok := s.requireScheduleAccess(w, r, ctx)
	if !ok {
		return
	}
	annotateAudit(r, "", "", map[string]any{"previousStatus": stored.Status, "newStatus": status})

	var nextRunAt *time.Time
	if status == types.ScheduleStatusActive {
		next, hasNext, err := specFromResponse(*stored).Next(time.Now().UTC())
		if err != nil {
			writeJSONError(w, "the stored schedule can no longer be planned: "+err.Error(), http.StatusBadRequest)
			return
		}
		if !hasNext {
			writeJSONError(w, "this schedule has no future run and cannot be resumed", http.StatusConflict)
			return
		}
		nextRunAt = &next
	}

	updated, err := s.store.SetScheduleStatus(ctx, stored.ApplicationID, stored.Name, status, nextRunAt)
	if err != nil {
		if store.IsScheduleNotFoundError(err) {
			writeJSONError(w, "not found", http.StatusNotFound)
			return
		}
		s.logger.Error("set schedule status failed", "scheduleId", stored.ID, "err", err)
		writeJSONError(w, "failed to update the schedule", http.StatusInternalServerError)
		return
	}
	s.logger.Info("schedule status changed from the dashboard",
		"scheduleId", stored.ID, "name", stored.Name, "status", status)
	writeJSON(w, updated, http.StatusOK)
}

func (s *Server) handleTriggerScheduleInternal(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()

	stored, ok := s.requireScheduleAccess(w, r, ctx)
	if !ok {
		return
	}
	if stored.Status == types.ScheduleStatusArchived {
		writeJSONError(w, "an archived schedule cannot be triggered", http.StatusConflict)
		return
	}

	var req types.ScheduleTriggerRequest
	if r.ContentLength > 0 {
		if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, scheduleBodyLimit)).Decode(&req); err != nil {
			writeJSONError(w, "invalid payload", http.StatusBadRequest)
			return
		}
	}

	runID, pipelineID, err := triggerScheduleRun(ctx, s.store, stored.ApplicationID, *stored, req.Input)
	if err != nil {
		s.logger.Error("manual schedule trigger failed", "scheduleId", stored.ID, "err", err)
		writeJSONError(w, "failed to create the manual run", http.StatusInternalServerError)
		return
	}

	s.logger.Info("schedule triggered from the dashboard",
		"scheduleId", stored.ID, "name", stored.Name, "runId", runID, "pipelineId", pipelineID)
	annotateAudit(r, "schedule.trigger", "", map[string]any{"scheduleRunId": runID, "pipelineId": pipelineID})
	writeJSON(w, map[string]any{
		"runId":      runID,
		"pipelineId": pipelineID,
		"trigger":    types.ScheduleTriggerManual,
	}, http.StatusCreated)
}

// handleUpdateSchedulePoliciesInternal lets an operator change how a schedule
// behaves when ticks collide, without touching what it runs.
//
// Only the two policies are writable here. Jitter and the catch-up ceiling stay
// with the definition the SDK declares, because those are tuning the schedule
// itself rather than deciding what to do about a run already in flight.
func (s *Server) handleUpdateSchedulePoliciesInternal(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()

	stored, ok := s.requireScheduleAccess(w, r, ctx)
	if !ok {
		return
	}

	var req struct {
		OverlapPolicy string `json:"overlapPolicy"`
		CatchupPolicy string `json:"catchupPolicy"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4<<10)).Decode(&req); err != nil {
		writeJSONError(w, "invalid request body", http.StatusBadRequest)
		return
	}

	// An omitted field keeps the stored value, so a caller can change one
	// policy without having to restate the other.
	overlap := stored.OverlapPolicy
	if strings.TrimSpace(req.OverlapPolicy) != "" {
		if overlap = types.NormalizeOverlapPolicy(req.OverlapPolicy); overlap == "" {
			writeJSONError(w, "overlapPolicy must be Skip, Queue, Replace, or Allow", http.StatusBadRequest)
			return
		}
	}
	catchup := stored.CatchupPolicy
	if strings.TrimSpace(req.CatchupPolicy) != "" {
		if catchup = types.NormalizeCatchupPolicy(req.CatchupPolicy); catchup == "" {
			writeJSONError(w, "catchupPolicy must be None, One, or All", http.StatusBadRequest)
			return
		}
	}
	annotateAudit(r, "schedule.update_policies", "", map[string]any{
		"previousOverlapPolicy": stored.OverlapPolicy, "overlapPolicy": overlap,
		"previousCatchupPolicy": stored.CatchupPolicy, "catchupPolicy": catchup,
	})

	updated, err := s.store.SetSchedulePolicies(ctx, stored.ApplicationID, stored.Name, overlap, catchup)
	if err != nil {
		if store.IsScheduleNotFoundError(err) {
			writeJSONError(w, "not found", http.StatusNotFound)
			return
		}
		s.logger.Error("set schedule policies failed", "scheduleId", stored.ID, "err", err)
		writeJSONError(w, "failed to update the schedule", http.StatusInternalServerError)
		return
	}
	s.logger.Info("schedule policies changed from the dashboard",
		"scheduleId", stored.ID, "name", stored.Name,
		"overlapPolicy", overlap, "catchupPolicy", catchup)
	writeJSON(w, updated, http.StatusOK)
}
