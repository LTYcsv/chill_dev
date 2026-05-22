package main

import "time"

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
