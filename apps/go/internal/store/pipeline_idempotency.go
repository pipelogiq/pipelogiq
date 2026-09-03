package store

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"

	"pipelogiq/internal/types"
)

var (
	// ErrPipelineIdempotencyKeyRequired indicates that the opt-in create method
	// was called without an idempotency key.
	ErrPipelineIdempotencyKeyRequired = errors.New("pipeline idempotency key is required")

	// ErrPipelineIdempotencyConflict indicates that an application already used
	// the key for a semantically different pipeline creation request.
	ErrPipelineIdempotencyConflict = errors.New("pipeline idempotency key was already used for a different request")
)

// CreatePipelineIdempotent atomically creates a pipeline or returns the
// existing pipeline created by the same application-scoped idempotency key.
// The database unique index is the concurrency authority; no process-local
// lock is used.
func (s *Store) CreatePipelineIdempotent(
	ctx context.Context,
	req types.PipelineCreateRequest,
	appID int,
	idempotencyKey string,
) (*types.PipelineResponse, bool, error) {
	key := strings.TrimSpace(idempotencyKey)
	if key == "" {
		return nil, false, ErrPipelineIdempotencyKeyRequired
	}

	requestHash, err := pipelineCreateRequestHash(req)
	if err != nil {
		return nil, false, fmt.Errorf("hash pipeline creation request: %w", err)
	}

	tx, err := s.db.BeginTxx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return nil, false, err
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback()
		}
	}()

	traceID := resolveTraceID(req.TraceID, req.PipelineContext)
	var pipelineID int
	err = tx.QueryRowContext(ctx, `
		INSERT INTO pipeline (
			application_id,
			name,
			status,
			created_at,
			is_completed,
			trace_id,
			idempotency_key,
			request_hash
		)
		VALUES ($1, $2, $3, CURRENT_TIMESTAMP, false, $4, $5, $6)
		ON CONFLICT (application_id, idempotency_key) DO NOTHING
		RETURNING id
	`, appID, req.Name, types.PipelineStatusNotStarted, traceID, key, requestHash).Scan(&pipelineID)

	switch {
	case err == nil:
		if err = s.insertKeywords(ctx, tx, pipelineID, req.PipelineKeywords); err != nil {
			return nil, false, err
		}
		if err = s.insertContextItems(ctx, tx, pipelineID, req.PipelineContext); err != nil {
			return nil, false, err
		}
		if err = s.insertStages(ctx, tx, pipelineID, req.Stages); err != nil {
			return nil, false, err
		}
		if err = tx.Commit(); err != nil {
			return nil, false, err
		}
		committed = true

		pipeline, loadErr := s.GetPipelineWithStages(ctx, pipelineID)
		if loadErr != nil {
			return nil, false, loadErr
		}
		return pipeline, true, nil

	case errors.Is(err, sql.ErrNoRows):
		var existingHash string
		if err = tx.QueryRowContext(ctx, `
			SELECT id, COALESCE(request_hash, '')
			FROM pipeline
			WHERE application_id = $1 AND idempotency_key = $2
		`, appID, key).Scan(&pipelineID, &existingHash); err != nil {
			return nil, false, err
		}
		if !constantTimeHashEqual(existingHash, requestHash) {
			return nil, false, ErrPipelineIdempotencyConflict
		}
		if err = tx.Commit(); err != nil {
			return nil, false, err
		}
		committed = true

		pipeline, loadErr := s.GetPipelineWithStages(ctx, pipelineID)
		if loadErr != nil {
			return nil, false, loadErr
		}
		return pipeline, false, nil

	default:
		return nil, false, err
	}
}

// GetPipelineByIdempotencyKey performs an exact, application-scoped lookup.
func (s *Store) GetPipelineByIdempotencyKey(
	ctx context.Context,
	appID int,
	idempotencyKey string,
) (*types.PipelineResponse, error) {
	key := strings.TrimSpace(idempotencyKey)
	if key == "" {
		return nil, ErrPipelineIdempotencyKeyRequired
	}

	var pipelineID int
	if err := s.db.GetContext(ctx, &pipelineID, `
		SELECT id
		FROM pipeline
		WHERE application_id = $1 AND idempotency_key = $2
	`, appID, key); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrPipelineNotFound
		}
		return nil, err
	}
	return s.GetPipelineFullDetail(ctx, pipelineID)
}

func pipelineCreateRequestHash(req types.PipelineCreateRequest) (string, error) {
	canonical := req
	canonical.ApiKey = ""
	canonical.TraceID = ""
	canonical.PipelineKeywords = append([]types.PipelineKeyword(nil), req.PipelineKeywords...)
	canonical.PipelineContext = make([]types.ContextItem, 0, len(req.PipelineContext))

	// Tracing metadata may legitimately differ when a client retries after an
	// unknown HTTP outcome. It is not part of the business creation intent.
	for _, item := range req.PipelineContext {
		if strings.EqualFold(item.Key, "traceparent") || strings.EqualFold(item.Key, "tracestate") {
			continue
		}
		canonical.PipelineContext = append(canonical.PipelineContext, item)
	}

	// Keyword and context ordering is not semantically significant.
	sort.SliceStable(canonical.PipelineKeywords, func(i, j int) bool {
		left := canonical.PipelineKeywords[i]
		right := canonical.PipelineKeywords[j]
		if left.Key == right.Key {
			return left.Value < right.Value
		}
		return left.Key < right.Key
	})
	sort.SliceStable(canonical.PipelineContext, func(i, j int) bool {
		left := canonical.PipelineContext[i]
		right := canonical.PipelineContext[j]
		if left.Key != right.Key {
			return left.Key < right.Key
		}
		if left.ValueType != right.ValueType {
			return left.ValueType < right.ValueType
		}
		return left.Value < right.Value
	})

	payload, err := json.Marshal(canonical)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(payload)
	return hex.EncodeToString(sum[:]), nil
}

func constantTimeHashEqual(left, right string) bool {
	leftBytes, leftErr := hex.DecodeString(left)
	rightBytes, rightErr := hex.DecodeString(right)
	if leftErr != nil || rightErr != nil || len(leftBytes) != sha256.Size || len(rightBytes) != sha256.Size {
		return false
	}

	var diff byte
	for idx := range leftBytes {
		diff |= leftBytes[idx] ^ rightBytes[idx]
	}
	return diff == 0
}
