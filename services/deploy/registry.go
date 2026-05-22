package main

import (
	"database/sql"
	"fmt"
	"log"
	"strings"
	"sync"
	"time"

	_ "github.com/lib/pq"
)

type ServiceConfig struct {
	ID          string    `json:"id"`
	ProjectID   string    `json:"project_id"`
	Name        string    `json:"name"`
	GitRepo     string    `json:"git_repo"`
	GitBranch   string    `json:"git_branch"`
	Port        int       `json:"port"`
	Environment string    `json:"environment"`
	CreatedAt   time.Time `json:"created_at"`
}

type ServiceRegistry struct {
	mu   sync.RWMutex
	data map[string]*ServiceConfig
	db   *sql.DB
}

func NewServiceRegistry() *ServiceRegistry {
	return &ServiceRegistry{data: make(map[string]*ServiceConfig)}
}

func NewServiceRegistryPG(databaseURL string) (*ServiceRegistry, error) {
	db, err := sql.Open("postgres", databaseURL)
	if err != nil {
		return nil, fmt.Errorf("open db: %w", err)
	}
	if err := db.Ping(); err != nil {
		return nil, fmt.Errorf("ping db: %w", err)
	}
	r := &ServiceRegistry{data: make(map[string]*ServiceConfig), db: db}
	if err := r.migrate(); err != nil {
		return nil, fmt.Errorf("migrate: %w", err)
	}
	if err := r.loadFromDB(); err != nil {
		return nil, fmt.Errorf("load from db: %w", err)
	}
	return r, nil
}

const createServiceRegistryTable = `
CREATE TABLE IF NOT EXISTS service_registry (
    id          TEXT PRIMARY KEY,
    project_id  TEXT NOT NULL DEFAULT '',
    name        TEXT NOT NULL DEFAULT '',
    git_repo    TEXT NOT NULL,
    git_branch  TEXT NOT NULL DEFAULT 'main',
    port        INTEGER NOT NULL DEFAULT 8080,
    environment TEXT NOT NULL DEFAULT 'production',
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW()
)`

func (r *ServiceRegistry) migrate() error {
	_, err := r.db.Exec(createServiceRegistryTable)
	return err
}

func (r *ServiceRegistry) loadFromDB() error {
	rows, err := r.db.Query(
		`SELECT id, project_id, name, git_repo, git_branch, port, environment, created_at FROM service_registry`)
	if err != nil {
		return err
	}
	defer rows.Close()

	for rows.Next() {
		var s ServiceConfig
		if err := rows.Scan(&s.ID, &s.ProjectID, &s.Name, &s.GitRepo, &s.GitBranch, &s.Port, &s.Environment, &s.CreatedAt); err != nil {
			return err
		}
		r.data[s.ID] = &s
	}
	log.Printf("[deploy] loaded %d services from postgres", len(r.data))
	return rows.Err()
}

func (r *ServiceRegistry) Save(s *ServiceConfig) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.data[s.ID] = s
	if r.db != nil {
		_, err := r.db.Exec(`
			INSERT INTO service_registry (id, project_id, name, git_repo, git_branch, port, environment, created_at)
			VALUES ($1,$2,$3,$4,$5,$6,$7,$8)
			ON CONFLICT (id) DO UPDATE SET
				project_id=$2, name=$3, git_repo=$4, git_branch=$5, port=$6, environment=$7`,
			s.ID, s.ProjectID, s.Name, s.GitRepo, s.GitBranch, s.Port, s.Environment, s.CreatedAt)
		if err != nil {
			log.Printf("[deploy] warn: failed to persist service %s: %v", s.ID, err)
		}
	}
}

func (r *ServiceRegistry) Get(id string) (*ServiceConfig, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	s, ok := r.data[id]
	return s, ok
}

func (r *ServiceRegistry) Delete(id string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.data, id)
	if r.db != nil {
		if _, err := r.db.Exec(`DELETE FROM service_registry WHERE id=$1`, id); err != nil {
			log.Printf("[deploy] warn: failed to delete service %s from db: %v", id, err)
		}
	}
}

func (r *ServiceRegistry) List() []*ServiceConfig {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]*ServiceConfig, 0, len(r.data))
	for _, s := range r.data {
		out = append(out, s)
	}
	return out
}

func (r *ServiceRegistry) FindByRepo(repoURL, branch string) (*ServiceConfig, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	normalized := normalizeRepoURL(repoURL)
	for _, s := range r.data {
		if normalizeRepoURL(s.GitRepo) == normalized && s.GitBranch == branch {
			return s, true
		}
	}
	return nil, false
}

func normalizeRepoURL(u string) string {
	u = strings.TrimSuffix(u, ".git")
	u = strings.Replace(u, "git@github.com:", "github.com/", 1)
	u = strings.TrimPrefix(u, "https://")
	u = strings.TrimPrefix(u, "http://")
	return u
}
