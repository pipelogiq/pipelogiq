package config

import "testing"

func TestLoadAPIRequiresJWTSecret(t *testing.T) {
	t.Setenv("APP_ID", "pipelogiq-test")
	t.Setenv("JWT_SECRET", "")

	_, err := LoadAPI()
	if err == nil {
		t.Fatal("expected error when JWT_SECRET is missing")
	}
}

func TestLoadAPIRejectsShortJWTSecret(t *testing.T) {
	t.Setenv("APP_ID", "pipelogiq-test")
	t.Setenv("JWT_SECRET", "short-secret")

	_, err := LoadAPI()
	if err == nil {
		t.Fatal("expected error when JWT_SECRET is too short")
	}
}

func TestLoadAPIParsesAllowedOrigins(t *testing.T) {
	t.Setenv("APP_ID", "pipelogiq-test")
	t.Setenv("JWT_SECRET", "0123456789abcdef0123456789abcdef")
	t.Setenv("CORS_ALLOWED_ORIGINS", "http://localhost:3300, https://pipelogiq.dev ")

	cfg, err := LoadAPI()
	if err != nil {
		t.Fatalf("LoadAPI() error = %v", err)
	}

	if len(cfg.AllowedOrigins) != 2 {
		t.Fatalf("len(cfg.AllowedOrigins) = %d, want 2", len(cfg.AllowedOrigins))
	}
	if cfg.AllowedOrigins[0] != "http://localhost:3300" {
		t.Fatalf("AllowedOrigins[0] = %q", cfg.AllowedOrigins[0])
	}
	if cfg.AllowedOrigins[1] != "https://pipelogiq.dev" {
		t.Fatalf("AllowedOrigins[1] = %q", cfg.AllowedOrigins[1])
	}
}
