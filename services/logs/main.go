package main

import (
	"context"
	"log"
	"net/http"
	"time"

	"github.com/nats-io/nats.go"
	"github.com/redis/go-redis/v9"
)

func main() {
	cfg := loadConfig()

	var rdb *redis.Client
	if cfg.RedisURL != "" {
		opt, err := redis.ParseURL(cfg.RedisURL)
		if err != nil {
			log.Printf("[logs] warn: invalid REDIS_URL: %v", err)
		} else {
			rdb = redis.NewClient(opt)
			if err := rdb.Ping(context.Background()).Err(); err != nil {
				log.Printf("[logs] warn: Redis unavailable (%v), running without persistence", err)
				rdb = nil
			} else {
				log.Println("[logs] Redis connected, log persistence enabled")
			}
		}
	}

	store := NewLogStore(rdb)

	var nc *nats.Conn
	nc, err := nats.Connect(cfg.NATSURL,
		nats.RetryOnFailedConnect(true),
		nats.MaxReconnects(10),
	)
	if err != nil {
		log.Printf("[logs] warn: NATS unavailable, pipeline log streaming disabled: %v", err)
	} else {
		subscribeNATS(nc, store)
	}

	srv := &Server{store: store}
	mux := http.NewServeMux()
	registerRoutes(mux, srv, nc, rdb, store)

	httpSrv := &http.Server{
		Addr:        ":" + cfg.Port,
		Handler:     mux,
		ReadTimeout: 5 * time.Second,
	}

	log.Printf("[logs-service] listening on :%s", cfg.Port)
	if err := httpSrv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Fatal(err)
	}
}
