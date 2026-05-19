package main

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	_ "github.com/lib/pq"
	"github.com/nats-io/nats.go"
)

// ─── Config ───────────────────────────────────────────────────

type Config struct {
	Port          string
	NATSURL       string
	WebhookSecret string
	SecretsSvcURL string
	DatabaseURL   string
}

func loadConfig() Config {
	return Config{
		Port:          getEnv("PORT", "8082"),
		NATSURL:       getEnv("NATS_URL", "nats://localhost:4222"),
		WebhookSecret: getEnv("WEBHOOK_SECRET", ""),
		SecretsSvcURL: getEnv("SECRETS_SVC_URL", "http://localhost:8086"),
		DatabaseURL:   getEnv("DATABASE_URL", ""),
	}
}

func getEnv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

// ─── Service Registry ─────────────────────────────────────────
// Maps git repo+branch → service configuration.

type ServiceConfig struct {
	ID          string `json:"id"`
	ProjectID   string `json:"project_id"`
	Name        string `json:"name"`
	GitRepo     string `json:"git_repo"`
	GitBranch   string `json:"git_branch"`
	Port        int    `json:"port"`
	Environment string `json:"environment"`
	CreatedAt   time.Time `json:"created_at"`
}

type ServiceRegistry struct {
	mu   sync.RWMutex
	data map[string]*ServiceConfig
	db   *sql.DB // nil → in-memory only
}

func NewServiceRegistry() *ServiceRegistry {
	return &ServiceRegistry{data: make(map[string]*ServiceConfig)}
}

// NewServiceRegistryPG connects to PostgreSQL, auto-migrates the service_registry
// table, and pre-populates the in-memory map from existing rows.
func NewServiceRegistryPG(databaseURL string) (*ServiceRegistry, error) {
	db, err := sql.Open("postgres", databaseURL)
	if err != nil {
		return nil, fmt.Errorf("open db: %w", err)
	}
	if err := db.Ping(); err != nil {
		return nil, fmt.Errorf("ping db: %w", err)
	}
	r := &ServiceRegistry{data: make(map[string]*ServiceConfig), db: db}
	if err := r.migrate(); err != nil {
		return nil, fmt.Errorf("migrate: %w", err)
	}
	if err := r.loadFromDB(); err != nil {
		return nil, fmt.Errorf("load from db: %w", err)
	}
	return r, nil
}

const createServiceRegistryTable = `
CREATE TABLE IF NOT EXISTS service_registry (
    id          TEXT PRIMARY KEY,
    project_id  TEXT NOT NULL DEFAULT '',
    name        TEXT NOT NULL DEFAULT '',
    git_repo    TEXT NOT NULL,
    git_branch  TEXT NOT NULL DEFAULT 'main',
    port        INTEGER NOT NULL DEFAULT 8080,
    environment TEXT NOT NULL DEFAULT 'production',
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW()
)`

func (r *ServiceRegistry) migrate() error {
	_, err := r.db.Exec(createServiceRegistryTable)
	return err
}

func (r *ServiceRegistry) loadFromDB() error {
	rows, err := r.db.Query(
		`SELECT id, project_id, name, git_repo, git_branch, port, environment, created_at FROM service_registry`)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var s ServiceConfig
		if err := rows.Scan(&s.ID, &s.ProjectID, &s.Name, &s.GitRepo, &s.GitBranch, &s.Port, &s.Environment, &s.CreatedAt); err != nil {
			return err
		}
		r.data[s.ID] = &s
	}
	log.Printf("[deploy] loaded %d services from postgres", len(r.data))
	return rows.Err()
}

func (r *ServiceRegistry) Save(s *ServiceConfig) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.data[s.ID] = s
	if r.db != nil {
		_, err := r.db.Exec(`
			INSERT INTO service_registry (id, project_id, name, git_repo, git_branch, port, environment, created_at)
			VALUES ($1,$2,$3,$4,$5,$6,$7,$8)
			ON CONFLICT (id) DO UPDATE SET
				project_id=$2, name=$3, git_repo=$4, git_branch=$5, port=$6, environment=$7`,
			s.ID, s.ProjectID, s.Name, s.GitRepo, s.GitBranch, s.Port, s.Environment, s.CreatedAt)
		if err != nil {
			log.Printf("[deploy] warn: failed to persist service %s: %v", s.ID, err)
		}
	}
}

func (r *ServiceRegistry) Get(id string) (*ServiceConfig, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	s, ok := r.data[id]
	return s, ok
}

func (r *ServiceRegistry) Delete(id string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.data, id)
	if r.db != nil {
		if _, err := r.db.Exec(`DELETE FROM service_registry WHERE id=$1`, id); err != nil {
			log.Printf("[deploy] warn: failed to delete service %s from db: %v", id, err)
		}
	}
}

func (r *ServiceRegistry) List() []*ServiceConfig {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]*ServiceConfig, 0, len(r.data))
	for _, s := range r.data {
		out = append(out, s)
	}
	return out
}

// FindByRepo finds a service matching the given clone URL and branch.
// Also tries normalizing HTTPS/SSH URL variants.
func (r *ServiceRegistry) FindByRepo(repoURL, branch string) (*ServiceConfig, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	normalized := normalizeRepoURL(repoURL)
	for _, s := range r.data {
		if normalizeRepoURL(s.GitRepo) == normalized && s.GitBranch == branch {
			return s, true
		}
	}
	return nil, false
}

// normalizeRepoURL strips .git suffix and normalizes github.com SSH/HTTPS variants.
func normalizeRepoURL(u string) string {
	u = strings.TrimSuffix(u, ".git")
	// git@github.com:user/repo → github.com/user/repo
	u = strings.Replace(u, "git@github.com:", "github.com/", 1)
	u = strings.TrimPrefix(u, "https://")
	u = strings.TrimPrefix(u, "http://")
	return u
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
	GitCommit   string `json:"git_commit"`
	Environment string `json:"environment"`
	TriggeredBy string `json:"triggered_by"`
}

// ─── Deployment Repository ────────────────────────────────────

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
	if b.nc == nil {
		return nil
	}
	data, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	return b.nc.Publish(subject, data)
}

func (b *EventBus) Subscribe(subject string, handler func(data []byte)) error {
	if b.nc == nil {
		return nil
	}
	_, err := b.nc.Subscribe(subject, func(m *nats.Msg) {
		handler(m.Data)
	})
	return err
}

// ─── Deploy Service ───────────────────────────────────────────

type DeployService struct {
	repo          *DeploymentRepo
	svcRegistry   *ServiceRegistry
	bus           *EventBus
	secretsSvcURL string
}

func NewDeployService(repo *DeploymentRepo, svcRegistry *ServiceRegistry, bus *EventBus, secretsSvcURL string) *DeployService {
	return &DeployService{repo: repo, svcRegistry: svcRegistry, bus: bus, secretsSvcURL: secretsSvcURL}
}

// updateStatus updates deployment status in the repo and publishes the log line
// to NATS so the logs service can stream it to CLI subscribers.
func (s *DeployService) updateStatus(deploymentID string, status DeploymentStatus, logLine string) {
	s.repo.UpdateStatus(deploymentID, status, logLine)
	if logLine != "" {
		s.bus.Publish("logs.line."+deploymentID, map[string]string{
			"deployment_id": deploymentID,
			"line":          logLine,
			"source":        "deploy",
			"ts":            time.Now().UTC().Format(time.RFC3339),
		})
	}
}

// fetchSecretEnvVars calls the secrets service and returns a KEY=VALUE map for
// the given serviceID + environment. Returns empty map (not an error) if the
// secrets service is unreachable — deployments must not fail due to missing secrets svc.
func (s *DeployService) fetchSecretEnvVars(serviceID, environment string) map[string]string {
	url := fmt.Sprintf("%s/api/v1/secrets/env-vars?service_id=%s&env_id=%s",
		s.secretsSvcURL, serviceID, environment)
	resp, err := http.Get(url) //nolint:gosec — internal service URL from config
	if err != nil {
		log.Printf("[deploy] secrets service unreachable, deploying without env vars: %v", err)
		return nil
	}
	defer resp.Body.Close()
	var envMap map[string]string
	if err := json.NewDecoder(resp.Body).Decode(&envMap); err != nil {
		log.Printf("[deploy] failed to parse secrets response: %v", err)
		return nil
	}
	return envMap
}

func (s *DeployService) TriggerDeploy(ctx context.Context, req DeployRequest) (*Deployment, error) {
	d := &Deployment{
		ID:          uuid.NewString(),
		ServiceID:   req.ServiceID,
		ProjectID:   req.ProjectID,
		GitRepo:     req.GitRepo,
		GitBranch:   req.GitBranch,
		GitCommit:   req.GitCommit,
		Environment: req.Environment,
		Status:      StatusQueued,
		TriggeredBy: req.TriggeredBy,
		StartedAt:   time.Now(),
	}
	s.repo.Save(d)

	if err := s.bus.Publish("build.requested", map[string]string{
		"deployment_id": d.ID,
		"service_id":    d.ServiceID,
		"git_repo":      d.GitRepo,
		"git_branch":    d.GitBranch,
		"environment":   d.Environment,
	}); err != nil {
		log.Printf("warn: failed to publish build.requested: %v", err)
	}

	s.updateStatus(d.ID, StatusBuilding, "build requested")
	log.Printf("[deploy] triggered deployment %s for service %s", d.ID, d.ServiceID)
	return d, nil
}

func (s *DeployService) Rollback(ctx context.Context, deploymentID string) error {
	d, ok := s.repo.Get(deploymentID)
	if !ok {
		return fmt.Errorf("deployment not found")
	}
	s.updateStatus(d.ID, StatusRolledBack, "manual rollback triggered")
	s.bus.Publish("deploy.rollback", map[string]string{
		"deployment_id": d.ID,
		"service_id":    d.ServiceID,
	})
	return nil
}

// runContainer stops any existing container for the service and starts a new one.
// envVars are injected as -e KEY=VALUE flags (sourced from the secrets service).
func (s *DeployService) runContainer(ctx context.Context, imageTag, containerName string, port int, envVars map[string]string) error {
	// Stop and remove existing container — ignore errors (may not exist)
	exec.CommandContext(ctx, "docker", "stop", containerName).Run()
	exec.CommandContext(ctx, "docker", "rm", containerName).Run()

	portFlag := fmt.Sprintf("%d:%d", port, port)
	args := []string{"run", "-d", "--name", containerName, "-p", portFlag, "--restart", "unless-stopped"}
	for k, v := range envVars {
		args = append(args, "-e", k+"="+v)
	}
	args = append(args, imageTag)

	cmd := exec.CommandContext(ctx, "docker", args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("docker run: %w: %s", err, strings.TrimSpace(string(out)))
	}
	log.Printf("[deploy] container started: %s (image=%s port=%d secrets=%d)", containerName, imageTag, port, len(envVars))
	return nil
}

func (s *DeployService) subscribeToEvents() {
	s.bus.Subscribe("build.completed", func(data []byte) {
		var payload map[string]string
		if err := json.Unmarshal(data, &payload); err != nil {
			return
		}
		deployID := payload["deployment_id"]
		imageTag := payload["image_tag"]

		d, ok := s.repo.Get(deployID)
		if !ok {
			log.Printf("[deploy] build.completed: deployment %s not found", deployID)
			return
		}
		s.updateStatus(deployID, StatusDeploying, "build complete, starting container")

		port := 8080
		if cfg, ok := s.svcRegistry.Get(d.ServiceID); ok {
			port = cfg.Port
		}

		go func() {
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
			defer cancel()

			envVars := s.fetchSecretEnvVars(d.ServiceID, d.Environment)
			if err := s.runContainer(ctx, imageTag, d.ServiceID, port, envVars); err != nil {
				log.Printf("[deploy] container run failed for %s: %v", deployID, err)
				s.updateStatus(deployID, StatusFailed, "container run failed: "+err.Error())
				s.bus.Publish("deploy.failed", map[string]string{
					"deployment_id": deployID,
					"service_id":    d.ServiceID,
					"error":         err.Error(),
				})
				return
			}
			s.updateStatus(deployID, StatusSuccess,
				fmt.Sprintf("container running on port %d", port))
			s.bus.Publish("runtime.deployed", map[string]string{
				"deployment_id": deployID,
				"service_id":    d.ServiceID,
			})
		}()
	})

	s.bus.Subscribe("build.failed", func(data []byte) {
		var payload map[string]string
		if err := json.Unmarshal(data, &payload); err != nil {
			return
		}
		s.updateStatus(payload["deployment_id"], StatusFailed,
			"build failed: "+payload["error"])
	})
}

// ─── GitHub Webhook ───────────────────────────────────────────

type GitHubPushEvent struct {
	Ref        string `json:"ref"` // "refs/heads/main"
	Repository struct {
		CloneURL string `json:"clone_url"`
		SSHURL   string `json:"ssh_url"`
	} `json:"repository"`
	HeadCommit struct {
		ID      string `json:"id"`
		Message string `json:"message"`
	} `json:"head_commit"`
	Pusher struct {
		Name  string `json:"name"`
		Email string `json:"email"`
	} `json:"pusher"`
}

func validateGitHubSignature(body []byte, signature, secret string) bool {
	if !strings.HasPrefix(signature, "sha256=") {
		return false
	}
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(body)
	expected := "sha256=" + hex.EncodeToString(mac.Sum(nil))
	return hmac.Equal([]byte(signature), []byte(expected))
}

// ─── HTTP Handlers ────────────────────────────────────────────

type Handler struct {
	svc           *DeployService
	webhookSecret string
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

// RegisterService registers a new service with its git repo/branch mapping.
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

// GitHubWebhook handles GitHub push events and triggers a deploy for matching services.
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

	// Try HTTPS clone URL first, fall back to SSH URL
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

// ─── Main ─────────────────────────────────────────────────────

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

	// Deployments
	mux.HandleFunc("POST /api/v1/deployments", h.Deploy)
	mux.HandleFunc("GET /api/v1/deployments/{id}", h.GetDeployment)
	mux.HandleFunc("GET /api/v1/deployments", h.ListDeployments)
	mux.HandleFunc("POST /api/v1/deployments/{id}/rollback", h.Rollback)

	// Service registry
	mux.HandleFunc("POST /api/v1/services", h.RegisterService)
	mux.HandleFunc("GET /api/v1/services", h.ListServices)
	mux.HandleFunc("GET /api/v1/services/{id}", h.GetService)
	mux.HandleFunc("DELETE /api/v1/services/{id}", h.DeleteService)

	// Git webhooks
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
