package main

import (
	"fmt"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
	"golang.org/x/crypto/bcrypt"
)

type AuthService struct {
	db     *DB
	secret string
}

func NewAuthService(db *DB, secret string) *AuthService {
	return &AuthService{db: db, secret: secret}
}

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
