//go:build !pro

package pro

import (
	"context"
	"io"
	"log/slog"
	"time"

	"github.com/jmoiron/sqlx"
)

// Load returns the not-licensed implementations. This is the build the open
// repository and public CI produce: no private module is required, everything
// compiles, and every gate is closed.
func Load(_ *sqlx.DB, logger *slog.Logger) Providers {
	if logger != nil {
		logger.Debug("pro features are not compiled into this build")
	}
	return Providers{
		SSO:       notLicensed{},
		Audit:     notLicensed{},
		Retention: notLicensed{},
		Compiled:  false,
	}
}

// notLicensed implements every Pro interface by refusing.
type notLicensed struct{}

func (notLicensed) Enabled() bool { return false }

func (notLicensed) AuthorizeURL(context.Context, string) (string, string, error) {
	return "", "", ErrNotLicensed
}

func (notLicensed) Exchange(context.Context, string, string, string) (*Identity, error) {
	return nil, ErrNotLicensed
}

func (notLicensed) Export(context.Context, AuditExportRequest, io.Writer) (AuditExportResult, error) {
	return AuditExportResult{}, ErrNotLicensed
}

func (notLicensed) Policy(context.Context, int) (RetentionPolicy, error) {
	return RetentionPolicy{}, ErrNotLicensed
}

func (notLicensed) SetPolicy(context.Context, RetentionPolicy) error { return ErrNotLicensed }

func (notLicensed) Purge(context.Context, int, time.Time) (PurgeResult, error) {
	return PurgeResult{}, ErrNotLicensed
}
