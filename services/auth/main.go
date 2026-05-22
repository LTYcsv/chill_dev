package main

import (
	"log"
	"net/http"
	"time"
)

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

	mux.HandleFunc("POST /api/v1/auth/register", h.Register)
	mux.HandleFunc("POST /api/v1/auth/login", h.Login)
	mux.HandleFunc("GET /api/v1/auth/validate", h.Validate)

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
