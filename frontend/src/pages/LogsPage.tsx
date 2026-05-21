import { useState, useEffect, useRef } from 'react'
import { useServices } from '../hooks/useServices'
import { getToken } from '../api/client'

interface RawLogLine {
  line: string
  level?: string
  ts?: string
}

function levelClass(line: string): string {
  if (line.includes('[ERROR]') || line.includes('[FATAL]')) return 'log-level-error'
  if (line.includes('[WARN]'))  return 'log-level-warn'
  return 'log-level-info'
}

function extractLevel(line: string): string {
  const m = line.match(/\[(INFO|WARN|ERROR|FATAL|DEBUG)\]/)
  return m ? `[${m[1]}]` : '[INFO]'
}

function extractMsg(line: string): string {
  return line.replace(/^\d{4}-\d{2}-\d{2}T[\d:.Z]+\s*/, '').replace(/\[.*?\]\s*/, '').trim() || line
}

function extractTime(line: string): string {
  const m = line.match(/(\d{2}:\d{2}:\d{2})/)
  return m ? m[1] : '--:--:--'
}

export function LogsPage() {
  const { data: services } = useServices()
  const [serviceId, setServiceId] = useState('')
  const [tail, setTail] = useState('100')
  const [lines, setLines] = useState<string[]>([])
  const [loading, setLoading] = useState(false)
  const [error, setError] = useState('')
  const bottomRef = useRef<HTMLDivElement>(null)

  const load = async () => {
    if (!serviceId) return
    setLoading(true)
    setError('')
    try {
      const res = await fetch(
        `/api/v1/logs?service_id=${encodeURIComponent(serviceId)}&tail=${tail}`,
        {
          headers: {
            Authorization: `Bearer ${getToken() ?? ''}`,
          },
        }
      )
      if (!res.ok) throw new Error(`HTTP ${res.status}`)
      const text = await res.text()
      // The logs endpoint returns docker logs directly (plain text or JSON)
      try {
        const json = JSON.parse(text)
        if (Array.isArray(json)) {
          setLines(json.map((l: RawLogLine | string) => typeof l === 'string' ? l : l.line ?? ''))
        } else if (json.lines) {
          setLines(json.lines)
        } else {
          setLines(text.split('\n').filter(Boolean))
        }
      } catch {
        setLines(text.split('\n').filter(Boolean))
      }
    } catch (err) {
      setError((err as Error).message)
    } finally {
      setLoading(false)
    }
  }

  useEffect(() => {
    if (serviceId) load()
  }, [serviceId])

  useEffect(() => {
    bottomRef.current?.scrollIntoView({ behavior: 'smooth' })
  }, [lines])

  return (
    <div className="content">
      <div className="filter-bar">
        <select
          className="input"
          style={{ width: 'auto', minWidth: 180 }}
          value={serviceId}
          onChange={e => setServiceId(e.target.value)}
        >
          <option value="">Select service…</option>
          {services.map(s => <option key={s.id} value={s.id}>{s.name}</option>)}
        </select>
        <select
          className="input"
          style={{ width: 'auto', minWidth: 100 }}
          value={tail}
          onChange={e => setTail(e.target.value)}
        >
          <option value="50">50 lines</option>
          <option value="100">100 lines</option>
          <option value="200">200 lines</option>
          <option value="500">500 lines</option>
        </select>
        <button className="btn" onClick={load} disabled={!serviceId || loading}>
          {loading ? 'Loading…' : 'Refresh'}
        </button>
      </div>

      <div className="logs-panel" style={{ flex: 1 }}>
        <div className="panel-head">
          <LogsIcon />
          {serviceId
            ? `Logs — ${services.find(s => s.id === serviceId)?.name ?? serviceId}`
            : 'Container Logs'}
          <span style={{ marginLeft: 'auto', fontSize: 11, color: 'var(--text3)' }}>
            {lines.length} lines
          </span>
        </div>

        <div className="logs-body" style={{ maxHeight: 'calc(100vh - 260px)', overflow: 'auto' }}>
          {!serviceId && (
            <div className="log-line">
              <span className="log-time">--:--:--</span>
              <span className="log-level-info">[INFO]</span>
              <span className="log-text">Select a service to view logs</span>
            </div>
          )}

          {serviceId && loading && (
            <div className="log-line">
              <span className="log-time">--:--:--</span>
              <span className="log-level-info">[INFO]</span>
              <span className="log-text">Loading…</span>
            </div>
          )}

          {error && (
            <div className="log-line">
              <span className="log-time">--:--:--</span>
              <span className="log-level-error">[ERROR]</span>
              <span className="log-text">{error}</span>
            </div>
          )}

          {!loading && !error && lines.length === 0 && serviceId && (
            <div className="log-line">
              <span className="log-time">--:--:--</span>
              <span className="log-level-info">[INFO]</span>
              <span className="log-text">No logs available for this service</span>
            </div>
          )}

          {lines.map((line, i) => (
            <div className="log-line" key={i}>
              <span className="log-time">{extractTime(line)}</span>
              <span className={levelClass(line)}>{extractLevel(line)}</span>
              <span className="log-text">{extractMsg(line)}</span>
            </div>
          ))}
          <div ref={bottomRef} />
        </div>
      </div>
    </div>
  )
}

function LogsIcon() {
  return <svg style={{ width: 14, height: 14, color: 'var(--text3)' }} viewBox="0 0 16 16" fill="none"><path d="M2 4h12M2 8h8M2 12h10" stroke="currentColor" strokeWidth="1.3" strokeLinecap="round" /></svg>
}
