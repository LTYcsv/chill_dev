package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os/exec"
	"strings"

	"github.com/nats-io/nats.go"
	"github.com/redis/go-redis/v9"
)

type Server struct {
	store *LogStore
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func registerRoutes(mux *http.ServeMux, srv *Server, nc *nats.Conn, rdb *redis.Client, store *LogStore) {
	mux.HandleFunc("GET /api/v1/logs", srv.containerLogs)
	mux.HandleFunc("GET /api/v1/logs/deployment/{id}", srv.deploymentLogs)

	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		natsStatus := "connected"
		if nc == nil || !nc.IsConnected() {
			natsStatus = "disconnected"
		}
		redisStatus := "disabled"
		if rdb != nil {
			if rdb.Ping(r.Context()).Err() == nil {
				redisStatus = "connected"
			} else {
				redisStatus = "disconnected"
			}
		}
		writeJSON(w, 200, map[string]any{
			"status":              "ok",
			"service":             "logs",
			"nats":                natsStatus,
			"redis":               redisStatus,
			"tracked_deployments": store.count(),
		})
	})
}

func (s *Server) deploymentLogs(w http.ResponseWriter, r *http.Request) {
	deploymentID := r.PathValue("id")
	follow := r.URL.Query().Get("follow") == "true"

	dl := s.store.getOrCreate(deploymentID)
	if !follow {
		dl.mu.Lock()
		lines := make([]LogLine, len(dl.lines))
		copy(lines, dl.lines)
		dl.mu.Unlock()
		writeJSON(w, 200, map[string]any{
			"deployment_id": deploymentID,
			"lines":         lines,
			"count":         len(lines),
		})
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.WriteHeader(200)

	flusher, ok := w.(http.Flusher)
	if !ok {
		return
	}

	ch, snapshot := dl.subscribe()
	for _, l := range snapshot {
		data, _ := json.Marshal(l)
		fmt.Fprintf(w, "data: %s\n\n", data)
	}
	flusher.Flush()

	if ch == nil {
		fmt.Fprintf(w, "event: done\ndata: {}\n\n")
		flusher.Flush()
		return
	}

	defer dl.unsubscribe(ch)

	ctx := r.Context()
	for {
		select {
		case <-ctx.Done():
			return
		case l, ok := <-ch:
			if !ok {
				fmt.Fprintf(w, "event: done\ndata: {}\n\n")
				flusher.Flush()
				return
			}
			data, _ := json.Marshal(l)
			fmt.Fprintf(w, "data: %s\n\n", data)
			flusher.Flush()
		}
	}
}

func (s *Server) containerLogs(w http.ResponseWriter, r *http.Request) {
	serviceID := r.URL.Query().Get("service_id")
	if serviceID == "" {
		writeJSON(w, 400, map[string]string{"error": "service_id is required"})
		return
	}

	tail := r.URL.Query().Get("tail")
	if tail == "" {
		tail = "100"
	}

	if r.URL.Query().Get("follow") == "true" {
		s.streamContainerLogs(w, r, serviceID, tail)
		return
	}

	cmd := exec.CommandContext(r.Context(), "docker", "logs", "--tail", tail, serviceID)
	out, err := cmd.CombinedOutput()
	if err != nil {
		writeJSON(w, 500, map[string]string{"error": "docker logs failed: " + err.Error()})
		return
	}

	lines := parseLines(string(out))
	writeJSON(w, 200, map[string]any{
		"service_id": serviceID,
		"lines":      lines,
		"count":      len(lines),
	})
}

func (s *Server) streamContainerLogs(w http.ResponseWriter, r *http.Request, serviceID, tail string) {
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.WriteHeader(200)

	flusher, ok := w.(http.Flusher)
	if !ok {
		return
	}

	ctx := r.Context()
	cmd := exec.CommandContext(ctx, "docker", "logs", "--tail", tail, "--follow", serviceID)

	pr, pw := io.Pipe()
	cmd.Stdout = pw
	cmd.Stderr = pw

	if err := cmd.Start(); err != nil {
		fmt.Fprintf(w, "data: error: %s\n\n", err.Error())
		flusher.Flush()
		return
	}

	go func() {
		_ = cmd.Wait()
		_ = pw.Close()
	}()

	sc := bufio.NewScanner(pr)
	for sc.Scan() {
		select {
		case <-ctx.Done():
			_ = cmd.Process.Kill()
			return
		default:
		}
		fmt.Fprintf(w, "data: %s\n\n", sc.Text())
		flusher.Flush()
	}
}

func parseLines(s string) []string {
	var lines []string
	sc := bufio.NewScanner(strings.NewReader(s))
	for sc.Scan() {
		if line := sc.Text(); line != "" {
			lines = append(lines, line)
		}
	}
	return lines
}
