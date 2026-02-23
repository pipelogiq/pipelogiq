package store

import (
	"context"
	"fmt"

	"pipelogiq/internal/types"
)

func (s *Store) GetUserApplications(ctx context.Context, userID int) ([]types.ApplicationResponse, error) {
	apps := []types.ApplicationResponse{}

	err := s.db.SelectContext(ctx, &apps, `
		SELECT a.id, a.name, a.description
		FROM application a
		JOIN user_application ua ON ua.application_id = a.id
		WHERE ua.user_id = $1
		ORDER BY a.id
	`, userID)

	if err != nil {
		return nil, err
	}

	// Load API keys for each application
	for i := range apps {
		keys, _ := s.GetApiKeys(ctx, apps[i].ID)
		apps[i].ApiKeys = keys
	}

	return apps, nil
}

func (s *Store) SaveApplication(ctx context.Context, userID int, req types.SaveApplicationRequest) ([]types.ApplicationResponse, error) {
	tx, err := s.db.BeginTxx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer func() {
		if err != nil {
			_ = tx.Rollback()
		}
	}()

	var appID int

	if req.ID != nil && *req.ID > 0 {
		// Update existing
		_, err = tx.ExecContext(ctx, `
			UPDATE application SET name = $1, description = $2 WHERE id = $3
		`, req.Name, req.Description, *req.ID)
		if err != nil {
			return nil, fmt.Errorf("update application: %w", err)
		}
		appID = *req.ID
	} else {
		// Create new
		err = tx.QueryRowContext(ctx, `
			INSERT INTO application (name, description) VALUES ($1, $2) RETURNING id
		`, req.Name, req.Description).Scan(&appID)
		if err != nil {
			return nil, fmt.Errorf("insert application: %w", err)
		}

		// Link to user
		_, err = tx.ExecContext(ctx, `
			INSERT INTO user_application (user_id, application_id) VALUES ($1, $2)
		`, userID, appID)
		if err != nil {
			return nil, fmt.Errorf("link user to application: %w", err)
		}
	}

	if err = tx.Commit(); err != nil {
		return nil, err
	}

	return s.GetUserApplications(ctx, userID)
}
