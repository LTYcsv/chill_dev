import { useDeployments } from '../hooks/useDeployments'
import { useServices } from '../hooks/useServices'
import { LogStrip } from '../components/LogStrip'
import { useState } from 'react'
import type { Deployment } from '../api/types'

function duration(d: Deployment): string {
  if (!d.started_at) return '—'
  const end = d.finished_at ? new Date(d.finished_at) : new Date()
  const ms = end.getTime() - new Date(d.started_at).getTime()
  const s = Math.floor(ms / 1000)
  if (s < 60) return `${s}s`
  return `${Math.floor(s / 60)}m ${s % 60}s`
}

function timeAgo(iso: string): string {
  const diff = Date.now() - new Date(iso).getTime()
  const m = Math.floor(diff / 60000)
  if (m < 1) return 'now'
  if (m < 60) return `${m}m ago`
  const h = Math.floor(m / 60)
  if (h < 24) return `${h}h ago`
  return `${Math.floor(h / 24)}d ago`
}

export function BuildsPage() {
  const { data: deployments } = useDeployments()
  const { data: services } = useServices()
  const [expandedId, setExpandedId] = useState<string | null>(null)

  const svcName = (id: string) => services.find(s => s.id === id)?.name ?? id.slice(0, 8)

  const active = deployments.filter(d =>
    d.status === 'pending' || d.status === 'building' || d.status === 'deploying'
  )
  const recent = deployments.filter(d =>
    d.status === 'running' || d.status === 'failed' || d.status === 'rolled_back'
  ).slice(0, 20)

  const BuildRow = ({ d }: { d: Deployment }) => {
    const isExpanded = expandedId === d.id
    const isActive = d.status === 'building' || d.status === 'deploying'
    const statusColor = d.status === 'running' ? 'var(--green)'
      : d.status === 'failed' ? 'var(--red)'
      : d.status === 'building' || d.status === 'deploying' ? 'var(--amber)'
      : 'var(--text3)'

    return (
      <div key={d.id}>
        <div
          className="dep-row"
          onClick={() => setExpandedId(isExpanded ? null : d.id)}
        >
          <div className="build-status-indicator" style={{ background: statusColor, opacity: isActive ? undefined : 0.7 }} />

          <div style={{ flex: 1, minWidth: 0 }}>
            <div className="svc-name">{svcName(d.service_id)}</div>
            <div className="svc-sub">
              {d.git_branch}{d.git_commit ? `@${d.git_commit.slice(0, 7)}` : ''} · {d.triggered_by || 'manual'}
            </div>
          </div>

          <span className="svc-env">{d.environment}</span>

          <span style={{ fontSize: 11.5, color: 'var(--text3)', flexShrink: 0, fontFamily: 'var(--mono)' }}>
            {duration(d)}
          </span>

          <span
            style={{ fontSize: 11.5, color: statusColor, fontWeight: 500, flexShrink: 0 }}
          >
            {d.status}
          </span>

          <span style={{ fontSize: 11.5, color: 'var(--text3)', flexShrink: 0 }}>
            {timeAgo(d.created_at)}
          </span>

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
  }

  return (
    <div className="content">
      {active.length > 0 && (
        <div className="panel">
          <div className="panel-head">
            <BuildsIcon />
            In Progress
            <span className="count">{active.length}</span>
          </div>
          {active.map(d => <BuildRow key={d.id} d={d} />)}
        </div>
      )}

      <div className="panel">
        <div className="panel-head">
          <BuildsIcon />
          Recent Builds
          <span className="count">{recent.length}</span>
        </div>

        {recent.length === 0 && (
          <div className="empty-row">No builds yet</div>
        )}

        {recent.map(d => <BuildRow key={d.id} d={d} />)}
      </div>
    </div>
  )
}

function BuildsIcon() {
  return <svg style={{ width: 14, height: 14, color: 'var(--text3)' }} viewBox="0 0 16 16" fill="none"><path d="M8 2L2 5v4c0 3 3 5 6 5s6-2 6-5V5L8 2z" stroke="currentColor" strokeWidth="1.3" strokeLinejoin="round" /></svg>
}
