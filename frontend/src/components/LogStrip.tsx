import { useState, useEffect } from 'react'
import { apiFetch } from '../api/client'
import type { LogLine } from '../api/types'

interface Props {
  deploymentId: string
  serviceName: string
}

function levelClass(line: string): string {
  if (line.includes('[ERROR]') || line.includes('[FATAL]')) return 'log-level-error'
  if (line.includes('[WARN]')) return 'log-level-warn'
  return 'log-level-info'
}

function extractLevel(line: string): string {
  const m = line.match(/\[(INFO|WARN|ERROR|FATAL|DEBUG)\]/)
  return m ? `[${m[1]}]` : '[INFO]'
}

function extractMsg(line: string): string {
  return line.replace(/\[.*?\]\s*/, '').trim()
}

function formatTime(iso: string): string {
  try {
    const d = new Date(iso)
    return d.toTimeString().slice(0, 8)
  } catch {
    return '--:--:--'
  }
}

export function LogStrip({ deploymentId, serviceName }: Props) {
  const [lines, setLines] = useState<LogLine[]>([])

  useEffect(() => {
    let alive = true
    apiFetch<LogLine[]>(`/api/v1/logs/deployment/${deploymentId}`)
      .then(data => { if (alive) setLines((data ?? []).slice(-5)) })
      .catch(() => {})
    return () => { alive = false }
  }, [deploymentId])

  const hasFailed = lines.some(l => l.line.includes('[ERROR]') || l.line.includes('[FATAL]'))

  return (
    <div className="logs-panel">
      <div className="panel-head">
        <svg style={{ width: 14, height: 14, color: 'var(--text3)' }} viewBox="0 0 16 16" fill="none">
          <path d="M2 4h12M2 8h8M2 12h10" stroke="currentColor" strokeWidth="1.3" strokeLinecap="round" />
        </svg>
        Live logs — {serviceName}
        <span style={{ marginLeft: 'auto', display: 'flex', gap: 6, alignItems: 'center' }}>
          <span style={{
            fontSize: 11, color: hasFailed ? 'var(--red)' : 'var(--green)',
            background: hasFailed ? 'var(--red2)' : 'var(--green2)',
            border: `1px solid ${hasFailed ? 'rgba(240,82,82,0.2)' : 'rgba(35,209,139,0.2)'}`,
            borderRadius: 4, padding: '2px 7px',
          }}>
            {hasFailed ? '● failed' : '● running'}
          </span>
          <button className="btn" style={{ padding: '3px 9px', fontSize: 11 }}>Follow</button>
        </span>
      </div>

      <div className="logs-body">
        {lines.length === 0 && (
          <div className="log-line">
            <span className="log-time">--:--:--</span>
            <span className="log-level-info">[INFO]</span>
            <span className="log-text">Loading logs…</span>
          </div>
        )}
        {lines.map((l, i) => (
          <div className="log-line" key={i}>
            <span className="log-time">{formatTime(l.ts)}</span>
            <span className={levelClass(l.line)}>{extractLevel(l.line)}</span>
            <span className="log-text">{extractMsg(l.line)}</span>
          </div>
        ))}
      </div>
    </div>
  )
}
