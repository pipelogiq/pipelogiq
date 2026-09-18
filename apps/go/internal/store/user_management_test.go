package store

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"reflect"
	"strings"
	"testing"

	"github.com/jmoiron/sqlx"
	_ "modernc.org/sqlite"

	"pipelogiq/internal/types"
)

const userManagementTestSchema = `
CREATE TABLE "user" (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	first_name TEXT NOT NULL,
	last_name TEXT,
	email TEXT NOT NULL,
	password TEXT NOT NULL,
	role TEXT NOT NULL,
	is_active BOOLEAN NOT NULL DEFAULT TRUE,
	created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
	token_version INTEGER NOT NULL DEFAULT 0
);
CREATE UNIQUE INDEX uq_user_email_normalized ON "user" (lower(trim(email)));
CREATE TABLE application (id INTEGER PRIMARY KEY, name TEXT NOT NULL, description TEXT);
CREATE TABLE user_application (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	user_id INTEGER NOT NULL REFERENCES "user"(id),
	application_id INTEGER NOT NULL REFERENCES application(id)
);
INSERT INTO application (id, name) VALUES (1, 'First'), (2, 'Second');
`

func setupUserManagementStore(t *testing.T) (*Store, *sqlx.DB) {
	t.Helper()
	db, err := sqlx.Open("sqlite", testDSN("user_management"))
	if err != nil {
		t.Fatal(err)
	}
	db.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = db.Close() })
	if _, err := db.Exec(userManagementTestSchema); err != nil {
		t.Fatal(err)
	}
	return New(db, slog.New(slog.NewTextHandler(io.Discard, nil))), db
}

func managedUserRequest(email, role string, applicationIDs ...int) types.SaveUserRequest {
	return types.SaveUserRequest{FirstName: "Test", Email: email, Role: role, IsActive: true, ApplicationIDs: applicationIDs}
}

func TestUserManagementCreateAndList(t *testing.T) {
	st, db := setupUserManagementStore(t)
	ctx := context.Background()
	user, err := st.CreateUser(ctx, managedUserRequest("  Person@Example.com  ", " user ", 2, 1, 2), "private-password-hash")
	if err != nil {
		t.Fatal(err)
	}
	if user.Email != "person@example.com" || user.Role != "operator" || !user.IsActive || user.TokenVersion != 0 || !reflect.DeepEqual(user.ApplicationIDs, []int{1, 2}) {
		t.Fatalf("unexpected new user: %+v", user)
	}
	if user.CreatedAt.IsZero() {
		t.Fatal("createdAt was not populated")
	}
	if _, err := st.CreateUser(ctx, managedUserRequest("PERSON@example.com", "viewer"), "other-hash"); !errors.Is(err, ErrEmailInUse) {
		t.Fatalf("case-insensitive duplicate error = %v", err)
	}
	if _, err := db.Exec(`INSERT INTO "user" (first_name,email,password,role) VALUES ('Duplicate',' PERSON@example.com ','hash','viewer')`); err == nil {
		t.Fatal("database accepted duplicate normalized email")
	} else if !errors.Is(userWriteError(err), ErrEmailInUse) {
		t.Fatalf("database duplicate was not mapped: %v", err)
	}
	users, err := st.ListUsers(ctx)
	if err != nil || len(users) != 1 || !reflect.DeepEqual(users[0], *user) {
		t.Fatalf("ListUsers = %+v, %v", users, err)
	}
	encoded, err := json.Marshal(users)
	if err != nil {
		t.Fatal(err)
	}
	for _, secret := range []string{"password", "private-password-hash", "tokenVersion", "token_version"} {
		if strings.Contains(string(encoded), secret) {
			t.Fatalf("management response contains %q: %s", secret, encoded)
		}
	}
	apps, err := st.ListUserApplications(ctx)
	if err != nil || len(apps) != 2 || apps[0].ID != 1 || apps[1].ID != 2 {
		t.Fatalf("ListUserApplications = %+v, %v", apps, err)
	}
	byEmail, hash, err := st.GetUserByEmail(ctx, "  PERSON@example.com  ")
	if err != nil || hash != "private-password-hash" || byEmail.ID != user.ID || !byEmail.IsActive {
		t.Fatalf("GetUserByEmail = %+v, %v", byEmail, err)
	}
	access, err := st.GetUserAccess(ctx, user.ID)
	if err != nil || access.Role != "operator" || !access.IsActive || !reflect.DeepEqual(access.ApplicationIDs, []int{1, 2}) {
		t.Fatalf("GetUserAccess = %+v, %v", access, err)
	}
}

func TestUserManagementUpdateAtomicallyChangesAccess(t *testing.T) {
	st, db := setupUserManagementStore(t)
	ctx := context.Background()
	user, err := st.CreateUser(ctx, managedUserRequest("operator@example.com", "operator", 1), "hash")
	if err != nil {
		t.Fatal(err)
	}
	req := managedUserRequest(" Updated@Example.com ", "viewer", 2)
	req.IsActive = false
	updated, err := st.UpdateUser(ctx, 900, user.ID, req)
	if err != nil {
		t.Fatal(err)
	}
	if updated.Email != "updated@example.com" || updated.Role != "viewer" || updated.IsActive || updated.TokenVersion != 1 || !reflect.DeepEqual(updated.ApplicationIDs, []int{2}) {
		t.Fatalf("unexpected updated user: %+v", updated)
	}
	access, err := st.GetUserAccess(ctx, user.ID)
	if err != nil || access.Role != "viewer" || access.IsActive || access.TokenVersion != 1 || !reflect.DeepEqual(access.ApplicationIDs, []int{2}) {
		t.Fatalf("updated access = %+v, %v", access, err)
	}
	var hash string
	if err := db.Get(&hash, `SELECT password FROM "user" WHERE id = $1`, user.ID); err != nil || hash != "hash" {
		t.Fatalf("profile update changed password: %q, %v", hash, err)
	}

	// A membership insertion failure after the account update must roll back
	// both the version bump and deletion of existing memberships.
	if _, err := db.Exec(`CREATE TRIGGER reject_membership BEFORE INSERT ON user_application BEGIN SELECT RAISE(ABORT, 'membership rejected'); END`); err != nil {
		t.Fatal(err)
	}
	req.IsActive = true
	req.Role = "operator"
	req.ApplicationIDs = []int{1}
	if _, err := st.UpdateUser(ctx, 900, user.ID, req); err == nil {
		t.Fatal("expected membership insertion failure")
	}
	after, err := st.GetUserAccess(ctx, user.ID)
	if err != nil || !reflect.DeepEqual(after, access) {
		t.Fatalf("failed update was not rolled back: %+v, %v", after, err)
	}
}

func TestUserManagementValidationDoesNotMutate(t *testing.T) {
	st, db := setupUserManagementStore(t)
	ctx := context.Background()
	user, err := st.CreateUser(ctx, managedUserRequest("one@example.com", "operator", 1), "hash")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.CreateUser(ctx, managedUserRequest("two@example.com", "viewer"), "hash"); err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		name string
		req  types.SaveUserRequest
		want error
	}{
		{"missing application", managedUserRequest("changed@example.com", "admin", 1, 999), ErrInvalidApplication},
		{"invalid application", managedUserRequest("changed@example.com", "admin", -1), ErrInvalidApplication},
		{"invalid role", managedUserRequest("changed@example.com", "owner", 1), ErrInvalidUserRole},
		{"duplicate email", managedUserRequest(" TWO@EXAMPLE.COM ", "admin", 2), ErrEmailInUse},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := st.UpdateUser(ctx, 900, user.ID, tc.req); !errors.Is(err, tc.want) {
				t.Fatalf("error = %v, want %v", err, tc.want)
			}
			current, err := st.GetUserByID(ctx, user.ID)
			if err != nil || current.Email != user.Email || current.Role != user.Role || current.TokenVersion != 0 {
				t.Fatalf("failed update changed account: %+v, %v", current, err)
			}
			ids, err := st.GetUserApplicationIDs(ctx, user.ID)
			if err != nil || !reflect.DeepEqual(ids, []int{1}) {
				t.Fatalf("failed update changed memberships: %v, %v", ids, err)
			}
		})
	}
	if _, err := st.CreateUser(ctx, managedUserRequest("new@example.com", "viewer", 999), "hash"); !errors.Is(err, ErrInvalidApplication) {
		t.Fatalf("create with invalid membership = %v", err)
	}
	var count int
	if err := db.Get(&count, `SELECT count(*) FROM "user"`); err != nil || count != 2 {
		t.Fatalf("failed creation left an account: count = %d, error = %v", count, err)
	}
}

func TestUserManagementProtectsAdministrators(t *testing.T) {
	for _, role := range []string{"admin", "viewer"} {
		t.Run(role, func(t *testing.T) {
			st, _ := setupUserManagementStore(t)
			ctx := context.Background()
			admin, err := st.CreateUser(ctx, managedUserRequest("admin@example.com", "admin"), "hash")
			if err != nil {
				t.Fatal(err)
			}
			req := managedUserRequest("admin@example.com", role)
			req.IsActive = role != "admin"
			if _, err := st.UpdateUser(ctx, admin.ID, admin.ID, req); !errors.Is(err, ErrSelfDeactivation) {
				t.Fatalf("self-protection = %v", err)
			}
			inactiveReq := managedUserRequest("inactive-admin@example.com", "admin")
			inactiveReq.IsActive = false
			if _, err := st.CreateUser(ctx, inactiveReq, "hash"); err != nil {
				t.Fatal(err)
			}
			if _, err := st.UpdateUser(ctx, 900, admin.ID, req); !errors.Is(err, ErrLastAdmin) {
				t.Fatalf("last active administrator protection = %v", err)
			}
			second, err := st.CreateUser(ctx, managedUserRequest("second@example.com", "administrator"), "hash")
			if err != nil {
				t.Fatal(err)
			}
			if _, err := st.UpdateUser(ctx, second.ID, admin.ID, req); err != nil {
				t.Fatalf("second administrator should allow update: %v", err)
			}
		})
	}
}

func TestUserManagementEmptyMembershipAndMissingUsers(t *testing.T) {
	st, _ := setupUserManagementStore(t)
	ctx := context.Background()
	users, err := st.ListUsers(ctx)
	if err != nil || users == nil || len(users) != 0 {
		t.Fatalf("empty users = %+v, %v", users, err)
	}
	user, err := st.CreateUser(ctx, managedUserRequest("viewer@example.com", "viewer"), "hash")
	if err != nil {
		t.Fatal(err)
	}
	access, err := st.GetUserAccess(ctx, user.ID)
	if err != nil || access.ApplicationIDs == nil || len(access.ApplicationIDs) != 0 || !access.IsActive {
		t.Fatalf("empty memberships = %+v, %v", access, err)
	}
	if _, err := st.GetUserByID(ctx, 999); !errors.Is(err, ErrUserNotFound) {
		t.Fatalf("GetUserByID missing error = %v", err)
	}
	if _, _, err := st.GetUserByEmail(ctx, "missing@example.com"); !errors.Is(err, ErrUserNotFound) {
		t.Fatalf("GetUserByEmail missing error = %v", err)
	}
	if _, err := st.GetUserAccess(ctx, 999); !errors.Is(err, ErrUserNotFound) {
		t.Fatalf("GetUserAccess missing error = %v", err)
	}
	if _, err := st.UpdateUser(ctx, 900, 999, managedUserRequest("missing@example.com", "viewer")); !errors.Is(err, ErrUserNotFound) {
		t.Fatalf("UpdateUser missing error = %v", err)
	}
}

func TestUserManagementBootstrapMatchesNormalizedEmail(t *testing.T) {
	st, db := setupUserManagementStore(t)
	ctx := context.Background()
	user, err := st.CreateUser(ctx, managedUserRequest("admin@example.com", "admin"), "hash")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`CREATE TABLE user_team (user_id INTEGER)`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`UPDATE "user" SET email = ' ADMIN@example.com ' WHERE id = $1`, user.ID); err != nil {
		t.Fatal(err)
	}
	if err := st.BootstrapAdmin(ctx, AdminBootstrap{Email: "admin@example.com"}); err != nil {
		t.Fatalf("bootstrap should recognize the existing normalized email: %v", err)
	}
	var count int
	if err := db.Get(&count, `SELECT count(*) FROM "user"`); err != nil || count != 1 {
		t.Fatalf("bootstrap created a duplicate: count=%d, error=%v", count, err)
	}
}
