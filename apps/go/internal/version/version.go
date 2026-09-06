package version

import (
	"encoding/json"
	"net/http"
	"os"
	"runtime/debug"
	"strings"
)

// Set via ldflags at build time:
//
//	-X pipelogiq/internal/version.Version=v0.4.0-preview.1
//	-X pipelogiq/internal/version.Commit=abc1234
//	-X pipelogiq/internal/version.Date=2025-01-01T00:00:00Z
var (
	Version = "dev"
	Commit  = "unknown"
	Date    = "unknown"
)

type Info struct {
	Version   string `json:"version"`
	Commit    string `json:"commit"`
	BuildDate string `json:"buildDate"`
	GoVersion string `json:"goVersion"`
	// Tier is the resolved licence tier: "community", "pro", or "enterprise".
	// It is here so an operator can read it from the same place they read the
	// version, without an authenticated call.
	Tier string `json:"tier"`
}

func Get() Info {
	goVersion := "unknown"
	if bi, ok := debug.ReadBuildInfo(); ok {
		goVersion = bi.GoVersion
	}

	version := resolvedBuildField(Version, "APP_VERSION", "dev")
	commit := resolvedBuildField(Commit, "APP_COMMIT", "unknown")
	buildDate := resolvedBuildField(Date, "APP_BUILD_DATE", "unknown")

	return Info{
		Version:   version,
		Commit:    commit,
		BuildDate: buildDate,
		GoVersion: goVersion,
		Tier:      TierProvider(),
	}
}

// TierProvider returns the licence tier. It is a variable so the version package
// stays free of a dependency on the licence package, which keeps the import
// graph one-directional.
var TierProvider = func() string { return "community" }

func HandleVersion(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(Get())
}

func resolvedBuildField(current string, envKey string, sentinel string) string {
	trimmedCurrent := strings.TrimSpace(current)
	if trimmedCurrent != "" && trimmedCurrent != sentinel {
		return trimmedCurrent
	}

	if envValue := strings.TrimSpace(os.Getenv(envKey)); envValue != "" {
		return envValue
	}

	if trimmedCurrent != "" {
		return trimmedCurrent
	}

	return sentinel
}
