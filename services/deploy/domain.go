package main

import "time"

type DeploymentStatus string

const (
	StatusQueued     DeploymentStatus = "queued"
	StatusBuilding   DeploymentStatus = "building"
	StatusDeploying  DeploymentStatus = "deploying"
	StatusSuccess    DeploymentStatus = "success"
	StatusFailed     DeploymentStatus = "failed"
	StatusRolledBack DeploymentStatus = "rolled_back"
)

type Deployment struct {
	ID          string           `json:"id"`
	ServiceID   string           `json:"service_id"`
	ProjectID   string           `json:"project_id"`
	GitRepo     string           `json:"git_repo"`
	GitBranch   string           `json:"git_branch"`
	GitCommit   string           `json:"git_commit"`
	Environment string           `json:"environment"`
	Status      DeploymentStatus `json:"status"`
	TriggeredBy string           `json:"triggered_by"`
	Logs        []string         `json:"logs"`
	StartedAt   time.Time        `json:"started_at"`
	FinishedAt  *time.Time       `json:"finished_at,omitempty"`
}

type DeployRequest struct {
	ServiceID   string `json:"service_id"`
	ProjectID   string `json:"project_id"`
	GitRepo     string `json:"git_repo"`
	GitBranch   string `json:"git_branch"`
	GitCommit   string `json:"git_commit"`
	Environment string `json:"environment"`
	TriggeredBy string `json:"triggered_by"`
}
