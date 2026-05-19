package main

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"sync"
	"time"

	"github.com/google/uuid"
	_ "github.com/lib/pq"
)

type Config struct {
	Port          string
	EncryptionKey string
	DatabaseURL   string
}

func loadConfig() Config {
	return Config{
		Port:          getEnv("PORT", "8086"),
		EncryptionKey: getEnv("ENCRYPTION_KEY", "12345678901234567890123456789012"),
		DatabaseURL:   getEnv("DATABASE_URL", ""),
	}
}

func getEnv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

// ─── Domain ───────────────────────────────────────────────────

type Secret struct {
	ID             string    `json:"id"`
	ServiceID      string    `json:"service_id"`
	EnvironmentID  string    `json:"environment_id"`
	Key            string    `json:"key"`
	ValueEncrypted string    `json:"-"` // never serialized
	CreatedAt      time.Time `json:"created_at"`
	UpdatedAt      time.Time `json:"updated_at"`
}

// ─── Crypto ───────────────────────────────────────────────────

type Encryptor struct {
	key []byte
}

func NewEncryptor(key string) (*Encryptor, error) {
	k := []byte(key)
	if len(k) != 32 {
		return nil, fmt.Errorf("encryption key must be exactly 32 bytes, got %d", len(k))
	}
	return &Encryptor{key: k}, nil
}

func (e *Encryptor) Encrypt(plaintext string) (string, error) {
	block, err := aes.NewCipher(e.key)
	if err != nil {
		return "", err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err = io.ReadFull(rand.Reader, nonce); err != nil {
		return "", err
	}
	ciphertext := gcm.Seal(nonce, nonce, []byte(plaintext), nil)
	return base64.StdEncoding.EncodeToString(ciphertext), nil
}

func (e *Encryptor) Decrypt(encoded string) (string, error) {
	ciphertext, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		return "", err
	}
	block, err := aes.NewCipher(e.key)
	if err != nil {
		return "", err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}
	if len(ciphertext) < gcm.NonceSize() {
		return "", fmt.Errorf("ciphertext too short")
	}
	nonce, ciphertext := ciphertext[:gcm.NonceSize()], ciphertext[gcm.NonceSize():]
	plaintext, err := gcm.Open(nil, nonce, ciphertext, nil)
	if err != nil {
		return "", err
	}
	return string(plaintext), nil
}

// ─── Store interface ──────────────────────────────────────────

type SecretStore interface {
	// Set creates or updates a secret; returns the stored secret.
	Set(serviceID, envID, key, encryptedValue string) (*Secret, error)
	// List returns all secrets for a service+env (ValueEncrypted is populated).
	List(serviceID, envID string) ([]*Secret, error)
	// GetByID returns the secret by ID (ValueEncrypted is populated), or nil if not found.
	GetByID(id string) (*Secret, error)
	// Delete removes a secret; returns true if it existed.
	Delete(id string) (bool, error)
}

// ─── In-memory store ──────────────────────────────────────────

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

// ─── PostgreSQL store ─────────────────────────────────────────

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

// ─── HTTP ─────────────────────────────────────────────────────

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}

type Handler struct {
	store SecretStore
	enc   *Encryptor
}

// PUT /api/v1/secrets — create or update a secret
func (h *Handler) Set(w http.ResponseWriter, r *http.Request) {
	var req struct {
		ServiceID     string `json:"service_id"`
		EnvironmentID string `json:"environment_id"`
		Key           string `json:"key"`
		Value         string `json:"value"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, 400, map[string]string{"error": "invalid request"})
		return
	}
	if req.Key == "" || req.Value == "" {
		writeJSON(w, 400, map[string]string{"error": "key and value are required"})
		return
	}
	encrypted, err := h.enc.Encrypt(req.Value)
	if err != nil {
		writeJSON(w, 500, map[string]string{"error": "encryption failed"})
		return
	}
	s, err := h.store.Set(req.ServiceID, req.EnvironmentID, req.Key, encrypted)
	if err != nil {
		log.Printf("[secrets] store.Set error: %v", err)
		writeJSON(w, 500, map[string]string{"error": "store failed"})
		return
	}
	writeJSON(w, 200, map[string]interface{}{
		"id": s.ID, "service_id": s.ServiceID, "environment_id": s.EnvironmentID,
		"key": s.Key, "updated_at": s.UpdatedAt,
	})
}

// GET /api/v1/secrets?service_id=X&env_id=Y — list keys (no values)
func (h *Handler) List(w http.ResponseWriter, r *http.Request) {
	serviceID := r.URL.Query().Get("service_id")
	envID := r.URL.Query().Get("env_id")
	secrets, err := h.store.List(serviceID, envID)
	if err != nil {
		log.Printf("[secrets] store.List error: %v", err)
		writeJSON(w, 500, map[string]string{"error": "store error"})
		return
	}
	type safeSecret struct {
		ID            string    `json:"id"`
		Key           string    `json:"key"`
		ServiceID     string    `json:"service_id"`
		EnvironmentID string    `json:"environment_id"`
		UpdatedAt     time.Time `json:"updated_at"`
	}
	out := make([]safeSecret, 0, len(secrets))
	for _, s := range secrets {
		out = append(out, safeSecret{ID: s.ID, Key: s.Key,
			ServiceID: s.ServiceID, EnvironmentID: s.EnvironmentID, UpdatedAt: s.UpdatedAt})
	}
	writeJSON(w, 200, out)
}

// GET /api/v1/secrets/{id}/value — decrypt and return value (internal only)
func (h *Handler) GetValue(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	s, err := h.store.GetByID(id)
	if err != nil {
		log.Printf("[secrets] store.GetByID error: %v", err)
		writeJSON(w, 500, map[string]string{"error": "store error"})
		return
	}
	if s == nil {
		writeJSON(w, 404, map[string]string{"error": "not found"})
		return
	}
	value, err := h.enc.Decrypt(s.ValueEncrypted)
	if err != nil {
		writeJSON(w, 500, map[string]string{"error": "decryption failed"})
		return
	}
	writeJSON(w, 200, map[string]string{"key": s.Key, "value": value})
}

// GET /api/v1/secrets/env-vars?service_id=X&env_id=Y — all secrets as KEY=VALUE map (internal only)
func (h *Handler) EnvVars(w http.ResponseWriter, r *http.Request) {
	serviceID := r.URL.Query().Get("service_id")
	envID := r.URL.Query().Get("env_id")
	secrets, err := h.store.List(serviceID, envID)
	if err != nil {
		log.Printf("[secrets] store.List error: %v", err)
		writeJSON(w, 500, map[string]string{"error": "store error"})
		return
	}
	envMap := make(map[string]string, len(secrets))
	for _, s := range secrets {
		value, err := h.enc.Decrypt(s.ValueEncrypted)
		if err != nil {
			log.Printf("[secrets] decrypt error for key %s: %v", s.Key, err)
			continue
		}
		envMap[s.Key] = value
	}
	writeJSON(w, 200, envMap)
}

// DELETE /api/v1/secrets/{id}
func (h *Handler) Delete(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	ok, err := h.store.Delete(id)
	if err != nil {
		log.Printf("[secrets] store.Delete error: %v", err)
		writeJSON(w, 500, map[string]string{"error": "store error"})
		return
	}
	if !ok {
		writeJSON(w, 404, map[string]string{"error": "not found"})
		return
	}
	writeJSON(w, 200, map[string]string{"status": "deleted"})
}

func main() {
	cfg := loadConfig()

	enc, err := NewEncryptor(cfg.EncryptionKey)
	if err != nil {
		log.Fatalf("invalid encryption key: %v", err)
	}

	var store SecretStore
	if cfg.DatabaseURL != "" {
		pg, err := NewPGSecretStore(cfg.DatabaseURL)
		if err != nil {
			log.Fatalf("postgres connect: %v", err)
		}
		store = pg
		log.Printf("[secrets-service] using PostgreSQL storage")
	} else {
		store = NewMemSecretStore()
		log.Printf("[secrets-service] using in-memory storage (set DATABASE_URL for persistence)")
	}

	h := &Handler{store: store, enc: enc}

	mux := http.NewServeMux()
	mux.HandleFunc("PUT /api/v1/secrets", h.Set)
	mux.HandleFunc("GET /api/v1/secrets", h.List)
	mux.HandleFunc("GET /api/v1/secrets/env-vars", h.EnvVars)
	mux.HandleFunc("GET /api/v1/secrets/{id}/value", h.GetValue)
	mux.HandleFunc("DELETE /api/v1/secrets/{id}", h.Delete)
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		backend := "memory"
		if cfg.DatabaseURL != "" {
			backend = "postgres"
		}
		writeJSON(w, 200, map[string]string{"status": "ok", "service": "secrets", "backend": backend})
	})

	srv := &http.Server{
		Addr:         ":" + cfg.Port,
		Handler:      mux,
		ReadTimeout:  5 * time.Second,
		WriteTimeout: 10 * time.Second,
	}

	log.Printf("[secrets-service] listening on :%s", cfg.Port)
	if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Fatal(err)
	}
}
