.PHONY: help dev stop logs ps build test lint migrate ngrok webhook-url webhook-test pipeline-test

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
	@export REGISTRY_HOST=localhost:5001 && \
	for svc in $(SERVICES); do \
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

# ─── Deploy pipeline smoke test ───────────────────────────────
smoke-deploy: ## Test Git→Build→Deploy pipeline end-to-end
	@echo "=== Register service ==="
	@curl -s -X POST http://localhost:8082/api/v1/services \
		-H "Content-Type: application/json" \
		-d '{"name":"demo","git_repo":"https://github.com/example/demo","git_branch":"main","port":3000,"environment":"production"}' | jq .
	@echo "\n=== Trigger manual deploy ==="
	@SVC_ID=$$(curl -s http://localhost:8082/api/v1/services | jq -r '.[0].id') && \
	curl -s -X POST http://localhost:8082/api/v1/deployments \
		-H "Content-Type: application/json" \
		-d "{\"service_id\":\"$$SVC_ID\",\"project_id\":\"proj-1\",\"git_repo\":\"https://github.com/example/demo\",\"git_branch\":\"main\",\"environment\":\"production\",\"triggered_by\":\"test\"}" | jq .
	@echo "\n=== Deploy service health ==="
	@curl -s http://localhost:8082/healthz | jq .
	@echo "\n=== Build service health ==="
	@curl -s http://localhost:8083/healthz | jq .

# ─── ngrok / webhook dev ──────────────────────────────────────
ngrok: ## Start ngrok tunnel on :8080 and print GitHub webhook URL
	@pkill ngrok 2>/dev/null; true
	@ngrok http 8080 > /dev/null &
	@echo "Waiting for ngrok..."
	@sleep 2
	@URL=$$(curl -s http://localhost:4040/api/tunnels | jq -r '.tunnels[] | select(.proto=="https") | .public_url') && \
	HOOK="$$URL/api/v1/webhooks/github" && \
	echo "" && \
	echo "  Tunnel:   $$URL" && \
	echo "  Webhook:  $$HOOK" && \
	echo "" && \
	echo "GitHub → repo → Settings → Webhooks → Add webhook" && \
	echo "  Payload URL:  $$HOOK" && \
	echo "  Content type: application/json" && \
	echo "  Secret:       $${WEBHOOK_SECRET:-(not set — set WEBHOOK_SECRET env var)}"

webhook-url: ## Print current ngrok webhook URL (ngrok must already be running)
	@URL=$$(curl -s http://localhost:4040/api/tunnels | jq -r '.tunnels[] | select(.proto=="https") | .public_url') && \
	echo "$$URL/api/v1/webhooks/github"

webhook-test: ## Simulate a GitHub push event locally (REPO=<url> BRANCH=<branch>)
	@REPO=$${REPO:-} && BRANCH=$${BRANCH:-main} && \
	[ -n "$$REPO" ] || (echo "Usage: make webhook-test REPO=https://github.com/user/repo" && exit 1) && \
	echo "Simulating push: $$REPO @ $$BRANCH" && \
	curl -s -X POST http://localhost:8080/api/v1/webhooks/github \
		-H "Content-Type: application/json" \
		-H "X-GitHub-Event: push" \
		-d "{\"ref\":\"refs/heads/$$BRANCH\",\"repository\":{\"clone_url\":\"$$REPO\",\"ssh_url\":\"$$REPO\"},\"head_commit\":{\"id\":\"$$(date +%s)\",\"message\":\"test deploy\"},\"pusher\":{\"name\":\"dev\",\"email\":\"dev@localhost\"}}" | jq .

# ─── Full pipeline test ───────────────────────────────────────
pipeline-test: ## Register service + trigger deploy (REPO=<https url> BRANCH=<branch>)
	@REPO=$${REPO:-} && BRANCH=$${BRANCH:-main} && \
	[ -n "$$REPO" ] || (echo "Usage: make pipeline-test REPO=https://github.com/user/repo" && exit 1) && \
	echo "=== Register service ===" && \
	RESULT=$$(curl -s -X POST http://localhost:8082/api/v1/services \
		-H "Content-Type: application/json" \
		-d "{\"name\":\"pipeline-test\",\"git_repo\":\"$$REPO\",\"git_branch\":\"$$BRANCH\",\"port\":3000,\"environment\":\"production\"}") && \
	echo "$$RESULT" | jq . && \
	SVC_ID=$$(echo "$$RESULT" | jq -r .id) && \
	echo "" && \
	echo "=== Trigger deploy ===" && \
	DEPLOY=$$(curl -s -X POST http://localhost:8082/api/v1/deployments \
		-H "Content-Type: application/json" \
		-d "{\"service_id\":\"$$SVC_ID\",\"git_repo\":\"$$REPO\",\"git_branch\":\"$$BRANCH\",\"environment\":\"production\",\"triggered_by\":\"make pipeline-test\"}") && \
	echo "$$DEPLOY" | jq . && \
	DEPLOY_ID=$$(echo "$$DEPLOY" | jq -r .id) && \
	echo "" && \
	echo "Watch logs:  devp logs --deployment $$DEPLOY_ID --follow" && \
	echo "Poll status: curl -s http://localhost:8082/api/v1/deployments/$$DEPLOY_ID | jq .status"

.DEFAULT_GOAL := help
