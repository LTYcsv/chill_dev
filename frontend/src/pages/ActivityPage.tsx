import { useState } from 'react'
import { useDeployments } from '../hooks/useDeployments'
import { useServices } from '../hooks/useServices'
import type { DeploymentStatus } from '../api/types'

function timeAgo(iso: string): string {
  const diff = Date.now() - new Date(iso).getTime()
  const m = Math.floor(diff / 60000)
  if (m < 1) return 'now'
  if (m < 60) return `${m}m ago`
  const h = Math.floor(m / 60)
  if (h < 24) return `${h}h ago`
  return `${Math.floor(h / 24)}d ago`
}

function formatDate(iso: string): string {
  return new Date(iso).toLocaleString(undefined, {
    month: 'short', day: 'numeric',
    hour: '2-digit', minute: '2-digit',
  })
}

type IconType = 'success' | 'fail' | 'deploy' | 'warn'

function iconFor(status: DeploymentStatus): IconType {
  if (status === 'running') return 'success'
  if (status === 'failed') return 'fail'
  if (status === 'building' || status === 'deploying') return 'deploy'
  return 'warn'
}

const STATUS_COLOR: Record<DeploymentStatus, string> = {
  running:     'var(--green)',
  failed:      'var(--red)',
  building:    'var(--amber)',
  deploying:   'var(--accent)',
  pending:     'var(--amber)',
  rolled_back: 'var(--text3)',
}

export function ActivityPage() {
  const { data: deployments } = useDeployments()
  const { data: services } = useServices()
  const [filterStatus, setFilterStatus] = useState('')

  const svcName = (id: string) => services.find(s => s.id === id)?.name ?? id.slice(0, 8)

  const filtered = filterStatus
    ? deployments.filter(d => d.status === filterStatus)
    : deployments

  return (
    <div className="content">
      <div className="filter-bar">
        <select
          className="input"
          style={{ width: 'auto', minWidth: 140 }}
          value={filterStatus}
          onChange={e => setFilterStatus(e.target.value)}
        >
          <option value="">All statuses</option>
          <option value="running">Running</option>
          <option value="failed">Failed</option>
          <option value="building">Building</option>
          <option value="deploying">Deploying</option>
          <option value="pending">Pending</option>
          <option value="rolled_back">Rolled back</option>
        </select>
        <span style={{ marginLeft: 'auto', fontSize: 12, color: 'var(--text3)' }}>
          {filtered.length} event{filtered.length !== 1 ? 's' : ''}
        </span>
      </div>

      <div className="panel">
        <div className="panel-head">
          <ActivityIcon />
          Activity
          <span className="count">{filtered.length}</span>
        </div>

        {filtered.length === 0 && (
          <div className="empty-row">No events yet</div>
        )}

        {filtered.map(d => {
          const type = iconFor(d.status)
          const commitShort = d.git_commit ? d.git_commit.slice(0, 7) : null
          return (
            <div className="activity-row" key={d.id}>
              <div className={`feed-icon ${type}`}>
                {type === 'success' && <CheckIcon />}
                {type === 'fail' && <XIcon />}
                {type === 'deploy' && <ArrowIcon />}
                {type === 'warn' && <DotIcon />}
              </div>

              <div className="feed-body">
                <div className="feed-title">
                  <span style={{ color: 'var(--text)' }}>{svcName(d.service_id)}</span>
                  {' '}
                  <span style={{ color: STATUS_COLOR[d.status] ?? 'var(--text3)' }}>
                    {d.status}
                  </span>
                </div>
                <div className="feed-desc">
                  {d.git_branch}{commitShort ? `@${commitShort}` : ''} · {d.triggered_by || 'webhook'} · {d.environment}
                </div>
              </div>

              <div style={{ textAlign: 'right', flexShrink: 0 }}>
                <div className="feed-time">{timeAgo(d.created_at)}</div>
                <div style={{ fontSize: 10.5, color: 'var(--text3)', marginTop: 2 }}>
                  {formatDate(d.created_at)}
                </div>
              </div>
            </div>
          )
        })}
      </div>
    </div>
  )
}

function ActivityIcon() {
  return <svg style={{ width: 14, height: 14, color: 'var(--text3)' }} viewBox="0 0 16 16" fill="none"><circle cx="8" cy="8" r="5.5" stroke="currentColor" strokeWidth="1.5" /><path d="M8 5v3.5l2 1" stroke="currentColor" strokeWidth="1.3" strokeLinecap="round" /></svg>
}
function CheckIcon() {
  return <svg viewBox="0 0 16 16" fill="none"><path d="M3 8.5l3.5 3.5L13 5" stroke="currentColor" strokeWidth="1.5" strokeLinecap="round" strokeLinejoin="round" /></svg>
}
function XIcon() {
  return <svg viewBox="0 0 16 16" fill="none"><path d="M5 5l6 6M11 5l-6 6" stroke="currentColor" strokeWidth="1.5" strokeLinecap="round" /></svg>
}
function ArrowIcon() {
  return <svg viewBox="0 0 16 16" fill="none"><path d="M3 8h10M9 4l4 4-4 4" stroke="currentColor" strokeWidth="1.5" strokeLinecap="round" strokeLinejoin="round" /></svg>
}
function DotIcon() {
  return <svg viewBox="0 0 16 16" fill="none"><path d="M8 4.5v4.5M8 11.5v1" stroke="currentColor" strokeWidth="1.5" strokeLinecap="round" /></svg>
}
