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

// These exact hashes were published by the historical demo-data migration.
// Match hashes, not emails: renamed demos remain unsafe; changed passwords do not.
var legacyDemoPasswordHashes = []string{
	"$2a$12$7k/eb090r0jNWF3aMgBXw.V9p9U.7x4c2eNNbP0TAYqUEIPMX0nwG",
	"$2a$12$TGNopcSVE8D6P4GxsPWbneNjP0Iua1ngopHciWaOPYebih8O1myoC",
	"$2a$12$.dBosWNEr/3HJkA19X885eUk7OiK2CNaRTkAAg/HIPlQEKpCjS1..",
}

// IsLegacyDemoPasswordHash identifies credentials shipped as public demo data.
func IsLegacyDemoPasswordHash(hash string) bool {
	for _, candidate := range legacyDemoPasswordHashes {
		if hash == candidate {
			return true
		}
	}
	return false
}

// AdminBootstrap describes the initial administrator provisioned from environment.
type AdminBootstrap struct {
	Email        string
	PasswordHash string
	FirstName    string
	LastName     string
}

// BootstrapAdmin provisions the administrator account from configuration and disables
// unchanged demo credentials, preserving accounts and application membership.
// It is idempotent and safe to run on every start.
func (s *Store) BootstrapAdmin(ctx context.Context, cfg AdminBootstrap) error {
	email := strings.ToLower(strings.TrimSpace(cfg.Email))
	hash := strings.TrimSpace(cfg.PasswordHash)

	if hash != "" {
		if _, err := bcrypt.Cost([]byte(hash)); err != nil || IsLegacyDemoPasswordHash(hash) {
			return errors.New("ADMIN_PASSWORD_HASH must be a valid bcrypt hash of a new, non-demo password")
		}
	}
	if email == "" {
		if _, err := disableLegacyDemoUsers(ctx, s.db, 0); err != nil {
			return err
		}
		return s.warnIfNoAdmin(ctx)
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
		SELECT id FROM "user" WHERE lower(trim(email)) = $1 LIMIT 1
	`, email).Scan(&adminID)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return fmt.Errorf("lookup admin user: %w", err)
	}
	exists := !errors.Is(err, sql.ErrNoRows)

	// Without an explicit hash, provision a one-time random password rather than shipping a
	// known credential. It is printed once and never stored in plain text.
	generatedPassword := ""
	if hash == "" && !exists {
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

	switch {
	case !exists:
		if err = tx.QueryRowContext(ctx, `
			INSERT INTO "user" (first_name, last_name, email, password, role)
			VALUES ($1, $2, $3, $4, 'admin')
			RETURNING id
		`, firstName, strings.TrimSpace(cfg.LastName), email, hash).Scan(&adminID); err != nil {
			return fmt.Errorf("create admin user: %w", err)
		}
		s.logger.Info("bootstrap: administrator created", "email", email)
		if generatedPassword != "" {
			s.logger.Warn("bootstrap: generated a one-time administrator password; sign in and change it",
				"email", email, "password", generatedPassword)
		}
	case hash != "":
		// A replacement hash reactivates the configured administrator and
		// revokes old sessions. An unchanged restart leaves sessions intact.
		if _, err = tx.ExecContext(ctx, `
            UPDATE "user" SET password = $2, role = 'admin', is_active = TRUE,
                token_version = token_version + 1
            WHERE id = $1 AND (password <> $2 OR is_active = FALSE
                OR lower(trim(role)) NOT IN ('admin','administrator','superadmin'))
		`, adminID, hash); err != nil {
			return fmt.Errorf("update admin user: %w", err)
		}
	default:
		// No replacement credential: never reactivate a disabled account.
		if _, err = tx.ExecContext(ctx, `
            UPDATE "user" SET role = 'admin', token_version = token_version + 1
            WHERE id = $1 AND is_active = TRUE
                AND lower(trim(role)) NOT IN ('admin','administrator','superadmin')
		`, adminID); err != nil {
			return fmt.Errorf("promote admin user: %w", err)
		}
	}

	disabled, err := disableLegacyDemoUsers(ctx, tx, adminID)
	if err != nil {
		return err
	}
	if disabled > 0 {
		s.logger.Warn("bootstrap: disabled unchanged demo credentials", "count", disabled)
	}

	adopted, err := adoptOrphanedApplications(ctx, tx, adminID)
	if err != nil {
		return err
	}
	if adopted > 0 {
		s.logger.Warn("bootstrap: assigned applications that had no members to the administrator",
			"count", adopted, "email", email)
	}

	return tx.Commit()
}

// execQuerier is the subset of a transaction used by the bootstrap helpers.
type execQuerier interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
}

// Preserve identity/history and memberships while disabling public credentials.
// Only the configured administrator inherits access owned by unchanged demos.
func disableLegacyDemoUsers(ctx context.Context, tx execQuerier, adminID int) (int64, error) {
	hashes := make([]any, 0, len(legacyDemoPasswordHashes))
	for _, hash := range legacyDemoPasswordHashes {
		hashes = append(hashes, hash)
	}
	if adminID > 0 {
		args := append([]any{adminID}, hashes...)
		if _, err := tx.ExecContext(ctx, fmt.Sprintf(`
            INSERT INTO user_application (user_id, application_id)
            SELECT DISTINCT $1, ua.application_id FROM user_application ua
            JOIN "user" u ON u.id = ua.user_id
            WHERE u.password IN (%s) AND NOT EXISTS (
                SELECT 1 FROM user_application existing
                WHERE existing.user_id = $1 AND existing.application_id = ua.application_id
            )
        `, placeholders(len(hashes), 2)), args...); err != nil {
			return 0, fmt.Errorf("adopt demo application access: %w", err)
		}
	}
	result, err := tx.ExecContext(ctx, fmt.Sprintf(`
        UPDATE "user" SET is_active = FALSE, token_version = token_version + 1
        WHERE password IN (%s) AND is_active = TRUE
    `, placeholders(len(hashes), 1)), hashes...)
	if err != nil {
		return 0, fmt.Errorf("disable demo credentials: %w", err)
	}
	return result.RowsAffected()
}

// placeholders renders "$start, $start+1, ..." for count bind parameters.
func placeholders(count, start int) string {
	parts := make([]string, count)
	for i := range parts {
		parts[i] = fmt.Sprintf("$%d", start+i)
	}
	return strings.Join(parts, ", ")
}

// adoptOrphanedApplications gives the administrator membership of every application that
// nobody belongs to. Access is derived from membership, so on an upgrade — where the only
// member may have been a purged demo account — an unowned application would otherwise
// become invisible to everyone while its pipelines keep running.
func adoptOrphanedApplications(ctx context.Context, tx execQuerier, adminID int) (int64, error) {
	res, err := tx.ExecContext(ctx, `
		INSERT INTO user_application (user_id, application_id)
		SELECT $1, a.id
		FROM application a
		WHERE NOT EXISTS (SELECT 1 FROM user_application ua WHERE ua.application_id = a.id)
	`, adminID)
	if err != nil {
		return 0, fmt.Errorf("adopt orphaned applications: %w", err)
	}

	affected, err := res.RowsAffected()
	if err != nil {
		return 0, nil
	}
	return affected, nil
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
		SELECT count(*) FROM "user" WHERE is_active = TRUE AND lower(trim(role)) IN ('admin', 'administrator', 'superadmin')
	`).Scan(&admins); err != nil {
		return fmt.Errorf("count administrators: %w", err)
	}
	if admins == 0 {
		s.logger.Error("no administrator account exists; set ADMIN_EMAIL and ADMIN_PASSWORD_HASH to provision one")
	}
	return nil
}
