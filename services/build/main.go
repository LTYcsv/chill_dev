package main

import (
	"log"
	"net/http"
	"time"

	"github.com/nats-io/nats.go"
)

func main() {
	cfg := loadConfig()

	var nc *nats.Conn
	var err error
	nc, err = nats.Connect(cfg.NATSURL,
		nats.RetryOnFailedConnect(true),
		nats.MaxReconnects(5),
	)
	if err != nil {
		log.Printf("warn: NATS unavailable: %v", err)
	}

	builder := NewBuilder(cfg.RegistryHost, nc)
	startWorker(builder, nc)

	mux := http.NewServeMux()
	registerRoutes(mux, builder, nc, cfg)

	srv := &http.Server{
		Addr:         ":" + cfg.Port,
		Handler:      mux,
		ReadTimeout:  5 * time.Second,
		WriteTimeout: 10 * time.Second,
	}

	log.Printf("[build-service] listening on :%s", cfg.Port)
	if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Fatal(err)
	}
}
