package version

import "testing"

func TestGetVersionSourceAndOverrides(t *testing.T) {
	original := Version
	t.Cleanup(func() { Version = original })
	for _, tc := range []struct {
		name, compiled, environment, want string
	}{
		{"source default", "dev", "", DefaultVersion},
		{"runtime fallback", "dev", "v9.0.0", "v9.0.0"},
		{"release cannot be relabeled", DefaultVersion, "v9.0.0", DefaultVersion},
		{"injected build wins", "v8.0.0", "v9.0.0", "v8.0.0"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			Version = tc.compiled
			t.Setenv("APP_VERSION", tc.environment)
			if got := Get().Version; got != tc.want {
				t.Fatalf("version = %q, want %q", got, tc.want)
			}
		})
	}
}
