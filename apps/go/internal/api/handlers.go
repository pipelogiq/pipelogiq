package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"pipelogiq/internal/store"
	"pipelogiq/internal/types"
)

// Pipeline handlers

func (s *Server) handleGetPipelines(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()

	scope := scopeFromContext(r.Context())

	req := types.GetPipelinesRequest{
		ApplicationIDs:    scope.applicationIDs(),
		PageNumber:        parseQueryIntPtr(r.URL.Query().Get("pageNumber")),
		PageSize:          parseQueryIntPtr(r.URL.Query().Get("pageSize")),
		ApplicationID:     parseQueryIntPtr(r.URL.Query().Get("applicationId")),
		Search:            parseQueryStringPtr(r.URL.Query().Get("search")),
		Keywords:          r.URL.Query()["keywords"],
		Tags:              r.URL.Query()["tags"],
		Environment:       parseQueryStringPtr(r.URL.Query().Get("environment")),
		Owner:             parseQueryStringPtr(r.URL.Query().Get("owner")),
		Statuses:          r.URL.Query()["statuses"],
		PipelineStartFrom: parseQueryStringPtr(r.URL.Query().Get("pipelineStartFrom")),
		PipelineStartTo:   parseQueryStringPtr(r.URL.Query().Get("pipelineStartTo")),
		PipelineEndFrom:   parseQueryStringPtr(r.URL.Query().Get("pipelineEndFrom")),
		PipelineEndTo:     parseQueryStringPtr(r.URL.Query().Get("pipelineEndTo")),
	}

	for _, parameter := range []string{"pageNumber", "pageSize"} {
		if raw := r.URL.Query().Get(parameter); raw != "" {
			if value, err := strconv.Atoi(raw); err != nil || value < 1 {
				writeJSONError(w, parameter+" must be a positive integer", http.StatusBadRequest)
				return
			}
		}
	}
	effectivePageSize := 10
	if req.PageSize != nil {
		effectivePageSize = min(*req.PageSize, 1000)
	}
	if req.PageNumber != nil && *req.PageNumber-1 > int(^uint(0)>>1)/effectivePageSize {
		writeJSONError(w, "pageNumber is too large", http.StatusBadRequest)
		return
	}

	for _, raw := range r.URL.Query()["contextFilters"] {
		key, value, ok := parsePipelineKeyValueFilter(raw)
		if !ok {
			writeJSONError(w, "contextFilters must contain JSON objects with a non-empty key and a value", http.StatusBadRequest)
			return
		}
		req.ContextFilters = append(req.ContextFilters, types.ContextFilter{Key: key, Value: value})
	}
	for _, raw := range r.URL.Query()["labels"] {
		key, value, ok := parsePipelineKeyValueFilter(raw)
		if !ok {
			writeJSONError(w, "labels must contain JSON objects with a non-empty key and a value", http.StatusBadRequest)
			return
		}
		req.Labels = append(req.Labels, types.PipelineKeyword{Key: key, Value: value})
	}
	if len(req.ContextFilters)+len(req.Labels)+len(req.Tags)+len(req.Keywords) > 100 {
		writeJSONError(w, "too many filters", http.StatusBadRequest)
		return
	}

	if req.ApplicationID != nil && !scope.allows(*req.ApplicationID) {
		writeJSONError(w, "not found", http.StatusNotFound)
		return
	}

	if scope.isEmpty() {
		writeJSON(w, types.PagedResult[types.PipelineResponse]{Items: []types.PipelineResponse{}}, http.StatusOK)
		return
	}

	result, err := s.store.GetPipelines(ctx, req)
	if err != nil {
		s.logger.Error("get pipelines failed", "err", err)
		http.Error(w, "failed to get pipelines", http.StatusInternalServerError)
		return
	}
	for i := range result.Items {
		redacted := types.RedactPipelineResponse(&result.Items[i])
		result.Items[i] = *redacted
	}

	writeJSON(w, result, http.StatusOK)
}

// A missing/null value must not silently become an empty substring filter.
// An explicit empty string is a valid value, including for an empty label.
func parsePipelineKeyValueFilter(raw string) (key, value string, ok bool) {
	var filter struct {
		Key   *string `json:"key"`
		Value *string `json:"value"`
	}
	if err := json.Unmarshal([]byte(raw), &filter); err != nil || filter.Key == nil || strings.TrimSpace(*filter.Key) == "" || filter.Value == nil {
		return "", "", false
	}
	return *filter.Key, *filter.Value, true
}

func (s *Server) handleRerunStage(w http.ResponseWriter, r *http.Request) {
	var req types.RerunStageRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid payload", http.StatusBadRequest)
		return
	}
	annotateAudit(r, "stage.rerun", "stage:"+strconv.Itoa(req.StageID), map[string]any{
		"stageId": req.StageID, "rerunAllNextStages": req.RerunAllNextStages,
	})

	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()

	if !s.requireStageAccess(w, r, ctx, req.StageID) {
		return
	}

	if err := s.store.RerunStage(ctx, req.StageID, req.RerunAllNextStages); err != nil {
		s.logger.Error("rerun stage failed", "err", err)
		http.Error(w, "failed to rerun stage", http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusOK)
}

func (s *Server) handlePausePipeline(w http.ResponseWriter, r *http.Request) {
	pipelineID, err := strconv.Atoi(chi.URLParam(r, "id"))
	if err != nil {
		http.Error(w, "invalid pipeline id", http.StatusBadRequest)
		return
	}
	annotateAudit(r, "pipeline.pause", "pipeline:"+strconv.Itoa(pipelineID), map[string]any{"pipelineId": pipelineID})

	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()

	if !s.requirePipelineAccess(w, r, ctx, pipelineID) {
		return
	}

	if err = s.store.PausePipeline(ctx, pipelineID); err != nil {
		switch {
		case errors.Is(err, store.ErrPipelineNotFound):
			http.Error(w, "pipeline not found", http.StatusNotFound)
		case errors.Is(err, store.ErrPipelineNotPausable):
			http.Error(w, "pipeline is not pausable", http.StatusConflict)
		default:
			s.logger.Error("pause pipeline failed", "pipelineId", pipelineID, "err", err)
			http.Error(w, "failed to pause pipeline", http.StatusInternalServerError)
		}
		return
	}

	w.WriteHeader(http.StatusOK)
}

func (s *Server) handleResumePipeline(w http.ResponseWriter, r *http.Request) {
	pipelineID, err := strconv.Atoi(chi.URLParam(r, "id"))
	if err != nil {
		http.Error(w, "invalid pipeline id", http.StatusBadRequest)
		return
	}
	annotateAudit(r, "pipeline.resume", "pipeline:"+strconv.Itoa(pipelineID), map[string]any{"pipelineId": pipelineID})

	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()

	if !s.requirePipelineAccess(w, r, ctx, pipelineID) {
		return
	}

	if err = s.store.ResumePipeline(ctx, pipelineID); err != nil {
		switch {
		case errors.Is(err, store.ErrPipelineNotFound):
			http.Error(w, "pipeline not found", http.StatusNotFound)
		case errors.Is(err, store.ErrPipelineNotPaused):
			http.Error(w, "pipeline is not paused", http.StatusConflict)
		case errors.Is(err, store.ErrPipelineNotPausable):
			http.Error(w, "pipeline cannot be resumed", http.StatusConflict)
		default:
			s.logger.Error("resume pipeline failed", "pipelineId", pipelineID, "err", err)
			http.Error(w, "failed to resume pipeline", http.StatusInternalServerError)
		}
		return
	}

	w.WriteHeader(http.StatusOK)
}

func (s *Server) handleSkipStage(w http.ResponseWriter, r *http.Request) {
	var req types.SkipStageRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid payload", http.StatusBadRequest)
		return
	}
	annotateAudit(r, "stage.skip", "stage:"+strconv.Itoa(req.StageID), map[string]any{"stageId": req.StageID})

	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()

	if !s.requireStageAccess(w, r, ctx, req.StageID) {
		return
	}

	if err := s.store.SkipStage(ctx, req.StageID); err != nil {
		s.logger.Error("skip stage failed", "err", err)
		http.Error(w, "failed to skip stage", http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusOK)
}

func (s *Server) handleBulkPipelineAction(w http.ResponseWriter, r *http.Request) {
	var req types.BulkPipelineActionRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid payload", http.StatusBadRequest)
		return
	}

	req.Action = types.BulkPipelineAction(strings.ToLower(strings.TrimSpace(string(req.Action))))
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()

	var ids []int
	var scope string
	var run func(int) error

	switch req.Action {
	case types.BulkPipelineActionPause:
		ids = uniquePositiveInts(req.PipelineIDs)
		scope = "pipeline"
		run = func(id int) error { return s.store.PausePipeline(ctx, id) }
	case types.BulkPipelineActionResume:
		ids = uniquePositiveInts(req.PipelineIDs)
		scope = "pipeline"
		run = func(id int) error { return s.store.ResumePipeline(ctx, id) }
	case types.BulkPipelineActionRerun:
		ids = uniquePositiveInts(req.StageIDs)
		scope = "stage"
		run = func(id int) error { return s.store.RerunStage(ctx, id, req.RerunAllNextStages) }
	case types.BulkPipelineActionSkip:
		ids = uniquePositiveInts(req.StageIDs)
		scope = "stage"
		run = func(id int) error { return s.store.SkipStage(ctx, id) }
	default:
		http.Error(w, "unsupported bulk action", http.StatusBadRequest)
		return
	}
	auditDetails := map[string]any{scope + "Ids": ids}
	if req.Action == types.BulkPipelineActionRerun {
		auditDetails["rerunAllNextStages"] = req.RerunAllNextStages
	}
	annotateAudit(r, scope+".bulk_"+string(req.Action), scope+"s", auditDetails)

	if len(ids) == 0 {
		http.Error(w, "bulk action requires at least one target id", http.StatusBadRequest)
		return
	}

	requested := len(ids)
	var denied []int
	if scope == "stage" {
		ids, denied = s.scopedStageIDs(ctx, ids)
	} else {
		ids, denied = s.scopedPipelineIDs(ctx, ids)
	}

	resp := types.BulkPipelineActionResponse{
		Action:    req.Action,
		Requested: requested,
		Results:   make([]types.BulkPipelineActionItemResult, 0, requested),
	}

	// Targets outside the caller's applications are reported as not found, never acted on.
	for _, id := range denied {
		resp.Failed++
		resp.Results = append(resp.Results, types.BulkPipelineActionItemResult{
			ID:    id,
			Scope: scope,
			Error: "not found",
		})
	}

	for _, id := range ids {
		result := types.BulkPipelineActionItemResult{
			ID:    id,
			Scope: scope,
		}
		if err := run(id); err != nil {
			result.Error = err.Error()
			resp.Failed++
			s.logger.Warn("bulk pipeline action item failed", "action", req.Action, "scope", scope, "id", id, "err", err)
		} else {
			result.Success = true
			resp.Succeeded++
		}
		resp.Results = append(resp.Results, result)
	}
	// Store only identifiers and outcomes: backend error strings can contain
	// database values or other sensitive data unrelated to the operation.
	auditResults := make([]map[string]any, 0, len(resp.Results))
	for _, result := range resp.Results {
		auditResults = append(auditResults, map[string]any{
			"id": result.ID, "scope": result.Scope, "success": result.Success,
		})
	}
	annotateAudit(r, "", "", map[string]any{
		"requested": resp.Requested, "succeeded": resp.Succeeded,
		"failed": resp.Failed, "results": auditResults,
	})
	if resp.Failed > 0 {
		outcome := "failed"
		if resp.Succeeded > 0 {
			outcome = "partial"
		}
		annotateAuditOutcome(r, outcome)
	}

	writeJSON(w, resp, http.StatusOK)
}

func uniquePositiveInts(values []int) []int {
	if len(values) == 0 {
		return nil
	}
	seen := make(map[int]struct{}, len(values))
	result := make([]int, 0, len(values))
	for _, value := range values {
		if value <= 0 {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		result = append(result, value)
	}
	return result
}

func (s *Server) handleGetPipelineLogs(w http.ResponseWriter, r *http.Request) {
	pipelineIDStr := chi.URLParam(r, "pipelineId")
	pipelineID, err := strconv.Atoi(pipelineIDStr)
	if err != nil {
		http.Error(w, "invalid pipeline id", http.StatusBadRequest)
		return
	}

	var stageID *int
	stageIDStr := chi.URLParam(r, "stageId")
	if stageIDStr != "" {
		if id, err := strconv.Atoi(stageIDStr); err == nil {
			stageID = &id
		}
	}

	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()

	if !s.requirePipelineAccess(w, r, ctx, pipelineID) {
		return
	}

	logs, err := s.store.GetStageLogs(ctx, pipelineID, stageID)
	if err != nil {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	contextItems, err := s.store.GetPipelineContext(ctx, pipelineID)
	if err != nil {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}

	writeJSON(w, types.RedactStageLogs(logs, contextItems), http.StatusOK)
}

// Application handlers

func (s *Server) handleGetApplications(w http.ResponseWriter, r *http.Request) {
	userID := getUserIDFromContext(r.Context())
	if userID == 0 {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()

	apps, err := s.store.GetUserApplications(ctx, userID)
	if err != nil {
		s.logger.Error("get applications failed", "err", err)
		http.Error(w, "failed to get applications", http.StatusInternalServerError)
		return
	}

	writeJSON(w, apps, http.StatusOK)
}

func (s *Server) handleSaveApplication(w http.ResponseWriter, r *http.Request) {
	userID := getUserIDFromContext(r.Context())
	if userID == 0 {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	var req types.SaveApplicationRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid payload", http.StatusBadRequest)
		return
	}
	if req.ID != nil && *req.ID > 0 {
		annotateAudit(r, "application.update", "application:"+strconv.Itoa(*req.ID), map[string]any{"applicationId": *req.ID})
		if !s.requireApplicationAccess(w, r, *req.ID) {
			return
		}
	} else {
		annotateAudit(r, "application.create", "applications", nil)
	}

	if strings.TrimSpace(req.Name) == "" {
		http.Error(w, "name is required", http.StatusBadRequest)
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()

	apps, err := s.store.SaveApplication(ctx, userID, req)
	if err != nil {
		s.logger.Error("save application failed", "err", err)
		http.Error(w, "failed to save application", http.StatusInternalServerError)
		return
	}

	writeJSON(w, apps, http.StatusOK)
}

// ApiKey handlers

func (s *Server) handleGenerateApiKey(w http.ResponseWriter, r *http.Request) {
	userID := getUserIDFromContext(r.Context())
	if userID == 0 {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	var req types.GenerateApiKeyRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid payload", http.StatusBadRequest)
		return
	}

	hasExistingApplication := req.ApplicationID != nil && *req.ApplicationID > 0
	hasNewApplication := req.NewApplication != nil
	if hasExistingApplication {
		annotateAudit(r, "api_key.create", "application:"+strconv.Itoa(*req.ApplicationID), map[string]any{"applicationId": *req.ApplicationID})
	}

	if hasExistingApplication && hasNewApplication {
		http.Error(w, "provide either applicationId or newApplication", http.StatusBadRequest)
		return
	}

	if !hasExistingApplication && !hasNewApplication {
		http.Error(w, "applicationId or newApplication is required", http.StatusBadRequest)
		return
	}

	if hasNewApplication && strings.TrimSpace(req.NewApplication.Name) == "" {
		http.Error(w, "newApplication.name is required", http.StatusBadRequest)
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()

	key, err := s.store.GenerateApiKey(ctx, userID, req)
	if err != nil {
		s.logger.Error("generate api key failed", "err", err)
		errMsg := strings.ToLower(err.Error())
		if strings.Contains(errMsg, "applicationid or newapplication is required") ||
			strings.Contains(errMsg, "provide either applicationid or newapplication") ||
			strings.Contains(errMsg, "newapplication.name is required") {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		if strings.Contains(errMsg, "application not found or access denied") {
			http.Error(w, err.Error(), http.StatusNotFound)
			return
		}
		http.Error(w, "failed to generate api key", http.StatusInternalServerError)
		return
	}

	annotateAudit(r, "api_key.create", "api_key:"+strconv.Itoa(key.ID), map[string]any{
		"apiKeyId": key.ID, "applicationId": key.ApplicationID, "createdApplication": hasNewApplication,
	})
	writeJSON(w, key, http.StatusOK)
}

func (s *Server) handleGetApiKeys(w http.ResponseWriter, r *http.Request) {
	appIDStr := r.URL.Query().Get("applicationId")
	appID, err := strconv.Atoi(appIDStr)
	if err != nil {
		http.Error(w, "applicationId is required", http.StatusBadRequest)
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()

	if !s.requireApplicationAccess(w, r, appID) {
		return
	}

	keys, err := s.store.GetApiKeys(ctx, appID)
	if err != nil {
		s.logger.Error("get api keys failed", "err", err)
		http.Error(w, "failed to get api keys", http.StatusInternalServerError)
		return
	}

	writeJSON(w, keys, http.StatusOK)
}

func (s *Server) handleDisableApiKey(w http.ResponseWriter, r *http.Request) {
	var req types.DisableApiKeyRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid payload", http.StatusBadRequest)
		return
	}
	annotateAudit(r, "api_key.disable", "api_key:"+strconv.Itoa(req.ApiKeyID), map[string]any{"apiKeyId": req.ApiKeyID})

	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()

	appID, err := s.store.ApiKeyApplicationID(ctx, req.ApiKeyID)
	if err != nil {
		if !errors.Is(err, store.ErrApiKeyNotFound) {
			s.logger.Error("resolve api key application failed", "apiKeyId", req.ApiKeyID, "err", err)
		}
		writeJSONError(w, "not found", http.StatusNotFound)
		return
	}
	if !s.requireApplicationAccess(w, r, appID) {
		return
	}
	annotateAudit(r, "", "", map[string]any{"applicationId": appID})

	if err := s.store.DisableApiKey(ctx, req.ApiKeyID); err != nil {
		s.logger.Error("disable api key failed", "err", err)
		http.Error(w, "failed to disable api key", http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusOK)
}

// Keywords handler

func (s *Server) handleGetKeywords(w http.ResponseWriter, r *http.Request) {
	search := parseQueryStringPtr(r.URL.Query().Get("keySearch"))

	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()

	keywords, err := s.store.GetKeywords(ctx, scopeFromContext(r.Context()).applicationIDs(), search)
	if err != nil {
		s.logger.Error("get keywords failed", "err", err)
		http.Error(w, "failed to get keywords", http.StatusInternalServerError)
		return
	}

	writeJSON(w, keywords, http.StatusOK)
}

// Log handlers

func (s *Server) handleGetLogsByAppID(w http.ResponseWriter, r *http.Request) {
	appIDStr := chi.URLParam(r, "appId")
	appID, err := strconv.Atoi(appIDStr)
	if err != nil {
		http.Error(w, "invalid app id", http.StatusBadRequest)
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()

	if !s.requireApplicationAccess(w, r, appID) {
		return
	}

	logs, err := s.store.GetLogsByAppID(ctx, appID)
	if err != nil {
		s.logger.Error("get logs failed", "err", err)
		http.Error(w, "failed to get logs", http.StatusInternalServerError)
		return
	}

	writeJSON(w, logs, http.StatusOK)
}

// Helper functions

func parseQueryIntPtr(value string) *int {
	if value == "" {
		return nil
	}
	if i, err := strconv.Atoi(value); err == nil {
		return &i
	}
	return nil
}

func parseQueryStringPtr(value string) *string {
	if value == "" {
		return nil
	}
	return &value
}
