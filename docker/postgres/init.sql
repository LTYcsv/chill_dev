-- Developer Infrastructure Platform — PostgreSQL Schema
-- MVP: core tables only, designed to replace in-memory repos

-- ─── Extensions ───────────────────────────────────────────────
CREATE EXTENSION IF NOT EXISTS "uuid-ossp";
CREATE EXTENSION IF NOT EXISTS "pgcrypto";

-- ─── Auth ─────────────────────────────────────────────────────
CREATE TABLE IF NOT EXISTS users (
    id          UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    email       TEXT UNIQUE NOT NULL,
    name        TEXT NOT NULL,
    password_hash TEXT NOT NULL,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS teams (
    id          UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    name        TEXT NOT NULL,
    plan        TEXT NOT NULL DEFAULT 'free',  -- free | small_team | enterprise
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS team_members (
    team_id     UUID NOT NULL REFERENCES teams(id) ON DELETE CASCADE,
    user_id     UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    role        TEXT NOT NULL DEFAULT 'member', -- owner | admin | member
    joined_at   TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (team_id, user_id)
);

-- ─── Projects & Services ──────────────────────────────────────
CREATE TABLE IF NOT EXISTS projects (
    id          UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    team_id     UUID NOT NULL REFERENCES teams(id) ON DELETE CASCADE,
    name        TEXT NOT NULL,
    description TEXT,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS services (
    id          UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    project_id  UUID NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    name        TEXT NOT NULL,
    git_repo    TEXT NOT NULL,
    git_branch  TEXT NOT NULL DEFAULT 'main',
    template    TEXT,          -- node | go | python | static | docker
    port        INT NOT NULL DEFAULT 8080,
    replicas    INT NOT NULL DEFAULT 1,
    status      TEXT NOT NULL DEFAULT 'pending',
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- ─── Environments ─────────────────────────────────────────────
CREATE TABLE IF NOT EXISTS environments (
    id          UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    project_id  UUID NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    name        TEXT NOT NULL,  -- production, staging, preview-pr-42
    type        TEXT NOT NULL,  -- production | staging | preview
    domain      TEXT,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- ─── Deployments ──────────────────────────────────────────────
CREATE TABLE IF NOT EXISTS deployments (
    id              UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    service_id      UUID NOT NULL REFERENCES services(id),
    environment_id  UUID REFERENCES environments(id),
    git_commit      TEXT,
    git_message     TEXT,
    image_tag       TEXT,
    status          TEXT NOT NULL DEFAULT 'queued',
    triggered_by    UUID REFERENCES users(id),
    started_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    finished_at     TIMESTAMPTZ
);

CREATE INDEX IF NOT EXISTS idx_deployments_service ON deployments(service_id);
CREATE INDEX IF NOT EXISTS idx_deployments_status  ON deployments(status);

-- ─── Secrets ──────────────────────────────────────────────────
-- service_id / environment_id are opaque strings (no FK) because
-- the deploy service registry is in-memory for MVP.
CREATE TABLE IF NOT EXISTS secrets (
    id              UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    service_id      TEXT NOT NULL,
    environment_id  TEXT NOT NULL DEFAULT '',
    key             TEXT NOT NULL,
    value_encrypted TEXT NOT NULL,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE(service_id, environment_id, key)
);

-- ─── Graph / Dependencies ─────────────────────────────────────
CREATE TABLE IF NOT EXISTS graph_nodes (
    id          TEXT PRIMARY KEY,  -- service_id or external name
    project_id  UUID NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    environment TEXT NOT NULL,
    type        TEXT NOT NULL,     -- service | database | queue | cache | external
    name        TEXT NOT NULL,
    status      TEXT NOT NULL DEFAULT 'unknown',
    image_tag   TEXT,
    metadata    JSONB,
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS graph_edges (
    id          TEXT PRIMARY KEY,
    from_node   TEXT NOT NULL,
    to_node     TEXT NOT NULL,
    protocol    TEXT NOT NULL DEFAULT 'http',
    rps         INT NOT NULL DEFAULT 0,
    p99_ms      INT NOT NULL DEFAULT 0,
    error_pct   NUMERIC(5,2) NOT NULL DEFAULT 0,
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- ─── Graph Snapshots (for time-travel) ────────────────────────
CREATE TABLE IF NOT EXISTS graph_snapshots (
    id          UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    project_id  UUID NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    environment TEXT NOT NULL,
    data        JSONB NOT NULL,
    taken_at    TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_graph_snapshots_project_env_time
    ON graph_snapshots(project_id, environment, taken_at DESC);

-- ─── Audit Log ────────────────────────────────────────────────
CREATE TABLE IF NOT EXISTS audit_log (
    id          UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    team_id     UUID,
    user_id     UUID,
    action      TEXT NOT NULL,
    resource    TEXT,
    resource_id TEXT,
    metadata    JSONB,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
