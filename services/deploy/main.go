package main

import (
	"log"
	"net/http"
	"time"
)

func main() {
	cfg := loadConfig()

	repo := NewDeploymentRepo()

	var svcRegistry *ServiceRegistry
	if cfg.DatabaseURL != "" {
		var err error
		svcRegistry, err = NewServiceRegistryPG(cfg.DatabaseURL)
		if err != nil {
			log.Printf("[deploy] warn: postgres unavailable (%v), falling back to in-memory registry", err)
			svcRegistry = NewServiceRegistry()
		}
	} else {
		svcRegistry = NewServiceRegistry()
	}

	bus, err := NewEventBus(cfg.NATSURL)
	if err != nil {
		log.Printf("warn: NATS unavailable (%v), running without event bus", err)
		bus = &EventBus{}
	}

	svc := NewDeployService(repo, svcRegistry, bus, cfg.SecretsSvcURL)
	svc.subscribeToEvents()

	h := &Handler{svc: svc, webhookSecret: cfg.WebhookSecret}

	mux := http.NewServeMux()
	registerRoutes(mux, h, bus, svcRegistry)

	srv := &http.Server{
		Addr:         ":" + cfg.Port,
		Handler:      mux,
		ReadTimeout:  5 * time.Second,
		WriteTimeout: 10 * time.Second,
	}

	log.Printf("[deploy-service] listening on :%s", cfg.Port)
	if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Fatal(err)
	}
}
