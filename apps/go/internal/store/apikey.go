package store

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jmoiron/sqlx"

	"pipelogiq/internal/types"
)

func (s *Store) GetApiKeys(ctx context.Context, applicationID int) ([]types.ApiKeyResponse, error) {
	keys := []types.ApiKeyResponse{}

	err := s.db.SelectContext(ctx, &keys, `
		SELECT id, application_id, name, key, created_at, disabled_at, expires_at, last_used
		FROM api_key
		WHERE application_id = $1
		ORDER BY id
	`, applicationID)

	if err != nil {
		return nil, err
	}

	return keys, nil
}

func (s *Store) GenerateApiKey(ctx context.Context, userID int, req types.GenerateApiKeyRequest) (*types.ApiKeyResponse, error) {
	tx, err := s.db.BeginTxx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer func() {
		if err != nil {
			_ = tx.Rollback()
		}
	}()

	applicationID, err := resolveApplicationForAPIKey(ctx, tx, userID, req)
	if err != nil {
		return nil, err
	}

	key, err := generateRandomKey(32)
	if err != nil {
		return nil, fmt.Errorf("generate key: %w", err)
	}

	now := time.Now()
	var id int

	err = tx.QueryRowContext(ctx, `
		INSERT INTO api_key (application_id, name, key, created_at, expires_at)
		VALUES ($1, $2, $3, $4, $5)
		RETURNING id
	`, applicationID, req.Name, key, now, req.ExpiresAt).Scan(&id)

	if err != nil {
		return nil, fmt.Errorf("insert api key: %w", err)
	}

	if err = tx.Commit(); err != nil {
		return nil, err
	}

	return &types.ApiKeyResponse{
		ID:            id,
		ApplicationID: applicationID,
		Name:          req.Name,
		Key:           &key,
		CreatedAt:     &now,
		ExpiresAt:     req.ExpiresAt,
	}, nil
}

func resolveApplicationForAPIKey(ctx context.Context, tx *sqlx.Tx, userID int, req types.GenerateApiKeyRequest) (int, error) {
	hasExisting := req.ApplicationID != nil && *req.ApplicationID > 0
	hasNew := req.NewApplication != nil

	if hasExisting && hasNew {
		return 0, errors.New("provide either applicationId or newApplication")
	}
	if !hasExisting && !hasNew {
		return 0, errors.New("applicationId or newApplication is required")
	}

	if hasExisting {
		var hasAccess bool
		if err := tx.QueryRowContext(ctx, `
			SELECT EXISTS (
				SELECT 1
				FROM user_application
				WHERE user_id = $1 AND application_id = $2
			)
		`, userID, *req.ApplicationID).Scan(&hasAccess); err != nil {
			return 0, fmt.Errorf("validate application: %w", err)
		}
		if !hasAccess {
			return 0, errors.New("application not found or access denied")
		}
		return *req.ApplicationID, nil
	}

	name := strings.TrimSpace(req.NewApplication.Name)
	if name == "" {
		return 0, errors.New("newApplication.name is required")
	}

	var appID int
	if err := tx.QueryRowContext(ctx, `
		INSERT INTO application (name, description)
		VALUES ($1, $2)
		RETURNING id
	`, name, req.NewApplication.Description).Scan(&appID); err != nil {
		return 0, fmt.Errorf("create application: %w", err)
	}

	if _, err := tx.ExecContext(ctx, `
		INSERT INTO user_application (user_id, application_id)
		VALUES ($1, $2)
	`, userID, appID); err != nil {
		return 0, fmt.Errorf("link user to application: %w", err)
	}

	return appID, nil
}

func (s *Store) DisableApiKey(ctx context.Context, apiKeyID int) error {
	_, err := s.db.ExecContext(ctx, `
		UPDATE api_key SET disabled_at = NOW() WHERE id = $1
	`, apiKeyID)
	return err
}

func generateRandomKey(length int) (string, error) {
	bytes := make([]byte, length)
	if _, err := rand.Read(bytes); err != nil {
		return "", err
	}
	return hex.EncodeToString(bytes), nil
}
