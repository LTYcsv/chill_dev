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
	"sync"
	"time"

	"github.com/nats-io/nats.go"
)

// ─── Config ───────────────────────────────────────────────────

type Config struct {
	Port    string
	NATSURL string
}

func loadConfig() Config {
	return Config{
		Port:    getEnv("PORT", "8085"),
		NATSURL: getEnv("NATS_URL", "nats://localhost:4222"),
	}
}

func getEnv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

// ─── Log Store ────────────────────────────────────────────────

type LogLine struct {
	DeploymentID string    `json:"deployment_id"`
	Line         string    `json:"line"`
	Source       string    `json:"source"` // "build" | "deploy"
	TS           time.Time `json:"ts"`
}

const maxLinesPerDeployment = 2000

// DeploymentLog is a ring buffer with fan-out to live SSE subscribers.
type DeploymentLog struct {
	mu    sync.Mutex
	lines []LogLine
	done  bool
	subs  map[chan LogLine]struct{}
}

func newDeploymentLog() *DeploymentLog {
	return &DeploymentLog{subs: make(map[chan LogLine]struct{})}
}

func (d *DeploymentLog) append(l LogLine) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if len(d.lines) >= maxLinesPerDeployment {
		d.lines = d.lines[1:]
	}
	d.lines = append(d.lines, l)
	if !d.done {
		for ch := range d.subs {
			select {
			case ch <- l:
			default: // subscriber too slow — drop
			}
		}
	}
}

// markDone signals all subscribers that the deployment has reached a terminal state.
func (d *DeploymentLog) markDone() {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.done = true
	for ch := range d.subs {
		close(ch)
	}
	d.subs = make(map[chan LogLine]struct{})
}

// subscribe returns a snapshot of buffered lines and a channel for future lines.
// If the deployment is already done, the channel is nil.
func (d *DeploymentLog) subscribe() (chan LogLine, []LogLine) {
	d.mu.Lock()
	defer d.mu.Unlock()
	snapshot := make([]LogLine, len(d.lines))
	copy(snapshot, d.lines)
	if d.done {
		return nil, snapshot
	}
	ch := make(chan LogLine, 128)
	d.subs[ch] = struct{}{}
	return ch, snapshot
}

// unsubscribe removes the channel from the fan-out set.
// The caller must stop reading from ch after this returns.
func (d *DeploymentLog) unsubscribe(ch chan LogLine) {
	d.mu.Lock()
	defer d.mu.Unlock()
	// markDone may have already closed and removed ch; delete is a no-op in that case.
	delete(d.subs, ch)
}

type LogStore struct {
	mu   sync.RWMutex
	data map[string]*DeploymentLog
}

func NewLogStore() *LogStore {
	return &LogStore{data: make(map[string]*DeploymentLog)}
}

func (s *LogStore) getOrCreate(deploymentID string) *DeploymentLog {
	s.mu.Lock()
	defer s.mu.Unlock()
	if dl, ok := s.data[deploymentID]; ok {
		return dl
	}
	dl := newDeploymentLog()
	s.data[deploymentID] = dl
	return dl
}

func (s *LogStore) get(deploymentID string) *DeploymentLog {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.data[deploymentID]
}

func (s *LogStore) count() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.data)
}

func (s *LogStore) Ingest(l LogLine) {
	s.getOrCreate(l.DeploymentID).append(l)
}

func (s *LogStore) MarkDone(deploymentID string) {
	if dl := s.get(deploymentID); dl != nil {
		dl.markDone()
	}
}

// ─── NATS ─────────────────────────────────────────────────────

func subscribeNATS(nc *nats.Conn, store *LogStore) {
	// Individual log lines published by build/deploy services
	nc.Subscribe("logs.line.*", func(m *nats.Msg) {
		var l LogLine
		if err := json.Unmarshal(m.Data, &l); err != nil {
			return
		}
		if l.TS.IsZero() {
			l.TS = time.Now()
		}
		store.Ingest(l)
	})

	// Terminal events — mark deployment done and append a summary line
	terminalSubs := map[string]func(map[string]string) string{
		"build.completed": func(p map[string]string) string { return "build completed: " + p["image_tag"] },
		"build.failed":    func(p map[string]string) string { return "build failed: " + p["error"] },
		"runtime.deployed": func(p map[string]string) string { return "deployment successful" },
		"deploy.failed":   func(p map[string]string) string { return "deployment failed: " + p["error"] },
	}

	for subj, msgFn := range terminalSubs {
		subj, msgFn := subj, msgFn
		nc.Subscribe(subj, func(m *nats.Msg) {
			var payload map[string]string
			if err := json.Unmarshal(m.Data, &payload); err != nil {
				return
			}
			deployID := payload["deployment_id"]
			if deployID == "" {
				return
			}
			store.Ingest(LogLine{
				DeploymentID: deployID,
				Line:         msgFn(payload),
				Source:       "system",
				TS:           time.Now(),
			})
			store.MarkDone(deployID)
		})
	}

	log.Println("[logs] NATS subscriptions active: logs.line.*, build/runtime events")
}

// ─── HTTP Server ──────────────────────────────────────────────

type Server struct {
	store *LogStore
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}

// GET /api/v1/logs/deployment/{id}[?follow=true]
// Streams deployment pipeline logs (build + deploy events).
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

	// Flush buffered lines first
	for _, l := range snapshot {
		data, _ := json.Marshal(l)
		fmt.Fprintf(w, "data: %s\n\n", data)
	}
	flusher.Flush()

	if ch == nil {
		// Deployment already finished — send close event and exit
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
				// markDone closed the channel — deployment is done
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

// GET /api/v1/logs?service_id=<id>[&tail=N][&follow=true]
// Streams runtime logs from a Docker container.
// Container name == service_id (as set by deploy service).
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
		cmd.Wait()
		pw.Close()
	}()

	sc := bufio.NewScanner(pr)
	for sc.Scan() {
		select {
		case <-ctx.Done():
			cmd.Process.Kill()
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

// ─── Main ─────────────────────────────────────────────────────

func main() {
	cfg := loadConfig()
	store := NewLogStore()

	var nc *nats.Conn
	nc, err := nats.Connect(cfg.NATSURL,
		nats.RetryOnFailedConnect(true),
		nats.MaxReconnects(10),
	)
	if err != nil {
		log.Printf("[logs] warn: NATS unavailable, pipeline log streaming disabled: %v", err)
	} else {
		subscribeNATS(nc, store)
	}

	srv := &Server{store: store}
	mux := http.NewServeMux()

	mux.HandleFunc("GET /api/v1/logs", srv.containerLogs)
	mux.HandleFunc("GET /api/v1/logs/deployment/{id}", srv.deploymentLogs)

	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		natsStatus := "connected"
		if nc == nil || !nc.IsConnected() {
			natsStatus = "disconnected"
		}
		writeJSON(w, 200, map[string]any{
			"status":             "ok",
			"service":            "logs",
			"nats":               natsStatus,
			"tracked_deployments": store.count(),
		})
	})

	httpSrv := &http.Server{
		Addr:        ":" + cfg.Port,
		Handler:     mux,
		ReadTimeout: 5 * time.Second,
		// No WriteTimeout — streaming responses keep the connection open.
	}

	log.Printf("[logs-service] listening on :%s", cfg.Port)
	if err := httpSrv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Fatal(err)
	}
}
