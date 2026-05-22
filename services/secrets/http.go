package main

import (
	"encoding/json"
	"log"
	"net/http"
	"time"
)

type Handler struct {
	store SecretStore
	enc   *Encryptor
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func registerRoutes(mux *http.ServeMux, h *Handler, cfg Config) {
	mux.HandleFunc("PUT /api/v1/secrets", h.Set)
	mux.HandleFunc("GET /api/v1/secrets", h.List)
	mux.HandleFunc("GET /api/v1/secrets/env-vars", h.EnvVars)
	mux.HandleFunc("GET /api/v1/secrets/{id}/value", h.GetValue)
	mux.HandleFunc("DELETE /api/v1/secrets/{id}", h.Delete)
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		backend := "memory"
		if cfg.DatabaseURL != "" {
			backend = "postgres"
		}
		writeJSON(w, 200, map[string]string{"status": "ok", "service": "secrets", "backend": backend})
	})
}

func (h *Handler) Set(w http.ResponseWriter, r *http.Request) {
	var req struct {
		ServiceID     string `json:"service_id"`
		EnvironmentID string `json:"environment_id"`
		Key           string `json:"key"`
		Value         string `json:"value"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, 400, map[string]string{"error": "invalid request"})
		return
	}
	if req.Key == "" || req.Value == "" {
		writeJSON(w, 400, map[string]string{"error": "key and value are required"})
		return
	}
	encrypted, err := h.enc.Encrypt(req.Value)
	if err != nil {
		writeJSON(w, 500, map[string]string{"error": "encryption failed"})
		return
	}
	s, err := h.store.Set(req.ServiceID, req.EnvironmentID, req.Key, encrypted)
	if err != nil {
		log.Printf("[secrets] store.Set error: %v", err)
		writeJSON(w, 500, map[string]string{"error": "store failed"})
		return
	}
	writeJSON(w, 200, map[string]any{
		"id": s.ID, "service_id": s.ServiceID, "environment_id": s.EnvironmentID,
		"key": s.Key, "updated_at": s.UpdatedAt,
	})
}

func (h *Handler) List(w http.ResponseWriter, r *http.Request) {
	serviceID := r.URL.Query().Get("service_id")
	envID := r.URL.Query().Get("env_id")
	secrets, err := h.store.List(serviceID, envID)
	if err != nil {
		log.Printf("[secrets] store.List error: %v", err)
		writeJSON(w, 500, map[string]string{"error": "store error"})
		return
	}
	type safeSecret struct {
		ID            string    `json:"id"`
		Key           string    `json:"key"`
		ServiceID     string    `json:"service_id"`
		EnvironmentID string    `json:"environment_id"`
		UpdatedAt     time.Time `json:"updated_at"`
	}
	out := make([]safeSecret, 0, len(secrets))
	for _, s := range secrets {
		out = append(out, safeSecret{
			ID: s.ID, Key: s.Key, ServiceID: s.ServiceID, EnvironmentID: s.EnvironmentID, UpdatedAt: s.UpdatedAt,
		})
	}
	writeJSON(w, 200, out)
}

func (h *Handler) GetValue(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	s, err := h.store.GetByID(id)
	if err != nil {
		log.Printf("[secrets] store.GetByID error: %v", err)
		writeJSON(w, 500, map[string]string{"error": "store error"})
		return
	}
	if s == nil {
		writeJSON(w, 404, map[string]string{"error": "not found"})
		return
	}
	value, err := h.enc.Decrypt(s.ValueEncrypted)
	if err != nil {
		writeJSON(w, 500, map[string]string{"error": "decryption failed"})
		return
	}
	writeJSON(w, 200, map[string]string{"key": s.Key, "value": value})
}

func (h *Handler) EnvVars(w http.ResponseWriter, r *http.Request) {
	serviceID := r.URL.Query().Get("service_id")
	envID := r.URL.Query().Get("env_id")
	secrets, err := h.store.List(serviceID, envID)
	if err != nil {
		log.Printf("[secrets] store.List error: %v", err)
		writeJSON(w, 500, map[string]string{"error": "store error"})
		return
	}
	envMap := make(map[string]string, len(secrets))
	for _, s := range secrets {
		value, err := h.enc.Decrypt(s.ValueEncrypted)
		if err != nil {
			log.Printf("[secrets] decrypt error for key %s: %v", s.Key, err)
			continue
		}
		envMap[s.Key] = value
	}
	writeJSON(w, 200, envMap)
}

func (h *Handler) Delete(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	ok, err := h.store.Delete(id)
	if err != nil {
		log.Printf("[secrets] store.Delete error: %v", err)
		writeJSON(w, 500, map[string]string{"error": "store error"})
		return
	}
	if !ok {
		writeJSON(w, 404, map[string]string{"error": "not found"})
		return
	}
	writeJSON(w, 200, map[string]string{"status": "deleted"})
}
