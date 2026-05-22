package main

import (
	"database/sql"
	"fmt"
	"sync"
	"time"

	"github.com/google/uuid"
	_ "github.com/lib/pq"
)

type SecretStore interface {
	Set(serviceID, envID, key, encryptedValue string) (*Secret, error)
	List(serviceID, envID string) ([]*Secret, error)
	GetByID(id string) (*Secret, error)
	Delete(id string) (bool, error)
}

type MemSecretStore struct {
	mu   sync.RWMutex
	data map[string]*Secret
}

func NewMemSecretStore() *MemSecretStore {
	return &MemSecretStore{data: make(map[string]*Secret)}
}

func (r *MemSecretStore) Set(serviceID, envID, key, encryptedValue string) (*Secret, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, s := range r.data {
		if s.ServiceID == serviceID && s.EnvironmentID == envID && s.Key == key {
			s.ValueEncrypted = encryptedValue
			s.UpdatedAt = time.Now()
			return s, nil
		}
	}
	s := &Secret{
		ID:             uuid.NewString(),
		ServiceID:      serviceID,
		EnvironmentID:  envID,
		Key:            key,
		ValueEncrypted: encryptedValue,
		CreatedAt:      time.Now(),
		UpdatedAt:      time.Now(),
	}
	r.data[s.ID] = s
	return s, nil
}

func (r *MemSecretStore) List(serviceID, envID string) ([]*Secret, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	var out []*Secret
	for _, s := range r.data {
		if s.ServiceID == serviceID && s.EnvironmentID == envID {
			out = append(out, s)
		}
	}
	return out, nil
}

func (r *MemSecretStore) GetByID(id string) (*Secret, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	s, ok := r.data[id]
	if !ok {
		return nil, nil
	}
	return s, nil
}

func (r *MemSecretStore) Delete(id string) (bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	_, ok := r.data[id]
	delete(r.data, id)
	return ok, nil
}

type PGSecretStore struct {
	db *sql.DB
}

func NewPGSecretStore(databaseURL string) (*PGSecretStore, error) {
	db, err := sql.Open("postgres", databaseURL)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(10)
	db.SetMaxIdleConns(3)
	if err := db.Ping(); err != nil {
		return nil, fmt.Errorf("postgres ping: %w", err)
	}
	return &PGSecretStore{db: db}, nil
}

func (r *PGSecretStore) Set(serviceID, envID, key, encryptedValue string) (*Secret, error) {
	const q = `
		INSERT INTO secrets (service_id, environment_id, key, value_encrypted)
		VALUES ($1, $2, $3, $4)
		ON CONFLICT (service_id, environment_id, key)
		DO UPDATE SET value_encrypted = EXCLUDED.value_encrypted, updated_at = NOW()
		RETURNING id, created_at, updated_at`
	s := &Secret{ServiceID: serviceID, EnvironmentID: envID, Key: key, ValueEncrypted: encryptedValue}
	err := r.db.QueryRow(q, serviceID, envID, key, encryptedValue).
		Scan(&s.ID, &s.CreatedAt, &s.UpdatedAt)
	return s, err
}

func (r *PGSecretStore) List(serviceID, envID string) ([]*Secret, error) {
	const q = `SELECT id, key, service_id, environment_id, value_encrypted, created_at, updated_at
	           FROM secrets WHERE service_id = $1 AND environment_id = $2`
	rows, err := r.db.Query(q, serviceID, envID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*Secret
	for rows.Next() {
		s := &Secret{}
		if err := rows.Scan(&s.ID, &s.Key, &s.ServiceID, &s.EnvironmentID,
			&s.ValueEncrypted, &s.CreatedAt, &s.UpdatedAt); err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

func (r *PGSecretStore) GetByID(id string) (*Secret, error) {
	const q = `SELECT id, key, service_id, environment_id, value_encrypted, created_at, updated_at
	           FROM secrets WHERE id = $1`
	s := &Secret{}
	err := r.db.QueryRow(q, id).Scan(&s.ID, &s.Key, &s.ServiceID, &s.EnvironmentID,
		&s.ValueEncrypted, &s.CreatedAt, &s.UpdatedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	return s, err
}

func (r *PGSecretStore) Delete(id string) (bool, error) {
	res, err := r.db.Exec(`DELETE FROM secrets WHERE id = $1`, id)
	if err != nil {
		return false, err
	}
	n, _ := res.RowsAffected()
	return n > 0, nil
}
