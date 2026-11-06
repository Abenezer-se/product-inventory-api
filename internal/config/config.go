package config

import (
	"errors"
	"os"
)

// Config holds runtime configuration read from environment variables.
type Config struct {
	Port  string
	Env   string
	DBURL string
}

// Load reads configuration from the environment.
func Load() (Config, error) {
	cfg := Config{
		Port:  getEnv("PORT", "8080"),
		Env:   getEnv("ENV", "development"),
		DBURL: os.Getenv("DB_URL"),
	}
	if cfg.DBURL == "" {
		return Config{}, errors.New("DB_URL environment variable is required")
	}
	return cfg, nil
}

func getEnv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
