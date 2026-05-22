package main

import (
	"fmt"
	"sync"
	"time"
)

type DeploymentRepo struct {
	mu   sync.RWMutex
	data map[string]*Deployment
}

func NewDeploymentRepo() *DeploymentRepo {
	return &DeploymentRepo{data: make(map[string]*Deployment)}
}

func (r *DeploymentRepo) Save(d *Deployment) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.data[d.ID] = d
}

func (r *DeploymentRepo) Get(id string) (*Deployment, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	d, ok := r.data[id]
	return d, ok
}

func (r *DeploymentRepo) ListByService(serviceID string) []*Deployment {
	r.mu.RLock()
	defer r.mu.RUnlock()
	var out []*Deployment
	for _, d := range r.data {
		if serviceID == "" || d.ServiceID == serviceID {
			out = append(out, d)
		}
	}
	return out
}

func (r *DeploymentRepo) UpdateStatus(id string, status DeploymentStatus, logLine string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	d, ok := r.data[id]
	if !ok {
		return
	}
	d.Status = status
	if logLine != "" {
		d.Logs = append(d.Logs, fmt.Sprintf("[%s] %s", time.Now().Format(time.RFC3339), logLine))
	}
	if status == StatusSuccess || status == StatusFailed || status == StatusRolledBack {
		t := time.Now()
		d.FinishedAt = &t
	}
}
