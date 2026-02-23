package api

import (
	"context"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"pipelogiq/internal/types"
)

func (s *Server) handleGetWorkers(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()

	limit := 100
	if value := strings.TrimSpace(r.URL.Query().Get("limit")); value != "" {
		if parsed, err := strconv.Atoi(value); err == nil {
			limit = parsed
		}
	}

	stateFilter := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("state")))
	applicationID := parseQueryIntPtr(r.URL.Query().Get("applicationId"))
	search := parseQueryStringPtr(r.URL.Query().Get("search"))

	workers, err := s.store.ListWorkers(ctx, types.WorkerListRequest{
		ApplicationID: applicationID,
		Search:        search,
		Limit:         limit,
	})
	if err != nil {
		s.logger.Error("list workers failed", "err", err)
		http.Error(w, "failed to list workers", http.StatusInternalServerError)
		return
	}

	now := time.Now().UTC()
	filtered := make([]types.WorkerStatusResponse, 0, len(workers))
	onlineCount := 0
	offlineCount := 0
	degradedCount := 0

	for _, worker := range workers {
		effectiveState := resolveEffectiveWorkerState(worker, now, s.cfg.WorkerOfflineAfter)
		worker.EffectiveState = effectiveState

		if stateFilter != "" && stateFilter != "all" {
			if effectiveState != stateFilter && strings.ToLower(worker.State) != stateFilter {
				continue
			}
		}

		switch effectiveState {
		case types.WorkerStateOffline:
			offlineCount++
		case types.WorkerStateDegraded, types.WorkerStateError:
			degradedCount++
			onlineCount++
		default:
			onlineCount++
		}

		filtered = append(filtered, worker)
	}

	writeJSON(w, types.WorkerStatusListResponse{
		Items:           filtered,
		TotalCount:      len(filtered),
		OnlineCount:     onlineCount,
		OfflineCount:    offlineCount,
		DegradedCount:   degradedCount,
		OfflineAfterSec: int64(s.cfg.WorkerOfflineAfter.Seconds()),
	}, http.StatusOK)
}

func (s *Server) handleGetWorkerEvents(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()

	limit := 200
	if value := strings.TrimSpace(r.URL.Query().Get("limit")); value != "" {
		if parsed, err := strconv.Atoi(value); err == nil {
			limit = parsed
		}
	}

	workerID := parseQueryStringPtr(r.URL.Query().Get("workerId"))
	if pathWorkerID := strings.TrimSpace(chi.URLParam(r, "workerId")); pathWorkerID != "" {
		workerID = &pathWorkerID
	}

	applicationID := parseQueryIntPtr(r.URL.Query().Get("applicationId"))
	events, err := s.store.ListWorkerEvents(ctx, types.WorkerEventListRequest{
		WorkerID:      workerID,
		ApplicationID: applicationID,
		Limit:         limit,
	})
	if err != nil {
		s.logger.Error("list worker events failed", "err", err)
		http.Error(w, "failed to list worker events", http.StatusInternalServerError)
		return
	}

	writeJSON(w, events, http.StatusOK)
}

func resolveEffectiveWorkerState(worker types.WorkerStatusResponse, now time.Time, offlineAfter time.Duration) string {
	if worker.State == types.WorkerStateStopped {
		return types.WorkerStateStopped
	}
	if offlineAfter <= 0 {
		offlineAfter = 45 * time.Second
	}

	lastSeen, err := time.Parse(time.RFC3339, worker.LastSeenAt)
	if err != nil {
		return strings.ToLower(strings.TrimSpace(worker.State))
	}
	if now.Sub(lastSeen) > offlineAfter {
		return types.WorkerStateOffline
	}

	state := strings.ToLower(strings.TrimSpace(worker.State))
	if state == "" {
		return types.WorkerStateStarting
	}
	return state
}
