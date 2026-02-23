package api

import (
	"context"
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"pipelogiq/internal/types"
)

// Pipeline handlers

func (s *Server) handleGetPipelines(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()

	req := types.GetPipelinesRequest{
		PageNumber:        parseQueryIntPtr(r.URL.Query().Get("pageNumber")),
		PageSize:          parseQueryIntPtr(r.URL.Query().Get("pageSize")),
		ApplicationID:     parseQueryIntPtr(r.URL.Query().Get("applicationId")),
		Search:            parseQueryStringPtr(r.URL.Query().Get("search")),
		Keywords:          r.URL.Query()["keywords"],
		Statuses:          r.URL.Query()["statuses"],
		PipelineStartFrom: parseQueryStringPtr(r.URL.Query().Get("pipelineStartFrom")),
		PipelineStartTo:   parseQueryStringPtr(r.URL.Query().Get("pipelineStartTo")),
		PipelineEndFrom:   parseQueryStringPtr(r.URL.Query().Get("pipelineEndFrom")),
		PipelineEndTo:     parseQueryStringPtr(r.URL.Query().Get("pipelineEndTo")),
	}

	result, err := s.store.GetPipelines(ctx, req)
	if err != nil {
		s.logger.Error("get pipelines failed", "err", err)
		http.Error(w, "failed to get pipelines", http.StatusInternalServerError)
		return
	}

	writeJSON(w, result, http.StatusOK)
}

func (s *Server) handleRerunStage(w http.ResponseWriter, r *http.Request) {
	var req types.RerunStageRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid payload", http.StatusBadRequest)
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()

	if err := s.store.RerunStage(ctx, req.StageID, req.RerunAllNextStages); err != nil {
		s.logger.Error("rerun stage failed", "err", err)
		http.Error(w, "failed to rerun stage", http.StatusInternalServerError)
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

	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()

	if err := s.store.SkipStage(ctx, req.StageID); err != nil {
		s.logger.Error("skip stage failed", "err", err)
		http.Error(w, "failed to skip stage", http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusOK)
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

	logs, err := s.store.GetStageLogs(ctx, pipelineID, stageID)
	if err != nil {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}

	writeJSON(w, logs, http.StatusOK)
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

	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()

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

	keywords, err := s.store.GetKeywords(ctx, search)
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
