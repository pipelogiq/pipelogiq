package api

import (
	"context"
	"log/slog"
	"time"

	"github.com/jmoiron/sqlx"

	"pipelogiq/internal/store"
	"pipelogiq/internal/types"
)

type stagePolicyRuntime struct {
	logger *slog.Logger
	// repo is the legacy file-backed repository. Used when dbRepo is nil.
	repo *policyRepository
	// dbRepo is the DB-backed repository. Takes precedence when non-nil.
	dbRepo *dbPolicyRepository
}

// NewStagePolicyRuntime creates a runtime backed by the file store.
// Deprecated: prefer NewStagePolicyRuntimeWithDB which persists events to PostgreSQL.
func NewStagePolicyRuntime(logger *slog.Logger) store.StagePolicyRuntime {
	return &stagePolicyRuntime{logger: logger}
}

// NewStagePolicyRuntimeWithRepository creates a runtime backed by the given file-backed repository.
func NewStagePolicyRuntimeWithRepository(logger *slog.Logger, repo *policyRepository) store.StagePolicyRuntime {
	return &stagePolicyRuntime{logger: logger, repo: repo}
}

// NewStagePolicyRuntimeWithDB creates a runtime backed by PostgreSQL.
// Policies are read from the policy table and trigger events are persisted to policy_event.
func NewStagePolicyRuntimeWithDB(db *sqlx.DB, logger *slog.Logger) store.StagePolicyRuntime {
	return &stagePolicyRuntime{logger: logger, dbRepo: newDBPolicyRepository(db, logger)}
}

func (r *stagePolicyRuntime) RuntimePolicies(ctx context.Context) ([]types.Policy, error) {
	if r.dbRepo != nil {
		return r.dbRepo.runtimePolicies(ctx)
	}
	repo := r.repo
	if repo == nil {
		repo = newPolicyRepository(r.logger)
	}
	return repo.runtimePolicies(), nil
}

func (r *stagePolicyRuntime) RecordRuntimeEvaluation(ctx context.Context, scope types.PolicyRuntimeScope, evaluation types.PolicyRuntimeEvaluation, source string) error {
	if len(evaluation.MatchedPolicies) == 0 {
		return nil
	}

	for _, matched := range evaluation.MatchedPolicies {
		if matched.PolicyID == "" {
			continue
		}
		details := map[string]any{
			"source":     source,
			"pipelineId": scope.PipelineID,
			"stageId":    scope.StageID,
			"stageName":  scope.StageName,
			"handler":    scope.StageHandlerName,
			"action":     string(matched.Action),
			"reason":     matched.Reason,
		}
		if evaluation.EffectiveTimeoutMs != nil {
			details["effectiveTimeoutMs"] = *evaluation.EffectiveTimeoutMs
		}
		if evaluation.Action == types.PolicyRuntimeActionBlock {
			details["blocked"] = true
		}
		if evaluation.Action == types.PolicyRuntimeActionThrottle {
			details["throttled"] = true
		}
		if matched.ThrottleUntil != nil {
			details["throttleUntil"] = matched.ThrottleUntil.UTC().Format(time.RFC3339)
		}

		if r.dbRepo != nil {
			r.dbRepo.recordTrigger(ctx, matched.PolicyID, source, details)
		} else {
			repo := r.repo
			if repo == nil {
				repo = newPolicyRepository(r.logger)
			}
			repo.recordTrigger(matched.PolicyID, source, details)
		}
	}
	return nil
}
