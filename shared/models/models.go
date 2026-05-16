package models

import (
	"time"
)

// ─── User & Auth ───────────────────────────────────────────────

type User struct {
	ID        string    `json:"id" db:"id"`
	Email     string    `json:"email" db:"email"`
	Name      string    `json:"name" db:"name"`
	CreatedAt time.Time `json:"created_at" db:"created_at"`
}

type Team struct {
	ID        string    `json:"id" db:"id"`
	Name      string    `json:"name" db:"name"`
	Plan      Plan      `json:"plan" db:"plan"`
	CreatedAt time.Time `json:"created_at" db:"created_at"`
}

type Plan string

const (
	PlanFree       Plan = "free"
	PlanSmallTeam  Plan = "small_team"
	PlanEnterprise Plan = "enterprise"
)

// ─── Project & Service ────────────────────────────────────────

type Project struct {
	ID          string    `json:"id" db:"id"`
	TeamID      string    `json:"team_id" db:"team_id"`
	Name        string    `json:"name" db:"name"`
	Description string    `json:"description" db:"description"`
	CreatedAt   time.Time `json:"created_at" db:"created_at"`
}

type Service struct {
	ID          string        `json:"id" db:"id"`
	ProjectID   string        `json:"project_id" db:"project_id"`
	Name        string        `json:"name" db:"name"`
	GitRepo     string        `json:"git_repo" db:"git_repo"`
	GitBranch   string        `json:"git_branch" db:"git_branch"`
	Template    string        `json:"template" db:"template"`
	Port        int           `json:"port" db:"port"`
	Replicas    int           `json:"replicas" db:"replicas"`
	Status      ServiceStatus `json:"status" db:"status"`
	CreatedAt   time.Time     `json:"created_at" db:"created_at"`
	UpdatedAt   time.Time     `json:"updated_at" db:"updated_at"`
}

type ServiceStatus string

const (
	ServiceStatusPending   ServiceStatus = "pending"
	ServiceStatusBuilding  ServiceStatus = "building"
	ServiceStatusDeploying ServiceStatus = "deploying"
	ServiceStatusRunning   ServiceStatus = "running"
	ServiceStatusFailed    ServiceStatus = "failed"
	ServiceStatusStopped   ServiceStatus = "stopped"
)

// ─── Deployment ───────────────────────────────────────────────

type Deployment struct {
	ID          string           `json:"id" db:"id"`
	ServiceID   string           `json:"service_id" db:"service_id"`
	GitCommit   string           `json:"git_commit" db:"git_commit"`
	GitMessage  string           `json:"git_message" db:"git_message"`
	ImageTag    string           `json:"image_tag" db:"image_tag"`
	Status      DeploymentStatus `json:"status" db:"status"`
	TriggeredBy string           `json:"triggered_by" db:"triggered_by"`
	StartedAt   time.Time        `json:"started_at" db:"started_at"`
	FinishedAt  *time.Time       `json:"finished_at,omitempty" db:"finished_at"`
}

type DeploymentStatus string

const (
	DeploymentStatusQueued    DeploymentStatus = "queued"
	DeploymentStatusBuilding  DeploymentStatus = "building"
	DeploymentStatusDeploying DeploymentStatus = "deploying"
	DeploymentStatusSuccess   DeploymentStatus = "success"
	DeploymentStatusFailed    DeploymentStatus = "failed"
	DeploymentStatusRolledBack DeploymentStatus = "rolled_back"
)

// ─── Environment ──────────────────────────────────────────────

type Environment struct {
	ID        string          `json:"id" db:"id"`
	ProjectID string          `json:"project_id" db:"project_id"`
	Name      string          `json:"name" db:"name"` // production, staging, preview-pr-42
	Type      EnvironmentType `json:"type" db:"type"`
	Domain    string          `json:"domain" db:"domain"`
	CreatedAt time.Time       `json:"created_at" db:"created_at"`
}

type EnvironmentType string

const (
	EnvProduction EnvironmentType = "production"
	EnvStaging    EnvironmentType = "staging"
	EnvPreview    EnvironmentType = "preview"
)

// ─── Secrets ──────────────────────────────────────────────────

type Secret struct {
	ID            string    `json:"id" db:"id"`
	ServiceID     string    `json:"service_id" db:"service_id"`
	EnvironmentID string    `json:"environment_id" db:"environment_id"`
	Key           string    `json:"key" db:"key"`
	ValueEncrypted []byte   `json:"-" db:"value_encrypted"`
	CreatedAt     time.Time `json:"created_at" db:"created_at"`
}

// ─── Infrastructure Graph ─────────────────────────────────────

type GraphNode struct {
	ID       string            `json:"id"`
	Type     NodeType          `json:"type"` // service, database, queue, cache
	Name     string            `json:"name"`
	Status   ServiceStatus     `json:"status"`
	Metadata map[string]string `json:"metadata"`
}

type NodeType string

const (
	NodeTypeService  NodeType = "service"
	NodeTypeDatabase NodeType = "database"
	NodeTypeQueue    NodeType = "queue"
	NodeTypeCache    NodeType = "cache"
	NodeTypeExternal NodeType = "external"
)

type GraphEdge struct {
	From     string   `json:"from"`
	To       string   `json:"to"`
	Protocol string   `json:"protocol"` // http, grpc, tcp, nats
	Traffic  int64    `json:"traffic"`  // requests/min
}

type InfraGraph struct {
	ProjectID string      `json:"project_id"`
	Nodes     []GraphNode `json:"nodes"`
	Edges     []GraphEdge `json:"edges"`
	Timestamp time.Time   `json:"timestamp"`
}

// ─── NATS Events ──────────────────────────────────────────────

type Event struct {
	Type    string      `json:"type"`
	Payload interface{} `json:"payload"`
}

const (
	EventDeployRequested  = "deploy.requested"
	EventBuildStarted     = "build.started"
	EventBuildCompleted   = "build.completed"
	EventBuildFailed      = "build.failed"
	EventDeployStarted    = "deploy.started"
	EventDeployCompleted  = "deploy.completed"
	EventDeployFailed     = "deploy.failed"
	EventServiceHealthy   = "service.healthy"
	EventServiceUnhealthy = "service.unhealthy"
)
