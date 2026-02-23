package version

import (
	"encoding/json"
	"net/http"
	"runtime/debug"
)

// Set via ldflags at build time:
//
//	-X pipelogiq/internal/version.Version=v0.1.0
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
}

func Get() Info {
	goVersion := "unknown"
	if bi, ok := debug.ReadBuildInfo(); ok {
		goVersion = bi.GoVersion
	}
	return Info{
		Version:   Version,
		Commit:    Commit,
		BuildDate: Date,
		GoVersion: goVersion,
	}
}

func HandleVersion(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(Get())
}
