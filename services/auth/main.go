package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
	_ "github.com/lib/pq"
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
		JWTSecret: getEnv("JWT_SECRET", ""),
		DBDSN:     getEnv("DATABASE_URL", "postgres://devplatform:devplatform@localhost:5432/devplatform?sslmode=disable"),
	}
}

func getEnv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

var knownWeakSecrets = []string{
	"dev-secret-change-in-production",
	"change-me-in-production-please",
	"secret",
	"jwt-secret",
	"",
}

func validateConfig(cfg Config) {
	for _, bad := range knownWeakSecrets {
		if cfg.JWTSecret == bad {
			log.Fatalf("[auth] JWT_SECRET is not set or is a known insecure default — set a strong random value (openssl rand -hex 32)")
		}
	}
	if len(cfg.JWTSecret) < 32 {
		log.Fatalf("[auth] JWT_SECRET must be at least 32 characters")
	}
}

// ─── Domain ───────────────────────────────────────────────────

const (
	RoleOwner  = "owner"
	RoleAdmin  = "admin"
	RoleMember = "member"
)

type User struct {
	ID           string    `json:"id"`
	Email        string    `json:"email"`
	Name         string    `json:"name"`
	PasswordHash string    `json:"-"`
	CreatedAt    time.Time `json:"created_at"`
}

type Team struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	Plan      string    `json:"plan"`
	CreatedAt time.Time `json:"created_at"`
}

type TeamMember struct {
	TeamID   string    `json:"team_id"`
	UserID   string    `json:"user_id"`
	Email    string    `json:"email,omitempty"`
	Name     string    `json:"name,omitempty"`
	Role     string    `json:"role"`
	JoinedAt time.Time `json:"joined_at"`
}

type Claims struct {
	UserID string `json:"user_id"`
	Email  string `json:"email"`
	TeamID string `json:"team_id"`
	Role   string `json:"role"`
	jwt.RegisteredClaims
}

type contextKey int

const claimsKey contextKey = 0

// roleAtLeast returns true if actual has at least the privilege level of required.
func roleAtLeast(required, actual string) bool {
	order := map[string]int{RoleOwner: 3, RoleAdmin: 2, RoleMember: 1}
	return order[actual] >= order[required]
}

// ─── Database ─────────────────────────────────────────────────

type DB struct {
	db *sql.DB
}

func NewDB(dsn string) (*DB, error) {
	db, err := sql.Open("postgres", dsn)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(10)
	db.SetMaxIdleConns(3)
	if err := db.Ping(); err != nil {
		return nil, fmt.Errorf("postgres ping: %w", err)
	}
	return &DB{db: db}, nil
}

func (d *DB) CreateUser(u *User) error {
	const q = `INSERT INTO users (id, email, name, password_hash) VALUES ($1, $2, $3, $4)`
	_, err := d.db.Exec(q, u.ID, u.Email, u.Name, u.PasswordHash)
	return err
}

func (d *DB) FindUserByEmail(email string) (*User, error) {
	const q = `SELECT id, email, name, password_hash, created_at FROM users WHERE email = $1`
	u := &User{}
	err := d.db.QueryRow(q, email).Scan(&u.ID, &u.Email, &u.Name, &u.PasswordHash, &u.CreatedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	return u, err
}

func (d *DB) FindUserByID(id string) (*User, error) {
	const q = `SELECT id, email, name, created_at FROM users WHERE id = $1`
	u := &User{}
	err := d.db.QueryRow(q, id).Scan(&u.ID, &u.Email, &u.Name, &u.CreatedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	return u, err
}

func (d *DB) CreateTeam(t *Team) error {
	const q = `INSERT INTO teams (id, name, plan) VALUES ($1, $2, $3) RETURNING created_at`
	return d.db.QueryRow(q, t.ID, t.Name, t.Plan).Scan(&t.CreatedAt)
}

func (d *DB) FindTeamByID(id string) (*Team, error) {
	const q = `SELECT id, name, plan, created_at FROM teams WHERE id = $1`
	t := &Team{}
	err := d.db.QueryRow(q, id).Scan(&t.ID, &t.Name, &t.Plan, &t.CreatedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	return t, err
}

func (d *DB) AddMember(teamID, userID, role string) error {
	const q = `
		INSERT INTO team_members (team_id, user_id, role)
		VALUES ($1, $2, $3)
		ON CONFLICT (team_id, user_id) DO UPDATE SET role = EXCLUDED.role`
	_, err := d.db.Exec(q, teamID, userID, role)
	return err
}

func (d *DB) FindMembership(teamID, userID string) (*TeamMember, error) {
	const q = `SELECT team_id, user_id, role, joined_at FROM team_members WHERE team_id = $1 AND user_id = $2`
	m := &TeamMember{}
	err := d.db.QueryRow(q, teamID, userID).Scan(&m.TeamID, &m.UserID, &m.Role, &m.JoinedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	return m, err
}

func (d *DB) ListUserTeams(userID string) ([]*TeamMember, error) {
	const q = `SELECT team_id, user_id, role, joined_at FROM team_members WHERE user_id = $1 ORDER BY joined_at`
	rows, err := d.db.Query(q, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*TeamMember
	for rows.Next() {
		m := &TeamMember{}
		if err := rows.Scan(&m.TeamID, &m.UserID, &m.Role, &m.JoinedAt); err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

func (d *DB) ListTeamMembers(teamID string) ([]*TeamMember, error) {
	const q = `
		SELECT tm.team_id, tm.user_id, u.email, u.name, tm.role, tm.joined_at
		FROM team_members tm
		JOIN users u ON u.id = tm.user_id
		WHERE tm.team_id = $1
		ORDER BY tm.joined_at`
	rows, err := d.db.Query(q, teamID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*TeamMember
	for rows.Next() {
		m := &TeamMember{}
		if err := rows.Scan(&m.TeamID, &m.UserID, &m.Email, &m.Name, &m.Role, &m.JoinedAt); err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

func (d *DB) RemoveMember(teamID, userID string) (bool, error) {
	res, err := d.db.Exec(`DELETE FROM team_members WHERE team_id = $1 AND user_id = $2`, teamID, userID)
	if err != nil {
		return false, err
	}
	n, _ := res.RowsAffected()
	return n > 0, nil
}

// ─── Service ──────────────────────────────────────────────────

type AuthService struct {
	db     *DB
	secret string
}

func NewAuthService(db *DB, secret string) *AuthService {
	return &AuthService{db: db, secret: secret}
}

// Register creates a user and a personal team (owner). Returns a scoped JWT.
func (s *AuthService) Register(email, name, password string) (string, *User, *Team, error) {
	existing, err := s.db.FindUserByEmail(email)
	if err != nil {
		return "", nil, nil, err
	}
	if existing != nil {
		return "", nil, nil, fmt.Errorf("email already registered")
	}

	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return "", nil, nil, err
	}

	u := &User{ID: uuid.NewString(), Email: email, Name: name, PasswordHash: string(hash)}
	if err := s.db.CreateUser(u); err != nil {
		return "", nil, nil, err
	}

	team := &Team{ID: uuid.NewString(), Name: name + "'s Team", Plan: "free"}
	if err := s.db.CreateTeam(team); err != nil {
		return "", nil, nil, err
	}
	if err := s.db.AddMember(team.ID, u.ID, RoleOwner); err != nil {
		return "", nil, nil, err
	}

	token, err := s.generateToken(u.ID, u.Email, team.ID, RoleOwner)
	return token, u, team, err
}

// Login authenticates a user and returns a JWT scoped to teamID (first team if empty).
func (s *AuthService) Login(email, password, teamID string) (string, error) {
	u, err := s.db.FindUserByEmail(email)
	if err != nil {
		return "", err
	}
	if u == nil {
		return "", fmt.Errorf("invalid credentials")
	}
	if err := bcrypt.CompareHashAndPassword([]byte(u.PasswordHash), []byte(password)); err != nil {
		return "", fmt.Errorf("invalid credentials")
	}

	if teamID == "" {
		memberships, err := s.db.ListUserTeams(u.ID)
		if err != nil {
			return "", err
		}
		if len(memberships) == 0 {
			return "", fmt.Errorf("user has no teams")
		}
		teamID = memberships[0].TeamID
	}

	m, err := s.db.FindMembership(teamID, u.ID)
	if err != nil {
		return "", err
	}
	if m == nil {
		return "", fmt.Errorf("not a member of this team")
	}

	return s.generateToken(u.ID, u.Email, teamID, m.Role)
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

func (s *AuthService) generateToken(userID, email, teamID, role string) (string, error) {
	claims := Claims{
		UserID: userID,
		Email:  email,
		TeamID: teamID,
		Role:   role,
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(24 * time.Hour)),
			IssuedAt:  jwt.NewNumericDate(time.Now()),
		},
	}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	return token.SignedString([]byte(s.secret))
}

// ─── HTTP ─────────────────────────────────────────────────────

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}

type Handler struct {
	svc *AuthService
}

func bearerToken(r *http.Request) string {
	v := r.Header.Get("Authorization")
	if strings.HasPrefix(v, "Bearer ") {
		return v[7:]
	}
	return v
}

// authMiddleware validates the JWT and stores claims in context.
func (h *Handler) authMiddleware(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		claims, err := h.svc.ValidateToken(bearerToken(r))
		if err != nil {
			writeJSON(w, 401, map[string]string{"error": "unauthorized"})
			return
		}
		next(w, r.WithContext(context.WithValue(r.Context(), claimsKey, claims)))
	}
}

// requireMinRole wraps authMiddleware and additionally enforces that the caller's JWT
// team matches the {id} path param and has at least minRole privilege.
func (h *Handler) requireMinRole(minRole string, next http.HandlerFunc) http.HandlerFunc {
	return h.authMiddleware(func(w http.ResponseWriter, r *http.Request) {
		claims := r.Context().Value(claimsKey).(*Claims)
		if teamID := r.PathValue("id"); teamID != "" && claims.TeamID != teamID {
			writeJSON(w, 403, map[string]string{"error": "forbidden: wrong team"})
			return
		}
		if !roleAtLeast(minRole, claims.Role) {
			writeJSON(w, 403, map[string]string{"error": "forbidden: insufficient role"})
			return
		}
		next(w, r)
	})
}

// POST /api/v1/auth/register
func (h *Handler) Register(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Email    string `json:"email"`
		Name     string `json:"name"`
		Password string `json:"password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Email == "" || req.Password == "" {
		writeJSON(w, 400, map[string]string{"error": "email, name, and password are required"})
		return
	}
	token, user, team, err := h.svc.Register(req.Email, req.Name, req.Password)
	if err != nil {
		writeJSON(w, 409, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, 201, map[string]interface{}{
		"token":   token,
		"user":    user,
		"team":    team,
		"role":    RoleOwner,
	})
}

// POST /api/v1/auth/login
func (h *Handler) Login(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Email    string `json:"email"`
		Password string `json:"password"`
		TeamID   string `json:"team_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, 400, map[string]string{"error": "invalid request"})
		return
	}
	token, err := h.svc.Login(req.Email, req.Password, req.TeamID)
	if err != nil {
		writeJSON(w, 401, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, 200, map[string]string{"token": token})
}

// GET /api/v1/auth/validate  — used by gateway to verify + extract identity
func (h *Handler) Validate(w http.ResponseWriter, r *http.Request) {
	claims, err := h.svc.ValidateToken(bearerToken(r))
	if err != nil {
		writeJSON(w, 401, map[string]string{"error": "unauthorized"})
		return
	}
	writeJSON(w, 200, map[string]interface{}{
		"user_id": claims.UserID,
		"email":   claims.Email,
		"team_id": claims.TeamID,
		"role":    claims.Role,
	})
}

// POST /api/v1/teams  — create a new team; caller becomes owner
func (h *Handler) CreateTeam(w http.ResponseWriter, r *http.Request) {
	claims := r.Context().Value(claimsKey).(*Claims)
	var req struct {
		Name string `json:"name"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Name == "" {
		writeJSON(w, 400, map[string]string{"error": "name is required"})
		return
	}
	team := &Team{ID: uuid.NewString(), Name: req.Name, Plan: "free"}
	if err := h.svc.db.CreateTeam(team); err != nil {
		log.Printf("[auth] create team: %v", err)
		writeJSON(w, 500, map[string]string{"error": "failed to create team"})
		return
	}
	if err := h.svc.db.AddMember(team.ID, claims.UserID, RoleOwner); err != nil {
		log.Printf("[auth] add owner: %v", err)
		writeJSON(w, 500, map[string]string{"error": "failed to assign owner"})
		return
	}
	writeJSON(w, 201, team)
}

// GET /api/v1/teams/{id}
func (h *Handler) GetTeam(w http.ResponseWriter, r *http.Request) {
	team, err := h.svc.db.FindTeamByID(r.PathValue("id"))
	if err != nil {
		log.Printf("[auth] get team: %v", err)
		writeJSON(w, 500, map[string]string{"error": "db error"})
		return
	}
	if team == nil {
		writeJSON(w, 404, map[string]string{"error": "team not found"})
		return
	}
	writeJSON(w, 200, team)
}

// GET /api/v1/teams/{id}/members
func (h *Handler) ListMembers(w http.ResponseWriter, r *http.Request) {
	members, err := h.svc.db.ListTeamMembers(r.PathValue("id"))
	if err != nil {
		log.Printf("[auth] list members: %v", err)
		writeJSON(w, 500, map[string]string{"error": "db error"})
		return
	}
	if members == nil {
		members = []*TeamMember{}
	}
	writeJSON(w, 200, members)
}

// POST /api/v1/teams/{id}/members  — add or update a member's role
func (h *Handler) AddMember(w http.ResponseWriter, r *http.Request) {
	caller := r.Context().Value(claimsKey).(*Claims)
	teamID := r.PathValue("id")

	var req struct {
		Email string `json:"email"`
		Role  string `json:"role"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Email == "" {
		writeJSON(w, 400, map[string]string{"error": "email is required"})
		return
	}
	if req.Role == "" {
		req.Role = RoleMember
	}
	if req.Role != RoleOwner && req.Role != RoleAdmin && req.Role != RoleMember {
		writeJSON(w, 400, map[string]string{"error": "role must be owner, admin, or member"})
		return
	}
	// Only owners can grant owner role
	if req.Role == RoleOwner && caller.Role != RoleOwner {
		writeJSON(w, 403, map[string]string{"error": "only owners can grant the owner role"})
		return
	}

	u, err := h.svc.db.FindUserByEmail(req.Email)
	if err != nil {
		writeJSON(w, 500, map[string]string{"error": "db error"})
		return
	}
	if u == nil {
		writeJSON(w, 404, map[string]string{"error": "user not found"})
		return
	}

	if err := h.svc.db.AddMember(teamID, u.ID, req.Role); err != nil {
		log.Printf("[auth] add member: %v", err)
		writeJSON(w, 500, map[string]string{"error": "failed to add member"})
		return
	}
	writeJSON(w, 200, map[string]string{"team_id": teamID, "user_id": u.ID, "role": req.Role})
}

// DELETE /api/v1/teams/{id}/members/{user_id}
func (h *Handler) RemoveMember(w http.ResponseWriter, r *http.Request) {
	teamID := r.PathValue("id")
	userID := r.PathValue("user_id")

	// Guard: cannot remove the last owner
	members, err := h.svc.db.ListTeamMembers(teamID)
	if err != nil {
		writeJSON(w, 500, map[string]string{"error": "db error"})
		return
	}
	ownerCount := 0
	for _, m := range members {
		if m.Role == RoleOwner {
			ownerCount++
		}
	}
	for _, m := range members {
		if m.UserID == userID && m.Role == RoleOwner && ownerCount <= 1 {
			writeJSON(w, 409, map[string]string{"error": "cannot remove the last owner"})
			return
		}
	}

	ok, err := h.svc.db.RemoveMember(teamID, userID)
	if err != nil {
		log.Printf("[auth] remove member: %v", err)
		writeJSON(w, 500, map[string]string{"error": "db error"})
		return
	}
	if !ok {
		writeJSON(w, 404, map[string]string{"error": "member not found"})
		return
	}
	writeJSON(w, 200, map[string]string{"status": "removed"})
}

// ─── Main ─────────────────────────────────────────────────────

func main() {
	cfg := loadConfig()
	validateConfig(cfg)

	db, err := NewDB(cfg.DBDSN)
	if err != nil {
		log.Fatalf("[auth] db connect: %v", err)
	}

	svc := NewAuthService(db, cfg.JWTSecret)
	h := &Handler{svc: svc}

	mux := http.NewServeMux()

	// Public endpoints
	mux.HandleFunc("POST /api/v1/auth/register", h.Register)
	mux.HandleFunc("POST /api/v1/auth/login", h.Login)
	mux.HandleFunc("GET /api/v1/auth/validate", h.Validate)

	// Team management (auth required, role enforced per-route)
	mux.HandleFunc("POST /api/v1/teams", h.authMiddleware(h.CreateTeam))
	mux.HandleFunc("GET /api/v1/teams/{id}", h.requireMinRole(RoleMember, h.GetTeam))
	mux.HandleFunc("GET /api/v1/teams/{id}/members", h.requireMinRole(RoleMember, h.ListMembers))
	mux.HandleFunc("POST /api/v1/teams/{id}/members", h.requireMinRole(RoleAdmin, h.AddMember))
	mux.HandleFunc("DELETE /api/v1/teams/{id}/members/{user_id}", h.requireMinRole(RoleAdmin, h.RemoveMember))

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
}
