package main

import (
	"time"

	"github.com/golang-jwt/jwt/v5"
)

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

func roleAtLeast(required, actual string) bool {
	order := map[string]int{RoleOwner: 3, RoleAdmin: 2, RoleMember: 1}
	return order[actual] >= order[required]
}
