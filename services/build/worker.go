package main

import (
	"context"
	"encoding/json"
	"log"

	"github.com/nats-io/nats.go"
)

func startWorker(builder *Builder, nc *nats.Conn) {
	if nc == nil {
		log.Println("[build] NATS unavailable, worker not started")
		return
	}
	nc.Subscribe("build.requested", func(m *nats.Msg) {
		var req BuildRequest
		if err := json.Unmarshal(m.Data, &req); err != nil {
			log.Printf("[build] invalid message: %v", err)
			return
		}
		go builder.Build(context.Background(), req)
	})
	log.Println("[build] worker subscribed to build.requested")
}
