package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func registerRoutes(mux *http.ServeMux, srv *Server) {
	mux.HandleFunc("GET /api/v1/graph", func(w http.ResponseWriter, r *http.Request) {
		projectID := r.URL.Query().Get("project_id")
		env := r.URL.Query().Get("env")
		atStr := r.URL.Query().Get("at")

		if atStr != "" {
			at, err := time.Parse(time.RFC3339, atStr)
			if err != nil {
				writeJSON(w, 400, map[string]string{"error": "invalid 'at' format, use RFC3339"})
				return
			}
			g, err := srv.getGraphAt(projectID, env, at)
			if err != nil {
				writeJSON(w, 500, map[string]string{"error": err.Error()})
				return
			}
			writeJSON(w, 200, g)
			return
		}
		writeJSON(w, 200, srv.getGraph(projectID, env))
	})

	mux.HandleFunc("GET /api/v1/graph/timeline", func(w http.ResponseWriter, r *http.Request) {
		if srv.store == nil {
			writeJSON(w, 503, map[string]string{"error": "time travel requires DATABASE_URL"})
			return
		}
		projectID := r.URL.Query().Get("project_id")
		env := r.URL.Query().Get("env")
		limit := 50
		if l := r.URL.Query().Get("limit"); l != "" {
			fmt.Sscanf(l, "%d", &limit)
		}
		if limit > 500 {
			limit = 500
		}
		diffs, err := srv.store.listDiffs(projectID, env, limit)
		if err != nil {
			writeJSON(w, 500, map[string]string{"error": err.Error()})
			return
		}
		if diffs == nil {
			diffs = []DiffRecord{}
		}
		writeJSON(w, 200, diffs)
	})

	mux.HandleFunc("POST /api/v1/graph/nodes", func(w http.ResponseWriter, r *http.Request) {
		var n GraphNode
		if err := json.NewDecoder(r.Body).Decode(&n); err != nil {
			writeJSON(w, 400, map[string]string{"error": "invalid request"})
			return
		}
		if n.ID == "" {
			writeJSON(w, 400, map[string]string{"error": "id is required"})
			return
		}
		srv.UpsertNode(n, "api")
		writeJSON(w, 200, n)
	})

	mux.HandleFunc("DELETE /api/v1/graph/nodes/{id}", func(w http.ResponseWriter, r *http.Request) {
		id := r.PathValue("id")
		projectID := r.URL.Query().Get("project_id")
		env := r.URL.Query().Get("env")
		srv.DeleteNode(id, projectID, env, "api")
		writeJSON(w, 200, map[string]string{"status": "deleted"})
	})

	mux.HandleFunc("POST /api/v1/graph/edges", func(w http.ResponseWriter, r *http.Request) {
		var e GraphEdge
		if err := json.NewDecoder(r.Body).Decode(&e); err != nil {
			writeJSON(w, 400, map[string]string{"error": "invalid request"})
			return
		}
		if e.ID == "" || e.From == "" || e.To == "" {
			writeJSON(w, 400, map[string]string{"error": "id, from, and to are required"})
			return
		}
		projectID := r.URL.Query().Get("project_id")
		env := r.URL.Query().Get("env")
		srv.UpsertEdge(e, projectID, env, "api")
		writeJSON(w, 200, e)
	})

	mux.HandleFunc("DELETE /api/v1/graph/edges/{id}", func(w http.ResponseWriter, r *http.Request) {
		id := r.PathValue("id")
		projectID := r.URL.Query().Get("project_id")
		env := r.URL.Query().Get("env")
		srv.DeleteEdge(id, projectID, env, "api")
		writeJSON(w, 200, map[string]string{"status": "deleted"})
	})

	mux.HandleFunc("GET /api/v1/graph/blast-radius/{nodeID}", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, 200, srv.blastRadius(r.PathValue("nodeID")))
	})

	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		dbStatus := "disabled"
		if srv.store != nil {
			if err := srv.store.db.Ping(); err == nil {
				dbStatus = "connected"
			} else {
				dbStatus = "disconnected"
			}
		}
		srv.mu.RLock()
		nodeCount := len(srv.nodes)
		srv.mu.RUnlock()
		writeJSON(w, 200, map[string]any{
			"status":      "ok",
			"service":     "graph",
			"nodes":       nodeCount,
			"time_travel": dbStatus,
		})
	})
}
