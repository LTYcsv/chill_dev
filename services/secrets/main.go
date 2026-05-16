package main

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
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
)

type Config struct {
	Port          string
	EncryptionKey string // 32-byte hex key for AES-256-GCM
}

func loadConfig() Config {
	return Config{
		Port:          getEnv("PORT", "8086"),
		EncryptionKey: getEnv("ENCRYPTION_KEY", "12345678901234567890123456789012"), // 32 chars
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
	ValueEncrypted string    `json:"-"` // never serialize value
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

// ─── Repository ───────────────────────────────────────────────

type SecretRepo struct {
	mu   sync.RWMutex
	data map[string]*Secret // id -> secret
}

func NewSecretRepo() *SecretRepo {
	return &SecretRepo{data: make(map[string]*Secret)}
}

func (r *SecretRepo) Set(serviceID, envID, key, encryptedValue string) *Secret {
	r.mu.Lock()
	defer r.mu.Unlock()

	// Look for existing secret with same service+env+key
	for _, s := range r.data {
		if s.ServiceID == serviceID && s.EnvironmentID == envID && s.Key == key {
			s.ValueEncrypted = encryptedValue
			s.UpdatedAt = time.Now()
			return s
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
	return s
}

func (r *SecretRepo) List(serviceID, envID string) []*Secret {
	r.mu.RLock()
	defer r.mu.RUnlock()
	var out []*Secret
	for _, s := range r.data {
		if s.ServiceID == serviceID && s.EnvironmentID == envID {
			out = append(out, s)
		}
	}
	return out
}

func (r *SecretRepo) GetByID(id string) (*Secret, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	s, ok := r.data[id]
	return s, ok
}

func (r *SecretRepo) Delete(id string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	_, ok := r.data[id]
	delete(r.data, id)
	return ok
}

// ─── HTTP ─────────────────────────────────────────────────────

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}

type Handler struct {
	repo *SecretRepo
	enc  *Encryptor
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

	s := h.repo.Set(req.ServiceID, req.EnvironmentID, req.Key, encrypted)
	writeJSON(w, 200, map[string]interface{}{
		"id":             s.ID,
		"service_id":     s.ServiceID,
		"environment_id": s.EnvironmentID,
		"key":            s.Key,
		"updated_at":     s.UpdatedAt,
	})
}

// GET /api/v1/secrets?service_id=X&env_id=Y — list keys (no values)
func (h *Handler) List(w http.ResponseWriter, r *http.Request) {
	serviceID := r.URL.Query().Get("service_id")
	envID := r.URL.Query().Get("env_id")
	secrets := h.repo.List(serviceID, envID)

	// Never return values in list
	type safeSecret struct {
		ID            string    `json:"id"`
		Key           string    `json:"key"`
		ServiceID     string    `json:"service_id"`
		EnvironmentID string    `json:"environment_id"`
		UpdatedAt     time.Time `json:"updated_at"`
	}
	var out []safeSecret
	for _, s := range secrets {
		out = append(out, safeSecret{
			ID:            s.ID,
			Key:           s.Key,
			ServiceID:     s.ServiceID,
			EnvironmentID: s.EnvironmentID,
			UpdatedAt:     s.UpdatedAt,
		})
	}
	writeJSON(w, 200, out)
}

// GET /api/v1/secrets/{id}/value — decrypt and return value (internal only)
func (h *Handler) GetValue(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	s, ok := h.repo.GetByID(id)
	if !ok {
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

// GET /api/v1/secrets/env-vars?service_id=X&env_id=Y — return all as KEY=VALUE map
func (h *Handler) EnvVars(w http.ResponseWriter, r *http.Request) {
	serviceID := r.URL.Query().Get("service_id")
	envID := r.URL.Query().Get("env_id")
	secrets := h.repo.List(serviceID, envID)

	envMap := make(map[string]string)
	for _, s := range secrets {
		value, err := h.enc.Decrypt(s.ValueEncrypted)
		if err != nil {
			continue
		}
		envMap[s.Key] = value
	}
	writeJSON(w, 200, envMap)
}

func (h *Handler) Delete(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if !h.repo.Delete(id) {
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

	repo := NewSecretRepo()
	h := &Handler{repo: repo, enc: enc}

	mux := http.NewServeMux()
	mux.HandleFunc("PUT /api/v1/secrets", h.Set)
	mux.HandleFunc("GET /api/v1/secrets", h.List)
	mux.HandleFunc("GET /api/v1/secrets/{id}/value", h.GetValue)
	mux.HandleFunc("GET /api/v1/secrets/env-vars", h.EnvVars)
	mux.HandleFunc("DELETE /api/v1/secrets/{id}", h.Delete)
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, 200, map[string]string{"status": "ok", "service": "secrets"})
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
