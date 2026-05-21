import { useState } from 'react'

interface Props {
  project?: string
  page?: string
  env?: string
  onEnvChange?: (env: string) => void
  onTimeTravelClick?: () => void
  onDeployClick?: () => void
}

const ENVS = ['production', 'staging', 'development']

export function Topbar({ project = 'my-project', page = 'dashboard', env = 'production', onEnvChange, onTimeTravelClick, onDeployClick }: Props) {
  const [envOpen, setEnvOpen] = useState(false)

  return (
    <div className="topbar">
      <div className="topbar-title">
        {project} <span>/ {page}</span>
      </div>

      <div style={{ position: 'relative' }}>
        <div className="env-tag" onClick={() => setEnvOpen(v => !v)}>
          <span className="env-dot" />
          {env}
          <svg style={{ width: 11, height: 11, marginLeft: 2 }} viewBox="0 0 12 12" fill="none">
            <path d="M3 5l3 3 3-3" stroke="currentColor" strokeWidth="1.3" strokeLinecap="round" strokeLinejoin="round" />
          </svg>
        </div>

        {envOpen && (
          <div style={{
            position: 'absolute', top: '100%', right: 0, marginTop: 4,
            background: 'var(--bg3)', border: '1px solid var(--border)',
            borderRadius: 8, overflow: 'hidden', zIndex: 10, minWidth: 120,
          }}>
            {ENVS.map(e => (
              <div
                key={e}
                onClick={() => { onEnvChange?.(e); setEnvOpen(false) }}
                style={{
                  padding: '8px 14px',
                  fontSize: 13,
                  color: e === env ? 'var(--accent)' : 'var(--text2)',
                  cursor: 'pointer',
                  background: e === env ? 'var(--accent2)' : 'transparent',
                }}
                onMouseEnter={el => (el.currentTarget.style.background = e === env ? 'var(--accent2)' : 'var(--bg4)')}
                onMouseLeave={el => (el.currentTarget.style.background = e === env ? 'var(--accent2)' : 'transparent')}
              >
                {e}
              </div>
            ))}
          </div>
        )}
      </div>

      <button className="btn" onClick={onTimeTravelClick}>
        <svg style={{ width: 13, height: 13 }} viewBox="0 0 16 16" fill="none">
          <circle cx="8" cy="8" r="5.5" stroke="currentColor" strokeWidth="1.4" />
          <path d="M8 5v3.5l2 1" stroke="currentColor" strokeWidth="1.3" strokeLinecap="round" />
        </svg>
        Time Travel
      </button>

      <button className="btn btn-primary" onClick={onDeployClick}>
        <svg style={{ width: 13, height: 13 }} viewBox="0 0 16 16" fill="none">
          <path d="M3 13V6l5-4 5 4v7" stroke="white" strokeWidth="1.4" strokeLinejoin="round" />
        </svg>
        Deploy
      </button>
    </div>
  )
}
