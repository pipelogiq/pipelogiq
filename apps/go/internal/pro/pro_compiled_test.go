//go:build pro

package pro

import (
	"bytes"
	"log/slog"
	"testing"
)

// TestProSeamCompiled is the release gate: a build carrying the pro tag must link
// the real providers, not the stub. The release workflow runs exactly this test.
func TestProSeamCompiled(t *testing.T) {
	providers := Load(nil, slog.New(slog.NewTextHandler(bytes.NewBuffer(nil), nil)))
	if !providers.Compiled {
		t.Fatal("pro build tag set but the stub providers were linked")
	}
}
