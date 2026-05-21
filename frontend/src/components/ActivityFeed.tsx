import type { Deployment } from '../api/types'

function timeAgo(iso: string): string {
  const diff = Date.now() - new Date(iso).getTime()
  const m = Math.floor(diff / 60000)
  if (m < 1) return 'now'
  if (m < 60) return `${m} min`
  const h = Math.floor(m / 60)
  if (h < 24) return `${h} h`
  return `${Math.floor(h / 24)} d`
}

type IconType = 'success' | 'fail' | 'deploy' | 'warn'

function iconFor(d: Deployment): IconType {
  if (d.status === 'running') return 'success'
  if (d.status === 'failed') return 'fail'
  if (d.status === 'building' || d.status === 'deploying') return 'deploy'
  return 'warn'
}

function SuccessIcon() {
  return <svg viewBox="0 0 16 16" fill="none"><path d="M3 8.5l3.5 3.5L13 5" stroke="currentColor" strokeWidth="1.5" strokeLinecap="round" strokeLinejoin="round" /></svg>
}

function FailIcon() {
  return <svg viewBox="0 0 16 16" fill="none"><path d="M5 5l6 6M11 5l-6 6" stroke="currentColor" strokeWidth="1.5" strokeLinecap="round" /></svg>
}

function DeployIcon() {
  return <svg viewBox="0 0 16 16" fill="none"><path d="M3 8h10M9 4l4 4-4 4" stroke="currentColor" strokeWidth="1.5" strokeLinecap="round" strokeLinejoin="round" /></svg>
}

function WarnIcon() {
  return <svg viewBox="0 0 16 16" fill="none"><path d="M8 4.5v4.5M8 11.5v1" stroke="currentColor" strokeWidth="1.5" strokeLinecap="round" /></svg>
}

const ICONS: Record<IconType, React.ReactNode> = {
  success: <SuccessIcon />,
  fail: <FailIcon />,
  deploy: <DeployIcon />,
  warn: <WarnIcon />,
}

interface Props {
  deployments: Deployment[]
}

export function ActivityFeed({ deployments }: Props) {
  const recent = deployments.slice(0, 6)

  return (
    <div className="panel">
      <div className="panel-head">
        <svg style={{ width: 14, height: 14, color: 'var(--text3)' }} viewBox="0 0 16 16" fill="none">
          <path d="M2 4h12M2 8h8M2 12h10" stroke="currentColor" strokeWidth="1.3" strokeLinecap="round" />
        </svg>
        Activity
      </div>

      {recent.length === 0 && (
        <div className="empty-row">No recent deployments</div>
      )}

      {recent.map(d => {
        const type = iconFor(d)
        return (
          <div className="feed-item" key={d.id}>
            <div className={`feed-icon ${type}`}>{ICONS[type]}</div>
            <div className="feed-body">
              <div className="feed-title">
                {d.status === 'running' ? `${d.git_branch} deployed` :
                 d.status === 'failed' ? `deploy failed` :
                 d.status === 'building' ? `build started` :
                 d.status}
              </div>
              <div className="feed-desc">
                {d.git_branch} · {d.triggered_by || 'webhook'}
                {d.git_commit ? ` · ${d.git_commit.slice(0, 7)}` : ''}
              </div>
            </div>
            <div className="feed-time">{timeAgo(d.created_at)}</div>
          </div>
        )
      })}
    </div>
  )
}
