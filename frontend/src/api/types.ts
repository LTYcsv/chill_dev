export interface ServiceConfig {
  id: string
  project_id: string
  name: string
  git_repo: string
  git_branch: string
  port: number
  environment: string
  created_at: string
}

export type DeploymentStatus =
  | 'pending'
  | 'building'
  | 'deploying'
  | 'running'
  | 'failed'
  | 'rolled_back'

export interface Deployment {
  id: string
  service_id: string
  project_id: string
  git_repo: string
  git_branch: string
  git_commit: string
  environment: string
  status: DeploymentStatus
  triggered_by: string
  logs: string[]
  created_at: string
  started_at?: string
  finished_at?: string
}

export type NodeType = 'service' | 'database' | 'queue' | 'cache' | 'external'
export type NodeStatus = 'running' | 'degraded' | 'down' | 'unknown'

export interface GraphNode {
  id: string
  type: NodeType
  name: string
  project_id: string
  environment: string
  status: NodeStatus
  image_tag?: string
  metadata?: Record<string, string>
}

export interface GraphEdge {
  id: string
  from: string
  to: string
  protocol: string
  rps?: number
  p99_ms?: number
  error_pct?: number
}

export interface InfraGraph {
  project_id: string
  environment: string
  nodes: GraphNode[]
  edges: GraphEdge[]
  snapshot: string
}

export interface BlastRadiusResult {
  node_id: string
  node_name: string
  affected_nodes: { id: string; name: string; type: string; depth: number }[]
  affected_ids: string[]
  affected_names: string[]
  total_affected: number
  severity: 'high' | 'medium' | 'low'
}

export interface HealthStatus {
  status: 'healthy' | 'degraded'
  services: Record<string, 'ok' | 'down'>
}

export interface LogLine {
  deployment_id: string
  line: string
  source: 'build' | 'deploy'
  ts: string
}

export interface LoginResponse {
  token: string
}

export interface AuthUser {
  user_id: string
  email: string
  team_id: string
  role: string
}

export interface Secret {
  id: string
  service_id: string
  environment_id: string
  key: string
  created_at: string
  updated_at: string
}

export interface GraphSnapshot {
  snapshot: string
  node_count: number
  edge_count: number
}
