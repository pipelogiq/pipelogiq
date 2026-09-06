//go:build pro

package pro

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jmoiron/sqlx"
)

// allowedKeepDays are the windows the dashboard offers. An arbitrary value is
// rejected rather than silently rounded, so what an operator picked is what runs.
var allowedKeepDays = map[int]struct{}{30: {}, 90: {}, 365: {}}

// retention purges finished pipelines and everything hanging off them.
//
// It never touches an unfinished pipeline, whatever the window says. A pipeline
// that has been running for a year is a problem to investigate, not data to
// delete.
type retention struct {
	db *sqlx.DB
}

func newRetention(db *sqlx.DB) Retention {
	return &retention{db: db}
}

func (r *retention) Policy(ctx context.Context, applicationID int) (RetentionPolicy, error) {
	policy := RetentionPolicy{ApplicationID: applicationID, KeepDays: 90}
	var archiveTarget sql.NullString

	err := r.db.QueryRowxContext(ctx, `
		SELECT keep_days, enabled, archive_target
		FROM retention_policy
		WHERE application_id = $1
	`, applicationID).Scan(&policy.KeepDays, &policy.Enabled, &archiveTarget)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			// No policy means the Community behaviour: keep everything.
			return RetentionPolicy{ApplicationID: applicationID, KeepDays: 0, Enabled: false}, nil
		}
		return RetentionPolicy{}, fmt.Errorf("load retention policy: %w", err)
	}
	policy.ArchiveTarget = archiveTarget.String
	return policy, nil
}

func (r *retention) SetPolicy(ctx context.Context, policy RetentionPolicy) error {
	if policy.ApplicationID <= 0 {
		return errors.New("applicationId is required")
	}
	if policy.Enabled {
		if _, ok := allowedKeepDays[policy.KeepDays]; !ok {
			return fmt.Errorf("keepDays must be 30, 90, or 365, got %d", policy.KeepDays)
		}
	}

	var archiveTarget any
	if trimmed := strings.TrimSpace(policy.ArchiveTarget); trimmed != "" {
		archiveTarget = trimmed
	}

	if _, err := r.db.ExecContext(ctx, `
		INSERT INTO retention_policy (application_id, keep_days, enabled, archive_target, updated_at)
		VALUES ($1, $2, $3, $4, CURRENT_TIMESTAMP)
		ON CONFLICT (application_id) DO UPDATE SET
			keep_days = EXCLUDED.keep_days,
			enabled = EXCLUDED.enabled,
			archive_target = EXCLUDED.archive_target,
			updated_at = CURRENT_TIMESTAMP
	`, policy.ApplicationID, policy.KeepDays, policy.Enabled, archiveTarget); err != nil {
		return fmt.Errorf("store retention policy: %w", err)
	}
	return nil
}

func (r *retention) Purge(
	ctx context.Context,
	applicationID int,
	now time.Time,
) (PurgeResult, error) {
	policy, err := r.Policy(ctx, applicationID)
	if err != nil {
		return PurgeResult{}, err
	}
	if !policy.Enabled || policy.KeepDays <= 0 {
		return PurgeResult{}, nil
	}

	cutoff := now.UTC().AddDate(0, 0, -policy.KeepDays)

	tx, err := r.db.BeginTxx(ctx, nil)
	if err != nil {
		return PurgeResult{}, err
	}
	defer func() { _ = tx.Rollback() }()

	// is_completed is the authority, not status: a pipeline is only eligible once
	// the engine has finished with it.
	pipelineIDs := []int{}
	if err = tx.SelectContext(ctx, &pipelineIDs, `
		SELECT id FROM pipeline
		WHERE application_id = $1
		  AND is_completed = true
		  AND finished_at IS NOT NULL
		  AND finished_at < $2
		ORDER BY id
		LIMIT 5000
	`, applicationID, cutoff); err != nil {
		return PurgeResult{}, fmt.Errorf("select purgeable pipelines: %w", err)
	}
	if len(pipelineIDs) == 0 {
		return PurgeResult{}, tx.Commit()
	}

	result := PurgeResult{Archived: strings.TrimSpace(policy.ArchiveTarget) != ""}

	query, args, err := sqlx.In(`SELECT id FROM stage WHERE pipeline_id IN (?)`, pipelineIDs)
	if err != nil {
		return PurgeResult{}, err
	}
	stageIDs := []int{}
	if err = tx.SelectContext(ctx, &stageIDs, tx.Rebind(query), args...); err != nil {
		return PurgeResult{}, fmt.Errorf("select purgeable stages: %w", err)
	}

	if len(stageIDs) > 0 {
		for _, table := range []string{"stage_log", "stage_io", "stage_options"} {
			deleteQuery, deleteArgs, buildErr := sqlx.In(
				fmt.Sprintf(`DELETE FROM %s WHERE stage_id IN (?)`, table), stageIDs)
			if buildErr != nil {
				return PurgeResult{}, buildErr
			}
			deleted, execErr := tx.ExecContext(ctx, tx.Rebind(deleteQuery), deleteArgs...)
			if execErr != nil {
				return PurgeResult{}, fmt.Errorf("purge %s: %w", table, execErr)
			}
			if table == "stage_log" {
				if affected, affErr := deleted.RowsAffected(); affErr == nil {
					result.Logs = int(affected)
				}
			}
		}
	}

	for _, spec := range []struct {
		table  string
		column string
		count  *int
	}{
		{"stage", "pipeline_id", &result.Stages},
		{"pipeline_context_item", "pipeline_id", nil},
		{"pipeline_keyword", "pipeline_id", nil},
		{"pipeline", "id", &result.Pipelines},
	} {
		deleteQuery, deleteArgs, buildErr := sqlx.In(
			fmt.Sprintf(`DELETE FROM %s WHERE %s IN (?)`, spec.table, spec.column), pipelineIDs)
		if buildErr != nil {
			return PurgeResult{}, buildErr
		}
		deleted, execErr := tx.ExecContext(ctx, tx.Rebind(deleteQuery), deleteArgs...)
		if execErr != nil {
			return PurgeResult{}, fmt.Errorf("purge %s: %w", spec.table, execErr)
		}
		if spec.count != nil {
			if affected, affErr := deleted.RowsAffected(); affErr == nil {
				*spec.count = int(affected)
			}
		}
	}

	if err = tx.Commit(); err != nil {
		return PurgeResult{}, err
	}
	return result, nil
}
