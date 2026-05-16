package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/nats-io/nats.go"
)

type Config struct {
	Port         string
	NATSURL      string
	RegistryHost string
}

func loadConfig() Config {
	return Config{
		Port:         getEnv("PORT", "8083"),
		NATSURL:      getEnv("NATS_URL", "nats://localhost:4222"),
		RegistryHost: getEnv("REGISTRY_HOST", "localhost:5000"),
	}
}

func getEnv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

// ─── Builder ──────────────────────────────────────────────────

type BuildRequest struct {
	DeploymentID string `json:"deployment_id"`
	ServiceID    string `json:"service_id"`
	GitRepo      string `json:"git_repo"`
	GitBranch    string `json:"git_branch"`
	Environment  string `json:"environment"`
}

type Builder struct {
	registryHost string
	nc           *nats.Conn
}

func NewBuilder(registryHost string, nc *nats.Conn) *Builder {
	return &Builder{registryHost: registryHost, nc: nc}
}

func (b *Builder) Build(ctx context.Context, req BuildRequest) {
	log.Printf("[build] starting build for deployment %s", req.DeploymentID)

	imageTag := fmt.Sprintf("%s/%s:%s", b.registryHost, req.ServiceID, req.DeploymentID[:8])

	// 1. Clone repo
	if err := b.cloneRepo(ctx, req.GitRepo, req.GitBranch, req.DeploymentID); err != nil {
		b.publishFailed(req.DeploymentID, "clone failed: "+err.Error())
		return
	}

	// 2. Build Docker image
	if err := b.buildImage(ctx, req.DeploymentID, imageTag); err != nil {
		b.publishFailed(req.DeploymentID, "build failed: "+err.Error())
		return
	}

	// 3. Push to registry
	if err := b.pushImage(ctx, imageTag); err != nil {
		b.publishFailed(req.DeploymentID, "push failed: "+err.Error())
		return
	}

	// 4. Cleanup
	os.RemoveAll("/tmp/build-" + req.DeploymentID)

	b.publishCompleted(req.DeploymentID, imageTag)
}

func (b *Builder) cloneRepo(ctx context.Context, repo, branch, deployID string) error {
	dir := "/tmp/build-" + deployID
	cmd := exec.CommandContext(ctx, "git", "clone", "--depth=1", "--branch", branch, repo, dir)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("%w: %s", err, string(out))
	}
	log.Printf("[build] cloned %s@%s", repo, branch)
	return nil
}

func (b *Builder) buildImage(ctx context.Context, deployID, imageTag string) error {
	dir := "/tmp/build-" + deployID

	// Auto-detect: use Dockerfile if exists, else Buildpacks (simplified here)
	dockerfilePath := dir + "/Dockerfile"
	if _, err := os.Stat(dockerfilePath); os.IsNotExist(err) {
		log.Printf("[build] no Dockerfile found, generating default for %s", deployID)
		if err := b.generateDockerfile(dir); err != nil {
			return err
		}
	}

	cmd := exec.CommandContext(ctx, "docker", "build", "-t", imageTag, dir)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("%w: %s", err, string(out))
	}
	log.Printf("[build] image built: %s", imageTag)
	return nil
}

func (b *Builder) generateDockerfile(dir string) error {
	// Simple heuristic: detect project type
	var dockerfile string

	if _, err := os.Stat(dir + "/go.mod"); err == nil {
		dockerfile = `FROM golang:1.22-alpine AS builder
WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN go build -o server .
FROM alpine:latest
WORKDIR /app
COPY --from=builder /app/server .
EXPOSE 8080
CMD ["./server"]`
	} else if _, err := os.Stat(dir + "/package.json"); err == nil {
		dockerfile = `FROM node:20-alpine
WORKDIR /app
COPY package*.json ./
RUN npm ci --only=production
COPY . .
EXPOSE 3000
CMD ["node", "index.js"]`
	} else {
		dockerfile = `FROM python:3.12-slim
WORKDIR /app
COPY requirements.txt .
RUN pip install -r requirements.txt
COPY . .
EXPOSE 8000
CMD ["python", "main.py"]`
	}

	return os.WriteFile(dir+"/Dockerfile", []byte(dockerfile), 0644)
}

func (b *Builder) pushImage(ctx context.Context, imageTag string) error {
	// Skip push in local dev if registry not available
	cmd := exec.CommandContext(ctx, "docker", "push", imageTag)
	out, err := cmd.CombinedOutput()
	if err != nil {
		// In dev mode, log warning but don't fail
		log.Printf("[build] warn: push failed (dev mode?): %s", string(out))
		return nil
	}
	return nil
}

func (b *Builder) publishCompleted(deploymentID, imageTag string) {
	if b.nc == nil {
		return
	}
	data, _ := json.Marshal(map[string]string{
		"deployment_id": deploymentID,
		"image_tag":     imageTag,
	})
	b.nc.Publish("build.completed", data)
	log.Printf("[build] published build.completed for %s", deploymentID)
}

func (b *Builder) publishFailed(deploymentID, reason string) {
	log.Printf("[build] failed: %s - %s", deploymentID, reason)
	if b.nc == nil {
		return
	}
	data, _ := json.Marshal(map[string]string{
		"deployment_id": deploymentID,
		"error":         reason,
	})
	b.nc.Publish("build.failed", data)
}

// ─── Worker loop ──────────────────────────────────────────────

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
		// Run in goroutine so we don't block NATS subscription
		go builder.Build(context.Background(), req)
	})
	log.Println("[build] worker subscribed to build.requested")
}

// ─── HTTP (health + manual trigger) ──────────────────────────

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}

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

	// Manual trigger (useful for testing without NATS)
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
			"status":      "ok",
			"service":     "build",
			"nats":        natsStatus,
			"registry":    cfg.RegistryHost,
			"docker":      detectDocker(),
		})
	})

	srv := &http.Server{
		Addr:        ":" + cfg.Port,
		Handler:     mux,
		ReadTimeout: 5 * time.Second,
		WriteTimeout: 10 * time.Second,
	}

	log.Printf("[build-service] listening on :%s", cfg.Port)
	if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Fatal(err)
	}
}

func detectDocker() string {
	cmd := exec.Command("docker", "info", "--format", "{{.ServerVersion}}")
	out, err := cmd.Output()
	if err != nil {
		return "unavailable"
	}
	return strings.TrimSpace(string(out))
}
