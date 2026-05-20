package main

import (
	"encoding/json"
	"fmt"
	"log"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"strings"
	"sync"
	"time"
)

// ─── Config ───────────────────────────────────────────────────

type Config struct {
	Port        string
	AuthSvcURL  string
	DeploySvcURL string
	BuildSvcURL  string
	GraphSvcURL  string
	SecretsSvcURL string
	LogsSvcURL   string
}

func loadConfig() Config {
	return Config{
		Port:          getEnv("PORT", "8080"),
		AuthSvcURL:    getEnv("AUTH_SVC_URL", "http://localhost:8081"),
		DeploySvcURL:  getEnv("DEPLOY_SVC_URL", "http://localhost:8082"),
		BuildSvcURL:   getEnv("BUILD_SVC_URL", "http://localhost:8083"),
		GraphSvcURL:   getEnv("GRAPH_SVC_URL", "http://localhost:8087"),
		SecretsSvcURL: getEnv("SECRETS_SVC_URL", "http://localhost:8086"),
		LogsSvcURL:    getEnv("LOGS_SVC_URL", "http://localhost:8085"),
	}
}

func getEnv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

// ─── Middleware ───────────────────────────────────────────────

type Middleware func(http.Handler) http.Handler

func chain(h http.Handler, middlewares ...Middleware) http.Handler {
	for i := len(middlewares) - 1; i >= 0; i-- {
		h = middlewares[i](h)
	}
	return h
}

func Logger() Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()
			rw := &responseWriter{ResponseWriter: w, status: 200}
			next.ServeHTTP(rw, r)
			log.Printf("[gateway] %s %s %d %s", r.Method, r.URL.Path, rw.status, time.Since(start))
		})
	}
}

func CORS() Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Access-Control-Allow-Origin", "*")
			w.Header().Set("Access-Control-Allow-Methods", "GET,POST,PUT,DELETE,OPTIONS")
			w.Header().Set("Access-Control-Allow-Headers", "Authorization,Content-Type")
			if r.Method == http.MethodOptions {
				w.WriteHeader(204)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

// ─── Rate Limiting ────────────────────────────────────────────

type rateLimiter struct {
	mu      sync.Mutex
	buckets map[string]*rlBucket
	rate    int
	window  time.Duration
}

type rlBucket struct {
	count   int
	resetAt time.Time
}

func newRateLimiter(rate int, window time.Duration) *rateLimiter {
	rl := &rateLimiter{
		buckets: make(map[string]*rlBucket),
		rate:    rate,
		window:  window,
	}
	go rl.periodicCleanup()
	return rl
}

func (rl *rateLimiter) allow(key string) bool {
	rl.mu.Lock()
	defer rl.mu.Unlock()
	now := time.Now()
	b, ok := rl.buckets[key]
	if !ok || now.After(b.resetAt) {
		rl.buckets[key] = &rlBucket{count: 1, resetAt: now.Add(rl.window)}
		return true
	}
	if b.count >= rl.rate {
		return false
	}
	b.count++
	return true
}

func (rl *rateLimiter) periodicCleanup() {
	t := time.NewTicker(5 * time.Minute)
	defer t.Stop()
	for range t.C {
		rl.mu.Lock()
		now := time.Now()
		for k, b := range rl.buckets {
			if now.After(b.resetAt) {
				delete(rl.buckets, k)
			}
		}
		rl.mu.Unlock()
	}
}

func (rl *rateLimiter) Middleware() Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if !rl.allow(clientIP(r)) {
				w.Header().Set("Retry-After", "60")
				writeJSON(w, 429, map[string]string{"error": "rate limit exceeded"})
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

func (rl *rateLimiter) wrap(next http.Handler) http.Handler {
	return rl.Middleware()(next)
}

func clientIP(r *http.Request) string {
	if ip := r.Header.Get("X-Real-IP"); ip != "" {
		return ip
	}
	if fwd := r.Header.Get("X-Forwarded-For"); fwd != "" {
		if i := strings.IndexByte(fwd, ','); i >= 0 {
			return strings.TrimSpace(fwd[:i])
		}
		return strings.TrimSpace(fwd)
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

// AuthMiddleware validates JWT by calling auth-service /validate
func AuthMiddleware(authSvcURL string) Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Skip auth for public routes
			if isPublicRoute(r.URL.Path) {
				next.ServeHTTP(w, r)
				return
			}

			token := r.Header.Get("Authorization")
			if token == "" {
				writeJSON(w, 401, map[string]string{"error": "authorization required"})
				return
			}

			// Call auth-service to validate
			req, _ := http.NewRequestWithContext(r.Context(), "GET", authSvcURL+"/api/v1/auth/validate", nil)
			req.Header.Set("Authorization", token)
			resp, err := http.DefaultClient.Do(req)
			if err != nil || resp.StatusCode != 200 {
				writeJSON(w, 401, map[string]string{"error": "invalid token"})
				return
			}
			defer resp.Body.Close()

			// Forward user identity headers downstream
			var claims map[string]string
			json.NewDecoder(resp.Body).Decode(&claims)
			r.Header.Set("X-User-ID", claims["user_id"])
			r.Header.Set("X-User-Email", claims["email"])

			next.ServeHTTP(w, r)
		})
	}
}

func isPublicRoute(path string) bool {
	public := []string{
		"/healthz",
		"/api/v1/auth/login",
		"/api/v1/auth/register",
		"/api/v1/webhooks/github", // GitHub calls this directly, no JWT
	}
	for _, p := range public {
		if path == p || strings.HasPrefix(path, p) {
			return true
		}
	}
	return false
}

type responseWriter struct {
	http.ResponseWriter
	status int
}

func (rw *responseWriter) WriteHeader(status int) {
	rw.status = status
	rw.ResponseWriter.WriteHeader(status)
}

// ─── Reverse Proxy ────────────────────────────────────────────

func newProxy(target string) http.Handler {
	targetURL, err := url.Parse(target)
	if err != nil {
		log.Fatalf("invalid upstream URL: %s", target)
	}
	proxy := httputil.NewSingleHostReverseProxy(targetURL)
	proxy.ErrorHandler = func(w http.ResponseWriter, r *http.Request, err error) {
		log.Printf("[gateway] upstream error: %v", err)
		writeJSON(w, 502, map[string]string{"error": "upstream unavailable"})
	}
	return proxy
}

// stripAndProxy strips a prefix before forwarding
func stripAndProxy(prefix, upstream string) http.Handler {
	proxy := newProxy(upstream)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		r.URL.Path = strings.TrimPrefix(r.URL.Path, prefix)
		if r.URL.Path == "" {
			r.URL.Path = "/"
		}
		proxy.ServeHTTP(w, r)
	})
}

// ─── Routes ───────────────────────────────────────────────────

func buildRouter(cfg Config) http.Handler {
	mux := http.NewServeMux()

	// Login and register get a strict per-IP limiter (10 req/min) to block brute-force.
	// More specific patterns take precedence over the catch-all /api/v1/auth/ below.
	authProxy := newProxy(cfg.AuthSvcURL)
	loginLimiter := newRateLimiter(10, time.Minute)
	mux.Handle("POST /api/v1/auth/login", loginLimiter.wrap(authProxy))
	mux.Handle("POST /api/v1/auth/register", loginLimiter.wrap(authProxy))

	// Auth service (public + protected) — everything else
	mux.Handle("/api/v1/auth/", authProxy)

	// Protected routes → downstream microservices
	mux.Handle("/api/v1/deployments", newProxy(cfg.DeploySvcURL))
	mux.Handle("/api/v1/deployments/", newProxy(cfg.DeploySvcURL))
	mux.Handle("/api/v1/services", newProxy(cfg.DeploySvcURL))
	mux.Handle("/api/v1/services/", newProxy(cfg.DeploySvcURL))
	mux.Handle("/api/v1/webhooks/", newProxy(cfg.DeploySvcURL))

	mux.Handle("/api/v1/builds", newProxy(cfg.BuildSvcURL))
	mux.Handle("/api/v1/builds/", newProxy(cfg.BuildSvcURL))

	mux.Handle("/api/v1/graph", newProxy(cfg.GraphSvcURL))
	mux.Handle("/api/v1/graph/", newProxy(cfg.GraphSvcURL))

	mux.Handle("/api/v1/secrets", newProxy(cfg.SecretsSvcURL))
	mux.Handle("/api/v1/secrets/", newProxy(cfg.SecretsSvcURL))

	mux.Handle("/api/v1/logs", newProxy(cfg.LogsSvcURL))
	mux.Handle("/api/v1/logs/", newProxy(cfg.LogsSvcURL))

	// Gateway health (aggregates upstream health)
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		services := map[string]string{
			"auth":    cfg.AuthSvcURL,
			"deploy":  cfg.DeploySvcURL,
			"build":   cfg.BuildSvcURL,
			"graph":   cfg.GraphSvcURL,
			"secrets": cfg.SecretsSvcURL,
			"logs":    cfg.LogsSvcURL,
		}
		statuses := make(map[string]string)
		overall := "healthy"
		for name, svcURL := range services {
			resp, err := http.Get(svcURL + "/healthz")
			if err != nil || resp.StatusCode != 200 {
				statuses[name] = "down"
				overall = "degraded"
			} else {
				statuses[name] = "ok"
			}
		}
		writeJSON(w, 200, map[string]interface{}{
			"status":   overall,
			"services": statuses,
		})
	})

	// Wrap everything with middleware
	globalLimiter := newRateLimiter(200, time.Minute)
	return chain(mux,
		Logger(),
		CORS(),
		globalLimiter.Middleware(),
		AuthMiddleware(cfg.AuthSvcURL),
	)
}

// ─── Main ─────────────────────────────────────────────────────

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}

func main() {
	cfg := loadConfig()
	router := buildRouter(cfg)

	srv := &http.Server{
		Addr:         fmt.Sprintf(":%s", cfg.Port),
		Handler:      router,
		ReadTimeout:  10 * time.Second,
		WriteTimeout: 30 * time.Second,
		IdleTimeout:  120 * time.Second,
	}

	log.Printf("[gateway] listening on :%s", cfg.Port)
	log.Printf("[gateway] routing → auth:%s deploy:%s build:%s graph:%s secrets:%s logs:%s",
		cfg.AuthSvcURL, cfg.DeploySvcURL, cfg.BuildSvcURL, cfg.GraphSvcURL, cfg.SecretsSvcURL, cfg.LogsSvcURL)

	if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Fatal(err)
	}
}
