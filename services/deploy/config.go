package main

import "os"

type Config struct {
	Port          string
	NATSURL       string
	WebhookSecret string
	SecretsSvcURL string
	DatabaseURL   string
}

func loadConfig() Config {
	return Config{
		Port:          getEnv("PORT", "8082"),
		NATSURL:       getEnv("NATS_URL", "nats://localhost:4222"),
		WebhookSecret: getEnv("WEBHOOK_SECRET", ""),
		SecretsSvcURL: getEnv("SECRETS_SVC_URL", "http://localhost:8086"),
		DatabaseURL:   getEnv("DATABASE_URL", ""),
	}
}

func getEnv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
