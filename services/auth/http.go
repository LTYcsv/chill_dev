package main

import (
	"context"
	"encoding/json"
	"log"
	"net/http"
	"strings"

	"github.com/google/uuid"
)

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
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
		"token": token,
		"user":  user,
		"team":  team,
		"role":  RoleOwner,
	})
}

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

func (h *Handler) RemoveMember(w http.ResponseWriter, r *http.Request) {
	teamID := r.PathValue("id")
	userID := r.PathValue("user_id")

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
