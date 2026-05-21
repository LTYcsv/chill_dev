import { useState } from 'react'
import { useDeployments } from '../hooks/useDeployments'
import { useServices } from '../hooks/useServices'
import { apiFetch } from '../api/client'
import { LogStrip } from '../components/LogStrip'
import type { Deployment, DeploymentStatus } from '../api/types'

function timeAgo(iso: string): string {
  const diff = Date.now() - new Date(iso).getTime()
  const m = Math.floor(diff / 60000)
  if (m < 1) return 'now'
  if (m < 60) return `${m}m ago`
  const h = Math.floor(m / 60)
  if (h < 24) return `${h}h ago`
  return `${Math.floor(h / 24)}d ago`
}

const STATUS_LABELS: Record<DeploymentStatus, string> = {
  pending:     'Pending',
  building:    'Building',
  deploying:   'Deploying',
  running:     'Running',
  failed:      'Failed',
  rolled_back: 'Rolled back',
}

const STATUS_CLASS: Record<DeploymentStatus, string> = {
  pending:     'status-pill amber',
  building:    'status-pill amber',
  deploying:   'status-pill blue',
  running:     'status-pill green',
  failed:      'status-pill red',
  rolled_back: 'status-pill grey',
}

export function DeploymentsPage() {
  const { data: deployments } = useDeployments()
  const { data: services } = useServices()
  const [filterSvc, setFilterSvc] = useState('')
  const [filterStatus, setFilterStatus] = useState('')
  const [expandedId, setExpandedId] = useState<string | null>(null)
  const [rolling, setRolling] = useState<string | null>(null)

  const svcName = (id: string) => services.find(s => s.id === id)?.name ?? id.slice(0, 8)

  const filtered = deployments.filter(d => {
    if (filterSvc && d.service_id !== filterSvc) return false
    if (filterStatus && d.status !== filterStatus) return false
    return true
  })

  const handleRollback = async (d: Deployment) => {
    if (rolling) return
    setRolling(d.id)
    try {
      await apiFetch(`/api/v1/deployments/${d.id}/rollback`, { method: 'POST' })
    } catch (err) {
      alert((err as Error).message)
    } finally {
      setRolling(null)
    }
  }

  return (
    <div className="content">
      <div className="filter-bar">
        <select
          className="input"
          style={{ width: 'auto', minWidth: 160 }}
          value={filterSvc}
          onChange={e => setFilterSvc(e.target.value)}
        >
          <option value="">All services</option>
          {services.map(s => <option key={s.id} value={s.id}>{s.name}</option>)}
        </select>
        <select
          className="input"
          style={{ width: 'auto', minWidth: 140 }}
          value={filterStatus}
          onChange={e => setFilterStatus(e.target.value)}
        >
          <option value="">All statuses</option>
          {(Object.keys(STATUS_LABELS) as DeploymentStatus[]).map(s => (
            <option key={s} value={s}>{STATUS_LABELS[s]}</option>
          ))}
        </select>
        <span style={{ marginLeft: 'auto', fontSize: 12, color: 'var(--text3)' }}>
          {filtered.length} deployment{filtered.length !== 1 ? 's' : ''}
        </span>
      </div>

      <div className="panel">
        <div className="panel-head">
          <DeployIcon />
          Deployments
          <span className="count">{filtered.length}</span>
        </div>

        {filtered.length === 0 && (
          <div className="empty-row">No deployments match the filter</div>
        )}

        {filtered.map(d => {
          const isExpanded = expandedId === d.id
          const canRollback = d.status === 'running' || d.status === 'building' || d.status === 'deploying'
          const commitShort = d.git_commit ? d.git_commit.slice(0, 7) : null
          return (
            <div key={d.id}>
              <div
                className="dep-row"
                onClick={() => setExpandedId(isExpanded ? null : d.id)}
              >
                <span className={STATUS_CLASS[d.status] ?? 'status-pill grey'}>
                  {STATUS_LABELS[d.status] ?? d.status}
                </span>

                <div style={{ flex: 1, minWidth: 0 }}>
                  <div className="svc-name">{svcName(d.service_id)}</div>
                  <div className="svc-sub">
                    {d.git_branch}{commitShort ? `@${commitShort}` : ''} · {d.triggered_by || 'webhook'}
                  </div>
                </div>

                <span className="svc-env">{d.environment}</span>

                <span style={{ fontSize: 11.5, color: 'var(--text3)', flexShrink: 0 }}>
                  {timeAgo(d.created_at)}
                </span>

                {canRollback && (
                  <button
                    className="svc-deploy-btn"
                    onClick={e => { e.stopPropagation(); handleRollback(d) }}
                    disabled={rolling === d.id}
                  >
                    {rolling === d.id ? '···' : 'Rollback'}
                  </button>
                )}

                <svg
                  style={{ width: 12, height: 12, color: 'var(--text3)', flexShrink: 0, transform: isExpanded ? 'rotate(180deg)' : 'none', transition: 'transform 0.15s' }}
                  viewBox="0 0 12 12" fill="none"
                >
                  <path d="M3 5l3 3 3-3" stroke="currentColor" strokeWidth="1.3" strokeLinecap="round" strokeLinejoin="round" />
                </svg>
              </div>

              {isExpanded && (
                <LogStrip deploymentId={d.id} serviceName={svcName(d.service_id)} />
              )}
            </div>
          )
        })}
      </div>
    </div>
  )
}

function DeployIcon() {
  return (
    <svg style={{ width: 14, height: 14, color: 'var(--text3)' }} viewBox="0 0 16 16" fill="none">
      <path d="M3 13V6l5-4 5 4v7" stroke="currentColor" strokeWidth="1.3" strokeLinejoin="round" />
      <rect x="6" y="9" width="4" height="4" rx="0.8" stroke="currentColor" strokeWidth="1.2" />
    </svg>
  )
}
