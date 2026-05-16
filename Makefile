.PHONY: help dev stop logs ps build test lint migrate

SERVICES := auth deploy build logs secrets graph
GATEWAY  := gateway

# ─── Help ─────────────────────────────────────────────────────
help: ## Show this help
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | \
		awk 'BEGIN {FS = ":.*?## "}; {printf "\033[36m%-20s\033[0m %s\n", $$1, $$2}'

# ─── Dev lifecycle ────────────────────────────────────────────
dev: ## Start full local stack
	docker compose up -d postgres redis nats registry
	@echo "Waiting for infra..."
	@sleep 3
	@$(MAKE) run-all

run-all: ## Run all services locally (not in Docker)
	@for svc in $(SERVICES); do \
		echo "Starting $$svc..."; \
		cd services/$$svc && go run . & \
		cd ../..; \
	done
	cd gateway && go run . &
	@echo "All services started. Gateway → http://localhost:8080"

stop: ## Stop all background services
	@pkill -f "go run ." || true
	docker compose down

# ─── Docker compose ───────────────────────────────────────────
up: ## Start everything in Docker
	docker compose up --build -d

down: ## Tear down Docker stack
	docker compose down -v

logs: ## Tail all service logs
	docker compose logs -f

ps: ## Show service status
	docker compose ps

# ─── Build ────────────────────────────────────────────────────
build: ## Build all Go binaries
	@for svc in $(SERVICES); do \
		echo "Building $$svc..."; \
		cd services/$$svc && go build -o ../../bin/$$svc . && cd ../..; \
	done
	cd gateway && go build -o ../bin/gateway . && cd ..

build-images: ## Build Docker images for all services
	@for svc in $(SERVICES); do \
		docker build --build-arg SERVICE=$$svc -t devplatform/$$svc:dev \
			-f docker/Dockerfile.service .; \
	done

# ─── Testing ──────────────────────────────────────────────────
test: ## Run all tests
	go work sync
	@for svc in $(SERVICES); do \
		echo "Testing $$svc..."; \
		cd services/$$svc && go test ./... -v && cd ../..; \
	done

test-integration: ## Run integration tests (requires running stack)
	go test ./tests/integration/... -v -tags=integration

# ─── Code quality ─────────────────────────────────────────────
lint: ## Run golangci-lint
	golangci-lint run ./...

fmt: ## Format all Go code
	gofmt -w .

# ─── Database ─────────────────────────────────────────────────
migrate: ## Run DB migrations
	docker compose exec postgres psql -U devplatform -d devplatform -f /docker-entrypoint-initdb.d/init.sql

db-shell: ## Open psql shell
	docker compose exec postgres psql -U devplatform -d devplatform

# ─── CLI ──────────────────────────────────────────────────────
cli-build: ## Build the CLI binary
	cd cli && go build -o ../bin/devp . && cd ..
	@echo "CLI built: ./bin/devp"

# ─── Quick smoke test ─────────────────────────────────────────
smoke: ## Quick API smoke test
	@echo "=== Register user ==="
	@curl -s -X POST http://localhost:8080/api/v1/auth/register \
		-H "Content-Type: application/json" \
		-d '{"email":"test@example.com","name":"Test User","password":"secret123"}' | jq .
	@echo "\n=== Login ==="
	@TOKEN=$$(curl -s -X POST http://localhost:8080/api/v1/auth/login \
		-H "Content-Type: application/json" \
		-d '{"email":"test@example.com","password":"secret123"}' | jq -r .token) && \
	echo "Token: $$TOKEN" && \
	echo "\n=== Graph (demo project) ===" && \
	curl -s http://localhost:8080/api/v1/graph?project_id=demo\&env=production \
		-H "Authorization: Bearer $$TOKEN" | jq .
	@echo "\n=== Health check ==="
	@curl -s http://localhost:8080/healthz | jq .

.DEFAULT_GOAL := help
