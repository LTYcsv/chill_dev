package main

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"sort"
	"sync"
	"time"

	_ "github.com/lib/pq"
	"github.com/nats-io/nats.go"
)

// ─── Domain Types ─────────────────────────────────────────────

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

type AffectedNode struct {
	ID    string `json:"id"`
	Name  string `json:"name"`
	Type  string `json:"type"`
	Depth int    `json:"depth"`
}

type BlastRadiusResult struct {
	NodeID        string         `json:"node_id"`
	NodeName      string         `json:"node_name"`
	AffectedNodes []AffectedNode `json:"affected_nodes"`
	AffectedIDs   []string       `json:"affected_ids"`
	AffectedNames []string       `json:"affected_names"`
	TotalAffected int            `json:"total_affected"`
	Severity      string         `json:"severity"`
}

type GraphConfig struct {
	Nodes []GraphNode `json:"nodes"`
	Edges []GraphEdge `json:"edges"`
}

// ─── Time Travel Types ────────────────────────────────────────

type DiffOp string

const (
	DiffUpsertNode DiffOp = "upsert_node"
	DiffDeleteNode DiffOp = "delete_node"
	DiffUpsertEdge DiffOp = "upsert_edge"
	DiffDeleteEdge DiffOp = "delete_edge"
)

type GraphDiff struct {
	Op     DiffOp     `json:"op"`
	Node   *GraphNode `json:"node,omitempty"`
	NodeID string     `json:"node_id,omitempty"`
	Edge   *GraphEdge `json:"edge,omitempty"`
	EdgeID string     `json:"edge_id,omitempty"`
}

type DiffRecord struct {
	ID        int64     `json:"id"`
	ProjectID string    `json:"project_id"`
	Env       string    `json:"env"`
	Seq       int64     `json:"seq"`
	Source    string    `json:"source"`
	Diff      GraphDiff `json:"diff"`
	CreatedAt time.Time `json:"created_at"`
}

// ─── TTStore ──────────────────────────────────────────────────

const migrationSQL = `
CREATE TABLE IF NOT EXISTS tt_checkpoints (
    id         BIGSERIAL PRIMARY KEY,
    project_id TEXT NOT NULL,
    env        TEXT NOT NULL,
    seq        BIGINT NOT NULL,
    graph_json JSONB NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE TABLE IF NOT EXISTS tt_diffs (
    id         BIGSERIAL PRIMARY KEY,
    project_id TEXT NOT NULL,
    env        TEXT NOT NULL,
    seq        BIGINT NOT NULL,
    source     TEXT NOT NULL,
    patch_json JSONB NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX IF NOT EXISTS idx_tt_diffs_lookup ON tt_diffs (project_id, env, seq);
CREATE INDEX IF NOT EXISTS idx_tt_checkpoints_lookup ON tt_checkpoints (project_id, env, created_at);
`

type TTStore struct {
	db *sql.DB
}

func newTTStore(databaseURL string) (*TTStore, error) {
	db, err := sql.Open("postgres", databaseURL)
	if err != nil {
		return nil, err
	}
	if err := db.Ping(); err != nil {
		return nil, err
	}
	if _, err := db.Exec(migrationSQL); err != nil {
		return nil, err
	}
	return &TTStore{db: db}, nil
}

func (t *TTStore) saveCheckpoint(projectID, env string, seq int64, g InfraGraph) {
	data, err := json.Marshal(g)
	if err != nil {
		log.Printf("[graph] checkpoint marshal: %v", err)
		return
	}
	if _, err := t.db.Exec(
		`INSERT INTO tt_checkpoints (project_id, env, seq, graph_json) VALUES ($1,$2,$3,$4)`,
		projectID, env, seq, data,
	); err != nil {
		log.Printf("[graph] checkpoint save: %v", err)
	}
}

func (t *TTStore) saveDiff(projectID, env string, seq int64, source string, diff GraphDiff) {
	data, _ := json.Marshal(diff)
	if _, err := t.db.Exec(
		`INSERT INTO tt_diffs (project_id, env, seq, source, patch_json) VALUES ($1,$2,$3,$4,$5)`,
		projectID, env, seq, source, data,
	); err != nil {
		log.Printf("[graph] diff save: %v", err)
	}
}

type checkpointRow struct {
	Seq       int64
	GraphJSON []byte
}

func (t *TTStore) getCheckpointBefore(projectID, env string, at time.Time) (*checkpointRow, error) {
	var cp checkpointRow
	err := t.db.QueryRow(
		`SELECT seq, graph_json FROM tt_checkpoints
		 WHERE project_id=$1 AND env=$2 AND created_at <= $3
		 ORDER BY created_at DESC LIMIT 1`,
		projectID, env, at,
	).Scan(&cp.Seq, &cp.GraphJSON)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &cp, nil
}

func (t *TTStore) getDiffsAfter(projectID, env string, afterSeq int64, at time.Time) ([]DiffRecord, error) {
	rows, err := t.db.Query(
		`SELECT id, seq, source, patch_json, created_at FROM tt_diffs
		 WHERE project_id=$1 AND env=$2 AND seq > $3 AND created_at <= $4
		 ORDER BY seq ASC`,
		projectID, env, afterSeq, at,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanDiffRows(rows, projectID, env)
}

func (t *TTStore) listDiffs(projectID, env string, limit int) ([]DiffRecord, error) {
	rows, err := t.db.Query(
		`SELECT id, seq, source, patch_json, created_at FROM tt_diffs
		 WHERE project_id=$1 AND env=$2
		 ORDER BY seq DESC LIMIT $3`,
		projectID, env, limit,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanDiffRows(rows, projectID, env)
}

func scanDiffRows(rows *sql.Rows, projectID, env string) ([]DiffRecord, error) {
	var out []DiffRecord
	for rows.Next() {
		var r DiffRecord
		var patchJSON []byte
		if err := rows.Scan(&r.ID, &r.Seq, &r.Source, &patchJSON, &r.CreatedAt); err != nil {
			return nil, err
		}
		if err := json.Unmarshal(patchJSON, &r.Diff); err != nil {
			return nil, err
		}
		r.ProjectID = projectID
		r.Env = env
		out = append(out, r)
	}
	return out, rows.Err()
}

func (t *TTStore) maxSeqPerCombo() map[string]int64 {
	out := make(map[string]int64)
	rows, err := t.db.Query(`SELECT project_id, env, MAX(seq) FROM tt_diffs GROUP BY project_id, env`)
	if err != nil {
		return out
	}
	defer rows.Close()
	for rows.Next() {
		var projectID, env string
		var maxSeq int64
		if rows.Scan(&projectID, &env, &maxSeq) == nil {
			out[projectID+":"+env] = maxSeq
		}
	}
	return out
}

// ─── Server ───────────────────────────────────────────────────

type Server struct {
	mu    sync.RWMutex
	nodes map[string]*GraphNode
	edges map[string]*GraphEdge
	store *TTStore
	seqMu sync.Mutex
	seqs  map[string]int64 // "projectID:env" → last diff seq
}

func newServer(nodes map[string]*GraphNode, edges map[string]*GraphEdge, store *TTStore) *Server {
	s := &Server{
		nodes: nodes,
		edges: edges,
		store: store,
		seqs:  make(map[string]int64),
	}
	if store != nil {
		for k, v := range store.maxSeqPerCombo() {
			s.seqs[k] = v
		}
	}
	return s
}

func (s *Server) nextSeq(projectID, env string) int64 {
	s.seqMu.Lock()
	defer s.seqMu.Unlock()
	key := projectID + ":" + env
	s.seqs[key]++
	return s.seqs[key]
}

func (s *Server) record(projectID, env, source string, diff GraphDiff) {
	if s.store == nil {
		return
	}
	seq := s.nextSeq(projectID, env)
	s.store.saveDiff(projectID, env, seq, source, diff)
	if seq%100 == 0 {
		g := s.getGraph(projectID, env)
		s.store.saveCheckpoint(projectID, env, seq, g)
		log.Printf("[graph] checkpoint at seq=%d for %s/%s", seq, projectID, env)
	}
}

func (s *Server) saveInitialCheckpoints() {
	if s.store == nil {
		return
	}
	combos := make(map[string][2]string)
	s.mu.RLock()
	for _, n := range s.nodes {
		key := n.ProjectID + ":" + n.Environment
		combos[key] = [2]string{n.ProjectID, n.Environment}
	}
	s.mu.RUnlock()

	for _, combo := range combos {
		projectID, env := combo[0], combo[1]
		cp, _ := s.store.getCheckpointBefore(projectID, env, time.Now())
		if cp == nil {
			g := s.getGraph(projectID, env)
			s.store.saveCheckpoint(projectID, env, 0, g)
			log.Printf("[graph] initial checkpoint saved for %s/%s", projectID, env)
		}
	}
}

// startDailyCheckpoint saves a checkpoint for every known project/env once per day.
func (s *Server) startDailyCheckpoint() {
	go func() {
		ticker := time.NewTicker(24 * time.Hour)
		for range ticker.C {
			if s.store == nil {
				continue
			}
			combos := make(map[string][2]string)
			s.mu.RLock()
			for _, n := range s.nodes {
				key := n.ProjectID + ":" + n.Environment
				combos[key] = [2]string{n.ProjectID, n.Environment}
			}
			s.mu.RUnlock()
			s.seqMu.Lock()
			for _, combo := range combos {
				projectID, env := combo[0], combo[1]
				seq := s.seqs[projectID+":"+env]
				g := s.getGraph(projectID, env)
				s.store.saveCheckpoint(projectID, env, seq, g)
				log.Printf("[graph] daily checkpoint for %s/%s", projectID, env)
			}
			s.seqMu.Unlock()
		}
	}()
}

// ─── Graph Mutations ──────────────────────────────────────────

func (s *Server) UpsertNode(n GraphNode, source string) {
	s.mu.Lock()
	s.nodes[n.ID] = &n
	s.mu.Unlock()
	s.record(n.ProjectID, n.Environment, source, GraphDiff{Op: DiffUpsertNode, Node: &n})
}

func (s *Server) DeleteNode(id, projectID, env, source string) {
	s.mu.Lock()
	delete(s.nodes, id)
	s.mu.Unlock()
	s.record(projectID, env, source, GraphDiff{Op: DiffDeleteNode, NodeID: id})
}

func (s *Server) UpsertEdge(e GraphEdge, projectID, env, source string) {
	s.mu.Lock()
	s.edges[e.ID] = &e
	s.mu.Unlock()
	s.record(projectID, env, source, GraphDiff{Op: DiffUpsertEdge, Edge: &e})
}

func (s *Server) DeleteEdge(id, projectID, env, source string) {
	s.mu.Lock()
	delete(s.edges, id)
	s.mu.Unlock()
	s.record(projectID, env, source, GraphDiff{Op: DiffDeleteEdge, EdgeID: id})
}

func (s *Server) handleNATSEvent(serviceID string, status NodeStatus, source string) {
	s.mu.Lock()
	var snap GraphNode
	var found bool
	for _, n := range s.nodes {
		if n.ID == serviceID || n.Name == serviceID ||
			(n.Metadata != nil && n.Metadata["service_id"] == serviceID) {
			n.Status = status
			snap = *n
			found = true
			break
		}
	}
	s.mu.Unlock()
	if found {
		s.record(snap.ProjectID, snap.Environment, source, GraphDiff{Op: DiffUpsertNode, Node: &snap})
	}
}

func (s *Server) subscribeToNATS(nc *nats.Conn) {
	nc.Subscribe("runtime.deployed", func(m *nats.Msg) {
		var p map[string]string
		if json.Unmarshal(m.Data, &p) == nil {
			s.handleNATSEvent(p["service_id"], NodeRunning, "runtime.deployed")
		}
	})
	nc.Subscribe("deploy.failed", func(m *nats.Msg) {
		var p map[string]string
		if json.Unmarshal(m.Data, &p) == nil {
			s.handleNATSEvent(p["service_id"], NodeDegraded, "deploy.failed")
		}
	})
}

// ─── Time Travel Recovery ─────────────────────────────────────

func applyDiff(nodes map[string]*GraphNode, edges map[string]*GraphEdge, diff GraphDiff) {
	switch diff.Op {
	case DiffUpsertNode:
		if diff.Node != nil {
			n := *diff.Node
			nodes[n.ID] = &n
		}
	case DiffDeleteNode:
		delete(nodes, diff.NodeID)
	case DiffUpsertEdge:
		if diff.Edge != nil {
			e := *diff.Edge
			edges[e.ID] = &e
		}
	case DiffDeleteEdge:
		delete(edges, diff.EdgeID)
	}
}

func (s *Server) getGraphAt(projectID, env string, at time.Time) (InfraGraph, error) {
	if s.store == nil {
		return s.getGraph(projectID, env), nil
	}

	cp, err := s.store.getCheckpointBefore(projectID, env, at)
	if err != nil {
		return InfraGraph{}, err
	}

	nodes := make(map[string]*GraphNode)
	edges := make(map[string]*GraphEdge)
	var baseSeq int64

	if cp != nil {
		var base InfraGraph
		if err := json.Unmarshal(cp.GraphJSON, &base); err != nil {
			return InfraGraph{}, err
		}
		for i := range base.Nodes {
			n := base.Nodes[i]
			nodes[n.ID] = &n
		}
		for i := range base.Edges {
			e := base.Edges[i]
			edges[e.ID] = &e
		}
		baseSeq = cp.Seq
	}

	diffs, err := s.store.getDiffsAfter(projectID, env, baseSeq, at)
	if err != nil {
		return InfraGraph{}, err
	}
	for _, d := range diffs {
		applyDiff(nodes, edges, d.Diff)
	}

	g := buildGraph(projectID, env, nodes, edges, at)
	return g, nil
}

// ─── Graph Queries ────────────────────────────────────────────

func (s *Server) getGraph(projectID, env string) InfraGraph {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return buildGraph(projectID, env, s.nodes, s.edges, time.Now())
}

func buildGraph(projectID, env string, nodes map[string]*GraphNode, edges map[string]*GraphEdge, snapshot time.Time) InfraGraph {
	g := InfraGraph{
		ProjectID:   projectID,
		Environment: env,
		Snapshot:    snapshot,
	}
	for _, n := range nodes {
		if (projectID == "" || n.ProjectID == projectID) && (env == "" || n.Environment == env) {
			g.Nodes = append(g.Nodes, *n)
		}
	}
	nodeSet := make(map[string]bool, len(g.Nodes))
	for _, n := range g.Nodes {
		nodeSet[n.ID] = true
	}
	for _, e := range edges {
		if nodeSet[e.From] && nodeSet[e.To] {
			g.Edges = append(g.Edges, *e)
		}
	}
	return g
}

func (s *Server) findNode(idOrName string) *GraphNode {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if n, ok := s.nodes[idOrName]; ok {
		return n
	}
	for _, n := range s.nodes {
		if n.Name == idOrName {
			return n
		}
	}
	return nil
}

func (s *Server) blastRadius(idOrName string) BlastRadiusResult {
	node := s.findNode(idOrName)
	if node == nil {
		return BlastRadiusResult{NodeID: idOrName, Severity: "low"}
	}

	type entry struct {
		id    string
		depth int
	}

	s.mu.RLock()
	defer s.mu.RUnlock()

	visited := map[string]int{node.ID: 0}
	queue := []entry{{node.ID, 0}}
	var affected []AffectedNode

	for len(queue) > 0 {
		cur := queue[0]
		queue = queue[1:]
		for _, e := range s.edges {
			if e.To != cur.id {
				continue
			}
			if _, seen := visited[e.From]; seen {
				continue
			}
			depth := cur.depth + 1
			visited[e.From] = depth
			queue = append(queue, entry{e.From, depth})
			a := AffectedNode{ID: e.From, Depth: depth}
			if n, ok := s.nodes[e.From]; ok {
				a.Name = n.Name
				a.Type = string(n.Type)
			}
			affected = append(affected, a)
		}
	}

	sort.Slice(affected, func(i, j int) bool {
		if affected[i].Depth != affected[j].Depth {
			return affected[i].Depth < affected[j].Depth
		}
		return affected[i].Name < affected[j].Name
	})

	ids := make([]string, len(affected))
	names := make([]string, len(affected))
	for i, a := range affected {
		ids[i] = a.ID
		names[i] = a.Name
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
		NodeID:        node.ID,
		NodeName:      node.Name,
		AffectedNodes: affected,
		AffectedIDs:   ids,
		AffectedNames: names,
		TotalAffected: len(affected),
		Severity:      severity,
	}
}

// ─── Config Loader ────────────────────────────────────────────

func loadConfig(path string) (map[string]*GraphNode, map[string]*GraphEdge, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, nil, err
	}
	defer f.Close()

	var cfg GraphConfig
	if err := json.NewDecoder(f).Decode(&cfg); err != nil {
		return nil, nil, err
	}

	nodes := make(map[string]*GraphNode, len(cfg.Nodes))
	edges := make(map[string]*GraphEdge, len(cfg.Edges))
	for i := range cfg.Nodes {
		n := cfg.Nodes[i]
		nodes[n.ID] = &n
	}
	for i := range cfg.Edges {
		e := cfg.Edges[i]
		edges[e.ID] = &e
	}
	return nodes, edges, nil
}

// ─── HTTP ─────────────────────────────────────────────────────

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}

func main() {
	configPath := getEnv("GRAPH_CONFIG", "./graph.json")
	port := getEnv("PORT", "8087")
	databaseURL := getEnv("DATABASE_URL", "")
	natsURL := getEnv("NATS_URL", "nats://localhost:4222")

	nodes, edges, err := loadConfig(configPath)
	if err != nil {
		log.Fatalf("[graph] failed to load config %s: %v", configPath, err)
	}
	log.Printf("[graph] loaded %d nodes, %d edges from %s", len(nodes), len(edges), configPath)

	var store *TTStore
	if databaseURL != "" {
		store, err = newTTStore(databaseURL)
		if err != nil {
			log.Printf("[graph] warn: postgres unavailable (%v), running without time travel", err)
		} else {
			log.Printf("[graph] time travel store connected")
		}
	}

	srv := newServer(nodes, edges, store)
	srv.saveInitialCheckpoints()
	srv.startDailyCheckpoint()

	nc, err := nats.Connect(natsURL, nats.MaxReconnects(5))
	if err != nil {
		log.Printf("[graph] warn: NATS unavailable (%v), running without live updates", err)
	} else {
		srv.subscribeToNATS(nc)
		log.Printf("[graph] subscribed to NATS events")
	}

	mux := http.NewServeMux()

	// GET /api/v1/graph?project_id=X&env=Y[&at=RFC3339]
	mux.HandleFunc("GET /api/v1/graph", func(w http.ResponseWriter, r *http.Request) {
		projectID := r.URL.Query().Get("project_id")
		env := r.URL.Query().Get("env")
		atStr := r.URL.Query().Get("at")

		if atStr != "" {
			at, err := time.Parse(time.RFC3339, atStr)
			if err != nil {
				writeJSON(w, 400, map[string]string{"error": "invalid 'at' format, use RFC3339"})
				return
			}
			g, err := srv.getGraphAt(projectID, env, at)
			if err != nil {
				writeJSON(w, 500, map[string]string{"error": err.Error()})
				return
			}
			writeJSON(w, 200, g)
			return
		}
		writeJSON(w, 200, srv.getGraph(projectID, env))
	})

	// GET /api/v1/graph/timeline?project_id=X&env=Y[&limit=N]
	mux.HandleFunc("GET /api/v1/graph/timeline", func(w http.ResponseWriter, r *http.Request) {
		if srv.store == nil {
			writeJSON(w, 503, map[string]string{"error": "time travel requires DATABASE_URL"})
			return
		}
		projectID := r.URL.Query().Get("project_id")
		env := r.URL.Query().Get("env")
		limit := 50
		if l := r.URL.Query().Get("limit"); l != "" {
			fmt.Sscanf(l, "%d", &limit)
		}
		if limit > 500 {
			limit = 500
		}
		diffs, err := srv.store.listDiffs(projectID, env, limit)
		if err != nil {
			writeJSON(w, 500, map[string]string{"error": err.Error()})
			return
		}
		if diffs == nil {
			diffs = []DiffRecord{}
		}
		writeJSON(w, 200, diffs)
	})

	// POST /api/v1/graph/nodes — upsert a node
	mux.HandleFunc("POST /api/v1/graph/nodes", func(w http.ResponseWriter, r *http.Request) {
		var n GraphNode
		if err := json.NewDecoder(r.Body).Decode(&n); err != nil {
			writeJSON(w, 400, map[string]string{"error": "invalid request"})
			return
		}
		if n.ID == "" {
			writeJSON(w, 400, map[string]string{"error": "id is required"})
			return
		}
		srv.UpsertNode(n, "api")
		writeJSON(w, 200, n)
	})

	// DELETE /api/v1/graph/nodes/{id}?project_id=X&env=Y
	mux.HandleFunc("DELETE /api/v1/graph/nodes/{id}", func(w http.ResponseWriter, r *http.Request) {
		id := r.PathValue("id")
		projectID := r.URL.Query().Get("project_id")
		env := r.URL.Query().Get("env")
		srv.DeleteNode(id, projectID, env, "api")
		writeJSON(w, 200, map[string]string{"status": "deleted"})
	})

	// POST /api/v1/graph/edges — upsert an edge
	mux.HandleFunc("POST /api/v1/graph/edges", func(w http.ResponseWriter, r *http.Request) {
		var e GraphEdge
		if err := json.NewDecoder(r.Body).Decode(&e); err != nil {
			writeJSON(w, 400, map[string]string{"error": "invalid request"})
			return
		}
		if e.ID == "" || e.From == "" || e.To == "" {
			writeJSON(w, 400, map[string]string{"error": "id, from, and to are required"})
			return
		}
		projectID := r.URL.Query().Get("project_id")
		env := r.URL.Query().Get("env")
		srv.UpsertEdge(e, projectID, env, "api")
		writeJSON(w, 200, e)
	})

	// DELETE /api/v1/graph/edges/{id}?project_id=X&env=Y
	mux.HandleFunc("DELETE /api/v1/graph/edges/{id}", func(w http.ResponseWriter, r *http.Request) {
		id := r.PathValue("id")
		projectID := r.URL.Query().Get("project_id")
		env := r.URL.Query().Get("env")
		srv.DeleteEdge(id, projectID, env, "api")
		writeJSON(w, 200, map[string]string{"status": "deleted"})
	})

	// GET /api/v1/graph/blast-radius/{nodeID}
	mux.HandleFunc("GET /api/v1/graph/blast-radius/{nodeID}", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, 200, srv.blastRadius(r.PathValue("nodeID")))
	})

	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		dbStatus := "disabled"
		if srv.store != nil {
			if err := srv.store.db.Ping(); err == nil {
				dbStatus = "connected"
			} else {
				dbStatus = "disconnected"
			}
		}
		srv.mu.RLock()
		nodeCount := len(srv.nodes)
		srv.mu.RUnlock()
		writeJSON(w, 200, map[string]any{
			"status":        "ok",
			"service":       "graph",
			"nodes":         nodeCount,
			"time_travel":   dbStatus,
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

