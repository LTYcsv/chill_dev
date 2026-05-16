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
                         └──────┘ └──┬───┘ └──┬───┘ └──────┘ └───────┘ └─────────┘
                                     │        │
                                     └────────┴──→  NATS (event bus)
                                                      ├── build.requested
                                                      ├── build.completed
                                                      ├── runtime.deployed
                                                      └── service.unhealthy

Infrastructure:  PostgreSQL · Redis · NATS · Docker Registry · Traefik
```

## Services

| Service | Port | Responsibility |
|---------|------|---------------|
| **gateway** | 8080 | Auth middleware, reverse proxy, health aggregation |
| **auth** | 8081 | Register, login, JWT issue & validation |
| **deploy** | 8082 | Deployment lifecycle, rollback, NATS orchestration |
| **build** | 8083 | Clone repo → build Docker image → push to registry |
| **logs** | 8085 | Stream and store service logs |
| **secrets** | 8086 | AES-256-GCM encrypted secret storage |
| **graph** | 8087 | **Infrastructure Knowledge Graph** — blast radius, time-travel |

## Infrastructure Knowledge Graph

The core **unfair advantage** per the strategy doc. The `graph` service maintains a live model of your infrastructure:

- **Nodes**: services, databases, queues, caches, external systems
- **Edges**: HTTP, gRPC, TCP, NATS, PostgreSQL connections with live RPS + latency
- **Blast Radius**: given a node going down, compute exactly which services are affected
- **Time Travel**: snapshots taken after every deploy, query state at any past timestamp

```bash
# See your infra graph
devp graph --project myapp --env production

# "If postgres goes down, what breaks?"
devp graph --blast-radius postgres-main

# "What did production look like last Tuesday?"
curl "http://localhost:8080/api/v1/graph/time-travel?project_id=myapp&env=production&at=2024-01-09T14:00:00Z"
```

## Quick Start

### Prerequisites
- Docker + Docker Compose
- Go 1.22+

### Run locally (all services)

```bash
# Start infrastructure (postgres, redis, nats, registry)
make dev

# Or start everything in Docker
make up

# Smoke test the whole stack
make smoke
```

### CLI usage

```bash
# Build the CLI
make cli-build

# Login
export DEVP_API_URL=http://localhost:8080
devp login --email you@example.com --password secret
export DEVP_TOKEN=<token from login>

# Deploy a service
devp deploy \
  --service my-api \
  --project my-project \
  --env production \
  --repo https://github.com/me/my-api \
  --branch main

# Track deployment
devp status --deployment <deployment-id>

# Manage secrets
devp secrets set DATABASE_URL "postgres://..." --service my-api --env-id prod-env-id
devp secrets list --service my-api --env-id prod-env-id

# View infrastructure graph
devp graph --project my-project
devp graph --project my-project --blast-radius my-api
```

## API Reference

### Auth
```
POST /api/v1/auth/register   { email, name, password }
POST /api/v1/auth/login      { email, password } → { token }
GET  /api/v1/auth/validate   Authorization: Bearer <token>
```

### Deployments
```
POST /api/v1/deployments                { service_id, git_repo, git_branch, environment }
GET  /api/v1/deployments/{id}
GET  /api/v1/deployments?service_id=X
POST /api/v1/deployments/{id}/rollback
```

### Graph
```
GET  /api/v1/graph?project_id=X&env=production
GET  /api/v1/graph/blast-radius/{node_id}
GET  /api/v1/graph/time-travel?project_id=X&env=Y&at=<RFC3339>
POST /api/v1/graph/nodes    { id, type, name, project_id, environment, status }
POST /api/v1/graph/edges    { id, from, to, protocol, rps }
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
User triggers deploy
  → deploy-service publishes  build.requested
  → build-service subscribes, clones repo, builds Docker image
  → build-service publishes   build.completed  { image_tag }
  → deploy-service subscribes, publishes       runtime.deploy
  → runtime-service deploys to Kubernetes/Docker
  → runtime-service publishes  runtime.deployed
  → deploy-service marks deployment SUCCESS
  → graph-service subscribes, updates Knowledge Graph
  → graph-service takes snapshot (enables time-travel)
```

## Monetization Alignment (from strategy doc)

| Feature | Free | Small Team ($150/mo) | Enterprise |
|---------|------|---------------------|------------|
| Git Deploy | ✓ | ✓ | ✓ |
| Infrastructure Graph | ✓ | ✓ | ✓ |
| Secrets Management | ✓ | ✓ | ✓ |
| Blast Radius Analysis | ✓ | ✓ | ✓ |
| RBAC | — | ✓ | ✓ |
| Audit Logs | — | ✓ | ✓ |
| SSO | — | — | ✓ |
| Monitoring Retention | 1d | 30d | 1yr |
| Time Travel | — | 7d | 90d |
| SLA | — | — | 99.9% |

## Roadmap (aligned with strategy doc)

**Month 1 (current MVP)**: CLI, Docker deploy, Git integration, auth  
**Month 2**: Dashboard UI, logs viewer, environment management  
**Month 3**: Kubernetes abstraction, service templates  
**Month 4**: Preview environments, secrets UI  
**Month 5-6**: Full infrastructure graph UI, beta users, monitoring
