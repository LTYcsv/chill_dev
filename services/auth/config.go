package main

import (
	"log"
	"os"
)

type Config struct {
	Port      string
	JWTSecret string
	DBDSN     string
}

func loadConfig() Config {
	return Config{
		Port:      getEnv("PORT", "8081"),
		JWTSecret: getEnv("JWT_SECRET", ""),
		DBDSN:     getEnv("DATABASE_URL", "postgres://devplatform:devplatform@localhost:5432/devplatform?sslmode=disable"),
	}
}

func getEnv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

var knownWeakSecrets = []string{
	"dev-secret-change-in-production",
	"change-me-in-production-please",
	"secret",
	"jwt-secret",
	"",
}

func validateConfig(cfg Config) {
	for _, bad := range knownWeakSecrets {
		if cfg.JWTSecret == bad {
			log.Fatalf("[auth] JWT_SECRET is not set or is a known insecure default — set a strong random value (openssl rand -hex 32)")
		}
	}
	if len(cfg.JWTSecret) < 32 {
		log.Fatalf("[auth] JWT_SECRET must be at least 32 characters")
	}
}
