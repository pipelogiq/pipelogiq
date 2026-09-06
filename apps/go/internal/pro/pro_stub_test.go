//go:build !pro

package pro

import (
	"bytes"
	"context"
	"errors"
	"io"
	"log/slog"
	"testing"
	"time"
)

// TestStubBuildRefusesEverything is the contract public CI depends on: the open
// repository builds and its tests pass with no private code present, and every
// Pro entry point refuses.
func TestStubBuildRefusesEverything(t *testing.T) {
	providers := Load(nil, slog.New(slog.NewTextHandler(io.Discard, nil)))
	ctx := context.Background()

	if providers.Compiled {
		t.Fatalf("a build without the pro tag must report Compiled=false")
	}
	if providers.SSO == nil || providers.Audit == nil || providers.Retention == nil {
		t.Fatalf("the stub build must still provide every interface, not nil")
	}

	if providers.SSO.Enabled() {
		t.Fatalf("stub SSO must not report itself enabled")
	}

	if _, _, err := providers.SSO.AuthorizeURL(ctx, "https://example.test/callback"); !errors.Is(err, ErrNotLicensed) {
		t.Fatalf("AuthorizeURL err = %v, want ErrNotLicensed", err)
	}
	if _, err := providers.SSO.Exchange(ctx, "code", "state", "state"); !errors.Is(err, ErrNotLicensed) {
		t.Fatalf("Exchange err = %v, want ErrNotLicensed", err)
	}

	var buffer bytes.Buffer
	if _, err := providers.Audit.Export(ctx, AuditExportRequest{ApplicationID: 1}, &buffer); !errors.Is(err, ErrNotLicensed) {
		t.Fatalf("Export err = %v, want ErrNotLicensed", err)
	}
	if buffer.Len() != 0 {
		t.Fatalf("a refused export must write nothing, wrote %d bytes", buffer.Len())
	}

	if _, err := providers.Retention.Policy(ctx, 1); !errors.Is(err, ErrNotLicensed) {
		t.Fatalf("Policy err = %v, want ErrNotLicensed", err)
	}
	if err := providers.Retention.SetPolicy(ctx, RetentionPolicy{ApplicationID: 1}); !errors.Is(err, ErrNotLicensed) {
		t.Fatalf("SetPolicy err = %v, want ErrNotLicensed", err)
	}
	if _, err := providers.Retention.Purge(ctx, 1, time.Now()); !errors.Is(err, ErrNotLicensed) {
		t.Fatalf("Purge err = %v, want ErrNotLicensed", err)
	}
}
