//go:build pro

package pro

import (
	"log/slog"

	"github.com/jmoiron/sqlx"
)

// Load returns the real implementations. Compiling them in does not unlock
// anything: every handler still asks the licence first.
func Load(db *sqlx.DB, logger *slog.Logger) Providers {
	if logger != nil {
		logger.Info("pro features are compiled into this build; the licence decides what is unlocked")
	}
	return Providers{
		SSO:       newSSO(),
		Audit:     newAuditExport(db),
		Retention: newRetention(db),
		Compiled:  true,
	}
}
