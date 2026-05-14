package config

import "testing"

func TestLoadUsesLegacySecretEnvFallbacks(t *testing.T) {
	t.Setenv("DATABASE_URL", "")
	t.Setenv("DB_URL", "postgres://fallback")
	t.Setenv("GITOPS_DEFAULT_REPO_URL", "https://example.com/repo.git")
	t.Setenv("GITOPS_DEFAULT_USERNAME", "")
	t.Setenv("GIT_USERNAME", "legacy-user")
	t.Setenv("GITOPS_DEFAULT_TOKEN", "")
	t.Setenv("GIT_TOKEN", "legacy-token")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	if cfg.DatabaseURL != "postgres://fallback" {
		t.Fatalf("cfg.DatabaseURL = %q, want %q", cfg.DatabaseURL, "postgres://fallback")
	}
	if cfg.DefaultUsername != "legacy-user" {
		t.Fatalf("cfg.DefaultUsername = %q, want %q", cfg.DefaultUsername, "legacy-user")
	}
	if cfg.DefaultToken != "legacy-token" {
		t.Fatalf("cfg.DefaultToken = %q, want %q", cfg.DefaultToken, "legacy-token")
	}
}

