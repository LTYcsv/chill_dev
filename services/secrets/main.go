package main

import (
	"log"
	"net/http"
	"time"
)

func main() {
	cfg := loadConfig()
	validateConfig(cfg)

	enc, err := NewEncryptor(cfg.EncryptionKey)
	if err != nil {
		log.Fatalf("invalid encryption key: %v", err)
	}

	var store SecretStore
	if cfg.DatabaseURL != "" {
		pg, err := NewPGSecretStore(cfg.DatabaseURL)
		if err != nil {
			log.Fatalf("postgres connect: %v", err)
		}
		store = pg
		log.Printf("[secrets-service] using PostgreSQL storage")
	} else {
		store = NewMemSecretStore()
		log.Printf("[secrets-service] using in-memory storage (set DATABASE_URL for persistence)")
	}

	h := &Handler{store: store, enc: enc}

	mux := http.NewServeMux()
	registerRoutes(mux, h, cfg)

	srv := &http.Server{
		Addr:         ":" + cfg.Port,
		Handler:      mux,
		ReadTimeout:  5 * time.Second,
		WriteTimeout: 10 * time.Second,
	}

	log.Printf("[secrets-service] listening on :%s", cfg.Port)
	if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Fatal(err)
	}
}
