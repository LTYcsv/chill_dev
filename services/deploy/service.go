package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os/exec"
	"strings"
	"time"

	"github.com/google/uuid"
)

type DeployService struct {
	repo          *DeploymentRepo
	svcRegistry   *ServiceRegistry
	bus           *EventBus
	secretsSvcURL string
}

func NewDeployService(repo *DeploymentRepo, svcRegistry *ServiceRegistry, bus *EventBus, secretsSvcURL string) *DeployService {
	return &DeployService{repo: repo, svcRegistry: svcRegistry, bus: bus, secretsSvcURL: secretsSvcURL}
}

func (s *DeployService) updateStatus(deploymentID string, status DeploymentStatus, logLine string) {
	s.repo.UpdateStatus(deploymentID, status, logLine)
	if logLine != "" {
		_ = s.bus.Publish("logs.line."+deploymentID, map[string]string{
			"deployment_id": deploymentID,
			"line":          logLine,
			"source":        "deploy",
			"ts":            time.Now().UTC().Format(time.RFC3339),
		})
	}
}

func (s *DeployService) fetchSecretEnvVars(serviceID, environment string) map[string]string {
	url := fmt.Sprintf("%s/api/v1/secrets/env-vars?service_id=%s&env_id=%s",
		s.secretsSvcURL, serviceID, environment)
	resp, err := http.Get(url) //nolint:gosec
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
	_ = s.bus.Publish("deploy.rollback", map[string]string{
		"deployment_id": d.ID,
		"service_id":    d.ServiceID,
	})
	return nil
}

func (s *DeployService) runContainer(ctx context.Context, imageTag, containerName string, port int, envVars map[string]string) error {
	_ = exec.CommandContext(ctx, "docker", "stop", containerName).Run()
	_ = exec.CommandContext(ctx, "docker", "rm", containerName).Run()

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
	_ = s.bus.Subscribe("build.completed", func(data []byte) {
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
				_ = s.bus.Publish("deploy.failed", map[string]string{
					"deployment_id": deployID,
					"service_id":    d.ServiceID,
					"error":         err.Error(),
				})
				return
			}
			s.updateStatus(deployID, StatusSuccess, fmt.Sprintf("container running on port %d", port))
			_ = s.bus.Publish("runtime.deployed", map[string]string{
				"deployment_id": deployID,
				"service_id":    d.ServiceID,
			})
		}()
	})

	_ = s.bus.Subscribe("build.failed", func(data []byte) {
		var payload map[string]string
		if err := json.Unmarshal(data, &payload); err != nil {
			return
		}
		s.updateStatus(payload["deployment_id"], StatusFailed, "build failed: "+payload["error"])
	})
}
