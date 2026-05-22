package main

import "os"

type Config struct {
	Port     string
	NATSURL  string
	RedisURL string
}

func loadConfig() Config {
	return Config{
		Port:     getEnv("PORT", "8085"),
		NATSURL:  getEnv("NATS_URL", "nats://localhost:4222"),
		RedisURL: getEnv("REDIS_URL", ""),
	}
}

func getEnv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
