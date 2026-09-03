package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"pipelogiq/internal/store"
	"pipelogiq/internal/types"
)

func (s *ExternalServer) handleAcquireStageLease(w http.ResponseWriter, r *http.Request) {
	s.handleStageLease(w, r, false)
}

func (s *ExternalServer) handleRenewStageLease(w http.ResponseWriter, r *http.Request) {
	s.handleStageLease(w, r, true)
}

func (s *ExternalServer) handleStageLease(w http.ResponseWriter, r *http.Request, renew bool) {
	stageID, err := parseRouteID(chi.URLParam(r, "stageId"))
	if err != nil {
		http.Error(w, "stageId must be a positive integer", http.StatusBadRequest)
		return
	}

	var req types.StageLeaseRequest
	if err = json.NewDecoder(r.Body).Decode(&req); err != nil ||
		strings.TrimSpace(req.ExecutionID) == "" ||
		strings.TrimSpace(req.WorkerID) == "" {
		http.Error(w, "executionId and workerId are required", http.StatusBadRequest)
		return
	}
	sessionToken := extractWorkerSessionToken(r)
	if strings.TrimSpace(sessionToken) == "" {
		http.Error(w, "worker session token is required", http.StatusUnauthorized)
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()

	var result *types.StageLeaseResponse
	if renew {
		result, err = s.store.RenewStageLease(
			ctx, stageID, req, sessionToken, store.DefaultStageLeaseDuration,
		)
	} else {
		result, err = s.store.AcquireStageLease(
			ctx, stageID, req, sessionToken, store.DefaultStageLeaseDuration,
		)
	}
	if err != nil {
		switch {
		case store.IsInvalidWorkerSessionError(err):
			http.Error(w, "invalid worker session", http.StatusUnauthorized)
		default:
			s.logger.Error("stage lease operation failed",
				"stageId", stageID,
				"renew", renew,
				"err", err)
			http.Error(w, "stage lease operation failed", http.StatusInternalServerError)
		}
		return
	}
	writeJSON(w, result, http.StatusOK)
}

func (s *ExternalServer) handleCancelPipelineExternal(w http.ResponseWriter, r *http.Request) {
	pipelineID, err := parseRouteID(chi.URLParam(r, "pipelineId"))
	if err != nil {
		http.Error(w, "pipelineId must be a positive integer", http.StatusBadRequest)
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()
	appID, err := s.validateExternalAPIKey(ctx, r)
	if err != nil {
		http.Error(w, "invalid api key", http.StatusUnauthorized)
		return
	}

	pipeline, err := s.store.CancelPipelineForApplication(ctx, pipelineID, appID)
	if err != nil {
		switch {
		case errors.Is(err, store.ErrPipelineNotCancellable):
			http.Error(w, "pipeline is already terminal and cannot be cancelled", http.StatusConflict)
		case errors.Is(err, context.Canceled), errors.Is(err, context.DeadlineExceeded):
			http.Error(w, "cancellation timed out", http.StatusGatewayTimeout)
		default:
			http.Error(w, "pipeline not found", http.StatusNotFound)
		}
		return
	}
	writeJSON(w, types.RedactPipelineResponse(pipeline), http.StatusOK)
}
