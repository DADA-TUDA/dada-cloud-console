package config

import (
	"fmt"
	"os"
)

// Config holds all application configuration loaded from environment variables.
type Config struct {
	DBURL           string
	JWTSecret       string
	GitStateRepoPath string
	GitBotName      string
	GitBotEmail     string
	Port            string
	LogLevel        string
	DevMode         bool
}

// Load reads configuration from environment variables.
// Returns an error if any required variable is missing.
func Load() (*Config, error) {
	cfg := &Config{
		DBURL:            getEnv("DB_URL", ""),
		JWTSecret:        getEnv("JWT_SECRET", ""),
		GitStateRepoPath: getEnv("GIT_STATE_REPO_PATH", "/tmp/dada-state-repo"),
		GitBotName:       getEnv("GIT_BOT_NAME", "DADA Platform Bot"),
		GitBotEmail:      getEnv("GIT_BOT_EMAIL", "bot@dada-tuda.ru"),
		Port:             getEnv("PORT", "8080"),
		LogLevel:         getEnv("LOG_LEVEL", "info"),
		DevMode:          getEnv("DEV_MODE", "false") == "true",
	}

	if cfg.DBURL == "" {
		return nil, fmt.Errorf("DB_URL is required")
	}
	if cfg.JWTSecret == "" {
		return nil, fmt.Errorf("JWT_SECRET is required")
	}

	return cfg, nil
}

func getEnv(key, defaultVal string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return defaultVal
}
