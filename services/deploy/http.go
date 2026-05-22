package main

import (
	"encoding/json"
	"io"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
)

type Handler struct {
	svc           *DeployService
	webhookSecret string
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func registerRoutes(mux *http.ServeMux, h *Handler, bus *EventBus, svcRegistry *ServiceRegistry) {
	mux.HandleFunc("POST /api/v1/deployments", h.Deploy)
	mux.HandleFunc("GET /api/v1/deployments/{id}", h.GetDeployment)
	mux.HandleFunc("GET /api/v1/deployments", h.ListDeployments)
	mux.HandleFunc("POST /api/v1/deployments/{id}/rollback", h.Rollback)

	mux.HandleFunc("POST /api/v1/services", h.RegisterService)
	mux.HandleFunc("GET /api/v1/services", h.ListServices)
	mux.HandleFunc("GET /api/v1/services/{id}", h.GetService)
	mux.HandleFunc("DELETE /api/v1/services/{id}", h.DeleteService)

	mux.HandleFunc("POST /api/v1/webhooks/github", h.GitHubWebhook)

	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		natsStatus := "connected"
		if bus.nc == nil || !bus.nc.IsConnected() {
			natsStatus = "disconnected"
		}
		dbStatus := "disabled"
		if svcRegistry.db != nil {
			if err := svcRegistry.db.Ping(); err == nil {
				dbStatus = "connected"
			} else {
				dbStatus = "disconnected"
			}
		}
		writeJSON(w, 200, map[string]any{
			"status":   "ok",
			"service":  "deploy",
			"nats":     natsStatus,
			"postgres": dbStatus,
			"services": len(svcRegistry.List()),
		})
	})
}

func (h *Handler) Deploy(w http.ResponseWriter, r *http.Request) {
	var req DeployRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, 400, map[string]string{"error": "invalid request"})
		return
	}
	d, err := h.svc.TriggerDeploy(r.Context(), req)
	if err != nil {
		writeJSON(w, 500, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, 202, d)
}

func (h *Handler) GetDeployment(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	d, ok := h.svc.repo.Get(id)
	if !ok {
		writeJSON(w, 404, map[string]string{"error": "not found"})
		return
	}
	writeJSON(w, 200, d)
}

func (h *Handler) ListDeployments(w http.ResponseWriter, r *http.Request) {
	serviceID := r.URL.Query().Get("service_id")
	writeJSON(w, 200, h.svc.repo.ListByService(serviceID))
}

func (h *Handler) Rollback(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if err := h.svc.Rollback(r.Context(), id); err != nil {
		writeJSON(w, 400, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, 200, map[string]string{"status": "rollback initiated"})
}

func (h *Handler) RegisterService(w http.ResponseWriter, r *http.Request) {
	var cfg ServiceConfig
	if err := json.NewDecoder(r.Body).Decode(&cfg); err != nil {
		writeJSON(w, 400, map[string]string{"error": "invalid request"})
		return
	}
	if cfg.GitRepo == "" || cfg.GitBranch == "" {
		writeJSON(w, 400, map[string]string{"error": "git_repo and git_branch are required"})
		return
	}
	if cfg.Port == 0 {
		cfg.Port = 8080
	}
	if cfg.ID == "" {
		cfg.ID = uuid.NewString()
	}
	if cfg.Environment == "" {
		cfg.Environment = "production"
	}
	cfg.CreatedAt = time.Now()
	h.svc.svcRegistry.Save(&cfg)
	log.Printf("[deploy] registered service %s (%s@%s)", cfg.ID, cfg.GitRepo, cfg.GitBranch)
	writeJSON(w, 201, cfg)
}

func (h *Handler) ListServices(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, 200, h.svc.svcRegistry.List())
}

func (h *Handler) GetService(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	cfg, ok := h.svc.svcRegistry.Get(id)
	if !ok {
		writeJSON(w, 404, map[string]string{"error": "not found"})
		return
	}
	writeJSON(w, 200, cfg)
}

func (h *Handler) DeleteService(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if _, ok := h.svc.svcRegistry.Get(id); !ok {
		writeJSON(w, 404, map[string]string{"error": "not found"})
		return
	}
	h.svc.svcRegistry.Delete(id)
	writeJSON(w, 200, map[string]string{"status": "deleted"})
}

func (h *Handler) GitHubWebhook(w http.ResponseWriter, r *http.Request) {
	if r.Header.Get("X-GitHub-Event") != "push" {
		writeJSON(w, 200, map[string]string{
			"status": "ignored",
			"event":  r.Header.Get("X-GitHub-Event"),
		})
		return
	}

	body, err := io.ReadAll(r.Body)
	if err != nil {
		writeJSON(w, 400, map[string]string{"error": "cannot read body"})
		return
	}

	if h.webhookSecret != "" {
		sig := r.Header.Get("X-Hub-Signature-256")
		if !validateGitHubSignature(body, sig, h.webhookSecret) {
			writeJSON(w, 401, map[string]string{"error": "invalid signature"})
			return
		}
	}

	var event GitHubPushEvent
	if err := json.Unmarshal(body, &event); err != nil {
		writeJSON(w, 400, map[string]string{"error": "invalid payload"})
		return
	}

	branch := strings.TrimPrefix(event.Ref, "refs/heads/")
	svc, ok := h.svc.svcRegistry.FindByRepo(event.Repository.CloneURL, branch)
	if !ok {
		svc, ok = h.svc.svcRegistry.FindByRepo(event.Repository.SSHURL, branch)
	}
	if !ok {
		writeJSON(w, 200, map[string]string{
			"status": "no matching service",
			"repo":   event.Repository.CloneURL,
			"branch": branch,
		})
		return
	}

	d, err := h.svc.TriggerDeploy(r.Context(), DeployRequest{
		ServiceID:   svc.ID,
		ProjectID:   svc.ProjectID,
		GitRepo:     event.Repository.CloneURL,
		GitBranch:   branch,
		GitCommit:   event.HeadCommit.ID,
		Environment: svc.Environment,
		TriggeredBy: event.Pusher.Email,
	})
	if err != nil {
		writeJSON(w, 500, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, 202, d)
}
