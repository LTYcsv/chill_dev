package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"time"
)

// ─── Config ───────────────────────────────────────────────────

type Config struct {
	Port string
}

func loadConfig() Config {
	return Config{Port: getEnv("PORT", "8085")}
}

func getEnv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

// ─── Handlers ─────────────────────────────────────────────────

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}

// handleLogs serves GET /api/v1/logs?service_id=<id>&tail=<n>[&follow=true]
// Container name == service_id (set by deploy service).
func handleLogs(w http.ResponseWriter, r *http.Request) {
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
		streamLogs(w, r, serviceID, tail)
		return
	}

	cmd := exec.CommandContext(r.Context(), "docker", "logs", "--tail", tail, serviceID)
	out, err := cmd.CombinedOutput()
	if err != nil {
		writeJSON(w, 500, map[string]string{"error": "failed to get logs: " + err.Error()})
		return
	}

	lines := parseLines(string(out))
	writeJSON(w, 200, map[string]any{
		"service_id": serviceID,
		"lines":      lines,
		"count":      len(lines),
	})
}

// streamLogs streams container logs as SSE (text/event-stream).
func streamLogs(w http.ResponseWriter, r *http.Request, serviceID, tail string) {
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
		cmd.Wait()
		pw.Close()
	}()

	scanner := bufio.NewScanner(pr)
	for scanner.Scan() {
		select {
		case <-ctx.Done():
			cmd.Process.Kill()
			return
		default:
		}
		fmt.Fprintf(w, "data: %s\n\n", scanner.Text())
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

// ─── Main ─────────────────────────────────────────────────────

func main() {
	cfg := loadConfig()
	mux := http.NewServeMux()

	mux.HandleFunc("GET /api/v1/logs", handleLogs)

	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, 200, map[string]string{"status": "ok", "service": "logs"})
	})

	srv := &http.Server{
		Addr:        ":" + cfg.Port,
		Handler:     mux,
		ReadTimeout: 5 * time.Second,
		// No WriteTimeout — streaming responses need an open connection.
	}

	log.Printf("[logs-service] listening on :%s", cfg.Port)
	if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Fatal(err)
	}
}
