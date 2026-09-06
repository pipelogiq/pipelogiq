// Package pro is the seam between the Apache-2.0 core and the commercial
// features.
//
// The core never imports a Pro implementation directly. It calls the interfaces
// declared here, and gets one of two implementations:
//
//   - built WITHOUT the `pro` tag (this is what public CI does, and what anyone
//     cloning the open repository gets): every call returns ErrNotLicensed.
//     The repository builds and its tests pass with no private code present.
//   - built WITH `-tags pro` (this is what the released image does): the real
//     implementations are linked in, and the licence decides at runtime whether
//     a gate opens.
//
// There is therefore one image, not two. The tag decides what is compiled; the
// licence decides what is allowed. A Community deployment of the released image
// behaves exactly like a build without the tag, because every gate is closed.
package pro

import (
	"context"
	"errors"
	"io"
	"time"
)

// ErrNotLicensed is returned by every stub. Handlers translate it into 402 with
// the licence-required problem type.
var ErrNotLicensed = errors.New("this feature requires a Pro licence")

// ErrNotConfigured reports a licensed feature that the operator has not set up
// yet — an SSO provider with no issuer, for example. It is a 409, not a 402:
// nothing needs buying.
var ErrNotConfigured = errors.New("this feature is licensed but not configured")

// SSO is OIDC login. Local accounts always remain available for break-glass
// access, so a misconfigured provider can never lock an operator out.
type SSO interface {
	// Enabled reports whether a provider is configured.
	Enabled() bool
	// AuthorizeURL starts the flow and returns the provider URL to redirect to,
	// along with the state to store in a cookie.
	AuthorizeURL(ctx context.Context, redirectURI string) (authorizeURL string, state string, err error)
	// Exchange completes the callback and returns the authenticated identity.
	Exchange(ctx context.Context, code string, state string, expectedState string) (*Identity, error)
}

// Identity is who the provider says the caller is, after group-to-role mapping.
type Identity struct {
	Subject   string
	Email     string
	FirstName string
	LastName  string
	Groups    []string
	// Role is the mapped Pipelogiq role. Empty means the groups matched nothing
	// and the login must be refused.
	Role string
}

// AuditExport writes the audit trail as signed NDJSON.
type AuditExport interface {
	// Export streams one NDJSON record per audit row for the application and
	// date range, followed by a signature record over everything written.
	Export(ctx context.Context, req AuditExportRequest, out io.Writer) (AuditExportResult, error)
}

// AuditExportRequest scopes an export.
type AuditExportRequest struct {
	ApplicationID int
	From          time.Time
	To            time.Time
}

// AuditExportResult summarises what was written.
type AuditExportResult struct {
	Records   int    `json:"records"`
	Signature string `json:"signature"`
	Algorithm string `json:"algorithm"`
}

// Retention purges finished pipelines, their stage IO, and their logs.
// Community keeps the current unbounded behaviour and the manual SQL documented
// in docs/licensing.md.
type Retention interface {
	// Policy returns the configured retention for an application.
	Policy(ctx context.Context, applicationID int) (RetentionPolicy, error)
	// SetPolicy stores it.
	SetPolicy(ctx context.Context, policy RetentionPolicy) error
	// Purge deletes everything past the policy's window and reports what it
	// removed. It never touches an unfinished pipeline.
	Purge(ctx context.Context, applicationID int, now time.Time) (PurgeResult, error)
}

// RetentionPolicy is how long finished pipelines are kept.
type RetentionPolicy struct {
	ApplicationID int  `json:"applicationId"`
	KeepDays      int  `json:"keepDays"`
	Enabled       bool `json:"enabled"`
	// ArchiveTarget is an optional S3-compatible destination written before the
	// purge. Empty means purge without archiving.
	ArchiveTarget string `json:"archiveTarget,omitempty"`
}

// PurgeResult reports one purge run.
type PurgeResult struct {
	Pipelines int  `json:"pipelines"`
	Stages    int  `json:"stages"`
	Logs      int  `json:"logs"`
	Archived  bool `json:"archived"`
}

// Providers is everything the core needs from the commercial side. Build tags
// decide which implementation Load returns.
type Providers struct {
	SSO       SSO
	Audit     AuditExport
	Retention Retention
	// Compiled reports whether the Pro implementations are linked in. It is
	// diagnostic only: a licence still has to open each gate.
	Compiled bool
}
