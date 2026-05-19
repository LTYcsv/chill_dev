package main

import (
	"encoding/json"
	"log"
	"net/http"
	"os"
	"time"
)

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
	NodeUnknown  NodeStatus = "unknown"
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
}

type GraphEdge struct {
	ID       string  `json:"id"`
	From     string  `json:"from"`
	To       string  `json:"to"`
	Protocol string  `json:"protocol"`
	RPS      int     `json:"rps,omitempty"`
	P99Ms    int     `json:"p99_ms,omitempty"`
	ErrorPct float64 `json:"error_pct,omitempty"`
}

type InfraGraph struct {
	ProjectID   string      `json:"project_id"`
	Environment string      `json:"environment"`
	Nodes       []GraphNode `json:"nodes"`
	Edges       []GraphEdge `json:"edges"`
	Snapshot    time.Time   `json:"snapshot"`
}

type BlastRadiusResult struct {
	NodeID        string   `json:"node_id"`
	NodeName      string   `json:"node_name"`
	AffectedIDs   []string `json:"affected_ids"`
	AffectedNames []string `json:"affected_names"`
	Severity      string   `json:"severity"` // low, medium, high, critical
}

// GraphConfig is the shape of the config file.
type GraphConfig struct {
	Nodes []GraphNode `json:"nodes"`
	Edges []GraphEdge `json:"edges"`
}

type Server struct {
	nodes map[string]*GraphNode
	edges map[string]*GraphEdge
}

func loadConfig(path string) (*Server, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	var cfg GraphConfig
	if err := json.NewDecoder(f).Decode(&cfg); err != nil {
		return nil, err
	}

	srv := &Server{
		nodes: make(map[string]*GraphNode, len(cfg.Nodes)),
		edges: make(map[string]*GraphEdge, len(cfg.Edges)),
	}
	for i := range cfg.Nodes {
		n := cfg.Nodes[i]
		srv.nodes[n.ID] = &n
	}
	for i := range cfg.Edges {
		e := cfg.Edges[i]
		srv.edges[e.ID] = &e
	}
	return srv, nil
}

func (s *Server) getGraph(projectID, env string) InfraGraph {
	g := InfraGraph{
		ProjectID:   projectID,
		Environment: env,
		Snapshot:    time.Now(),
	}
	for _, n := range s.nodes {
		if (projectID == "" || n.ProjectID == projectID) && (env == "" || n.Environment == env) {
			g.Nodes = append(g.Nodes, *n)
		}
	}
	nodeSet := make(map[string]bool, len(g.Nodes))
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

// blastRadius performs BFS over incoming edges to find all nodes that depend on nodeID.
func (s *Server) blastRadius(nodeID string) BlastRadiusResult {
	node, ok := s.nodes[nodeID]
	if !ok {
		return BlastRadiusResult{NodeID: nodeID}
	}

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

	var ids, names []string
	for id := range affected {
		ids = append(ids, id)
		if n, ok := s.nodes[id]; ok {
			names = append(names, n.Name)
		}
	}

	severity := "low"
	switch {
	case len(affected) > 5:
		severity = "critical"
	case len(affected) > 2:
		severity = "high"
	case len(affected) > 0:
		severity = "medium"
	}

	return BlastRadiusResult{
		NodeID:        nodeID,
		NodeName:      node.Name,
		AffectedIDs:   ids,
		AffectedNames: names,
		Severity:      severity,
	}
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}

func main() {
	configPath := getEnv("GRAPH_CONFIG", "./graph.json")
	port := getEnv("PORT", "8087")

	srv, err := loadConfig(configPath)
	if err != nil {
		log.Fatalf("[graph] failed to load config %s: %v", configPath, err)
	}
	log.Printf("[graph] loaded %d nodes, %d edges from %s", len(srv.nodes), len(srv.edges), configPath)

	mux := http.NewServeMux()

	// GET /api/v1/graph?project_id=X&env=production
	mux.HandleFunc("GET /api/v1/graph", func(w http.ResponseWriter, r *http.Request) {
		projectID := r.URL.Query().Get("project_id")
		env := r.URL.Query().Get("env")
		writeJSON(w, 200, srv.getGraph(projectID, env))
	})

	// GET /api/v1/graph/blast-radius/{nodeID}
	mux.HandleFunc("GET /api/v1/graph/blast-radius/{nodeID}", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, 200, srv.blastRadius(r.PathValue("nodeID")))
	})

	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, 200, map[string]any{
			"status":  "ok",
			"service": "graph",
			"nodes":   len(srv.nodes),
			"edges":   len(srv.edges),
		})
	})

	s := &http.Server{
		Addr:         ":" + port,
		Handler:      mux,
		ReadTimeout:  5 * time.Second,
		WriteTimeout: 10 * time.Second,
	}

	log.Printf("[graph-service] listening on :%s (config: %s)", port, configPath)
	if err := s.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Fatal(err)
	}
}

func getEnv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
