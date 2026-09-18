package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jmoiron/sqlx"

	"pipelogiq/internal/types"
)

var (
	ErrUserNotFound       = errors.New("user not found")
	ErrEmailInUse         = errors.New("email is already in use")
	ErrLastAdmin          = errors.New("at least one active administrator is required")
	ErrSelfDeactivation   = errors.New("you cannot deactivate or demote your own account")
	ErrInvalidApplication = errors.New("one or more applications do not exist")
	ErrInvalidUserRole    = errors.New("invalid user role")
)

const managedUserColumns = `id, first_name, last_name, email, role, is_active, created_at, token_version`

func (s *Store) ListUsers(ctx context.Context) ([]types.ManagedUser, error) {
	users := []types.ManagedUser{}
	if err := s.db.SelectContext(ctx, &users, `SELECT `+managedUserColumns+` FROM "user" ORDER BY id`); err != nil {
		return nil, err
	}
	var memberships []struct {
		UserID        int `db:"user_id"`
		ApplicationID int `db:"application_id"`
	}
	if err := s.db.SelectContext(ctx, &memberships, `SELECT DISTINCT user_id, application_id FROM user_application ORDER BY user_id, application_id`); err != nil {
		return nil, err
	}
	byUser := make(map[int][]int)
	for _, membership := range memberships {
		byUser[membership.UserID] = append(byUser[membership.UserID], membership.ApplicationID)
	}
	for i := range users {
		users[i].Role = types.NormalizeUserRole(users[i].Role)
		users[i].ApplicationIDs = byUser[users[i].ID]
		if users[i].ApplicationIDs == nil {
			users[i].ApplicationIDs = []int{}
		}
	}
	return users, nil
}

func (s *Store) ListUserApplications(ctx context.Context) ([]types.UserApplication, error) {
	applications := []types.UserApplication{}
	err := s.db.SelectContext(ctx, &applications, `SELECT id, name, description FROM application ORDER BY name, id`)
	return applications, err
}

func (s *Store) CreateUser(ctx context.Context, req types.SaveUserRequest, passwordHash string) (*types.ManagedUser, error) {
	if !types.IsValidUserRole(req.Role) {
		return nil, ErrInvalidUserRole
	}
	tx, err := s.db.BeginTxx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	if err := s.lockUserManagement(ctx, tx); err != nil {
		return nil, err
	}
	applicationIDs, err := validateUserApplications(ctx, tx, req.ApplicationIDs)
	if err != nil {
		return nil, err
	}
	if err := checkUserEmailAvailable(ctx, tx, req.Email, 0); err != nil {
		return nil, err
	}
	var user types.ManagedUser
	err = tx.GetContext(ctx, &user, `
		INSERT INTO "user" (first_name, last_name, email, password, role, is_active)
		VALUES ($1, $2, $3, $4, $5, $6) RETURNING `+managedUserColumns,
		strings.TrimSpace(req.FirstName), req.LastName, strings.ToLower(strings.TrimSpace(req.Email)), passwordHash,
		types.NormalizeUserRole(req.Role), req.IsActive)
	if err != nil {
		return nil, userWriteError(err)
	}
	if err := replaceUserApplications(ctx, tx, user.ID, applicationIDs); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	user.ApplicationIDs = applicationIDs
	return &user, nil
}

func (s *Store) UpdateUser(ctx context.Context, actorID, targetID int, req types.SaveUserRequest) (*types.ManagedUser, error) {
	if !types.IsValidUserRole(req.Role) {
		return nil, ErrInvalidUserRole
	}
	req.Role = types.NormalizeUserRole(req.Role)
	tx, err := s.db.BeginTxx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	if err := s.lockUserManagement(ctx, tx); err != nil {
		return nil, err
	}
	var existing types.UserResponse
	if err := tx.GetContext(ctx, &existing, `SELECT `+managedUserColumns+` FROM "user" WHERE id = $1`, targetID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrUserNotFound
		}
		return nil, err
	}
	if targetID == actorID && (!req.IsActive || req.Role != types.UserRoleAdmin) {
		return nil, ErrSelfDeactivation
	}
	if existing.IsActive && types.NormalizeUserRole(existing.Role) == types.UserRoleAdmin && (!req.IsActive || req.Role != types.UserRoleAdmin) {
		var admins int
		if err := tx.GetContext(ctx, &admins, `SELECT count(*) FROM "user" WHERE is_active = TRUE AND lower(trim(role)) IN ('admin', 'administrator', 'superadmin')`); err != nil {
			return nil, err
		}
		if admins <= 1 {
			return nil, ErrLastAdmin
		}
	}
	applicationIDs, err := validateUserApplications(ctx, tx, req.ApplicationIDs)
	if err != nil {
		return nil, err
	}
	if err := checkUserEmailAvailable(ctx, tx, req.Email, targetID); err != nil {
		return nil, err
	}
	var user types.ManagedUser
	err = tx.GetContext(ctx, &user, `
		UPDATE "user" SET first_name = $1, last_name = $2, email = $3, role = $4,
			is_active = $5, token_version = token_version + 1
		WHERE id = $6 RETURNING `+managedUserColumns,
		strings.TrimSpace(req.FirstName), req.LastName, strings.ToLower(strings.TrimSpace(req.Email)), req.Role, req.IsActive, targetID)
	if err != nil {
		return nil, userWriteError(err)
	}
	if err := replaceUserApplications(ctx, tx, targetID, applicationIDs); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	user.ApplicationIDs = applicationIDs
	return &user, nil
}

// Serialize management writes before reading the current administrator count.
// Row locks on just the target are insufficient: two administrators could each
// concurrently demote the other after observing two active administrators.
func (s *Store) lockUserManagement(ctx context.Context, tx *sqlx.Tx) error {
	if s.isSQLiteDriver() {
		// Acquire SQLite's write lock before any snapshot reads.
		_, err := tx.ExecContext(ctx, `UPDATE "user" SET id = id WHERE 1 = 0`)
		return err
	}
	_, err := tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock(1885958253, 1970496882)`)
	return err
}

func checkUserEmailAvailable(ctx context.Context, tx *sqlx.Tx, email string, excludingID int) error {
	var exists bool
	if err := tx.GetContext(ctx, &exists, `SELECT EXISTS (SELECT 1 FROM "user" WHERE lower(trim(email)) = lower(trim($1)) AND id != $2)`, email, excludingID); err != nil {
		return err
	}
	if exists {
		return ErrEmailInUse
	}
	return nil
}

func validateUserApplications(ctx context.Context, tx *sqlx.Tx, requested []int) ([]int, error) {
	ids := make([]int, 0, len(requested))
	seen := make(map[int]struct{}, len(requested))
	for _, id := range requested {
		if id <= 0 {
			return nil, ErrInvalidApplication
		}
		if _, exists := seen[id]; !exists {
			seen[id] = struct{}{}
			ids = append(ids, id)
		}
	}
	sort.Ints(ids)
	if len(ids) == 0 {
		return ids, nil
	}
	query, args, err := sqlx.In(`SELECT count(*) FROM application WHERE id IN (?)`, ids)
	if err != nil {
		return nil, err
	}
	var count int
	if err := tx.GetContext(ctx, &count, tx.Rebind(query), args...); err != nil {
		return nil, err
	}
	if count != len(ids) {
		return nil, ErrInvalidApplication
	}
	return ids, nil
}

func replaceUserApplications(ctx context.Context, tx *sqlx.Tx, userID int, ids []int) error {
	if _, err := tx.ExecContext(ctx, `DELETE FROM user_application WHERE user_id = $1`, userID); err != nil {
		return err
	}
	for _, id := range ids {
		if _, err := tx.ExecContext(ctx, `INSERT INTO user_application (user_id, application_id) VALUES ($1, $2)`, userID, id); err != nil {
			return fmt.Errorf("update application membership: %w", err)
		}
	}
	return nil
}

func userWriteError(err error) error {
	if err == nil {
		return nil
	}
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == "23505" && (pgErr.ConstraintName == "uq_user_email_normalized" || strings.Contains(pgErr.ConstraintName, "email")) {
		return ErrEmailInUse
	}
	if strings.Contains(strings.ToLower(err.Error()), "unique constraint failed") && strings.Contains(err.Error(), "uq_user_email_normalized") {
		return ErrEmailInUse
	}
	return err
}
