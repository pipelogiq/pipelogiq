package api

import (
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"

	"pipelogiq/internal/license"
)

// installTestLicense signs a token with a throwaway key, points the verifier at
// it, and resolves it — so a test can run as any tier.
func installTestLicense(t *testing.T, tier string, features []string, expiresAt time.Time) {
	t.Helper()

	public, private, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}

	token, err := jwt.NewWithClaims(jwt.SigningMethodEdDSA, jwt.MapClaims{
		"sub":       "Test Co",
		"tier":      tier,
		"engineers": 50,
		"features":  features,
		"iat":       time.Now().Add(-time.Hour).Unix(),
		"exp":       expiresAt.Unix(),
	}).SignedString(private)
	if err != nil {
		t.Fatalf("sign token: %v", err)
	}

	license.SetForTest(t, base64.StdEncoding.EncodeToString(public), token)
}

func TestLicenseEndpoint(t *testing.T) {
	t.Run("community reports every feature locked", func(t *testing.T) {
		license.SetCommunityForTest(t)
		fx := setupScopeTest(t)

		res := fx.do(t, scopedUserID, http.MethodGet, "/license", nil)
		if res.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200; body=%s", res.Code, res.Body.String())
		}

		var payload struct {
			Tier      string `json:"tier"`
			Licensed  bool   `json:"licensed"`
			Catalogue []struct {
				Name      string `json:"name"`
				Unlocked  bool   `json:"unlocked"`
				Available bool   `json:"available"`
			} `json:"catalogue"`
		}
		if err := json.Unmarshal(res.Body.Bytes(), &payload); err != nil {
			t.Fatalf("decode: %v", err)
		}
		if payload.Tier != license.TierCommunity || payload.Licensed {
			t.Fatalf("unexpected payload %+v", payload)
		}
		if len(payload.Catalogue) != len(license.AllFeatures) {
			t.Fatalf("catalogue has %d entries, want %d", len(payload.Catalogue), len(license.AllFeatures))
		}
		// Locked features are rendered, not hidden: the dashboard needs them.
		for _, entry := range payload.Catalogue {
			if entry.Unlocked {
				t.Fatalf("%s must be locked on Community", entry.Name)
			}
		}
	})

	t.Run("a pro licence reports its features unlocked", func(t *testing.T) {
		installTestLicense(t, license.TierPro,
			[]string{license.FeatureSSO, license.FeatureAudit, license.FeatureRetention},
			time.Now().Add(365*24*time.Hour))
		fx := setupScopeTest(t)

		res := fx.do(t, scopedUserID, http.MethodGet, "/license", nil)
		if res.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200", res.Code)
		}

		var payload struct {
			Tier      string   `json:"tier"`
			Licensed  bool     `json:"licensed"`
			Features  []string `json:"features"`
			Company   string   `json:"company"`
			Engineers int      `json:"engineers"`
		}
		if err := json.Unmarshal(res.Body.Bytes(), &payload); err != nil {
			t.Fatalf("decode: %v", err)
		}
		if payload.Tier != license.TierPro || !payload.Licensed {
			t.Fatalf("unexpected payload %+v", payload)
		}
		if payload.Company != "Test Co" || payload.Engineers != 50 {
			t.Fatalf("claims lost: %+v", payload)
		}
		if len(payload.Features) != 3 {
			t.Fatalf("features = %v, want three", payload.Features)
		}
	})
}

func TestFeatureGating(t *testing.T) {
	gatedRoutes := []struct {
		name    string
		method  string
		path    string
		body    any
		feature string
	}{
		{
			name:    "audit export",
			method:  http.MethodGet,
			path:    fmt.Sprintf("/audit/export?applicationId=%d", ownApplicationID),
			feature: license.FeatureAudit,
		},
		{
			name:    "read retention policy",
			method:  http.MethodGet,
			path:    fmt.Sprintf("/retention/%d", ownApplicationID),
			feature: license.FeatureRetention,
		},
		{
			name:    "set retention policy",
			method:  http.MethodPut,
			path:    fmt.Sprintf("/retention/%d", ownApplicationID),
			body:    map[string]any{"keepDays": 30, "enabled": true},
			feature: license.FeatureRetention,
		},
		{
			name:    "purge",
			method:  http.MethodPost,
			path:    fmt.Sprintf("/retention/%d/purge", ownApplicationID),
			feature: license.FeatureRetention,
		},
	}

	t.Run("community answers 402 naming the feature", func(t *testing.T) {
		for _, route := range gatedRoutes {
			t.Run(route.name, func(t *testing.T) {
				license.SetCommunityForTest(t)
				fx := setupScopeTest(t)

				res := fx.do(t, scopedUserID, route.method, route.path, route.body)
				if res.Code != http.StatusPaymentRequired {
					t.Fatalf("status = %d, want 402; body=%s", res.Code, res.Body.String())
				}
				if got := res.Header().Get("Content-Type"); got != "application/problem+json" {
					t.Fatalf("content type = %q, want application/problem+json", got)
				}

				var problem problemDetails
				if err := json.Unmarshal(res.Body.Bytes(), &problem); err != nil {
					t.Fatalf("decode problem: %v", err)
				}
				if problem.Type != problemTypeLicenseRequired {
					t.Fatalf("problem type = %q, want %q", problem.Type, problemTypeLicenseRequired)
				}
				if problem.Status != http.StatusPaymentRequired {
					t.Fatalf("problem status = %d, want 402", problem.Status)
				}
				// The body must name the feature, so an operator knows what to buy.
				named := problem.Errors["feature"]
				if len(named) != 1 || named[0] != route.feature {
					t.Fatalf("problem must name %q, got %v", route.feature, problem.Errors)
				}
				if strings.TrimSpace(problem.Detail) == "" {
					t.Fatalf("a 402 must explain itself")
				}
			})
		}
	})

	t.Run("a licence that omits the feature still gates it", func(t *testing.T) {
		// A Pro tier is not a blanket unlock: the token lists its features, so a
		// tier can be extended without a new binary and cannot be widened by
		// accident.
		installTestLicense(t, license.TierPro,
			[]string{license.FeatureSSO}, time.Now().Add(24*time.Hour))
		fx := setupScopeTest(t)

		res := fx.do(t, scopedUserID, http.MethodGet, fmt.Sprintf("/retention/%d", ownApplicationID), nil)
		if res.Code != http.StatusPaymentRequired {
			t.Fatalf("status = %d, want 402; body=%s", res.Code, res.Body.String())
		}
	})

	t.Run("an unlocked feature passes the gate and reaches the stub", func(t *testing.T) {
		installTestLicense(t, license.TierPro,
			[]string{license.FeatureRetention}, time.Now().Add(24*time.Hour))
		fx := setupScopeTest(t)

		res := fx.do(t, scopedUserID, http.MethodGet, fmt.Sprintf("/retention/%d", ownApplicationID), nil)
		// Without the `pro` build tag the stub refuses, so this is 500 rather than
		// 402: the gate opened, and the implementation is simply not compiled in.
		// That difference is the point of the seam.
		if res.Code == http.StatusPaymentRequired {
			t.Fatalf("an unlocked feature must not answer 402")
		}
	})

	t.Run("the licence gate runs before membership", func(t *testing.T) {
		// A Community caller asking about someone else's application must learn
		// nothing about whether it exists.
		license.SetCommunityForTest(t)
		fx := setupScopeTest(t)

		res := fx.do(t, scopedUserID, http.MethodGet,
			fmt.Sprintf("/retention/%d", otherApplicationID), nil)
		if res.Code != http.StatusPaymentRequired {
			t.Fatalf("status = %d, want 402", res.Code)
		}
	})

	t.Run("a licensed caller is still scoped to its applications", func(t *testing.T) {
		installTestLicense(t, license.TierPro,
			[]string{license.FeatureRetention}, time.Now().Add(24*time.Hour))
		fx := setupScopeTest(t)

		res := fx.do(t, scopedUserID, http.MethodGet,
			fmt.Sprintf("/retention/%d", otherApplicationID), nil)
		if res.Code != http.StatusNotFound {
			t.Fatalf("status = %d, want 404: a licence is not authorization", res.Code)
		}
	})
}

func TestVersionCarriesTheTier(t *testing.T) {
	license.SetCommunityForTest(t)
	fx := setupScopeTest(t)

	res := fx.do(t, scopedUserID, http.MethodGet, "/version", nil)
	if res.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", res.Code)
	}

	var payload struct {
		Tier string `json:"tier"`
	}
	if err := json.Unmarshal(res.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if payload.Tier != license.TierCommunity {
		t.Fatalf("tier = %q, want community", payload.Tier)
	}
}
