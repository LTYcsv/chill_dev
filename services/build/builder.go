package main

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"time"

	"github.com/nats-io/nats.go"
)

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

func (b *Builder) publishLogLine(deploymentID, line string) {
	if b.nc == nil {
		return
	}
	data, _ := json.Marshal(map[string]string{
		"deployment_id": deploymentID,
		"line":          line,
		"source":        "build",
		"ts":            time.Now().UTC().Format(time.RFC3339),
	})
	b.nc.Publish("logs.line."+deploymentID, data)
}

func (b *Builder) runAndStream(ctx context.Context, deploymentID string, args ...string) error {
	cmd := exec.CommandContext(ctx, args[0], args[1:]...)

	pr, pw := io.Pipe()
	cmd.Stdout = pw
	cmd.Stderr = pw

	if err := cmd.Start(); err != nil {
		_ = pw.Close()
		return err
	}

	errCh := make(chan error, 1)
	go func() {
		err := cmd.Wait()
		_ = pw.Close()
		errCh <- err
	}()

	sc := bufio.NewScanner(pr)
	for sc.Scan() {
		line := sc.Text()
		log.Printf("[build:%s] %s", deploymentID[:8], line)
		b.publishLogLine(deploymentID, line)
	}

	return <-errCh
}

func (b *Builder) Build(ctx context.Context, req BuildRequest) {
	log.Printf("[build] starting deployment %s", req.DeploymentID)
	b.publishLogLine(req.DeploymentID, fmt.Sprintf("build started: %s @ %s", req.GitRepo, req.GitBranch))

	imageTag := fmt.Sprintf("%s/%s:%s", b.registryHost, req.ServiceID, req.DeploymentID[:8])

	b.publishLogLine(req.DeploymentID, "cloning repository...")
	if err := b.cloneRepo(ctx, req); err != nil {
		b.publishFailed(req.DeploymentID, "clone failed: "+err.Error())
		return
	}

	b.publishLogLine(req.DeploymentID, "building Docker image: "+imageTag)
	if err := b.buildImage(ctx, req, imageTag); err != nil {
		b.publishFailed(req.DeploymentID, "build failed: "+err.Error())
		return
	}

	b.publishLogLine(req.DeploymentID, "pushing image to registry...")
	if err := b.pushImage(ctx, req, imageTag); err != nil {
		b.publishFailed(req.DeploymentID, "push failed: "+err.Error())
		return
	}

	_ = os.RemoveAll("/tmp/build-" + req.DeploymentID)
	b.publishCompleted(req.DeploymentID, imageTag)
}

func (b *Builder) cloneRepo(ctx context.Context, req BuildRequest) error {
	dir := "/tmp/build-" + req.DeploymentID
	return b.runAndStream(ctx, req.DeploymentID,
		"git", "clone", "--depth=1", "--branch", req.GitBranch, req.GitRepo, dir)
}

func (b *Builder) buildImage(ctx context.Context, req BuildRequest, imageTag string) error {
	dir := "/tmp/build-" + req.DeploymentID

	if _, err := os.Stat(dir + "/Dockerfile"); os.IsNotExist(err) {
		b.publishLogLine(req.DeploymentID, "no Dockerfile found, generating default")
		if err := b.generateDockerfile(dir); err != nil {
			return err
		}
	}

	return b.runAndStream(ctx, req.DeploymentID, "docker", "build", "-t", imageTag, dir)
}

func (b *Builder) pushImage(ctx context.Context, req BuildRequest, imageTag string) error {
	if err := b.runAndStream(ctx, req.DeploymentID, "docker", "push", imageTag); err != nil {
		b.publishLogLine(req.DeploymentID, "warn: push failed (dev mode?): "+err.Error())
	}
	return nil
}

func (b *Builder) generateDockerfile(dir string) error {
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
	log.Printf("[build] failed: %s — %s", deploymentID, reason)
	if b.nc == nil {
		return
	}
	data, _ := json.Marshal(map[string]string{
		"deployment_id": deploymentID,
		"error":         reason,
	})
	b.nc.Publish("build.failed", data)
}
