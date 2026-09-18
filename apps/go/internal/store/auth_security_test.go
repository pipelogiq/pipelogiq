package store

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestChangeOwnPasswordRejectsConcurrentSecurityChanges(t *testing.T) {
	for _, change := range []string{"password reset", "logout", "deactivation", "profile update", "hash replaced"} {
		t.Run(change, func(t *testing.T) {
			st, db := setupUserManagementStore(t)
			ctx := context.Background()
			user, err := st.CreateUser(ctx, managedUserRequest("user@example.com", "operator", 1), "verified-old-hash")
			if err != nil {
				t.Fatal(err)
			}
			// The caller has already verified this hash and token version. A
			// security change wins before its replacement password is written.
			switch change {
			case "password reset":
				_, err = st.UpdatePassword(ctx, user.ID, "administrator-reset-hash")
			case "logout":
				_, err = st.BumpTokenVersion(ctx, user.ID)
			case "deactivation":
				_, err = db.Exec(`UPDATE "user" SET is_active = FALSE WHERE id = $1`, user.ID)
			case "profile update":
				_, err = st.UpdateUser(ctx, 900, user.ID, managedUserRequest("changed@example.com", "viewer", 2))
			case "hash replaced":
				_, err = db.Exec(`UPDATE "user" SET password = 'different-hash' WHERE id = $1`, user.ID)
			}
			if err != nil {
				t.Fatal(err)
			}
			before, hashBefore, err := st.GetUserByEmail(ctx, func() string {
				if change == "profile update" {
					return "changed@example.com"
				}
				return user.Email
			}())
			if err != nil {
				t.Fatal(err)
			}
			if _, err := st.ChangeOwnPassword(ctx, user.ID, user.TokenVersion, "verified-old-hash", "stale-session-hash"); !errors.Is(err, ErrSessionChanged) {
				t.Fatalf("stale password change = %v, want ErrSessionChanged", err)
			}
			after, hashAfter, err := st.GetUserByEmail(ctx, before.Email)
			if err != nil || hashAfter != hashBefore || after.TokenVersion != before.TokenVersion || after.IsActive != before.IsActive {
				t.Fatalf("rejected change mutated security state: before=%+v after=%+v err=%v", before, after, err)
			}
		})
	}
}

func TestChangeOwnPasswordAllowsOnlyOneConcurrentChange(t *testing.T) {
	st, _ := setupUserManagementStore(t)
	testConcurrentOwnPasswordChange(t, st)
}

func testConcurrentOwnPasswordChange(t *testing.T, st *Store) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	user, err := st.CreateUser(ctx, managedUserRequest("user@example.com", "operator"), "verified-old-hash")
	if err != nil {
		t.Fatal(err)
	}
	start := make(chan struct{})
	results := make(chan error, 2)
	for _, newHash := range []string{"first-new-hash", "second-new-hash"} {
		go func(hash string) {
			<-start
			_, err := st.ChangeOwnPassword(ctx, user.ID, user.TokenVersion, "verified-old-hash", hash)
			results <- err
		}(newHash)
	}
	close(start)
	succeeded, revoked := 0, 0
	for range 2 {
		switch err := <-results; {
		case err == nil:
			succeeded++
		case errors.Is(err, ErrSessionChanged):
			revoked++
		default:
			t.Fatal(err)
		}
	}
	current, hash, err := st.GetUserByEmail(ctx, user.Email)
	if err != nil {
		t.Fatal(err)
	}
	if succeeded != 1 || revoked != 1 || current.TokenVersion != 1 || (hash != "first-new-hash" && hash != "second-new-hash") {
		t.Fatalf("concurrent changes: successful=%d revoked=%d version=%d", succeeded, revoked, current.TokenVersion)
	}
}
