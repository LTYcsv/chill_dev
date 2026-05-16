package main

import (
	"encoding/json"
	"log"
	"net/http"
	"os"
	"sync"
	"time"

	"github.com/nats-io/nats.go"
)

// ─── Domain: Infrastructure Graph ────────────────────────────
// This is the core "unfair advantage" from the strategy doc:
// a live knowledge graph of all services, their dependencies,
// traffic, and state — enabling blast radius analysis and
// infrastructure time-travel.

type NodeType string
const (
	NodeService  NodeType = "service"
	NodeDatabase NodeType = "database"
	NodeQueue    NodeType = "queue"
	NodeCache    NodeType = "cache"
	NodeExternal NodeType = "external"
)

type NodeStatus string
const (
	NodeRunning  NodeStatus = "running"
	NodeDegraded NodeStatus = "degraded"
	NodeDown     NodeStatus = "down"
	NodeBuilding NodeStatus = "building"
)

type GraphNode struct {
	ID          string            `json:"id"`
	Type        NodeType          `json:"type"`
	Name        string            `json:"name"`
	ProjectID   string            `json:"project_id"`
	Environment string            `json:"environment"`
	Status      NodeStatus        `json:"status"`
	ImageTag    string            `json:"image_tag,omitempty"`
	Metadata    map[string]string `json:"metadata,omitempty"`
	UpdatedAt   time.Time         `json:"updated_at"`
}

type GraphEdge struct {
	ID       string `json:"id"`
	From     string `json:"from"`
	To       string `json:"to"`
	Protocol string `json:"protocol"` // http, grpc, tcp, nats, postgres
	RPS      int    `json:"rps"`      // requests per second
	P99Ms    int    `json:"p99_ms"`   // latency p99
	ErrorPct float64 `json:"error_pct"`
}

type InfraGraph struct {
	ProjectID   string      `json:"project_id"`
	Environment string      `json:"environment"`
	Nodes       []GraphNode `json:"nodes"`
	Edges       []GraphEdge `json:"edges"`
	Snapshot    time.Time   `json:"snapshot"`
}

// BlastRadius: given a node, which other nodes would be affected
// if this node goes down.
type BlastRadiusResult struct {
	NodeID       string   `json:"node_id"`
	NodeName     string   `json:"node_name"`
	AffectedIDs  []string `json:"affected_ids"`
	AffectedNames []string `json:"affected_names"`
	Severity     string   `json:"severity"` // low, medium, high, critical
}

// ─── Graph Store ──────────────────────────────────────────────

type GraphStore struct {
	mu    sync.RWMutex
	nodes map[string]*GraphNode // id -> node
	edges map[string]*GraphEdge // id -> edge
	// Snapshots for time-travel: list of (timestamp, graph snapshot JSON)
	history []graphSnapshot
}

type graphSnapshot struct {
	At   time.Time
	Data []byte // JSON-encoded InfraGraph
}

func NewGraphStore() *GraphStore {
	return &GraphStore{
		nodes: make(map[string]*GraphNode),
		edges: make(map[string]*GraphEdge),
	}
}

func (s *GraphStore) UpsertNode(n GraphNode) {
	s.mu.Lock()
	defer s.mu.Unlock()
	n.UpdatedAt = time.Now()
	s.nodes[n.ID] = &n
}

func (s *GraphStore) UpsertEdge(e GraphEdge) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.edges[e.ID] = &e
}

func (s *GraphStore) GetGraph(projectID, env string) InfraGraph {
	s.mu.RLock()
	defer s.mu.RUnlock()

	g := InfraGraph{
		ProjectID:   projectID,
		Environment: env,
		Snapshot:    time.Now(),
	}
	for _, n := range s.nodes {
		if n.ProjectID == projectID && n.Environment == env {
			g.Nodes = append(g.Nodes, *n)
		}
	}
	nodeSet := make(map[string]bool)
	for _, n := range g.Nodes {
		nodeSet[n.ID] = true
	}
	for _, e := range s.edges {
		if nodeSet[e.From] && nodeSet[e.To] {
			g.Edges = append(g.Edges, *e)
		}
	}
	return g
}

func (s *GraphStore) TakeSnapshot(projectID, env string) {
	g := s.GetGraph(projectID, env)
	data, _ := json.Marshal(g)
	s.mu.Lock()
	s.history = append(s.history, graphSnapshot{At: time.Now(), Data: data})
	// Keep last 100 snapshots
	if len(s.history) > 100 {
		s.history = s.history[len(s.history)-100:]
	}
	s.mu.Unlock()
}

func (s *GraphStore) GetSnapshotAt(projectID, env string, at time.Time) *InfraGraph {
	s.mu.RLock()
	defer s.mu.RUnlock()

	// Find the snapshot closest to (but not after) `at`
	var best *graphSnapshot
	for i := range s.history {
		snap := &s.history[i]
		if !snap.At.After(at) {
			best = snap
		}
	}
	if best == nil {
		return nil
	}
	var g InfraGraph
	json.Unmarshal(best.Data, &g)
	return &g
}

// BlastRadius: BFS from node outward through edges to find dependents
func (s *GraphStore) BlastRadius(nodeID string) BlastRadiusResult {
	s.mu.RLock()
	defer s.mu.RUnlock()

	node, ok := s.nodes[nodeID]
	if !ok {
		return BlastRadiusResult{NodeID: nodeID}
	}

	// Build adjacency: who depends on X (incoming edges to nodeID)
	affected := make(map[string]bool)
	queue := []string{nodeID}
	for len(queue) > 0 {
		cur := queue[0]
		queue = queue[1:]
		for _, e := range s.edges {
			if e.To == cur && !affected[e.From] {
				affected[e.From] = true
				queue = append(queue, e.From)
			}
		}
	}
	delete(affected, nodeID)

	var affectedIDs, affectedNames []string
	for id := range affected {
		affectedIDs = append(affectedIDs, id)
		if n, ok := s.nodes[id]; ok {
			affectedNames = append(affectedNames, n.Name)
		}
	}

	severity := "low"
	if len(affected) > 5 {
		severity = "critical"
	} else if len(affected) > 2 {
		severity = "high"
	} else if len(affected) > 0 {
		severity = "medium"
	}

	return BlastRadiusResult{
		NodeID:        nodeID,
		NodeName:      node.Name,
		AffectedIDs:   affectedIDs,
		AffectedNames: affectedNames,
		Severity:      severity,
	}
}

// ─── NATS integration ─────────────────────────────────────────

func subscribeEvents(nc *nats.Conn, store *GraphStore) {
	if nc == nil {
		return
	}

	// When a deploy completes, update the graph node
	nc.Subscribe("runtime.deployed", func(m *nats.Msg) {
		var payload map[string]string
		if err := json.Unmarshal(m.Data, &payload); err != nil {
			return
		}
		node := GraphNode{
			ID:          payload["service_id"],
			Type:        NodeService,
			Name:        payload["service_name"],
			ProjectID:   payload["project_id"],
			Environment: payload["environment"],
			Status:      NodeRunning,
			ImageTag:    payload["image_tag"],
		}
		store.UpsertNode(node)
		// Take a snapshot after each deploy for time-travel
		store.TakeSnapshot(node.ProjectID, node.Environment)
		log.Printf("[graph] updated node %s -> running", node.ID)
	})

	nc.Subscribe("service.unhealthy", func(m *nats.Msg) {
		var payload map[string]string
		if err := json.Unmarshal(m.Data, &payload); err != nil {
			return
		}
		if n, ok := store.nodes[payload["service_id"]]; ok {
			store.mu.Lock()
			n.Status = NodeDegraded
			n.UpdatedAt = time.Now()
			store.mu.Unlock()
		}
	})
}

// ─── HTTP ─────────────────────────────────────────────────────

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}

func main() {
	natsURL := getEnv("NATS_URL", "nats://localhost:4222")
	port := getEnv("PORT", "8087")

	store := NewGraphStore()

	// Seed some demo data for local development
	seedDemoData(store)

	var nc *nats.Conn
	nc, err := nats.Connect(natsURL, nats.MaxReconnects(5))
	if err != nil {
		log.Printf("warn: NATS unavailable: %v", err)
	}
	subscribeEvents(nc, store)

	mux := http.NewServeMux()

	// GET /api/v1/graph?project_id=X&env=production
	mux.HandleFunc("GET /api/v1/graph", func(w http.ResponseWriter, r *http.Request) {
		projectID := r.URL.Query().Get("project_id")
		env := r.URL.Query().Get("env")
		if env == "" {
			env = "production"
		}
		g := store.GetGraph(projectID, env)
		writeJSON(w, 200, g)
	})

	// GET /api/v1/graph/blast-radius/{node_id}
	mux.HandleFunc("GET /api/v1/graph/blast-radius/{nodeID}", func(w http.ResponseWriter, r *http.Request) {
		nodeID := r.PathValue("nodeID")
		result := store.BlastRadius(nodeID)
		writeJSON(w, 200, result)
	})

	// GET /api/v1/graph/time-travel?project_id=X&env=Y&at=2024-01-01T00:00:00Z
	mux.HandleFunc("GET /api/v1/graph/time-travel", func(w http.ResponseWriter, r *http.Request) {
		projectID := r.URL.Query().Get("project_id")
		env := r.URL.Query().Get("env")
		atStr := r.URL.Query().Get("at")
		at, err := time.Parse(time.RFC3339, atStr)
		if err != nil {
			writeJSON(w, 400, map[string]string{"error": "invalid 'at' timestamp, use RFC3339"})
			return
		}
		g := store.GetSnapshotAt(projectID, env, at)
		if g == nil {
			writeJSON(w, 404, map[string]string{"error": "no snapshot found before that timestamp"})
			return
		}
		writeJSON(w, 200, g)
	})

	// POST /api/v1/graph/nodes — register/update a node
	mux.HandleFunc("POST /api/v1/graph/nodes", func(w http.ResponseWriter, r *http.Request) {
		var node GraphNode
		if err := json.NewDecoder(r.Body).Decode(&node); err != nil {
			writeJSON(w, 400, map[string]string{"error": "invalid request"})
			return
		}
		store.UpsertNode(node)
		writeJSON(w, 200, node)
	})

	// POST /api/v1/graph/edges — declare a dependency
	mux.HandleFunc("POST /api/v1/graph/edges", func(w http.ResponseWriter, r *http.Request) {
		var edge GraphEdge
		if err := json.NewDecoder(r.Body).Decode(&edge); err != nil {
			writeJSON(w, 400, map[string]string{"error": "invalid request"})
			return
		}
		store.UpsertEdge(edge)
		writeJSON(w, 200, edge)
	})

	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, 200, map[string]string{"status": "ok", "service": "graph"})
	})

	srv := &http.Server{
		Addr:         ":" + port,
		Handler:      mux,
		ReadTimeout:  5 * time.Second,
		WriteTimeout: 10 * time.Second,
	}

	log.Printf("[graph-service] listening on :%s", port)
	if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Fatal(err)
	}
}

func getEnv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func seedDemoData(store *GraphStore) {
	now := time.Now()
	nodes := []GraphNode{
		{ID: "api-gateway", Name: "api-gateway", Type: NodeService, ProjectID: "demo", Environment: "production", Status: NodeRunning, UpdatedAt: now},
		{ID: "auth-svc", Name: "auth-service", Type: NodeService, ProjectID: "demo", Environment: "production", Status: NodeRunning, UpdatedAt: now},
		{ID: "user-svc", Name: "user-service", Type: NodeService, ProjectID: "demo", Environment: "production", Status: NodeRunning, UpdatedAt: now},
		{ID: "order-svc", Name: "order-service", Type: NodeService, ProjectID: "demo", Environment: "production", Status: NodeRunning, UpdatedAt: now},
		{ID: "postgres-main", Name: "postgres", Type: NodeDatabase, ProjectID: "demo", Environment: "production", Status: NodeRunning, UpdatedAt: now},
		{ID: "redis-cache", Name: "redis", Type: NodeCache, ProjectID: "demo", Environment: "production", Status: NodeRunning, UpdatedAt: now},
		{ID: "nats-bus", Name: "nats", Type: NodeQueue, ProjectID: "demo", Environment: "production", Status: NodeRunning, UpdatedAt: now},
	}
	for _, n := range nodes {
		store.UpsertNode(n)
	}
	edges := []GraphEdge{
		{ID: "e1", From: "api-gateway", To: "auth-svc", Protocol: "http", RPS: 500, P99Ms: 12},
		{ID: "e2", From: "api-gateway", To: "user-svc", Protocol: "http", RPS: 300, P99Ms: 25},
		{ID: "e3", From: "api-gateway", To: "order-svc", Protocol: "http", RPS: 150, P99Ms: 45},
		{ID: "e4", From: "user-svc", To: "postgres-main", Protocol: "postgres", RPS: 200, P99Ms: 5},
		{ID: "e5", From: "order-svc", To: "postgres-main", Protocol: "postgres", RPS: 100, P99Ms: 5},
		{ID: "e6", From: "auth-svc", To: "redis-cache", Protocol: "tcp", RPS: 800, P99Ms: 1},
		{ID: "e7", From: "order-svc", To: "nats-bus", Protocol: "nats", RPS: 50, P99Ms: 2},
	}
	for _, e := range edges {
		store.UpsertEdge(e)
	}
	store.TakeSnapshot("demo", "production")
}
