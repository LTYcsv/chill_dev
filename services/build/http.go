package main

import (
	"encoding/json"
	"net/http"

	"github.com/nats-io/nats.go"
)

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func registerRoutes(mux *http.ServeMux, builder *Builder, nc *nats.Conn, cfg Config) {
	mux.HandleFunc("POST /api/v1/builds", func(w http.ResponseWriter, r *http.Request) {
		var req BuildRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeJSON(w, 400, map[string]string{"error": "invalid request"})
			return
		}
		go builder.Build(r.Context(), req)
		writeJSON(w, 202, map[string]string{"status": "build started", "deployment_id": req.DeploymentID})
	})

	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		natsStatus := "connected"
		if nc == nil || !nc.IsConnected() {
			natsStatus = "disconnected"
		}
		writeJSON(w, 200, map[string]string{
			"status":   "ok",
			"service":  "build",
			"nats":     natsStatus,
			"registry": cfg.RegistryHost,
			"docker":   detectDocker(),
		})
	})
}
