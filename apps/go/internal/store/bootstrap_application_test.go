package store

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"testing"

	"github.com/jmoiron/sqlx"
	_ "modernc.org/sqlite"
)

const bootstrapTestSchema = `
CREATE TABLE application (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	name TEXT NOT NULL,
	description TEXT NULL,
	created_at TIMESTAMP NULL
);
CREATE TABLE api_key (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	application_id INTEGER NOT NULL,
	name TEXT NULL,
	key TEXT NULL,
		key_hash TEXT NULL,
		key_prefix TEXT NULL,
	created_at TIMESTAMP NULL,
	last_used TIMESTAMP NULL,
	disabled_at TIMESTAMP NULL,
	expires_at TIMESTAMP NULL
);
`

func setupBootstrapStore(t *testing.T) (*Store, *sqlx.DB) {
	t.Helper()

	db, err := sqlx.Open("sqlite", testDSN("bootstrap_app_test"))
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if _, err = db.Exec(bootstrapTestSchema); err != nil {
		_ = db.Close()
		t.Fatalf("create schema: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	return New(db, slog.New(slog.NewTextHandler(io.Discard, nil))), db
}

func TestBootstrapApplication(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	t.Run("creates the application and its key on a fresh host", func(t *testing.T) {
		t.Parallel()
		st, db := setupBootstrapStore(t)

		result, err := st.BootstrapApplication(ctx, ApplicationBootstrap{
			Name:   "tiqet",
			APIKey: "bootstrap-secret",
		})
		if err != nil {
			t.Fatalf("bootstrap: %v", err)
		}
		if !result.CreatedApp || !result.CreatedAPIKey || result.AlreadyExisted {
			t.Fatalf("unexpected result %+v", result)
		}
		if result.AppID != "app_1" {
			t.Fatalf("appId = %q, want app_1", result.AppID)
		}

		appID, err := st.ValidateAPIKey(ctx, "bootstrap-secret")
		if err != nil {
			t.Fatalf("bootstrapped key must authenticate: %v", err)
		}
		if appID != result.ApplicationID {
			t.Fatalf("key resolves to application %d, want %d", appID, result.ApplicationID)
		}

		var name string
		if err := db.Get(&name, `SELECT name FROM api_key WHERE key_hash = $1`, HashAPIKey("bootstrap-secret")); err != nil {
			t.Fatalf("load key row: %v", err)
		}
		if name != "bootstrap" {
			t.Fatalf("key name = %q, want bootstrap", name)
		}
	})

	t.Run("is a no-op once the application exists", func(t *testing.T) {
		t.Parallel()
		st, db := setupBootstrapStore(t)

		cfg := ApplicationBootstrap{Name: "tiqet", APIKey: "first-key"}
		if _, err := st.BootstrapApplication(ctx, cfg); err != nil {
			t.Fatalf("first bootstrap: %v", err)
		}

		// A restart with a rotated env var must not resurrect access.
		cfg.APIKey = "second-key"
		result, err := st.BootstrapApplication(ctx, cfg)
		if err != nil {
			t.Fatalf("second bootstrap: %v", err)
		}
		if result.CreatedApp || result.CreatedAPIKey || !result.AlreadyExisted {
			t.Fatalf("unexpected result %+v", result)
		}

		var apps, keys int
		if err := db.Get(&apps, `SELECT COUNT(*) FROM application`); err != nil {
			t.Fatalf("count applications: %v", err)
		}
		if err := db.Get(&keys, `SELECT COUNT(*) FROM api_key`); err != nil {
			t.Fatalf("count keys: %v", err)
		}
		if apps != 1 || keys != 1 {
			t.Fatalf("applications=%d keys=%d, want 1 and 1", apps, keys)
		}
		if _, err := st.ValidateAPIKey(ctx, "second-key"); err == nil {
			t.Fatalf("a rotated BOOTSTRAP_API_KEY must not be inserted for an existing application")
		}
	})

	t.Run("matches an existing application case-insensitively", func(t *testing.T) {
		t.Parallel()
		st, db := setupBootstrapStore(t)

		if _, err := db.Exec(`INSERT INTO application (name) VALUES ($1)`, "Tiqet"); err != nil {
			t.Fatalf("seed application: %v", err)
		}

		result, err := st.BootstrapApplication(ctx, ApplicationBootstrap{Name: "tiqet", APIKey: "k"})
		if err != nil {
			t.Fatalf("bootstrap: %v", err)
		}
		if result.CreatedApp {
			t.Fatalf("must not create a second application differing only in case")
		}
	})

	t.Run("an asserted app id that does not match fails startup", func(t *testing.T) {
		t.Parallel()
		st, db := setupBootstrapStore(t)

		_, err := st.BootstrapApplication(ctx, ApplicationBootstrap{
			Name:   "tiqet",
			AppID:  "app_99",
			APIKey: "k",
		})
		if !errors.Is(err, ErrApplicationQueueIDMismatch) {
			t.Fatalf("err = %v, want ErrApplicationQueueIDMismatch", err)
		}

		var apps int
		if err := db.Get(&apps, `SELECT COUNT(*) FROM application`); err != nil {
			t.Fatalf("count applications: %v", err)
		}
		if apps != 0 {
			t.Fatalf("a failed assertion must roll back, found %d applications", apps)
		}
	})

	t.Run("a matching app id assertion succeeds", func(t *testing.T) {
		t.Parallel()
		st, _ := setupBootstrapStore(t)

		result, err := st.BootstrapApplication(ctx, ApplicationBootstrap{
			Name:   "tiqet",
			AppID:  "app_1",
			APIKey: "k",
		})
		if err != nil {
			t.Fatalf("bootstrap: %v", err)
		}
		if result.AppID != "app_1" {
			t.Fatalf("appId = %q", result.AppID)
		}
	})

	t.Run("an unset name does nothing", func(t *testing.T) {
		t.Parallel()
		st, db := setupBootstrapStore(t)

		result, err := st.BootstrapApplication(ctx, ApplicationBootstrap{Name: "  "})
		if err != nil || result != nil {
			t.Fatalf("result=%+v err=%v, want nil and nil", result, err)
		}

		var apps int
		if err := db.Get(&apps, `SELECT COUNT(*) FROM application`); err != nil {
			t.Fatalf("count applications: %v", err)
		}
		if apps != 0 {
			t.Fatalf("applications = %d, want 0", apps)
		}
	})

	t.Run("an application without a key is still created", func(t *testing.T) {
		t.Parallel()
		st, db := setupBootstrapStore(t)

		result, err := st.BootstrapApplication(ctx, ApplicationBootstrap{Name: "tiqet"})
		if err != nil {
			t.Fatalf("bootstrap: %v", err)
		}
		if !result.CreatedApp || result.CreatedAPIKey {
			t.Fatalf("unexpected result %+v", result)
		}

		var keys int
		if err := db.Get(&keys, `SELECT COUNT(*) FROM api_key`); err != nil {
			t.Fatalf("count keys: %v", err)
		}
		if keys != 0 {
			t.Fatalf("keys = %d, want 0", keys)
		}
	})
}
