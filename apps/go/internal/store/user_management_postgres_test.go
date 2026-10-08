package store

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/stdlib"
	"github.com/jmoiron/sqlx"
)

// Isolated tables let these race tests run against an existing PostgreSQL
// service without changing application accounts or their memberships.
func setupPostgresUserManagementStore(t *testing.T) *Store {
	t.Helper()
	dsn := os.Getenv("PIPELOGIQ_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("requires PIPELOGIQ_TEST_DATABASE_URL")
	}
	admin, err := sqlx.Connect("pgx", dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = admin.Close() })
	schema := fmt.Sprintf("user_management_%d", time.Now().UnixNano())
	if _, err := admin.Exec(`CREATE SCHEMA ` + schema); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if _, err := admin.Exec(`DROP SCHEMA ` + schema + ` CASCADE`); err != nil {
			t.Error(err)
		}
	})
	config, err := pgx.ParseConfig(dsn)
	if err != nil {
		t.Fatal(err)
	}
	config.RuntimeParams["search_path"] = schema
	db := sqlx.NewDb(stdlib.OpenDB(*config), "pgx")
	t.Cleanup(func() { _ = db.Close() })
	if _, err := db.Exec(`
		CREATE TABLE "user" (
			id SERIAL PRIMARY KEY, first_name VARCHAR(255) NOT NULL, last_name VARCHAR(255),
			email VARCHAR(255) NOT NULL, password VARCHAR(255) NOT NULL, role VARCHAR(100) NOT NULL,
			is_active BOOLEAN NOT NULL DEFAULT TRUE, created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
			token_version INTEGER NOT NULL DEFAULT 0
		);
		CREATE UNIQUE INDEX uq_user_email_normalized ON "user" (lower(trim(email)));
		CREATE TABLE application (id SERIAL PRIMARY KEY, name VARCHAR(255) NOT NULL, description TEXT);
		CREATE TABLE user_application (
			id SERIAL PRIMARY KEY, user_id INTEGER NOT NULL REFERENCES "user"(id),
			application_id INTEGER NOT NULL REFERENCES application(id)
		);
	`); err != nil {
		t.Fatal(err)
	}
	return New(db, slog.New(slog.NewTextHandler(io.Discard, nil)))
}

func TestPostgresUserManagementConcurrentAdminDemotion(t *testing.T) {
	st := setupPostgresUserManagementStore(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	first, err := st.CreateUser(ctx, managedUserRequest("first@example.com", "admin"), "hash")
	if err != nil {
		t.Fatal(err)
	}
	second, err := st.CreateUser(ctx, managedUserRequest("second@example.com", "admin"), "hash")
	if err != nil {
		t.Fatal(err)
	}
	start := make(chan struct{})
	results := make(chan error, 2)
	for _, ids := range [][2]int{{first.ID, second.ID}, {second.ID, first.ID}} {
		go func(actorID, targetID int) {
			<-start
			_, err := st.UpdateUser(ctx, actorID, targetID, managedUserRequest(fmt.Sprintf("user%d@example.com", targetID), "viewer"))
			results <- err
		}(ids[0], ids[1])
	}
	close(start)
	var succeeded, protected int
	for i := 0; i < 2; i++ {
		err := <-results
		switch {
		case err == nil:
			succeeded++
		case errors.Is(err, ErrLastAdmin):
			protected++
		default:
			t.Fatalf("concurrent administrator update failed: %v", err)
		}
	}
	var activeAdmins int
	if err := st.db.GetContext(ctx, &activeAdmins, `SELECT count(*) FROM "user" WHERE role = 'admin' AND is_active`); err != nil {
		t.Fatal(err)
	}
	if succeeded != 1 || protected != 1 || activeAdmins != 1 {
		t.Fatalf("concurrent demotion: successful=%d protected=%d activeAdmins=%d", succeeded, protected, activeAdmins)
	}
}

func TestPostgresUserManagementConcurrentDuplicateEmails(t *testing.T) {
	st := setupPostgresUserManagementStore(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	start := make(chan struct{})
	results := make(chan error, 2)
	for _, email := range []string{" New@Example.com ", "new@example.com"} {
		go func(email string) {
			<-start
			_, err := st.CreateUser(ctx, managedUserRequest(email, "viewer"), "hash")
			results <- err
		}(email)
	}
	close(start)
	var succeeded, rejected int
	for i := 0; i < 2; i++ {
		err := <-results
		switch {
		case err == nil:
			succeeded++
		case errors.Is(err, ErrEmailInUse):
			rejected++
		default:
			t.Fatalf("concurrent create failed: %v", err)
		}
	}
	if succeeded != 1 || rejected != 1 {
		t.Fatalf("concurrent creation: successful=%d rejected=%d", succeeded, rejected)
	}
	if _, err := st.db.ExecContext(ctx, `INSERT INTO "user" (first_name,email,password,role) VALUES ('Duplicate',' NEW@EXAMPLE.COM ','hash','viewer')`); !errors.Is(userWriteError(err), ErrEmailInUse) {
		t.Fatalf("database duplicate protection = %v", err)
	}
}

func TestPostgresBootstrapAdminAdoptsLegacyDemoAccess(t *testing.T) {
	st := setupPostgresUserManagementStore(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if _, err := st.db.ExecContext(ctx, `INSERT INTO application (name) VALUES ($1)`, "Legacy application"); err != nil {
		t.Fatal(err)
	}
	admin, err := st.CreateUser(ctx, managedUserRequest("admin@example.com", "admin"), "private-admin-hash")
	if err != nil {
		t.Fatal(err)
	}
	demo, err := st.CreateUser(ctx, managedUserRequest("renamed-demo@example.com", "viewer", 1), legacyDemoPasswordHashes[0])
	if err != nil {
		t.Fatal(err)
	}

	if err := st.BootstrapAdmin(ctx, AdminBootstrap{Email: admin.Email}); err != nil {
		t.Fatalf("bootstrap administrator: %v", err)
	}
	adminAccess, err := st.GetUserAccess(ctx, admin.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(adminAccess.ApplicationIDs) != 1 || adminAccess.ApplicationIDs[0] != 1 {
		t.Fatalf("administrator access = %v, want [1]", adminAccess.ApplicationIDs)
	}
	demoAccess, err := st.GetUserAccess(ctx, demo.ID)
	if err != nil {
		t.Fatal(err)
	}
	if demoAccess.IsActive {
		t.Fatal("legacy demo user remains active")
	}
}

func TestPostgresUserManagementConcurrentPasswordChange(t *testing.T) {
	testConcurrentOwnPasswordChange(t, setupPostgresUserManagementStore(t))
}
