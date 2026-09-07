package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/go-chi/chi/v5"

	"pipelogiq/internal/schedule"
	"pipelogiq/internal/store"
	"pipelogiq/internal/types"
)

const (
	maxScheduleNameRunes = 200
	maxScheduleJitter    = 3600
	maxScheduleCatchup   = 1000
	scheduleBodyLimit    = 1 << 20
)

const (
	problemTypeValidation = "https://api.pipelogiq.dev/errors/validation"
	problemTypeNotFound   = "https://api.pipelogiq.dev/errors/not-found"
	problemTypeInternal   = "https://api.pipelogiq.dev/errors/internal"
	problemTypeConflict   = "https://api.pipelogiq.dev/errors/conflict"
)

func (s *ExternalServer) registerScheduleRoutes(router chi.Router) {
	router.Put("/schedules/{name}", s.handleUpsertSchedule)
	router.Get("/schedules", s.handleListSchedules)
	router.Get("/schedules/{name}", s.handleGetSchedule)
	router.Get("/schedules/{name}/runs", s.handleListScheduleRuns)
	router.Post("/schedules/{name}/pause", s.handlePauseSchedule)
	router.Post("/schedules/{name}/resume", s.handleResumeSchedule)
	router.Post("/schedules/{name}/trigger", s.handleTriggerSchedule)
	router.Post("/schedules/{name}/archive", s.handleArchiveSchedule)
}

// scheduleRequestContext authenticates and resolves the route name in one place.
func (s *ExternalServer) scheduleRequestContext(
	w http.ResponseWriter,
	r *http.Request,
) (context.Context, context.CancelFunc, int, string, bool) {
	name := strings.TrimSpace(chi.URLParam(r, "name"))
	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)

	appID, err := s.validateExternalAPIKey(ctx, r)
	if err != nil {
		cancel()
		writeProblemJSON(w, r, http.StatusUnauthorized,
			"https://api.pipelogiq.dev/errors/unauthorized",
			"Unauthorized", "Invalid API key", nil)
		return nil, nil, 0, "", false
	}

	if fieldErrors := validateScheduleName(name); len(fieldErrors) > 0 {
		cancel()
		writeProblemJSON(w, r, http.StatusBadRequest, problemTypeValidation,
			"Validation failed", "Schedule name is invalid", fieldErrors)
		return nil, nil, 0, "", false
	}

	return ctx, cancel, appID, name, true
}

func (s *ExternalServer) handleUpsertSchedule(w http.ResponseWriter, r *http.Request) {
	ctx, cancel, appID, name, ok := s.scheduleRequestContext(w, r)
	if !ok {
		return
	}
	defer cancel()

	var req types.ScheduleUpsertRequest
	if err := decodeJSONStrictWithLimit(w, r, &req, scheduleBodyLimit); err != nil {
		writeProblemJSON(w, r, http.StatusBadRequest, problemTypeValidation,
			"Validation failed", "Invalid request payload",
			map[string][]string{"body": {err.Error()}})
		return
	}

	upsert, fieldErrors := buildScheduleUpsert(name, req, fmt.Sprintf("application:%d", appID))
	if len(fieldErrors) > 0 {
		writeProblemJSON(w, r, http.StatusBadRequest, problemTypeValidation,
			"Validation failed", "Schedule definition contains invalid fields", fieldErrors)
		return
	}

	stored, created, err := s.store.UpsertSchedule(ctx, appID, upsert)
	if err != nil {
		s.logger.Error("upsert schedule failed", "applicationId", appID, "name", name, "err", err)
		writeProblemJSON(w, r, http.StatusInternalServerError, problemTypeInternal,
			"Internal server error", "Failed to store the schedule", nil)
		return
	}

	status := http.StatusOK
	if created {
		status = http.StatusCreated
	}
	s.logger.Info("schedule upserted",
		"applicationId", appID, "name", name,
		"created", created, "definitionVersion", stored.DefinitionVersion)
	writeJSON(w, stored, status)
}

func (s *ExternalServer) handleListSchedules(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()

	appID, err := s.validateExternalAPIKey(ctx, r)
	if err != nil {
		writeProblemJSON(w, r, http.StatusUnauthorized,
			"https://api.pipelogiq.dev/errors/unauthorized",
			"Unauthorized", "Invalid API key", nil)
		return
	}

	status := ""
	if raw := strings.TrimSpace(r.URL.Query().Get("status")); raw != "" {
		status = types.NormalizeScheduleStatus(raw)
		if status == "" {
			writeProblemJSON(w, r, http.StatusBadRequest, problemTypeValidation,
				"Validation failed", "Unknown status filter",
				map[string][]string{"status": {"status must be Active, Paused, or Archived"}})
			return
		}
	}

	limit, err := parseScheduleLimit(r.URL.Query().Get("limit"))
	if err != nil {
		writeProblemJSON(w, r, http.StatusBadRequest, problemTypeValidation,
			"Validation failed", "Invalid limit",
			map[string][]string{"limit": {err.Error()}})
		return
	}

	list, err := s.store.ListSchedules(ctx, appID, status, strings.TrimSpace(r.URL.Query().Get("cursor")), limit)
	if err != nil {
		s.logger.Error("list schedules failed", "applicationId", appID, "err", err)
		writeProblemJSON(w, r, http.StatusInternalServerError, problemTypeInternal,
			"Internal server error", "Failed to list schedules", nil)
		return
	}
	if err = s.store.AttachScheduleHealth(ctx, list.Schedules); err != nil {
		s.logger.Warn("attach schedule health failed", "applicationId", appID, "err", err)
	}
	writeJSON(w, list, http.StatusOK)
}

func (s *ExternalServer) handleGetSchedule(w http.ResponseWriter, r *http.Request) {
	ctx, cancel, appID, name, ok := s.scheduleRequestContext(w, r)
	if !ok {
		return
	}
	defer cancel()

	stored, err := s.store.GetSchedule(ctx, appID, name)
	if err != nil {
		s.writeScheduleLookupError(w, r, appID, name, err)
		return
	}
	single := []types.ScheduleResponse{*stored}
	if err = s.store.AttachScheduleHealth(ctx, single); err == nil {
		stored = &single[0]
	}
	writeJSON(w, stored, http.StatusOK)
}

func (s *ExternalServer) handleListScheduleRuns(w http.ResponseWriter, r *http.Request) {
	ctx, cancel, appID, name, ok := s.scheduleRequestContext(w, r)
	if !ok {
		return
	}
	defer cancel()

	stored, err := s.store.GetSchedule(ctx, appID, name)
	if err != nil {
		s.writeScheduleLookupError(w, r, appID, name, err)
		return
	}

	limit, err := parseScheduleLimit(r.URL.Query().Get("limit"))
	if err != nil {
		writeProblemJSON(w, r, http.StatusBadRequest, problemTypeValidation,
			"Validation failed", "Invalid limit",
			map[string][]string{"limit": {err.Error()}})
		return
	}

	runs, err := s.store.ListScheduleRuns(ctx, stored.ID, strings.TrimSpace(r.URL.Query().Get("cursor")), limit)
	if err != nil {
		s.logger.Error("list schedule runs failed", "scheduleId", stored.ID, "err", err)
		writeProblemJSON(w, r, http.StatusInternalServerError, problemTypeInternal,
			"Internal server error", "Failed to list schedule runs", nil)
		return
	}
	writeJSON(w, runs, http.StatusOK)
}

func (s *ExternalServer) handlePauseSchedule(w http.ResponseWriter, r *http.Request) {
	s.setScheduleStatus(w, r, types.ScheduleStatusPaused)
}

func (s *ExternalServer) handleArchiveSchedule(w http.ResponseWriter, r *http.Request) {
	s.setScheduleStatus(w, r, types.ScheduleStatusArchived)
}

func (s *ExternalServer) handleResumeSchedule(w http.ResponseWriter, r *http.Request) {
	s.setScheduleStatus(w, r, types.ScheduleStatusActive)
}

func (s *ExternalServer) setScheduleStatus(w http.ResponseWriter, r *http.Request, status string) {
	ctx, cancel, appID, name, ok := s.scheduleRequestContext(w, r)
	if !ok {
		return
	}
	defer cancel()

	stored, err := s.store.GetSchedule(ctx, appID, name)
	if err != nil {
		s.writeScheduleLookupError(w, r, appID, name, err)
		return
	}

	// Resuming recomputes the cursor from now, so a schedule paused for a week
	// does not wake up owing a week of ticks.
	var nextRunAt *time.Time
	if status == types.ScheduleStatusActive {
		spec := specFromResponse(*stored)
		next, hasNext, nextErr := spec.Next(time.Now().UTC())
		if nextErr != nil {
			writeProblemJSON(w, r, http.StatusBadRequest, problemTypeValidation,
				"Validation failed", "The stored schedule can no longer be planned",
				map[string][]string{"schedule": {nextErr.Error()}})
			return
		}
		if !hasNext {
			writeProblemJSON(w, r, http.StatusConflict, problemTypeConflict,
				"Conflict", "This schedule has no future run and cannot be resumed", nil)
			return
		}
		nextRunAt = &next
	}

	updated, err := s.store.SetScheduleStatus(ctx, appID, name, status, nextRunAt)
	if err != nil {
		s.writeScheduleLookupError(w, r, appID, name, err)
		return
	}
	s.logger.Info("schedule status changed",
		"applicationId", appID, "name", name, "status", status, "nextRunAt", nextRunAt)
	writeJSON(w, updated, http.StatusOK)
}

// handleTriggerSchedule creates one run immediately, without moving the cursor.
// The tick is stamped with the current instant, so it cannot collide with a
// planned tick under the (schedule, scheduled_for) uniqueness guard.
func (s *ExternalServer) handleTriggerSchedule(w http.ResponseWriter, r *http.Request) {
	ctx, cancel, appID, name, ok := s.scheduleRequestContext(w, r)
	if !ok {
		return
	}
	defer cancel()

	var req types.ScheduleTriggerRequest
	if r.ContentLength > 0 {
		if err := decodeJSONStrictWithLimit(w, r, &req, scheduleBodyLimit); err != nil {
			writeProblemJSON(w, r, http.StatusBadRequest, problemTypeValidation,
				"Validation failed", "Invalid request payload",
				map[string][]string{"body": {err.Error()}})
			return
		}
	}

	stored, err := s.store.GetSchedule(ctx, appID, name)
	if err != nil {
		s.writeScheduleLookupError(w, r, appID, name, err)
		return
	}
	if stored.Status == types.ScheduleStatusArchived {
		writeProblemJSON(w, r, http.StatusConflict, problemTypeConflict,
			"Conflict", "An archived schedule cannot be triggered", nil)
		return
	}

	run, pipelineID, err := triggerScheduleRun(ctx, s.store, appID, *stored, req.Input)
	if err != nil {
		s.logger.Error("manual schedule trigger failed",
			"applicationId", appID, "name", name, "err", err)
		writeProblemJSON(w, r, http.StatusInternalServerError, problemTypeInternal,
			"Internal server error", "Failed to create the manual run", nil)
		return
	}

	s.logger.Info("schedule triggered manually",
		"applicationId", appID, "name", name, "runId", run, "pipelineId", pipelineID)
	writeJSON(w, map[string]any{
		"runId":      run,
		"pipelineId": pipelineID,
		"trigger":    types.ScheduleTriggerManual,
	}, http.StatusCreated)
}

func (s *ExternalServer) writeScheduleLookupError(
	w http.ResponseWriter,
	r *http.Request,
	appID int,
	name string,
	err error,
) {
	if store.IsScheduleNotFoundError(err) {
		writeProblemJSON(w, r, http.StatusNotFound, problemTypeNotFound,
			"Not found", "Schedule not found", nil)
		return
	}
	s.logger.Error("schedule lookup failed", "applicationId", appID, "name", name, "err", err)
	writeProblemJSON(w, r, http.StatusInternalServerError, problemTypeInternal,
		"Internal server error", "Failed to load the schedule", nil)
}

// specFromResponse rebuilds the timing spec from a stored schedule.
func specFromResponse(stored types.ScheduleResponse) schedule.Spec {
	return schedule.Spec{
		Kind:            stored.Kind,
		CronExpression:  stored.CronExpression,
		TimeZone:        stored.TimeZone,
		IntervalSeconds: stored.IntervalSeconds,
		RunAt:           stored.RunAt,
		JitterSeconds:   stored.JitterSeconds,
	}
}

func parseScheduleLimit(raw string) (int, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return 50, nil
	}
	limit, err := strconv.Atoi(raw)
	if err != nil || limit <= 0 {
		return 0, errors.New("limit must be a positive integer")
	}
	if limit > 200 {
		return 0, errors.New("limit must not exceed 200")
	}
	return limit, nil
}

func validateScheduleName(name string) map[string][]string {
	result := make(map[string][]string)
	switch {
	case strings.TrimSpace(name) == "":
		addValidationError(result, "name", "name is required")
	case utf8.RuneCountInString(name) > maxScheduleNameRunes:
		addValidationError(result, "name",
			fmt.Sprintf("name must not exceed %d characters", maxScheduleNameRunes))
	case containsControlCharacters(name):
		addValidationError(result, "name", "name must not contain control characters")
	}
	return result
}

// buildScheduleUpsert validates the request and renders it into the persisted form,
// including the first next_run_at.
func buildScheduleUpsert(
	name string,
	req types.ScheduleUpsertRequest,
	actor string,
) (store.ScheduleUpsert, map[string][]string) {
	fieldErrors := validateScheduleName(name)

	kind := types.NormalizeScheduleKind(req.Kind)
	if kind == "" {
		addValidationError(fieldErrors, "kind", "kind must be Cron, Interval, or Once")
	}

	// An omitted policy is left to the stored value rather than silently
	// resetting to the default: the dashboard can set these, and a deploy that
	// says nothing about them should not undo that. An explicit value still
	// wins, so a schedule declared in code stays reproducible.
	overlap := ""
	if strings.TrimSpace(req.OverlapPolicy) != "" {
		if overlap = types.NormalizeOverlapPolicy(req.OverlapPolicy); overlap == "" {
			addValidationError(fieldErrors, "overlapPolicy", "overlapPolicy must be Skip, Queue, Replace, or Allow")
		}
	}

	catchup := ""
	if strings.TrimSpace(req.CatchupPolicy) != "" {
		if catchup = types.NormalizeCatchupPolicy(req.CatchupPolicy); catchup == "" {
			addValidationError(fieldErrors, "catchupPolicy", "catchupPolicy must be None, One, or All")
		}
	}

	timeZone := strings.TrimSpace(req.TimeZone)
	if timeZone == "" {
		timeZone = "UTC"
	}

	catchupMax := 10
	if req.CatchupMax != nil {
		catchupMax = *req.CatchupMax
		if catchupMax < 1 || catchupMax > maxScheduleCatchup {
			addValidationError(fieldErrors, "catchupMax",
				fmt.Sprintf("catchupMax must be between 1 and %d", maxScheduleCatchup))
		}
	}

	jitter := 0
	if req.JitterSeconds != nil {
		jitter = *req.JitterSeconds
		if jitter < 0 || jitter > maxScheduleJitter {
			addValidationError(fieldErrors, "jitterSeconds",
				fmt.Sprintf("jitterSeconds must be between 0 and %d", maxScheduleJitter))
		}
	}

	for field, messages := range validateScheduleDefinition(req.Definition) {
		fieldErrors[field] = messages
	}

	spec := schedule.Spec{
		Kind:            kind,
		CronExpression:  strings.TrimSpace(req.CronExpression),
		TimeZone:        timeZone,
		IntervalSeconds: req.IntervalSeconds,
		RunAt:           req.RunAt,
		JitterSeconds:   jitter,
	}
	if kind != "" {
		if err := spec.Validate(); err != nil {
			addValidationError(fieldErrors, scheduleErrorField(err), err.Error())
		}
	}

	if len(fieldErrors) > 0 {
		return store.ScheduleUpsert{}, fieldErrors
	}

	definitionJSON, err := json.Marshal(req.Definition)
	if err != nil {
		addValidationError(fieldErrors, "definition", "definition could not be serialised")
		return store.ScheduleUpsert{}, fieldErrors
	}

	upsert := store.ScheduleUpsert{
		Name:            name,
		Kind:            kind,
		CronExpression:  spec.CronExpression,
		TimeZone:        timeZone,
		IntervalSeconds: req.IntervalSeconds,
		RunAt:           req.RunAt,
		OverlapPolicy:   overlap,
		CatchupPolicy:   catchup,
		CatchupMax:      catchupMax,
		JitterSeconds:   jitter,
		DefinitionJSON:  string(definitionJSON),
		CreatedBy:       actor,
	}

	if next, ok, nextErr := spec.Next(time.Now().UTC()); nextErr == nil && ok {
		upsert.NextRunAt = &next
	} else if kind == types.ScheduleKindOnce {
		// A Once schedule whose runAt is already past is accepted and archived
		// rather than rejected: the client may be replaying a deployment.
		addValidationError(fieldErrors, "runAt", "runAt must be in the future")
		return store.ScheduleUpsert{}, fieldErrors
	}

	return upsert, nil
}

// scheduleErrorField maps a spec error onto the request field that caused it, so
// the problem+json body points at something the caller can fix.
func scheduleErrorField(err error) string {
	switch {
	case errors.Is(err, schedule.ErrInvalidCron):
		return "cronExpression"
	case errors.Is(err, schedule.ErrUnknownTimeZone):
		return "timeZone"
	case errors.Is(err, schedule.ErrIntervalTooSmall):
		return "intervalSeconds"
	case errors.Is(err, schedule.ErrMissingRunAt):
		return "runAt"
	default:
		return "kind"
	}
}

// validateScheduleDefinition reuses the pipeline creation rules: a schedule that
// stores a definition the create endpoint would reject is a schedule that fails
// on every tick.
func validateScheduleDefinition(definition types.PipelineCreateRequest) map[string][]string {
	result := make(map[string][]string)

	if strings.TrimSpace(definition.Name) == "" {
		addValidationError(result, "definition.name", "definition.name is required")
	} else if utf8.RuneCountInString(definition.Name) > maxPipelineNameRunes {
		addValidationError(result, "definition.name",
			fmt.Sprintf("definition.name must not exceed %d characters", maxPipelineNameRunes))
	}

	switch {
	case len(definition.Stages) == 0:
		addValidationError(result, "definition.stages", "at least one stage is required")
	case len(definition.Stages) > maxPipelineStages:
		addValidationError(result, "definition.stages",
			fmt.Sprintf("no more than %d stages are allowed", maxPipelineStages))
	}

	for idx, stage := range definition.Stages {
		prefix := fmt.Sprintf("definition.stages[%d]", idx)
		if strings.TrimSpace(stage.Name) == "" {
			addValidationError(result, prefix+".stageName", "stageName is required")
		}
		if strings.TrimSpace(stage.StageHandler) == "" {
			addValidationError(result, prefix+".stageHandlerName", "stageHandlerName is required")
		}
		if len(stage.Input) > maxStageInputBytes {
			addValidationError(result, prefix+".input",
				fmt.Sprintf("input must not exceed %d bytes", maxStageInputBytes))
		}
	}

	for idx, item := range definition.PipelineContext {
		if strings.TrimSpace(item.Key) == "" {
			addValidationError(result, fmt.Sprintf("definition.pipelineContextItems[%d].key", idx),
				"key is required")
		}
		if len(item.Value) > maxContextValueBytes {
			addValidationError(result, fmt.Sprintf("definition.pipelineContextItems[%d].value", idx),
				fmt.Sprintf("value must not exceed %d bytes", maxContextValueBytes))
		}
	}

	return result
}
