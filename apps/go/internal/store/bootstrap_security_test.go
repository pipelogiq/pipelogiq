package store

import (
	"context"
	"testing"

	"golang.org/x/crypto/bcrypt"
)

func TestBootstrapSafelyReplacesDisabledDemoCredential(t *testing.T) {
	st, db := setupUserManagementStore(t)
	ctx := context.Background()
	u, err := st.CreateUser(ctx, managedUserRequest("jegor@gmail.com", "admin", 1), legacyDemoPasswordHashes[0])
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`UPDATE "user" SET is_active=FALSE, token_version=2 WHERE id=$1`, u.ID); err != nil {
		t.Fatal(err)
	}
	hash, err := bcrypt.GenerateFromPassword([]byte("replacement-password-123"), bcrypt.MinCost)
	if err != nil {
		t.Fatal(err)
	}
	cfg := AdminBootstrap{Email: u.Email, PasswordHash: string(hash)}
	for i := 0; i < 2; i++ {
		if err := st.BootstrapAdmin(ctx, cfg); err != nil {
			t.Fatal(err)
		}
		access, err := st.GetUserAccess(ctx, u.ID)
		if err != nil || !access.IsActive || access.Role != "admin" || access.TokenVersion != 3 {
			t.Fatalf("bootstrap access = %+v, %v", access, err)
		}
	}
}

func TestBootstrapPreservesRealAccountsAndDisablesRenamedDemos(t *testing.T) {
	st, db := setupUserManagementStore(t)
	ctx := context.Background()
	admin, err := st.CreateUser(ctx, managedUserRequest("admin@example.test", "admin"), "existing-private-hash")
	if err != nil {
		t.Fatal(err)
	}
	real, err := st.CreateUser(ctx, managedUserRequest("leo@gmail.com", "viewer", 2), "changed-private-hash")
	if err != nil {
		t.Fatal(err)
	}
	demo, err := st.CreateUser(ctx, managedUserRequest("renamed@example.test", "viewer", 1), legacyDemoPasswordHashes[1])
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		if err := st.BootstrapAdmin(ctx, AdminBootstrap{Email: admin.Email}); err != nil {
			t.Fatal(err)
		}
	}
	access, err := st.GetUserAccess(ctx, demo.ID)
	if err != nil || access.IsActive || access.TokenVersion != 1 || len(access.ApplicationIDs) != 1 {
		t.Fatalf("demo identity lost or active: %+v %v", access, err)
	}
	access, err = st.GetUserAccess(ctx, real.ID)
	if err != nil || !access.IsActive || access.Role != "viewer" || access.TokenVersion != 0 || len(access.ApplicationIDs) != 1 {
		t.Fatalf("changed-password account changed: %+v %v", access, err)
	}
	access, err = st.GetUserAccess(ctx, admin.ID)
	if err != nil || access.TokenVersion != 0 || len(access.ApplicationIDs) != 1 || access.ApplicationIDs[0] != 1 {
		t.Fatalf("bootstrap transferred wrong access: %+v %v", access, err)
	}
	var count int
	if err := db.Get(&count, `SELECT count(*) FROM "user"`); err != nil || count != 3 {
		t.Fatalf("bootstrap removed history: count=%d, %v", count, err)
	}
}

func TestBootstrapDoesNotReactivateWithoutReplacementCredential(t *testing.T) {
	st, db := setupUserManagementStore(t)
	ctx := context.Background()
	u, err := st.CreateUser(ctx, managedUserRequest("disabled@example.test", "admin"), legacyDemoPasswordHashes[2])
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`UPDATE "user" SET is_active=FALSE, token_version=2 WHERE id=$1`, u.ID); err != nil {
		t.Fatal(err)
	}
	for _, cfg := range []AdminBootstrap{{Email: u.Email}, {}} {
		if err := st.BootstrapAdmin(ctx, cfg); err != nil {
			t.Fatal(err)
		}
	}
	access, err := st.GetUserAccess(ctx, u.ID)
	if err != nil || access.IsActive || access.TokenVersion != 2 {
		t.Fatalf("unsafe reactivation: %+v, %v", access, err)
	}
	for _, hash := range append([]string{"invalid-hash"}, legacyDemoPasswordHashes...) {
		if err := st.BootstrapAdmin(ctx, AdminBootstrap{Email: u.Email, PasswordHash: hash}); err == nil {
			t.Fatal("unsafe bootstrap credential accepted")
		}
	}
}
