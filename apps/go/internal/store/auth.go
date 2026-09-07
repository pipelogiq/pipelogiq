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
		SELECT id, first_name, last_name, email, password, role, created_at, token_version
		FROM "user"
		WHERE lower(email) = lower($1)
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
		SELECT id, first_name, last_name, email, role, created_at, token_version
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

// UserAccess is everything the request pipeline needs to know about a signed-in
// user, resolved in one place per request.
type UserAccess struct {
	ApplicationIDs []int
	TokenVersion   int
	Role           string
}

// GetUserAccess loads application membership and the current token version.
func (s *Store) GetUserAccess(ctx context.Context, userID int) (UserAccess, error) {
	var access UserAccess
	var row struct {
		TokenVersion int    `db:"token_version"`
		Role         string `db:"role"`
	}
	if err := s.db.GetContext(ctx, &row, `SELECT token_version, role FROM "user" WHERE id = $1`, userID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return access, errors.New("user not found")
		}
		return access, err
	}
	access.TokenVersion = row.TokenVersion
	access.Role = row.Role

	ids, err := s.GetUserApplicationIDs(ctx, userID)
	if err != nil {
		return access, err
	}
	access.ApplicationIDs = ids
	return access, nil
}

// BumpTokenVersion invalidates every JWT issued to the user and returns the new
// version so a fresh token can be minted for the caller.
func (s *Store) BumpTokenVersion(ctx context.Context, userID int) (int, error) {
	var version int
	err := s.db.QueryRowxContext(ctx, `
		UPDATE "user" SET token_version = token_version + 1 WHERE id = $1 RETURNING token_version
	`, userID).Scan(&version)
	return version, err
}

// UpdatePassword stores a new bcrypt hash and rotates the token version in the
// same statement, so every other session of the account ends immediately.
func (s *Store) UpdatePassword(ctx context.Context, userID int, hash string) (int, error) {
	var version int
	err := s.db.QueryRowxContext(ctx, `
		UPDATE "user" SET password = $2, token_version = token_version + 1 WHERE id = $1 RETURNING token_version
	`, userID, hash).Scan(&version)
	return version, err
}
