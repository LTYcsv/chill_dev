import type { ServiceConfig, DeploymentStatus, HealthStatus } from '../api/types'
import { apiFetch } from '../api/client'
import { useState } from 'react'

type Severity = 'high' | 'med' | 'low'

function computeBlast(svcName: string): Severity {
  const high = ['gateway', 'api-gateway', 'auth', 'auth-service']
  const low = ['logs', 'build', 'secrets']
  const n = svcName.toLowerCase()
  if (high.some(h => n.includes(h))) return 'high'
  if (low.some(l => n.includes(l))) return 'low'
  return 'med'
}

function svcStatus(name: string, health: HealthStatus | null): 'green' | 'amber' | 'red' | 'grey' {
  if (!health) return 'grey'
  const key = name.replace(/-service$/, '').replace(/-/g, '_')
  const directMatch = health.services[name] ?? health.services[key]
  if (directMatch === 'ok') return 'green'
  if (directMatch === 'down') return 'red'
  return 'grey'
}

interface Props {
  svc: ServiceConfig
  health: HealthStatus | null
  lastDeployStatus?: DeploymentStatus
  lastCommit?: string
  onDeploy?: (id: string) => void
  onDelete?: (id: string) => void
}

export function ServiceRow({ svc, health, lastDeployStatus, lastCommit, onDeploy, onDelete }: Props) {
  const [deploying, setDeploying] = useState(false)
  const status = svcStatus(svc.name, health)
  const blast = computeBlast(svc.name)
  const isFailed = status === 'red' || lastDeployStatus === 'failed'

  const handleDeploy = async () => {
    if (deploying) return
    setDeploying(true)
    try {
      await apiFetch('/api/v1/deployments', {
        method: 'POST',
        body: JSON.stringify({
          service_id: svc.id,
          project_id: svc.project_id || 'default',
          git_repo: svc.git_repo,
          git_branch: svc.git_branch,
          environment: svc.environment,
          triggered_by: 'dashboard',
        }),
      })
    } finally {
      setDeploying(false)
      onDeploy?.(svc.id)
    }
  }

  const commitShort = lastCommit ? lastCommit.slice(0, 7) : null
  const sub = [`:${svc.port}`, svc.git_branch + (commitShort ? `@${commitShort}` : '')]
    .filter(Boolean)
    .join(' · ')

  return (
    <div className="svc-row">
      <span className={`svc-status status-${status}`} />

      <div style={{ flex: 1, minWidth: 0 }}>
        <div className="svc-name">{svc.name}</div>
        <div className="svc-sub">{sub}</div>
      </div>

      <span className="svc-env">{svc.environment}</span>

      <div className="svc-metrics">
        <div className="svc-uptime" style={status === 'red' ? { color: 'var(--red)' } : status === 'amber' ? { color: 'var(--amber)' } : { color: 'var(--green)' }}>
          {status === 'red' ? 'down' : status === 'amber' ? 'degraded' : 'healthy'}
        </div>
        <div className="svc-latency">:{svc.port}</div>
      </div>

      <span className={`blast-badge blast-${blast}`}>● {blast}</span>

      <button
        className={`svc-deploy-btn${isFailed ? ' retry' : ''}`}
        onClick={handleDeploy}
        disabled={deploying}
      >
        {deploying ? '···' : isFailed ? 'Retry' : 'Deploy'}
      </button>

      {onDelete && (
        <button
          className="svc-deploy-btn"
          style={{ color: 'var(--red)', borderColor: 'rgba(240,82,82,0.25)' }}
          onClick={() => onDelete(svc.id)}
          title="Delete service"
        >
          ✕
        </button>
      )}
    </div>
  )
}
