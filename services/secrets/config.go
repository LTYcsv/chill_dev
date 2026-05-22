package main

import (
	"log"
	"os"
)

type Config struct {
	Port          string
	EncryptionKey string
	DatabaseURL   string
}

func loadConfig() Config {
	return Config{
		Port:          getEnv("PORT", "8086"),
		EncryptionKey: getEnv("ENCRYPTION_KEY", ""),
		DatabaseURL:   getEnv("DATABASE_URL", ""),
	}
}

func getEnv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

var knownWeakKeys = []string{
	"12345678901234567890123456789012",
	"00000000000000000000000000000000",
	"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
	"",
}

func validateConfig(cfg Config) {
	for _, bad := range knownWeakKeys {
		if cfg.EncryptionKey == bad {
			log.Fatalf("[secrets] ENCRYPTION_KEY is not set or is a known insecure default — generate one with: openssl rand -hex 16")
		}
	}
	if len(cfg.EncryptionKey) != 32 {
		log.Fatalf("[secrets] ENCRYPTION_KEY must be exactly 32 bytes for AES-256 (got %d)", len(cfg.EncryptionKey))
	}
}
