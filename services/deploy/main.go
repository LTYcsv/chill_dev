package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/nats-io/nats.go"
)

// ─── Config ───────────────────────────────────────────────────

type Config struct {
	Port    string
	NATSURL string
}

func loadConfig() Config {
	return Config{
		Port:    getEnv("PORT", "8082"),
		NATSURL: getEnv("NATS_URL", "nats://localhost:4222"),
	}
}

func getEnv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

// ─── Domain ───────────────────────────────────────────────────

type DeploymentStatus string

const (
	StatusQueued     DeploymentStatus = "queued"
	StatusBuilding   DeploymentStatus = "building"
	StatusDeploying  DeploymentStatus = "deploying"
	StatusSuccess    DeploymentStatus = "success"
	StatusFailed     DeploymentStatus = "failed"
	StatusRolledBack DeploymentStatus = "rolled_back"
)

type Deployment struct {
	ID          string           `json:"id"`
	ServiceID   string           `json:"service_id"`
	ProjectID   string           `json:"project_id"`
	GitRepo     string           `json:"git_repo"`
	GitBranch   string           `json:"git_branch"`
	GitCommit   string           `json:"git_commit"`
	Environment string           `json:"environment"`
	Status      DeploymentStatus `json:"status"`
	TriggeredBy string           `json:"triggered_by"`
	Logs        []string         `json:"logs"`
	StartedAt   time.Time        `json:"started_at"`
	FinishedAt  *time.Time       `json:"finished_at,omitempty"`
}

type DeployRequest struct {
	ServiceID   string `json:"service_id"`
	ProjectID   string `json:"project_id"`
	GitRepo     string `json:"git_repo"`
	GitBranch   string `json:"git_branch"`
	Environment string `json:"environment"`
	TriggeredBy string `json:"triggered_by"`
}

// ─── Repository ───────────────────────────────────────────────

type DeploymentRepo struct {
	mu   sync.RWMutex
	data map[string]*Deployment
}

func NewDeploymentRepo() *DeploymentRepo {
	return &DeploymentRepo{data: make(map[string]*Deployment)}
}

func (r *DeploymentRepo) Save(d *Deployment) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.data[d.ID] = d
}

func (r *DeploymentRepo) Get(id string) (*Deployment, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	d, ok := r.data[id]
	return d, ok
}

func (r *DeploymentRepo) ListByService(serviceID string) []*Deployment {
	r.mu.RLock()
	defer r.mu.RUnlock()
	var out []*Deployment
	for _, d := range r.data {
		if d.ServiceID == serviceID {
			out = append(out, d)
		}
	}
	return out
}

func (r *DeploymentRepo) UpdateStatus(id string, status DeploymentStatus, logLine string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	d, ok := r.data[id]
	if !ok {
		return
	}
	d.Status = status
	if logLine != "" {
		d.Logs = append(d.Logs, fmt.Sprintf("[%s] %s", time.Now().Format(time.RFC3339), logLine))
	}
	if status == StatusSuccess || status == StatusFailed || status == StatusRolledBack {
		t := time.Now()
		d.FinishedAt = &t
	}
}

// ─── NATS Event Bus ───────────────────────────────────────────

type EventBus struct {
	nc *nats.Conn
}

func NewEventBus(url string) (*EventBus, error) {
	nc, err := nats.Connect(url,
		nats.RetryOnFailedConnect(true),
		nats.MaxReconnects(10),
	)
	if err != nil {
		return nil, err
	}
	return &EventBus{nc: nc}, nil
}

func (b *EventBus) Publish(subject string, payload any) error {
	data, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	return b.nc.Publish(subject, data)
}

func (b *EventBus) Subscribe(subject string, handler func(data []byte)) error {
	_, err := b.nc.Subscribe(subject, func(m *nats.Msg) {
		handler(m.Data)
	})
	return err
}

// ─── Deploy Service ───────────────────────────────────────────

type DeployService struct {
	repo *DeploymentRepo
	bus  *EventBus
}

func NewDeployService(repo *DeploymentRepo, bus *EventBus) *DeployService {
	return &DeployService{repo: repo, bus: bus}
}

func (s *DeployService) TriggerDeploy(ctx context.Context, req DeployRequest) (*Deployment, error) {
	d := &Deployment{
		ID:          uuid.NewString(),
		ServiceID:   req.ServiceID,
		ProjectID:   req.ProjectID,
		GitRepo:     req.GitRepo,
		GitBranch:   req.GitBranch,
		Environment: req.Environment,
		Status:      StatusQueued,
		TriggeredBy: req.TriggeredBy,
		StartedAt:   time.Now(),
	}
	s.repo.Save(d)

	// Publish to build service via NATS
	if err := s.bus.Publish("build.requested", map[string]string{
		"deployment_id": d.ID,
		"service_id":    d.ServiceID,
		"git_repo":      d.GitRepo,
		"git_branch":    d.GitBranch,
		"environment":   d.Environment,
	}); err != nil {
		log.Printf("warn: failed to publish build.requested: %v", err)
	}

	log.Printf("[deploy] triggered deployment %s for service %s", d.ID, d.ServiceID)
	return d, nil
}

func (s *DeployService) Rollback(ctx context.Context, deploymentID string) error {
	d, ok := s.repo.Get(deploymentID)
	if !ok {
		return fmt.Errorf("deployment not found")
	}
	s.repo.UpdateStatus(d.ID, StatusRolledBack, "manual rollback triggered")

	s.bus.Publish("deploy.rollback", map[string]string{
		"deployment_id": d.ID,
		"service_id":    d.ServiceID,
	})
	return nil
}

func (s *DeployService) subscribeToEvents() {
	// Listen for build results from build-service
	s.bus.Subscribe("build.completed", func(data []byte) {
		var payload map[string]string
		if err := json.Unmarshal(data, &payload); err != nil {
			return
		}
		deployID := payload["deployment_id"]
		imageTag := payload["image_tag"]
		s.repo.UpdateStatus(deployID, StatusDeploying, "build completed, starting deploy")

		// Publish to runtime service
		s.bus.Publish("runtime.deploy", map[string]string{
			"deployment_id": deployID,
			"image_tag":     imageTag,
		})
	})

	s.bus.Subscribe("build.failed", func(data []byte) {
		var payload map[string]string
		if err := json.Unmarshal(data, &payload); err != nil {
			return
		}
		s.repo.UpdateStatus(payload["deployment_id"], StatusFailed, "build failed: "+payload["error"])
	})

	s.bus.Subscribe("runtime.deployed", func(data []byte) {
		var payload map[string]string
		if err := json.Unmarshal(data, &payload); err != nil {
			return
		}
		s.repo.UpdateStatus(payload["deployment_id"], StatusSuccess, "deployment successful")
	})
}

// ─── HTTP Handlers ────────────────────────────────────────────

type Handler struct {
	svc *DeployService
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
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
	list := h.svc.repo.ListByService(serviceID)
	writeJSON(w, 200, list)
}

func (h *Handler) Rollback(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if err := h.svc.Rollback(r.Context(), id); err != nil {
		writeJSON(w, 400, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, 200, map[string]string{"status": "rollback initiated"})
}

// ─── Main ─────────────────────────────────────────────────────

func main() {
	cfg := loadConfig()

	repo := NewDeploymentRepo()

	bus, err := NewEventBus(cfg.NATSURL)
	if err != nil {
		log.Printf("warn: NATS unavailable (%v), running without event bus", err)
		bus = &EventBus{} // no-op bus
	}

	svc := NewDeployService(repo, bus)
	svc.subscribeToEvents()

	h := &Handler{svc: svc}

	mux := http.NewServeMux()
	mux.HandleFunc("POST /api/v1/deployments", h.Deploy)
	mux.HandleFunc("GET /api/v1/deployments/{id}", h.GetDeployment)
	mux.HandleFunc("GET /api/v1/deployments", h.ListDeployments)
	mux.HandleFunc("POST /api/v1/deployments/{id}/rollback", h.Rollback)
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, 200, map[string]string{"status": "ok", "service": "deploy"})
	})

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
