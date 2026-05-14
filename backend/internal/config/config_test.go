package config

import "testing"

func TestLoadUsesDatabaseURLFallbackAndHttpPort(t *testing.T) {
	t.Setenv("DB_URL", "")
	t.Setenv("DATABASE_URL", "postgres://fallback")
	t.Setenv("JWT_SECRET", "secret")
	t.Setenv("HTTP_PORT", "9090")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	if cfg.DBURL != "postgres://fallback" {
		t.Fatalf("cfg.DBURL = %q, want %q", cfg.DBURL, "postgres://fallback")
	}
	if cfg.Port != "9090" {
		t.Fatalf("cfg.Port = %q, want %q", cfg.Port, "9090")
	}
}

