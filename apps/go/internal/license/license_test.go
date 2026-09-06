package license

import (
	"crypto/ed25519"
	"encoding/base64"
	"errors"
	"io"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

// testKeys installs a fresh signing key for the duration of one test.
func testKeys(t *testing.T) ed25519.PrivateKey {
	t.Helper()

	public, private, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	original := PublicKeyBase64
	PublicKeyBase64 = base64.StdEncoding.EncodeToString(public)
	t.Cleanup(func() { PublicKeyBase64 = original })
	return private
}

type tokenOptions struct {
	tier      string
	company   string
	engineers int
	features  []string
	issuedAt  time.Time
	expiresAt time.Time
	method    jwt.SigningMethod
	key       any
}

func signToken(t *testing.T, private ed25519.PrivateKey, opts tokenOptions) string {
	t.Helper()

	if opts.method == nil {
		opts.method = jwt.SigningMethodEdDSA
	}
	if opts.key == nil {
		opts.key = private
	}

	body := &claims{
		RegisteredClaims: jwt.RegisteredClaims{
			Subject:   opts.company,
			ExpiresAt: jwt.NewNumericDate(opts.expiresAt),
			IssuedAt:  jwt.NewNumericDate(opts.issuedAt),
		},
		Tier:      opts.tier,
		Engineers: opts.engineers,
		Features:  opts.features,
	}
	if opts.expiresAt.IsZero() {
		body.ExpiresAt = nil
	}

	signed, err := jwt.NewWithClaims(opts.method, body).SignedString(opts.key)
	if err != nil {
		t.Fatalf("sign token: %v", err)
	}
	return signed
}

func TestVerify(t *testing.T) {
	now := time.Date(2026, 6, 1, 12, 0, 0, 0, time.UTC)

	t.Run("a valid pro token unlocks its features", func(t *testing.T) {
		private := testKeys(t)
		token := signToken(t, private, tokenOptions{
			tier:      TierPro,
			company:   "Tiqet SL",
			engineers: 50,
			features:  []string{FeatureSSO, FeatureAudit, FeatureRetention},
			issuedAt:  now.Add(-24 * time.Hour),
			expiresAt: now.Add(365 * 24 * time.Hour),
		})

		status, err := Verify(token, now)
		if err != nil {
			t.Fatalf("Verify: %v", err)
		}
		if !status.Licensed || status.Tier != TierPro {
			t.Fatalf("unexpected status %+v", status)
		}
		if status.Company != "Tiqet SL" || status.Engineers != 50 {
			t.Fatalf("claims lost: %+v", status)
		}
		if status.InGrace {
			t.Fatalf("a token well inside its validity must not be in grace")
		}
		for _, feature := range []string{FeatureSSO, FeatureAudit, FeatureRetention} {
			if !status.Has(feature) {
				t.Fatalf("%s must be unlocked", feature)
			}
		}
		// Only the listed features unlock, so a tier can be extended without a
		// new binary — and cannot be extended by accident.
		if status.Has(FeatureAlertingOnCall) {
			t.Fatalf("a feature outside the token's list must stay locked")
		}
	})

	t.Run("an expired token keeps its features inside the grace period", func(t *testing.T) {
		private := testKeys(t)
		expired := now.Add(-2 * 24 * time.Hour)
		token := signToken(t, private, tokenOptions{
			tier:      TierPro,
			company:   "Tiqet SL",
			features:  []string{FeatureSSO},
			issuedAt:  now.Add(-400 * 24 * time.Hour),
			expiresAt: expired,
		})

		status, err := Verify(token, now)
		if err != nil {
			t.Fatalf("Verify inside grace must not error: %v", err)
		}
		if !status.Licensed || !status.InGrace {
			t.Fatalf("expected a licensed in-grace status, got %+v", status)
		}
		if !status.Has(FeatureSSO) {
			t.Fatalf("features must stay unlocked during the grace period")
		}
		if status.GraceEndsAt == nil || !status.GraceEndsAt.Equal(expired.Add(GracePeriod)) {
			t.Fatalf("graceEndsAt = %v, want %s", status.GraceEndsAt, expired.Add(GracePeriod))
		}
		if status.Reason == "" {
			t.Fatalf("an in-grace status must explain itself to an operator")
		}
	})

	t.Run("a token past its grace period falls back to Community", func(t *testing.T) {
		private := testKeys(t)
		expired := now.Add(-(GracePeriod + 24*time.Hour))
		token := signToken(t, private, tokenOptions{
			tier:      TierPro,
			company:   "Tiqet SL",
			features:  []string{FeatureSSO},
			issuedAt:  now.Add(-400 * 24 * time.Hour),
			expiresAt: expired,
		})

		status, err := Verify(token, now)
		if !errors.Is(err, ErrExpired) {
			t.Fatalf("err = %v, want ErrExpired", err)
		}
		if status.Licensed || status.Tier != TierCommunity {
			t.Fatalf("expected Community, got %+v", status)
		}
		if status.Has(FeatureSSO) {
			t.Fatalf("an expired licence past grace must not unlock anything")
		}
		if !strings.Contains(status.Reason, "grace period ended") {
			t.Fatalf("reason must say what happened, got %q", status.Reason)
		}
	})

	t.Run("rejections", func(t *testing.T) {
		cases := []struct {
			name  string
			build func(t *testing.T, private ed25519.PrivateKey) string
			want  error
		}{
			{
				name:  "missing token",
				build: func(*testing.T, ed25519.PrivateKey) string { return "  " },
				want:  ErrNoToken,
			},
			{
				name: "signed with the wrong key",
				build: func(t *testing.T, _ ed25519.PrivateKey) string {
					_, other, err := ed25519.GenerateKey(nil)
					if err != nil {
						t.Fatalf("generate: %v", err)
					}
					return signToken(t, other, tokenOptions{
						tier:      TierPro,
						expiresAt: now.Add(time.Hour),
					})
				},
				want: ErrInvalidToken,
			},
			{
				name: "tampered claims",
				build: func(t *testing.T, private ed25519.PrivateKey) string {
					token := signToken(t, private, tokenOptions{
						tier:      TierPro,
						features:  []string{FeatureSSO},
						expiresAt: now.Add(time.Hour),
					})
					// Flip a character in the payload; the signature no longer matches.
					parts := strings.Split(token, ".")
					payload := []byte(parts[1])
					if payload[0] == 'e' {
						payload[0] = 'f'
					} else {
						payload[0] = 'e'
					}
					parts[1] = string(payload)
					return strings.Join(parts, ".")
				},
				want: ErrInvalidToken,
			},
			{
				name: "algorithm downgraded to HMAC",
				build: func(t *testing.T, _ ed25519.PrivateKey) string {
					return signToken(t, nil, tokenOptions{
						tier:      TierPro,
						expiresAt: now.Add(time.Hour),
						method:    jwt.SigningMethodHS256,
						key:       []byte("not the ed25519 key"),
					})
				},
				want: ErrInvalidToken,
			},
			{
				name: "none algorithm",
				build: func(t *testing.T, _ ed25519.PrivateKey) string {
					return signToken(t, nil, tokenOptions{
						tier:      TierPro,
						expiresAt: now.Add(time.Hour),
						method:    jwt.SigningMethodNone,
						key:       jwt.UnsafeAllowNoneSignatureType,
					})
				},
				want: ErrInvalidToken,
			},
			{
				name: "unknown tier",
				build: func(t *testing.T, private ed25519.PrivateKey) string {
					return signToken(t, private, tokenOptions{
						tier:      "platinum",
						expiresAt: now.Add(time.Hour),
					})
				},
				want: ErrInvalidToken,
			},
			{
				name: "no expiry",
				build: func(t *testing.T, private ed25519.PrivateKey) string {
					return signToken(t, private, tokenOptions{tier: TierPro})
				},
				want: ErrInvalidToken,
			},
			{
				name:  "not a token at all",
				build: func(*testing.T, ed25519.PrivateKey) string { return "garbage" },
				want:  ErrInvalidToken,
			},
		}

		for _, tc := range cases {
			t.Run(tc.name, func(t *testing.T) {
				private := testKeys(t)
				status, err := Verify(tc.build(t, private), now)
				if !errors.Is(err, tc.want) {
					t.Fatalf("err = %v, want %v", err, tc.want)
				}
				if status.Licensed || status.Tier != TierCommunity {
					t.Fatalf("every rejection must produce Community, got %+v", status)
				}
				if len(status.Features) != 0 {
					t.Fatalf("Community must unlock nothing, got %v", status.Features)
				}
			})
		}
	})

	t.Run("the shipped placeholder key verifies nothing", func(t *testing.T) {
		// The open repository ships an all-zero key, so a build that does not
		// override it is Community whatever token it is handed.
		private := testKeys(t)
		token := signToken(t, private, tokenOptions{
			tier:      TierPro,
			expiresAt: now.Add(time.Hour),
		})
		PublicKeyBase64 = "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA="

		status, err := Verify(token, now)
		if err == nil {
			t.Fatalf("the placeholder key must not verify a real token")
		}
		if status.Licensed {
			t.Fatalf("status = %+v, want Community", status)
		}
	})

	t.Run("a malformed public key degrades to Community", func(t *testing.T) {
		original := PublicKeyBase64
		t.Cleanup(func() { PublicKeyBase64 = original })

		for _, key := range []string{"", "not base64!!", "c2hvcnQ="} {
			PublicKeyBase64 = key
			status, err := Verify("anything", now)
			if !errors.Is(err, ErrNoPublicKey) {
				t.Fatalf("key %q: err = %v, want ErrNoPublicKey", key, err)
			}
			if status.Licensed {
				t.Fatalf("key %q must not license anything", key)
			}
		}
	})
}

func TestResolveNeverFails(t *testing.T) {
	t.Cleanup(resetForTest)
	resetForTest()

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	status := Resolve("this is not a token", logger)
	if status.Licensed || status.Tier != TierCommunity {
		t.Fatalf("a broken token must resolve to Community, got %+v", status)
	}

	// Resolve caches, so a later call cannot change the answer under a running
	// process.
	if again := Resolve("", logger); again.Tier != status.Tier {
		t.Fatalf("Resolve must be stable within a process")
	}
	if Current().Tier != TierCommunity {
		t.Fatalf("Current() = %+v, want Community", Current())
	}
	if Has(FeatureSSO) {
		t.Fatalf("Community must gate every Pro feature")
	}
}

func TestFeatureCatalogueIsComplete(t *testing.T) {
	// Every gate name has an explanation, because the dashboard renders locked
	// features rather than hiding them.
	for _, feature := range AllFeatures {
		if strings.TrimSpace(FeatureDescriptions[feature]) == "" {
			t.Fatalf("feature %q has no description", feature)
		}
	}
	if len(FeatureDescriptions) != len(AllFeatures) {
		t.Fatalf("%d descriptions for %d features", len(FeatureDescriptions), len(AllFeatures))
	}
}
