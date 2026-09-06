// Package license verifies the commercial licence token and answers feature
// gates. Everything here is offline by construction.
//
// Three rules hold without exception:
//
//  1. There is no network call. Not on startup, not on renewal, not ever. The
//     public key is compiled in and the token is verified locally.
//  2. A pipeline never stops because of a licence. Gating happens at the API
//     surface, before work is created; nothing in the execution path consults
//     this package.
//  3. Missing, malformed, or unreadable means Community. A licensing problem
//     degrades a deployment to the free tier; it never fails a deployment.
package license

import (
	"crypto/ed25519"
	"encoding/base64"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

// Tiers.
const (
	TierCommunity  = "community"
	TierPro        = "pro"
	TierEnterprise = "enterprise"
)

// GracePeriod is how long an expired licence keeps its features while the
// dashboard shows a banner. After it, the deployment is Community — still fully
// functional for a single developer, and still running every pipeline.
const GracePeriod = 14 * 24 * time.Hour

// Feature gate names. They exist here even for features that ship later, so the
// pricing page and the code cannot drift apart.
const (
	FeatureSSO       = "sso"
	FeatureAudit     = "audit.export"
	FeatureRetention = "retention"

	// Named now, implemented later.
	FeatureSchedulesAdvanced = "schedules.advanced"
	FeatureAlertingOnCall    = "alerting.oncall"
	FeatureApprovalsSlack    = "approvals.slack"
	FeatureHAMultiInstance   = "ha.multi-instance"
	FeatureAgentCostCaps     = "agent.cost-caps"
)

// AllFeatures is every gate name, in the order the pricing page lists them.
var AllFeatures = []string{
	FeatureSSO,
	FeatureAudit,
	FeatureRetention,
	FeatureSchedulesAdvanced,
	FeatureAlertingOnCall,
	FeatureApprovalsSlack,
	FeatureHAMultiInstance,
	FeatureAgentCostCaps,
}

// FeatureDescriptions is the one-line explanation the dashboard shows next to a
// locked feature. A locked feature is rendered, not hidden.
var FeatureDescriptions = map[string]string{
	FeatureSSO:               "OIDC single sign-on with group-to-role mapping",
	FeatureAudit:             "Signed NDJSON export of the audit trail, with retention",
	FeatureRetention:         "Scheduled purge of finished pipelines, stage IO and logs",
	FeatureSchedulesAdvanced: "Catch-up and overlap policies beyond Skip and None",
	FeatureAlertingOnCall:    "PagerDuty and Opsgenie routing",
	FeatureApprovalsSlack:    "Approve and reject stages from Slack",
	FeatureHAMultiInstance:   "Priority queues and control-plane replicas",
	FeatureAgentCostCaps:     "Per-application spend caps for agent stages",
}

// Status is the resolved licence state. It is what GET /license returns and what
// every gate consults.
type Status struct {
	Tier      string     `json:"tier"`
	Licensed  bool       `json:"licensed"`
	Company   string     `json:"company,omitempty"`
	Engineers int        `json:"engineers,omitempty"`
	Features  []string   `json:"features"`
	IssuedAt  *time.Time `json:"issuedAt,omitempty"`
	ExpiresAt *time.Time `json:"expiresAt,omitempty"`
	// InGrace is true for an expired licence still inside GracePeriod. Features
	// stay unlocked and the dashboard shows a banner.
	InGrace bool `json:"inGrace"`
	// GraceEndsAt is when an expired licence stops unlocking features.
	GraceEndsAt *time.Time `json:"graceEndsAt,omitempty"`
	// Reason explains a Community result: "no licence configured", "expired",
	// "signature", and so on. It is safe to show an operator.
	Reason string `json:"reason,omitempty"`
}

// Has reports whether a feature is unlocked.
func (s Status) Has(feature string) bool {
	if !s.Licensed {
		return false
	}
	for _, candidate := range s.Features {
		if candidate == feature {
			return true
		}
	}
	return false
}

// Community is the unlicensed status, and the fallback for every failure.
func Community(reason string) Status {
	return Status{
		Tier:     TierCommunity,
		Licensed: false,
		Features: []string{},
		Reason:   reason,
	}
}

// claims is the token payload. Standard registered claims plus the two the tier
// needs.
type claims struct {
	jwt.RegisteredClaims
	Tier      string   `json:"tier"`
	Engineers int      `json:"engineers,omitempty"`
	Features  []string `json:"features,omitempty"`
}

var (
	// ErrNoToken reports an empty PIPELOGIQ_LICENSE.
	ErrNoToken = errors.New("no licence token configured")
	// ErrNoPublicKey reports a build with no verification key.
	ErrNoPublicKey = errors.New("no licence public key is compiled in")
	// ErrInvalidToken reports a token that does not verify.
	ErrInvalidToken = errors.New("licence token is not valid")
	// ErrExpired reports a token past its grace period.
	ErrExpired = errors.New("licence token expired beyond its grace period")
)

// Verify resolves a token into a status. It never returns an error the caller
// must handle to keep running: a failure is reported alongside a Community
// status so a deployment always has something usable.
func Verify(token string, now time.Time) (Status, error) {
	token = strings.TrimSpace(token)
	if token == "" {
		return Community("no licence configured"), ErrNoToken
	}

	key, err := publicKey()
	if err != nil {
		return Community("no verification key in this build"), err
	}

	// Expiry is handled below so the grace period can apply, and the algorithm is
	// pinned so a token cannot ask to be verified with a weaker one.
	parser := jwt.NewParser(
		jwt.WithValidMethods([]string{jwt.SigningMethodEdDSA.Alg()}),
		jwt.WithoutClaimsValidation(),
	)

	parsed := &claims{}
	if _, err = parser.ParseWithClaims(token, parsed, func(*jwt.Token) (any, error) {
		return key, nil
	}); err != nil {
		return Community("licence token is not valid"), fmt.Errorf("%w: %s", ErrInvalidToken, err.Error())
	}

	tier := strings.ToLower(strings.TrimSpace(parsed.Tier))
	if tier != TierPro && tier != TierEnterprise {
		return Community("licence names an unknown tier"), ErrInvalidToken
	}
	if parsed.ExpiresAt == nil {
		return Community("licence has no expiry"), ErrInvalidToken
	}

	expiresAt := parsed.ExpiresAt.Time.UTC()
	status := Status{
		Tier:      tier,
		Licensed:  true,
		Company:   strings.TrimSpace(parsed.Subject),
		Engineers: parsed.Engineers,
		Features:  append([]string(nil), parsed.Features...),
		ExpiresAt: &expiresAt,
	}
	if parsed.IssuedAt != nil {
		issuedAt := parsed.IssuedAt.Time.UTC()
		status.IssuedAt = &issuedAt
	}
	if status.Features == nil {
		status.Features = []string{}
	}

	if now.Before(expiresAt) {
		return status, nil
	}

	graceEnds := expiresAt.Add(GracePeriod)
	if now.Before(graceEnds) {
		status.InGrace = true
		status.GraceEndsAt = &graceEnds
		status.Reason = fmt.Sprintf("licence expired on %s; features stay unlocked until %s",
			expiresAt.Format(time.RFC3339), graceEnds.Format(time.RFC3339))
		return status, nil
	}

	community := Community(fmt.Sprintf("licence expired on %s and its grace period ended on %s",
		expiresAt.Format(time.RFC3339), graceEnds.Format(time.RFC3339)))
	community.Company = status.Company
	community.ExpiresAt = &expiresAt
	community.GraceEndsAt = &graceEnds
	return community, ErrExpired
}

// resolved caches the status for the process lifetime. The token is an
// environment variable, so it cannot change without a restart.
var (
	resolvedOnce sync.Once
	resolved     Status
)

// Resolve reads PIPELOGIQ_LICENSE once and logs the outcome. It never returns an
// error: an unlicensed or broken licence is a Community deployment, not a
// startup failure.
func Resolve(token string, logger *slog.Logger) Status {
	resolvedOnce.Do(func() {
		status, err := Verify(token, time.Now().UTC())
		resolved = status
		if logger == nil {
			return
		}
		switch {
		case errors.Is(err, ErrNoToken):
			logger.Info("licence: running as Community", "tier", status.Tier)
		case err != nil:
			// The token itself is never logged.
			logger.Warn("licence: falling back to Community",
				"tier", status.Tier, "reason", status.Reason)
		case status.InGrace:
			logger.Warn("licence: expired but inside its grace period",
				"tier", status.Tier, "company", status.Company, "graceEndsAt", status.GraceEndsAt)
		default:
			logger.Info("licence: active",
				"tier", status.Tier, "company", status.Company,
				"engineers", status.Engineers, "expiresAt", status.ExpiresAt,
				"features", status.Features)
		}
	})
	return resolved
}

// Current returns the status Resolve produced, or Community before Resolve ran.
func Current() Status {
	if resolved.Tier == "" {
		return Community("licence not resolved yet")
	}
	return resolved
}

// Has is the one-call gate handlers use.
func Has(feature string) bool { return Current().Has(feature) }

// resetForTest clears the once-cache. Test-only.
func resetForTest() {
	resolvedOnce = sync.Once{}
	resolved = Status{}
}

// publicKey decodes the compiled-in verification key.
func publicKey() (ed25519.PublicKey, error) {
	encoded := strings.TrimSpace(PublicKeyBase64)
	if encoded == "" {
		return nil, ErrNoPublicKey
	}
	raw, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		return nil, fmt.Errorf("%w: %s", ErrNoPublicKey, err.Error())
	}
	if len(raw) != ed25519.PublicKeySize {
		return nil, fmt.Errorf("%w: key is %d bytes, want %d",
			ErrNoPublicKey, len(raw), ed25519.PublicKeySize)
	}
	return ed25519.PublicKey(raw), nil
}

// SetForTest installs a public key and a resolved token for the duration of one
// test. It exists so the API tests can run as any tier without a real licence.
func SetForTest(t interface{ Cleanup(func()) }, publicKeyBase64, token string) {
	originalKey := PublicKeyBase64
	PublicKeyBase64 = publicKeyBase64
	resetForTest()
	Resolve(token, nil)

	t.Cleanup(func() {
		PublicKeyBase64 = originalKey
		resetForTest()
	})
}

// SetCommunityForTest forces the Community tier for one test.
func SetCommunityForTest(t interface{ Cleanup(func()) }) {
	resetForTest()
	Resolve("", nil)
	t.Cleanup(resetForTest)
}
