package api

import (
	"context"
	"errors"
	"net/http"
	"time"

	"pipelogiq/internal/store"
)

const applicationScopeKey contextKey = "applicationScope"

// applicationScope is the set of applications the authenticated user may read and act on.
// It is resolved once per request from user_application membership.
type applicationScope struct {
	ids   []int
	index map[int]struct{}
}

func newApplicationScope(ids []int) applicationScope {
	index := make(map[int]struct{}, len(ids))
	for _, id := range ids {
		index[id] = struct{}{}
	}
	return applicationScope{ids: ids, index: index}
}

// allows reports whether the application is inside the scope.
func (sc applicationScope) allows(applicationID int) bool {
	_, ok := sc.index[applicationID]
	return ok
}

// isEmpty reports whether the user belongs to no application at all.
func (sc applicationScope) isEmpty() bool {
	return len(sc.ids) == 0
}

// applicationIDs returns the scoped ids for use in store filters.
func (sc applicationScope) applicationIDs() []int {
	return sc.ids
}

// applicationScopeMiddleware resolves the caller's application membership and attaches it
// to the request context. Every data endpoint filters through it, so a user can only reach
// pipelines, stages, logs and workers of applications they belong to.
func (s *Server) applicationScopeMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		userID := getUserIDFromContext(r.Context())
		if userID == 0 {
			writeJSONError(w, "unauthorized", http.StatusUnauthorized)
			return
		}

		ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
		defer cancel()

		ids, err := s.store.GetUserApplicationIDs(ctx, userID)
		if err != nil {
			s.logger.Error("resolve application scope failed", "userId", userID, "err", err)
			writeJSONError(w, "failed to resolve access scope", http.StatusInternalServerError)
			return
		}

		next.ServeHTTP(w, r.WithContext(
			context.WithValue(r.Context(), applicationScopeKey, newApplicationScope(ids)),
		))
	})
}

// scopeFromContext returns the request scope, defaulting to an empty scope so that a
// missing middleware denies access rather than granting it.
func scopeFromContext(ctx context.Context) applicationScope {
	if scope, ok := ctx.Value(applicationScopeKey).(applicationScope); ok {
		return scope
	}
	return newApplicationScope(nil)
}

// requireApplicationAccess reports whether the caller may act on the application.
func (s *Server) requireApplicationAccess(w http.ResponseWriter, r *http.Request, applicationID int) bool {
	if scopeFromContext(r.Context()).allows(applicationID) {
		return true
	}
	writeJSONError(w, "not found", http.StatusNotFound)
	return false
}

// requirePipelineAccess resolves the pipeline's owner and reports whether the caller may
// act on it. Cross-application access answers 404 so pipeline ids stay unenumerable.
func (s *Server) requirePipelineAccess(w http.ResponseWriter, r *http.Request, ctx context.Context, pipelineID int) bool {
	appID, err := s.store.PipelineApplicationID(ctx, pipelineID)
	if err != nil {
		if !errors.Is(err, store.ErrPipelineNotFound) {
			s.logger.Error("resolve pipeline application failed", "pipelineId", pipelineID, "err", err)
		}
		writeJSONError(w, "not found", http.StatusNotFound)
		return false
	}
	return s.requireApplicationAccess(w, r, appID)
}

// requireStageAccess resolves the stage's owning application and reports whether the
// caller may act on it.
func (s *Server) requireStageAccess(w http.ResponseWriter, r *http.Request, ctx context.Context, stageID int) bool {
	appID, err := s.store.StageApplicationID(ctx, stageID)
	if err != nil {
		if !errors.Is(err, store.ErrStageNotFound) {
			s.logger.Error("resolve stage application failed", "stageId", stageID, "err", err)
		}
		writeJSONError(w, "not found", http.StatusNotFound)
		return false
	}
	return s.requireApplicationAccess(w, r, appID)
}

// scopedPipelineIDs keeps only the pipelines the caller may act on, returning the rest so
// bulk operations can report them individually instead of failing wholesale.
func (s *Server) scopedPipelineIDs(ctx context.Context, ids []int) (allowed []int, denied []int) {
	scope := scopeFromContext(ctx)
	for _, id := range ids {
		appID, err := s.store.PipelineApplicationID(ctx, id)
		if err != nil || !scope.allows(appID) {
			denied = append(denied, id)
			continue
		}
		allowed = append(allowed, id)
	}
	return allowed, denied
}

// scopedStageIDs is the stage equivalent of scopedPipelineIDs.
func (s *Server) scopedStageIDs(ctx context.Context, ids []int) (allowed []int, denied []int) {
	scope := scopeFromContext(ctx)
	for _, id := range ids {
		appID, err := s.store.StageApplicationID(ctx, id)
		if err != nil || !scope.allows(appID) {
			denied = append(denied, id)
			continue
		}
		allowed = append(allowed, id)
	}
	return allowed, denied
}
