package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
	"golang.org/x/crypto/bcrypt"
)

// ─── Config ───────────────────────────────────────────────────

type Config struct {
	Port      string
	JWTSecret string
	DBDSN     string
}

func loadConfig() Config {
	return Config{
		Port:      getEnv("PORT", "8081"),
		JWTSecret: getEnv("JWT_SECRET", "dev-secret-change-in-production"),
		DBDSN:     getEnv("DATABASE_URL", "postgres://devplatform:devplatform@localhost:5432/devplatform?sslmode=disable"),
	}
}

func getEnv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

// ─── Domain ───────────────────────────────────────────────────

type User struct {
	ID           string    `json:"id"`
	Email        string    `json:"email"`
	Name         string    `json:"name"`
	PasswordHash string    `json:"-"`
	CreatedAt    time.Time `json:"created_at"`
}

type Claims struct {
	UserID string `json:"user_id"`
	Email  string `json:"email"`
	jwt.RegisteredClaims
}

// ─── Repository (in-memory for MVP, replace with postgres) ────

type UserRepo struct {
	users map[string]*User // keyed by email
}

func NewUserRepo() *UserRepo {
	return &UserRepo{users: make(map[string]*User)}
}

func (r *UserRepo) Create(u *User) error {
	if _, exists := r.users[u.Email]; exists {
		return fmt.Errorf("user already exists")
	}
	r.users[u.Email] = u
	return nil
}

func (r *UserRepo) FindByEmail(email string) (*User, error) {
	u, ok := r.users[email]
	if !ok {
		return nil, fmt.Errorf("user not found")
	}
	return u, nil
}

// ─── Service ──────────────────────────────────────────────────

type AuthService struct {
	repo   *UserRepo
	secret string
}

func NewAuthService(repo *UserRepo, secret string) *AuthService {
	return &AuthService{repo: repo, secret: secret}
}

func (s *AuthService) Register(email, name, password string) (*User, error) {
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return nil, err
	}
	u := &User{
		ID:           uuid.NewString(),
		Email:        email,
		Name:         name,
		PasswordHash: string(hash),
		CreatedAt:    time.Now(),
	}
	if err := s.repo.Create(u); err != nil {
		return nil, err
	}
	return u, nil
}

func (s *AuthService) Login(email, password string) (string, error) {
	u, err := s.repo.FindByEmail(email)
	if err != nil {
		return "", fmt.Errorf("invalid credentials")
	}
	if err := bcrypt.CompareHashAndPassword([]byte(u.PasswordHash), []byte(password)); err != nil {
		return "", fmt.Errorf("invalid credentials")
	}
	return s.generateToken(u)
}

func (s *AuthService) ValidateToken(tokenStr string) (*Claims, error) {
	token, err := jwt.ParseWithClaims(tokenStr, &Claims{}, func(t *jwt.Token) (interface{}, error) {
		return []byte(s.secret), nil
	})
	if err != nil || !token.Valid {
		return nil, fmt.Errorf("invalid token")
	}
	return token.Claims.(*Claims), nil
}

func (s *AuthService) generateToken(u *User) (string, error) {
	claims := Claims{
		UserID: u.ID,
		Email:  u.Email,
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(24 * time.Hour)),
			IssuedAt:  jwt.NewNumericDate(time.Now()),
		},
	}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	return token.SignedString([]byte(s.secret))
}

// ─── HTTP Handlers ────────────────────────────────────────────

type Handler struct {
	svc *AuthService
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}

func (h *Handler) Register(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Email    string `json:"email"`
		Name     string `json:"name"`
		Password string `json:"password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, 400, map[string]string{"error": "invalid request"})
		return
	}
	user, err := h.svc.Register(req.Email, req.Name, req.Password)
	if err != nil {
		writeJSON(w, 409, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, 201, user)
}

func (h *Handler) Login(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Email    string `json:"email"`
		Password string `json:"password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, 400, map[string]string{"error": "invalid request"})
		return
	}
	token, err := h.svc.Login(req.Email, req.Password)
	if err != nil {
		writeJSON(w, 401, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, 200, map[string]string{"token": token})
}

func (h *Handler) Validate(w http.ResponseWriter, r *http.Request) {
	token := r.Header.Get("Authorization")
	if len(token) > 7 && token[:7] == "Bearer " {
		token = token[7:]
	}
	claims, err := h.svc.ValidateToken(token)
	if err != nil {
		writeJSON(w, 401, map[string]string{"error": "unauthorized"})
		return
	}
	writeJSON(w, 200, map[string]string{
		"user_id": claims.UserID,
		"email":   claims.Email,
	})
}

// ─── Main ─────────────────────────────────────────────────────

func main() {
	cfg := loadConfig()
	repo := NewUserRepo()
	svc := NewAuthService(repo, cfg.JWTSecret)
	h := &Handler{svc: svc}

	mux := http.NewServeMux()
	mux.HandleFunc("POST /api/v1/auth/register", h.Register)
	mux.HandleFunc("POST /api/v1/auth/login", h.Login)
	mux.HandleFunc("GET /api/v1/auth/validate", h.Validate)
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, 200, map[string]string{"status": "ok", "service": "auth"})
	})

	srv := &http.Server{
		Addr:         ":" + cfg.Port,
		Handler:      mux,
		ReadTimeout:  5 * time.Second,
		WriteTimeout: 10 * time.Second,
	}

	log.Printf("[auth-service] listening on :%s", cfg.Port)
	if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Fatal(err)
	}
	_ = context.Background()
}
