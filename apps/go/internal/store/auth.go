package store

import (
	"context"
	"database/sql"
	"errors"

	"pipelogiq/internal/types"
)

func (s *Store) GetUserByEmail(ctx context.Context, email string) (*types.UserResponse, string, error) {
	var user struct {
		types.UserResponse
		Password string `db:"password"`
	}

	err := s.db.GetContext(ctx, &user, `
		SELECT id, first_name, last_name, email, password, role, created_at
		FROM "user"
		WHERE email = $1
		LIMIT 1
	`, email)

	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, "", errors.New("user not found")
		}
		return nil, "", err
	}

	return &user.UserResponse, user.Password, nil
}

func (s *Store) GetUserByID(ctx context.Context, userID int) (*types.UserResponse, error) {
	var user types.UserResponse

	err := s.db.GetContext(ctx, &user, `
		SELECT id, first_name, last_name, email, role, created_at
		FROM "user"
		WHERE id = $1
	`, userID)

	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, errors.New("user not found")
		}
		return nil, err
	}

	return &user, nil
}
