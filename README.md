# Developer Infrastructure Platform — MVP

> "Kubernetes without Kubernetes" — DevOps without DevOps

Infrastructure visibility and orchestration platform for small/mid teams who need power without complexity.

---

## Architecture

```
                        ┌──────────────────────────────────────────────────┐
                        │                  API Gateway :8080               │
                        │   Auth Middleware → Reverse Proxy → Services     │
                        └────┬──────┬───────┬────────┬────────┬───────────┘
                             │      │       │        │        │
                         ┌───▼──┐ ┌─▼────┐ ┌▼─────┐ ┌▼─────┐ ┌▼──────┐ ┌─────────┐
                         │ Auth │ │Deploy│ │Build │ │Graph │ │Secrets│ │  Logs   │
                         │ :8081│ │:8082 │ │:8083 │ │:8087 │ │:8086  │ │  :8085  │
                         └──────┘ └──┬───┘ └──┬───┘ └──┬───┘ └───────┘ └─────────┘
                                     │        │         │
                                     └────────┴─────────┴──→  NATS (event bus)
                                                                ├── build.requested
                                                                ├── build.completed
                                                                ├── build.failed
                                                                ├── runtime.deployed
                                                                ├── deploy.failed
                                                                └── logs.line.*

Storage:  PostgreSQL (auth, deploy registry, graph time-travel)
          Redis (deployment log streaming)
          Docker Registry :5001 (built images)
```

## Services

| Service | Port | Responsibility |
|---------|------|---------------|
| **gateway** | 8080 | Auth middleware, CORS, reverse proxy to all services |
| **auth** | 8081 | Register, login, JWT issue & validation |
| **deploy** | 8082 | Service registry (PostgreSQL), deployments, rollback, GitHub webhooks |
| **build** | 8083 | Clone repo → build Docker image → push to registry via NATS |
| **logs** | 8085 | Deployment log streaming (SSE), container logs, Redis persistence |
| **secrets** | 8086 | AES-256-GCM encrypted secret storage (PostgreSQL) |
| **graph** | 8087 | Infrastructure Knowledge Graph — blast radius, time-travel, NATS live updates |

## Infrastructure Knowledge Graph

The `graph` service maintains a live model of your infrastructure:

- **Nodes**: services, databases, queues, caches, external systems
- **Edges**: HTTP, gRPC, TCP, NATS, PostgreSQL connections with live RPS + latency
- **Blast Radius**: given a node going down, compute exactly which services are affected (BFS, severity scoring)
- **Time Travel**: checkpoint + diff algorithm — restore infrastructure state at any past timestamp
- **Live Updates**: subscribes to `runtime.deployed` and `deploy.failed` NATS events, auto-updates graph

### Time Travel algorithm

```
getGraphAt(projectID, env, T):
  1. Load nearest checkpoint WHERE created_at <= T
  2. Apply all diffs with seq > checkpoint.seq AND created_at <= T in order
  3. Return reconstructed graph
```

Checkpoints are saved: on service start, every 100 diffs per project/env, and daily via a background ticker.

## Quick Start

### Prerequisites

- Docker + Docker Compose
- Go 1.22+

### Run locally

```bash
# Start infrastructure only (postgres, redis, nats, registry)
make dev

# Start everything in Docker (including all services)
make up

# Smoke test the whole stack
make smoke
```

### CLI

```bash
make cli-build          # builds ./bin/devp

export DEVP_API_URL=http://localhost:8080

# Auth
devp login --email you@example.com --password secret
export DEVP_TOKEN=<token>

# Register a service
devp services register \
  --name my-api \
  --repo https://github.com/me/my-api \
  --branch main \
  --port 3000 \
  --env production

devp services list
devp services get --id <service-id>
devp services delete --id <service-id>

# Deploy (checks blast radius before deploying)
devp deploy \
  --service <service-id> \
  --project my-project \
  --env production \
  --repo https://github.com/me/my-api \
  --branch main \
  [--watch]             # stream logs until deploy completes
  [--skip-blast-radius]

# Track deployment
devp status --deployment <deployment-id>

# Stream logs
devp logs --deployment <deployment-id> [--follow]
devp logs --service <service-id> [--tail 50] [--follow]

# Infrastructure graph
devp graph --project my-project [--env production]
devp graph --project my-project --blast-radius my-api
devp blast-radius --node <node-id-or-name>

# Secrets
devp secrets set DATABASE_URL "postgres://..." --service <id> --env-id <env-id>
devp secrets list --service <id> --env-id <env-id>

# Health check
devp health
```

### GitHub Webhook

```bash
# Start ngrok tunnel and print webhook URL
make ngrok

# Simulate a push event locally
make webhook-test REPO=https://github.com/user/repo BRANCH=main

# Full pipeline test: register service + trigger deploy
make pipeline-test REPO=https://github.com/user/repo BRANCH=main
```

Set `WEBHOOK_SECRET` env var to enable HMAC signature validation.

## API Reference

### Auth
```
POST /api/v1/auth/register   { email, name, password }
POST /api/v1/auth/login      { email, password } → { token }
GET  /api/v1/auth/validate   Authorization: Bearer <token>
```

### Services & Deployments
```
POST /api/v1/services                   { name, git_repo, git_branch, port, environment }
GET  /api/v1/services
GET  /api/v1/services/{id}
DELETE /api/v1/services/{id}

POST /api/v1/deployments                { service_id, project_id, git_repo, git_branch, environment, triggered_by }
GET  /api/v1/deployments/{id}
GET  /api/v1/deployments?service_id=X
POST /api/v1/deployments/{id}/rollback

POST /api/v1/webhooks/github            GitHub push webhook (X-GitHub-Event: push)
```

### Builds
```
GET  /api/v1/builds
GET  /api/v1/builds/{id}
```

### Logs
```
GET  /api/v1/logs/deployment/{id}[?follow=true]    SSE stream of deployment logs
GET  /api/v1/logs?service_id=X[&tail=N][&follow=true]  Container logs (SSE)
```

### Graph
```
GET  /api/v1/graph?project_id=X&env=Y[&at=RFC3339]     current graph or time-travel
GET  /api/v1/graph/timeline?project_id=X&env=Y[&limit=N]  diff log
GET  /api/v1/graph/blast-radius/{nodeID}

POST   /api/v1/graph/nodes              { id, type, name, project_id, environment, status }
DELETE /api/v1/graph/nodes/{id}?project_id=X&env=Y

POST   /api/v1/graph/edges              { id, from, to, protocol, rps }
DELETE /api/v1/graph/edges/{id}?project_id=X&env=Y
```

### Secrets
```
PUT    /api/v1/secrets              { service_id, environment_id, key, value }
GET    /api/v1/secrets?service_id=X&env_id=Y
GET    /api/v1/secrets/{id}/value
GET    /api/v1/secrets/env-vars?service_id=X&env_id=Y
DELETE /api/v1/secrets/{id}
```

## Event Flow (NATS)

```
GitHub push  →  POST /api/v1/webhooks/github
User CLI     →  POST /api/v1/deployments
                  → deploy-service publishes  build.requested
                  → build-service subscribes, clones repo, builds Docker image
                  → build-service publishes   build.completed  { image_tag }
                    OR                        build.failed     { error }
                  → deploy-service subscribes, runs Docker container with secrets injected
                  → deploy-service publishes   runtime.deployed  OR  deploy.failed
                  → graph-service subscribes, updates Knowledge Graph + saves diff
                  → logs-service subscribes, appends terminal log line
```

All log lines during a build/deploy are published to `logs.line.<deployment-id>` and streamed to CLI subscribers via Redis-backed SSE.

## Make Commands

| Command | Description |
|---------|-------------|
| `make dev` | Start infra (postgres, redis, nats, registry) + run all services locally |
| `make up` | Start everything in Docker |
| `make down` | Tear down Docker stack |
| `make stop` | Kill local Go processes + docker compose down |
| `make logs` | Tail all Docker service logs |
| `make ps` | Show Docker service status |
| `make build` | Compile all Go binaries to `./bin/` |
| `make build-images` | Build Docker images for all services |
| `make cli-build` | Build CLI binary `./bin/devp` |
| `make test` | Run unit tests for all services |
| `make test-integration` | Run integration tests (requires running stack) |
| `make lint` | Run golangci-lint |
| `make fmt` | Format all Go code |
| `make migrate` | Run DB migrations via psql |
| `make db-shell` | Open psql shell |
| `make smoke` | Quick API smoke test (register, login, graph) |
| `make smoke-deploy` | Test deploy service endpoints |
| `make ngrok` | Start ngrok tunnel, print GitHub webhook URL |
| `make webhook-url` | Print current ngrok webhook URL |
| `make webhook-test REPO=<url>` | Simulate GitHub push event locally |
| `make pipeline-test REPO=<url>` | Register service + trigger deploy end-to-end |

## Environment Variables

| Variable | Default | Used by |
|----------|---------|---------|
| `JWT_SECRET` | `change-me-in-production-please` | auth |
| `WEBHOOK_SECRET` | _(empty, validation disabled)_ | deploy |
| `ENCRYPTION_KEY` | `12345678...` (32 chars) | secrets |
| `DEVP_API_URL` | `http://localhost:8080` | CLI |
| `DEVP_TOKEN` | _(empty)_ | CLI |

## Monetization Tiers

| Feature | Free | Small Team ($150/mo) | Enterprise |
|---------|------|---------------------|------------|
| Git Deploy | ✓ | ✓ | ✓ |
| Infrastructure Graph | ✓ | ✓ | ✓ |
| Secrets Management | ✓ | ✓ | ✓ |
| Blast Radius Analysis | ✓ | ✓ | ✓ |
| GitHub Webhooks | ✓ | ✓ | ✓ |
| RBAC | — | ✓ | ✓ |
| Audit Logs | — | ✓ | ✓ |
| SSO | — | — | ✓ |
| Log Retention | 1d | 30d | 1yr |
| Time Travel | — | 7d | 90d |
| SLA | — | — | 99.9% |
