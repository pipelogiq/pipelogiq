//go:build pro

package pro

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

// fakeProvider is a minimal OIDC provider: a discovery document, a token
// endpoint, and an unsigned ID token, which is all the code flow reads.
func fakeProvider(t *testing.T, idTokenClaims map[string]any) *httptest.Server {
	t.Helper()

	mux := http.NewServeMux()
	server := httptest.NewServer(mux)
	t.Cleanup(server.Close)

	mux.HandleFunc("/.well-known/openid-configuration", func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]string{
			"issuer":                 server.URL,
			"authorization_endpoint": server.URL + "/authorize",
			"token_endpoint":         server.URL + "/token",
		})
	})

	mux.HandleFunc("/token", func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseForm(); err != nil {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		if r.Form.Get("grant_type") != "authorization_code" || r.Form.Get("code") == "" {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]string{
			"access_token": "access",
			"id_token":     unsignedIDToken(t, idTokenClaims),
		})
	})

	return server
}

func unsignedIDToken(t *testing.T, claims map[string]any) string {
	t.Helper()

	header := base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"RS256","typ":"JWT"}`))
	payload, err := json.Marshal(claims)
	if err != nil {
		t.Fatalf("marshal claims: %v", err)
	}
	return strings.Join([]string{
		header,
		base64.RawURLEncoding.EncodeToString(payload),
		base64.RawURLEncoding.EncodeToString([]byte("signature")),
	}, ".")
}

func configureSSO(t *testing.T, issuer, groupMap string) SSO {
	t.Helper()

	t.Setenv("SSO_ISSUER", issuer)
	t.Setenv("SSO_CLIENT_ID", "pipelogiq")
	t.Setenv("SSO_CLIENT_SECRET", "secret")
	t.Setenv("SSO_GROUP_ROLE_MAP", groupMap)
	return newSSO()
}

func TestSSOAuthorizeAndCallback(t *testing.T) {
	provider := fakeProvider(t, map[string]any{
		"sub":         "user-1",
		"email":       "ops@example.test",
		"given_name":  "Ops",
		"family_name": "Person",
		"groups":      []string{"pipelogiq-admins"},
	})
	sso := configureSSO(t, provider.URL, "pipelogiq-admins=Admin,pipelogiq-viewers=Viewer")
	ctx := context.Background()

	if !sso.Enabled() {
		t.Fatalf("a configured provider must report itself enabled")
	}

	authorizeURL, state, err := sso.AuthorizeURL(ctx, "https://pipelogiq.test/callback")
	if err != nil {
		t.Fatalf("AuthorizeURL: %v", err)
	}
	if state == "" {
		t.Fatalf("a state must be issued")
	}

	parsed, err := url.Parse(authorizeURL)
	if err != nil {
		t.Fatalf("parse authorize url: %v", err)
	}
	query := parsed.Query()
	if query.Get("client_id") != "pipelogiq" ||
		query.Get("response_type") != "code" ||
		query.Get("redirect_uri") != "https://pipelogiq.test/callback" ||
		query.Get("state") != state {
		t.Fatalf("authorize url is missing parameters: %s", authorizeURL)
	}

	identity, err := sso.Exchange(ctx, "the-code", state, state)
	if err != nil {
		t.Fatalf("Exchange: %v", err)
	}
	if identity.Subject != "user-1" || identity.Email != "ops@example.test" {
		t.Fatalf("identity = %+v", identity)
	}
	if identity.Role != "Admin" {
		t.Fatalf("role = %q, want Admin from the group map", identity.Role)
	}
}

func TestSSOCallbackRejections(t *testing.T) {
	ctx := context.Background()

	t.Run("a state mismatch is refused", func(t *testing.T) {
		provider := fakeProvider(t, map[string]any{
			"sub": "user-1", "email": "ops@example.test", "groups": []string{"pipelogiq-admins"},
		})
		sso := configureSSO(t, provider.URL, "pipelogiq-admins=Admin")

		if _, err := sso.Exchange(ctx, "code", "attacker-state", "real-state"); err == nil {
			t.Fatalf("a forged state must be refused")
		}
	})

	t.Run("a missing code is refused", func(t *testing.T) {
		provider := fakeProvider(t, map[string]any{"sub": "user-1", "email": "ops@example.test"})
		sso := configureSSO(t, provider.URL, "pipelogiq-admins=Admin")

		if _, err := sso.Exchange(ctx, "  ", "state", "state"); err == nil {
			t.Fatalf("a callback without a code must be refused")
		}
	})

	t.Run("groups that map to no role are refused", func(t *testing.T) {
		// Access is granted by mapping, never by default: a user in an unmapped
		// group must not silently become anything.
		provider := fakeProvider(t, map[string]any{
			"sub": "user-2", "email": "contractor@example.test", "groups": []string{"everyone"},
		})
		sso := configureSSO(t, provider.URL, "pipelogiq-admins=Admin")

		if _, err := sso.Exchange(ctx, "code", "state", "state"); err == nil {
			t.Fatalf("an unmapped group must not authenticate")
		}
	})

	t.Run("an id token without an email is refused", func(t *testing.T) {
		provider := fakeProvider(t, map[string]any{"sub": "user-3", "groups": []string{"pipelogiq-admins"}})
		sso := configureSSO(t, provider.URL, "pipelogiq-admins=Admin")

		if _, err := sso.Exchange(ctx, "code", "state", "state"); err == nil {
			t.Fatalf("an identity with no email must be refused")
		}
	})

	t.Run("no issuer means licensed but not configured", func(t *testing.T) {
		sso := configureSSO(t, "", "")
		if sso.Enabled() {
			t.Fatalf("an unconfigured provider must not report itself enabled")
		}
		if _, _, err := sso.AuthorizeURL(ctx, "https://pipelogiq.test/callback"); err != ErrNotConfigured {
			t.Fatalf("err = %v, want ErrNotConfigured", err)
		}
	})
}

func TestParseGroupRoleMap(t *testing.T) {
	mapped := parseGroupRoleMap("Admins=Admin, viewers=Viewer,broken,=Nothing,Empty=")
	if len(mapped) != 2 {
		t.Fatalf("parsed %v, want two usable entries", mapped)
	}
	// Group names are matched case-insensitively; providers are inconsistent.
	if mapped["admins"] != "Admin" || mapped["viewers"] != "Viewer" {
		t.Fatalf("parsed %v", mapped)
	}
}
