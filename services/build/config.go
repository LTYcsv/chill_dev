package main

import "os"

type Config struct {
	Port         string
	NATSURL      string
	RegistryHost string
}

func loadConfig() Config {
	return Config{
		Port:         getEnv("PORT", "8083"),
		NATSURL:      getEnv("NATS_URL", "nats://localhost:4222"),
		RegistryHost: getEnv("REGISTRY_HOST", "localhost:5000"),
	}
}

func getEnv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
