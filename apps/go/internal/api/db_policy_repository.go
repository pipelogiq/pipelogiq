package api

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"
	"github.com/jmoiron/sqlx"

	"pipelogiq/internal/types"
)

// dbPolicyRepository is a DB-backed store for policies used by the policy runtime.
// It replaces the file-backed policyRepository for RuntimePolicies and recordTrigger,
// making policies and trigger events durable in PostgreSQL.
type dbPolicyRepository struct {
	db     *sqlx.DB
	logger *slog.Logger
}

func newDBPolicyRepository(db *sqlx.DB, logger *slog.Logger) *dbPolicyRepository {
	return &dbPolicyRepository{db: db, logger: logger}
}

// NewDBPolicyRepository is the exported constructor for use in cmd packages.
func NewDBPolicyRepository(db *sqlx.DB, logger *slog.Logger) *dbPolicyRepository {
	return newDBPolicyRepository(db, logger)
}

// runtimePolicies returns the active policies the stage runtime enforces.
func (r *dbPolicyRepository) runtimePolicies(ctx context.Context) ([]types.Policy, error) {
	return r.listPolicies(ctx, true)
}

// listAll returns every policy regardless of status; the API repository loads
// its cache from it at startup.
func (r *dbPolicyRepository) listAll(ctx context.Context) ([]types.Policy, error) {
	return r.listPolicies(ctx, false)
}

func (r *dbPolicyRepository) listPolicies(ctx context.Context, activeOnly bool) ([]types.Policy, error) {
	type row struct {
		ID          string         `db:"id"`
		Name        string         `db:"name"`
		Description sql.NullString `db:"description"`
		Source      string         `db:"source"`
		Origin      sql.NullString `db:"origin"`
		Type        string         `db:"type"`
		Status      string         `db:"status"`
		Environment string         `db:"environment"`
		Targeting   string         `db:"targeting"`
		Rule        string         `db:"rule"`
		Version     int            `db:"version"`
		CreatedAt   time.Time      `db:"created_at"`
		CreatedBy   string         `db:"created_by"`
		UpdatedAt   time.Time      `db:"updated_at"`
		UpdatedBy   string         `db:"updated_by"`
	}

	rows, err := r.db.QueryxContext(ctx, `
		SELECT id, name, description, source, origin,
		       type, status, environment,
		       targeting::text, rule::text,
		       version, created_at, created_by, updated_at, updated_by
		FROM policy
		WHERE ($1 = false OR status = 'active')
		ORDER BY updated_at DESC
	`, activeOnly)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var result []types.Policy
	for rows.Next() {
		var r row
		if err := rows.StructScan(&r); err != nil {
			return nil, err
		}

		p := types.Policy{
			ID:          r.ID,
			Name:        r.Name,
			Source:      types.PolicySource(r.Source),
			Type:        types.PolicyType(r.Type),
			Status:      types.PolicyStatus(r.Status),
			Environment: types.PolicyEnvironment(r.Environment),
			Version:     r.Version,
			CreatedAt:   r.CreatedAt,
			CreatedBy:   r.CreatedBy,
			UpdatedAt:   r.UpdatedAt,
			UpdatedBy:   r.UpdatedBy,
		}

		if r.Description.Valid && r.Description.String != "" {
			p.Description = &r.Description.String
		}

		if r.Origin.Valid && r.Origin.String != "" && r.Origin.String != "null" {
			var origin types.PolicyOrigin
			if err := json.Unmarshal([]byte(r.Origin.String), &origin); err == nil {
				p.Origin = &origin
			}
		}

		if err := json.Unmarshal([]byte(r.Targeting), &p.Targeting); err != nil {
			p.Targeting = types.PolicyTargeting{}
		}

		if err := json.Unmarshal([]byte(r.Rule), &p.Rule); err != nil {
			p.Rule = types.PolicyRule{}
		}

		result = append(result, p)
	}

	return result, rows.Err()
}

// recordTrigger writes a policy_event row to the DB for every runtime trigger.
func (r *dbPolicyRepository) recordTrigger(ctx context.Context, policyID, actor string, details map[string]any) {
	detailsJSON, err := json.Marshal(details)
	if err != nil {
		r.logger.Error("marshal policy trigger details failed", "err", err)
		return
	}

	eventID := uuid.NewString()
	_, err = r.db.ExecContext(ctx, `
		INSERT INTO policy_event (id, policy_id, ts, actor, type, details)
		VALUES ($1, $2, CURRENT_TIMESTAMP, $3, $4, $5)
	`, eventID, policyID, actor, string(types.PolicyEventTypeTriggered), string(detailsJSON))
	if err != nil {
		r.logger.Error("insert policy_event failed", "err", err, "policyId", policyID)
	}
}

// upsert inserts or updates a policy row in the DB.
// Called by the HTTP handlers to keep DB in sync with the in-memory file store.
func (r *dbPolicyRepository) upsert(ctx context.Context, p types.Policy) error {
	targetingJSON, err := json.Marshal(p.Targeting)
	if err != nil {
		return err
	}
	ruleJSON, err := json.Marshal(p.Rule)
	if err != nil {
		return err
	}

	var originJSON []byte
	if p.Origin != nil {
		originJSON, err = json.Marshal(p.Origin)
		if err != nil {
			return err
		}
	}

	source := string(p.Source)
	if source == "" {
		source = string(types.PolicySourceSystem)
	}

	_, err = r.db.ExecContext(ctx, `
		INSERT INTO policy
		    (id, name, description, source, origin, type, status, environment,
		     targeting, rule, version, created_at, created_by, updated_at, updated_by)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9::jsonb,$10::jsonb,$11,$12,$13,$14,$15)
		ON CONFLICT (id) DO UPDATE SET
		    name        = EXCLUDED.name,
		    description = EXCLUDED.description,
		    source      = EXCLUDED.source,
		    origin      = EXCLUDED.origin,
		    type        = EXCLUDED.type,
		    status      = EXCLUDED.status,
		    environment = EXCLUDED.environment,
		    targeting   = EXCLUDED.targeting,
		    rule        = EXCLUDED.rule,
		    version     = EXCLUDED.version,
		    updated_at  = EXCLUDED.updated_at,
		    updated_by  = EXCLUDED.updated_by
	`,
		p.ID, p.Name, p.Description, source, nullableJSON(originJSON),
		string(p.Type), string(p.Status), string(p.Environment),
		string(targetingJSON), string(ruleJSON),
		p.Version, p.CreatedAt, p.CreatedBy, p.UpdatedAt, p.UpdatedBy,
	)
	return err
}

// deleteByID removes a policy row from the DB (e.g. when deleted via HTTP handler).
func (r *dbPolicyRepository) deleteByID(ctx context.Context, policyID string) error {
	_, err := r.db.ExecContext(ctx, `DELETE FROM policy WHERE id = $1`, policyID)
	return err
}

func nullableJSON(b []byte) any {
	if len(b) == 0 {
		return nil
	}
	return string(b)
}

// insertEvent persists a lifecycle event (created, updated, enabled, triggered…)
// so the audit trail and insights survive a restart.
func (r *dbPolicyRepository) insertEvent(ctx context.Context, event types.PolicyEvent) error {
	detailsJSON, err := json.Marshal(event.Details)
	if err != nil {
		return fmt.Errorf("marshal policy event details: %w", err)
	}
	_, err = r.db.ExecContext(ctx, `
		INSERT INTO policy_event (id, policy_id, ts, actor, type, details)
		VALUES ($1, $2, $3, $4, $5, $6)
	`, event.ID, event.PolicyID, event.TS.UTC(), event.Actor, string(event.Type), string(detailsJSON))
	return err
}

// listEvents returns every stored policy event, oldest first.
func (r *dbPolicyRepository) listEvents(ctx context.Context) ([]types.PolicyEvent, error) {
	type row struct {
		ID       string         `db:"id"`
		PolicyID string         `db:"policy_id"`
		TS       time.Time      `db:"ts"`
		Actor    sql.NullString `db:"actor"`
		Type     string         `db:"type"`
		Details  sql.NullString `db:"details"`
	}
	rows, err := r.db.QueryxContext(ctx, `
		SELECT id, policy_id, ts, actor, type, details::text
		FROM policy_event
		ORDER BY ts ASC
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var result []types.PolicyEvent
	for rows.Next() {
		var rw row
		if err := rows.StructScan(&rw); err != nil {
			return nil, err
		}
		event := types.PolicyEvent{ID: rw.ID, PolicyID: rw.PolicyID, TS: rw.TS, Actor: rw.Actor.String, Type: types.PolicyEventType(rw.Type)}
		if rw.Details.Valid && rw.Details.String != "" && rw.Details.String != "null" {
			_ = json.Unmarshal([]byte(rw.Details.String), &event.Details)
		}
		result = append(result, event)
	}
	return result, rows.Err()
}
