package store

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"

	"golang.org/x/crypto/bcrypt"
)

// legacyDemoEmails are the accounts created by the legacy "seed users and team"
// changeset. Their bcrypt hashes are published in the repository history, so they
// must never survive into a running deployment.
var legacyDemoEmails = []string{"jegor@gmail.com", "leo@gmail.com", "ww@gmail.com"}

// AdminBootstrap describes the initial administrator provisioned from environment.
type AdminBootstrap struct {
	Email        string
	PasswordHash string
	FirstName    string
	LastName     string
}

// BootstrapAdmin provisions the administrator account from configuration and removes
// the legacy demo accounts, transferring their application membership to the admin.
// It is idempotent and safe to run on every start.
func (s *Store) BootstrapAdmin(ctx context.Context, cfg AdminBootstrap) error {
	email := strings.ToLower(strings.TrimSpace(cfg.Email))
	hash := strings.TrimSpace(cfg.PasswordHash)

	if email == "" {
		return s.warnIfNoAdmin(ctx)
	}

	// Without an explicit hash, provision a one-time random password rather than shipping a
	// known credential. It is printed once and never stored in plain text.
	generatedPassword := ""
	if hash == "" {
		exists, err := s.userExists(ctx, email)
		if err != nil {
			return err
		}
		if exists {
			return s.warnIfNoAdmin(ctx)
		}

		generatedPassword, err = generatePassword()
		if err != nil {
			return fmt.Errorf("generate admin password: %w", err)
		}
		encoded, err := bcrypt.GenerateFromPassword([]byte(generatedPassword), bcrypt.DefaultCost)
		if err != nil {
			return fmt.Errorf("hash admin password: %w", err)
		}
		hash = string(encoded)
	}

	tx, err := s.db.BeginTxx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	firstName := strings.TrimSpace(cfg.FirstName)
	if firstName == "" {
		firstName = "Admin"
	}

	var adminID int
	err = tx.QueryRowContext(ctx, `
		SELECT id FROM "user" WHERE lower(email) = $1 LIMIT 1
	`, email).Scan(&adminID)

	switch {
	case errors.Is(err, sql.ErrNoRows):
		if err = tx.QueryRowContext(ctx, `
			INSERT INTO "user" (first_name, last_name, email, password, role)
			VALUES ($1, $2, $3, $4, 'Admin')
			RETURNING id
		`, firstName, strings.TrimSpace(cfg.LastName), email, hash).Scan(&adminID); err != nil {
			return fmt.Errorf("create admin user: %w", err)
		}
		s.logger.Info("bootstrap: administrator created", "email", email)
		if generatedPassword != "" {
			s.logger.Warn("bootstrap: generated a one-time administrator password; sign in and change it",
				"email", email, "password", generatedPassword)
		}
	case err != nil:
		return fmt.Errorf("lookup admin user: %w", err)
	default:
		if _, err = tx.ExecContext(ctx, `
			UPDATE "user" SET password = $2, role = 'Admin' WHERE id = $1
		`, adminID, hash); err != nil {
			return fmt.Errorf("update admin user: %w", err)
		}
	}

	removed, err := purgeLegacyDemoUsers(ctx, tx, adminID, email)
	if err != nil {
		return err
	}
	if removed > 0 {
		s.logger.Warn("bootstrap: removed legacy demo accounts seeded by migration", "count", removed)
	}

	return tx.Commit()
}

// execQuerier is the subset of a transaction used by the bootstrap helpers.
type execQuerier interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
}

// purgeLegacyDemoUsers moves application membership of the legacy demo accounts to the
// administrator and deletes those accounts. The administrator's own email is preserved
// so that reusing a legacy address as ADMIN_EMAIL stays supported.
func purgeLegacyDemoUsers(ctx context.Context, tx execQuerier, adminID int, adminEmail string) (int64, error) {
	targets := make([]any, 0, len(legacyDemoEmails))
	for _, candidate := range legacyDemoEmails {
		if candidate != adminEmail {
			targets = append(targets, candidate)
		}
	}
	if len(targets) == 0 {
		return 0, nil
	}

	// $1 is reserved for adminID in the transfer statement, so demo emails start at $2.
	transferArgs := append([]any{adminID}, targets...)
	if _, err := tx.ExecContext(ctx, fmt.Sprintf(`
		INSERT INTO user_application (user_id, application_id)
		SELECT $1, ua.application_id
		FROM user_application ua
		JOIN "user" u ON u.id = ua.user_id
		WHERE lower(u.email) IN (%s)
		  AND NOT EXISTS (
			SELECT 1 FROM user_application existing
			WHERE existing.user_id = $1 AND existing.application_id = ua.application_id
		  )
	`, placeholders(len(targets), 2)), transferArgs...); err != nil {
		return 0, fmt.Errorf("transfer application membership: %w", err)
	}

	list := placeholders(len(targets), 1)
	for _, table := range []string{"user_application", "user_team"} {
		if _, err := tx.ExecContext(ctx, fmt.Sprintf(`
			DELETE FROM %s
			WHERE user_id IN (SELECT id FROM "user" WHERE lower(email) IN (%s))
		`, table, list), targets...); err != nil {
			return 0, fmt.Errorf("clear %s for demo users: %w", table, err)
		}
	}

	res, err := tx.ExecContext(ctx, fmt.Sprintf(`
		DELETE FROM "user" WHERE lower(email) IN (%s)
	`, list), targets...)
	if err != nil {
		return 0, fmt.Errorf("delete demo users: %w", err)
	}

	affected, err := res.RowsAffected()
	if err != nil {
		return 0, nil
	}
	return affected, nil
}

// placeholders renders "$start, $start+1, ..." for count bind parameters.
func placeholders(count, start int) string {
	parts := make([]string, count)
	for i := range parts {
		parts[i] = fmt.Sprintf("$%d", start+i)
	}
	return strings.Join(parts, ", ")
}

// userExists reports whether an account with the given email is already present.
func (s *Store) userExists(ctx context.Context, email string) (bool, error) {
	var exists bool
	if err := s.db.QueryRowxContext(ctx, `
		SELECT EXISTS (SELECT 1 FROM "user" WHERE lower(email) = $1)
	`, email).Scan(&exists); err != nil {
		return false, fmt.Errorf("lookup user by email: %w", err)
	}
	return exists, nil
}

// generatePassword returns a URL-safe random password with 144 bits of entropy.
func generatePassword() (string, error) {
	buf := make([]byte, 18)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(buf), nil
}

// warnIfNoAdmin reports a misconfiguration when nobody can sign in as administrator.
func (s *Store) warnIfNoAdmin(ctx context.Context) error {
	var admins int
	if err := s.db.QueryRowxContext(ctx, `
		SELECT count(*) FROM "user" WHERE lower(role) IN ('admin', 'administrator', 'superadmin')
	`).Scan(&admins); err != nil {
		return fmt.Errorf("count administrators: %w", err)
	}
	if admins == 0 {
		s.logger.Error("no administrator account exists; set ADMIN_EMAIL and ADMIN_PASSWORD_HASH to provision one")
	}
	return nil
}
