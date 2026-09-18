package store

import (
	"context"
	"database/sql"
	"errors"

	"pipelogiq/internal/types"
)

var ErrSessionChanged = errors.New("the user session changed")

func (s *Store) GetUserByEmail(ctx context.Context, email string) (*types.UserResponse, string, error) {
	var user struct {
		types.UserResponse
		Password string `db:"password"`
	}

	err := s.db.GetContext(ctx, &user, `
		SELECT id, first_name, last_name, email, password, role, is_active, created_at, token_version
		FROM "user"
		WHERE lower(trim(email)) = lower(trim($1))
		LIMIT 1
	`, email)

	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, "", ErrUserNotFound
		}
		return nil, "", err
	}

	user.Role = types.NormalizeUserRole(user.Role)
	return &user.UserResponse, user.Password, nil
}

func (s *Store) GetUserByID(ctx context.Context, userID int) (*types.UserResponse, error) {
	var user types.UserResponse

	err := s.db.GetContext(ctx, &user, `
		SELECT id, first_name, last_name, email, role, is_active, created_at, token_version
		FROM "user"
		WHERE id = $1
	`, userID)

	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrUserNotFound
		}
		return nil, err
	}

	user.Role = types.NormalizeUserRole(user.Role)
	return &user, nil
}

// UserAccess is everything the request pipeline needs to know about a signed-in
// user, resolved in one place per request.
type UserAccess struct {
	ApplicationIDs []int
	TokenVersion   int
	Role           string
	IsActive       bool
}

// GetUserAccess loads application membership and the current token version.
func (s *Store) GetUserAccess(ctx context.Context, userID int) (UserAccess, error) {
	access := UserAccess{ApplicationIDs: []int{}}
	var rows []struct {
		TokenVersion  int           `db:"token_version"`
		Role          string        `db:"role"`
		IsActive      bool          `db:"is_active"`
		ApplicationID sql.NullInt64 `db:"application_id"`
	}
	// One statement gives a consistent view of permissions and token version
	// while an administrator is changing the account and its memberships.
	if err := s.db.SelectContext(ctx, &rows, `
		SELECT DISTINCT u.token_version, u.role, u.is_active, ua.application_id
		FROM "user" u LEFT JOIN user_application ua ON ua.user_id = u.id
		WHERE u.id = $1 ORDER BY ua.application_id
	`, userID); err != nil {
		return access, err
	}
	if len(rows) == 0 {
		return access, ErrUserNotFound
	}
	access.TokenVersion = rows[0].TokenVersion
	access.Role = types.NormalizeUserRole(rows[0].Role)
	access.IsActive = rows[0].IsActive
	for _, row := range rows {
		if row.ApplicationID.Valid {
			access.ApplicationIDs = append(access.ApplicationIDs, int(row.ApplicationID.Int64))
		}
	}
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

// ChangeOwnPassword completes password verification atomically with the write.
// An administrator reset, account update, logout, or another password change
// must invalidate a request that was already past authentication middleware.
func (s *Store) ChangeOwnPassword(ctx context.Context, userID, expectedVersion int, verifiedHash, newHash string) (int, error) {
	var version int
	err := s.db.QueryRowxContext(ctx, `
		UPDATE "user" SET password = $4, token_version = token_version + 1
		WHERE id = $1 AND token_version = $2 AND password = $3 AND is_active = TRUE
		RETURNING token_version
	`, userID, expectedVersion, verifiedHash, newHash).Scan(&version)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, ErrSessionChanged
	}
	return version, err
}
