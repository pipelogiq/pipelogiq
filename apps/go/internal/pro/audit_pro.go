//go:build pro

package pro

import (
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/jmoiron/sqlx"
)

// auditExport streams the audit trail as NDJSON and signs it.
//
// The signature covers a running SHA-256 of every record written, so a consumer
// can verify an export without re-reading the database, and cannot silently drop
// records from the middle of a file.
type auditExport struct {
	db         *sqlx.DB
	signingKey ed25519.PrivateKey
}

func newAuditExport(db *sqlx.DB) AuditExport {
	return &auditExport{db: db, signingKey: exportSigningKey()}
}

// exportSigningKey reads AUDIT_EXPORT_SIGNING_KEY, base64 of an Ed25519 seed.
// Without one, exports are still produced but carry an empty signature and the
// algorithm "none" — visible rather than silently unsigned.
func exportSigningKey() ed25519.PrivateKey {
	encoded := strings.TrimSpace(os.Getenv("AUDIT_EXPORT_SIGNING_KEY"))
	if encoded == "" {
		return nil
	}
	seed, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil || len(seed) != ed25519.SeedSize {
		return nil
	}
	return ed25519.NewKeyFromSeed(seed)
}

// auditRecord is one line of the export.
type auditRecord struct {
	Type          string          `json:"type"`
	ID            string          `json:"id"`
	Timestamp     time.Time       `json:"timestamp"`
	Actor         string          `json:"actor"`
	Action        string          `json:"action"`
	ApplicationID int             `json:"applicationId"`
	Subject       string          `json:"subject,omitempty"`
	Details       json.RawMessage `json:"details,omitempty"`
}

// exportTrailer closes the file: the record count and the signature over the
// running digest.
type exportTrailer struct {
	Type          string    `json:"type"`
	Records       int       `json:"records"`
	ApplicationID int       `json:"applicationId"`
	From          time.Time `json:"from"`
	To            time.Time `json:"to"`
	Digest        string    `json:"digest"`
	Algorithm     string    `json:"algorithm"`
	Signature     string    `json:"signature,omitempty"`
}

func (a *auditExport) Export(
	ctx context.Context,
	req AuditExportRequest,
	out io.Writer,
) (AuditExportResult, error) {
	if req.ApplicationID <= 0 {
		return AuditExportResult{}, errors.New("applicationId is required")
	}
	if req.To.Before(req.From) {
		return AuditExportResult{}, errors.New("the export range ends before it starts")
	}

	digest := sha256.New()
	encoder := json.NewEncoder(io.MultiWriter(out, digest))

	records := 0
	emit := func(record auditRecord) error {
		record.Type = "record"
		if err := encoder.Encode(record); err != nil {
			return err
		}
		records++
		return nil
	}

	if err := a.streamPolicyEvents(ctx, req, emit); err != nil {
		return AuditExportResult{}, err
	}
	if err := a.streamStageAudit(ctx, req, emit); err != nil {
		return AuditExportResult{}, err
	}

	trailer := exportTrailer{
		Type:          "trailer",
		Records:       records,
		ApplicationID: req.ApplicationID,
		From:          req.From.UTC(),
		To:            req.To.UTC(),
		Digest:        base64.StdEncoding.EncodeToString(digest.Sum(nil)),
		Algorithm:     "none",
	}
	if a.signingKey != nil {
		trailer.Algorithm = "ed25519"
		trailer.Signature = base64.StdEncoding.EncodeToString(
			ed25519.Sign(a.signingKey, digest.Sum(nil)))
	}

	// The trailer is written to the output only: signing its own digest is not
	// possible, and a consumer recomputes the digest from the records above it.
	if err := json.NewEncoder(out).Encode(trailer); err != nil {
		return AuditExportResult{}, err
	}

	return AuditExportResult{
		Records:   records,
		Signature: trailer.Signature,
		Algorithm: trailer.Algorithm,
	}, nil
}

// streamPolicyEvents exports policy mutations, which already carry an actor.
func (a *auditExport) streamPolicyEvents(
	ctx context.Context,
	req AuditExportRequest,
	emit func(auditRecord) error,
) error {
	rows, err := a.db.QueryxContext(ctx, `
		SELECT e.id, e.ts, e.actor, e.type, e.policy_id, e.details
		FROM policy_event e
		WHERE e.ts >= $1 AND e.ts < $2
		ORDER BY e.ts, e.id
	`, req.From.UTC(), req.To.UTC())
	if err != nil {
		return fmt.Errorf("read policy events: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var (
			id, actor, eventType, policyID string
			ts                             time.Time
			details                        []byte
		)
		if err = rows.Scan(&id, &ts, &actor, &eventType, &policyID, &details); err != nil {
			return fmt.Errorf("scan policy event: %w", err)
		}
		if err = emit(auditRecord{
			ID:            id,
			Timestamp:     ts.UTC(),
			Actor:         actor,
			Action:        "policy." + eventType,
			ApplicationID: req.ApplicationID,
			Subject:       policyID,
			Details:       json.RawMessage(details),
		}); err != nil {
			return err
		}
	}
	return rows.Err()
}

// streamStageAudit exports the stage-level audit lines the store writes for
// every mutation on a pipeline of this application.
func (a *auditExport) streamStageAudit(
	ctx context.Context,
	req AuditExportRequest,
	emit func(auditRecord) error,
) error {
	rows, err := a.db.QueryxContext(ctx, `
		SELECT l.id, l.created_at, COALESCE(l.log, ''), s.id, p.id
		FROM stage_log l
		JOIN stage s ON s.id = l.stage_id
		JOIN pipeline p ON p.id = s.pipeline_id
		WHERE p.application_id = $1
		  AND l.created_at >= $2 AND l.created_at < $3
		  AND l.log_level = 'AUDIT'
		ORDER BY l.created_at, l.id
	`, req.ApplicationID, req.From.UTC(), req.To.UTC())
	if err != nil {
		return fmt.Errorf("read stage audit: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var (
			id, stageID, pipelineID int64
			createdAt               time.Time
			message                 string
		)
		if err = rows.Scan(&id, &createdAt, &message, &stageID, &pipelineID); err != nil {
			return fmt.Errorf("scan stage audit: %w", err)
		}
		details, _ := json.Marshal(map[string]any{
			"message":    message,
			"stageId":    stageID,
			"pipelineId": pipelineID,
		})
		if err = emit(auditRecord{
			ID:            fmt.Sprintf("stage-log:%d", id),
			Timestamp:     createdAt.UTC(),
			Actor:         "system",
			Action:        "stage.audit",
			ApplicationID: req.ApplicationID,
			Subject:       fmt.Sprintf("stage:%d", stageID),
			Details:       details,
		}); err != nil {
			return err
		}
	}
	return rows.Err()
}
