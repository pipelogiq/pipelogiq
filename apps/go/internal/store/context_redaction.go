package store

import (
	"context"
	"fmt"

	"github.com/jmoiron/sqlx"
	"pipelogiq/internal/types"
)

// Retain values only for masking historical output after a sensitive item changes
// or is deleted. They are never part of the worker's mutable context.
func rememberSensitiveValuesTx(ctx context.Context, tx *sqlx.Tx, pipelineID int, values []string) error {
	for _, value := range values {
		if value == "" {
			continue
		}
		_, err := tx.ExecContext(ctx, `INSERT INTO pipeline_redaction_secret (pipeline_id, value)
			SELECT $1, $2 WHERE NOT EXISTS (SELECT 1 FROM pipeline_redaction_secret WHERE pipeline_id=$1 AND value=$2)`, pipelineID, value)
		if err != nil {
			return fmt.Errorf("retain sensitive redaction value: %w", err)
		}
	}
	return nil
}

func (s *Store) getRedactionHistory(ctx context.Context, pipelineIDs []int) (map[int][]types.ContextItem, error) {
	result := make(map[int][]types.ContextItem)
	if len(pipelineIDs) == 0 {
		return result, nil
	}
	query, args, err := sqlx.In(`SELECT pipeline_id, value FROM pipeline_redaction_secret WHERE pipeline_id IN (?)`, pipelineIDs)
	if err != nil {
		return nil, err
	}
	var rows []struct {
		PipelineID int    `db:"pipeline_id"`
		Value      string `db:"value"`
	}
	if err := s.db.SelectContext(ctx, &rows, s.db.Rebind(query), args...); err != nil {
		return nil, fmt.Errorf("load sensitive redaction history: %w", err)
	}
	for _, row := range rows {
		result[row.PipelineID] = append(result[row.PipelineID], types.ContextItem{Value: row.Value, IsSensitive: true})
	}
	return result, nil
}
