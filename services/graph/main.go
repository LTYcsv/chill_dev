package main

import (
	"log"
	"net/http"
	"time"

	"github.com/nats-io/nats.go"
)

func main() {
	cfg := loadAppConfig()

	nodes, edges, err := loadConfig(cfg.ConfigPath)
	if err != nil {
		log.Fatalf("[graph] failed to load config %s: %v", cfg.ConfigPath, err)
	}
	log.Printf("[graph] loaded %d nodes, %d edges from %s", len(nodes), len(edges), cfg.ConfigPath)

	var store *TTStore
	if cfg.DatabaseURL != "" {
		store, err = newTTStore(cfg.DatabaseURL)
		if err != nil {
			log.Printf("[graph] warn: postgres unavailable (%v), running without time travel", err)
		} else {
			log.Printf("[graph] time travel store connected")
		}
	}

	srv := newServer(nodes, edges, store)
	srv.saveInitialCheckpoints()
	srv.startDailyCheckpoint()

	var nc *nats.Conn
	nc, err = nats.Connect(cfg.NATSURL, nats.MaxReconnects(5))
	if err != nil {
		log.Printf("[graph] warn: NATS unavailable (%v), running without live updates", err)
	} else {
		srv.subscribeToNATS(nc)
		log.Printf("[graph] subscribed to NATS events")
	}

	mux := http.NewServeMux()
	registerRoutes(mux, srv)

	s := &http.Server{
		Addr:         ":" + cfg.Port,
		Handler:      mux,
		ReadTimeout:  5 * time.Second,
		WriteTimeout: 10 * time.Second,
	}

	log.Printf("[graph-service] listening on :%s (config: %s)", cfg.Port, cfg.ConfigPath)
	if err := s.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Fatal(err)
	}
}
