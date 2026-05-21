import { useState } from 'react'
import { useGraph } from '../hooks/useGraph'
import { apiFetch } from '../api/client'
import type { BlastRadiusResult } from '../api/types'

const DEPTH_COLORS = ['var(--red)', 'var(--amber)', 'var(--accent)', 'var(--text2)']

interface Props {
  env: string
}

export function BlastRadiusPage({ env }: Props) {
  const graph = useGraph('default', env)
  const nodes = graph?.nodes ?? []
  const [selectedNodeId, setSelectedNodeId] = useState('')
  const [result, setResult] = useState<BlastRadiusResult | null>(null)
  const [loading, setLoading] = useState(false)
  const [error, setError] = useState('')

  const analyze = async () => {
    if (!selectedNodeId) return
    setLoading(true)
    setError('')
    setResult(null)
    try {
      const r = await apiFetch<BlastRadiusResult>(
        `/api/v1/graph/blast-radius/${selectedNodeId}?project_id=default&env=${encodeURIComponent(env)}`
      )
      setResult(r)
    } catch (err) {
      setError((err as Error).message)
    } finally {
      setLoading(false)
    }
  }

  const severityClass = (s: string) =>
    s === 'high' ? 'blast-high' : s === 'medium' ? 'blast-med' : 'blast-low'

  return (
    <div className="content">
      <div className="panel">
        <div className="panel-head">
          <BlastIcon />
          Blast Radius
        </div>

        <div style={{ padding: '16px 18px', display: 'flex', gap: 10, alignItems: 'flex-end', borderBottom: '1px solid var(--border)' }}>
          <div style={{ flex: 1 }}>
            <div className="form-label" style={{ marginBottom: 6 }}>Select node to analyze</div>
            <select
              className="input"
              value={selectedNodeId}
              onChange={e => { setSelectedNodeId(e.target.value); setResult(null) }}
            >
              <option value="">— pick a node —</option>
              {nodes.map(n => (
                <option key={n.id} value={n.id}>
                  {n.name} ({n.type} · {n.status})
                </option>
              ))}
            </select>
          </div>
          <button
            className="btn btn-primary"
            style={{ padding: '9px 18px' }}
            onClick={analyze}
            disabled={!selectedNodeId || loading}
          >
            {loading ? 'Analyzing…' : 'Analyze'}
          </button>
        </div>

        {nodes.length === 0 && (
          <div className="empty-row">No graph nodes found. Add nodes via <span style={{ color: 'var(--accent)' }}>POST /api/v1/graph/nodes</span></div>
        )}

        {error && (
          <div className="empty-row" style={{ color: 'var(--red)' }}>{error}</div>
        )}

        {result && (
          <div style={{ padding: '18px 18px' }}>
            <div style={{ display: 'flex', alignItems: 'center', gap: 12, marginBottom: 16 }}>
              <div>
                <div style={{ fontSize: 15, fontWeight: 600 }}>{result.node_name}</div>
                <div style={{ fontSize: 12, color: 'var(--text3)', marginTop: 2 }}>
                  {result.total_affected} node{result.total_affected !== 1 ? 's' : ''} affected
                </div>
              </div>
              <span className={`blast-badge ${severityClass(result.severity)}`} style={{ marginLeft: 'auto' }}>
                ● {result.severity}
              </span>
            </div>

            {result.affected_nodes.length === 0 ? (
              <div style={{ fontSize: 12.5, color: 'var(--text3)' }}>No downstream dependencies found.</div>
            ) : (
              <div style={{ display: 'flex', flexDirection: 'column', gap: 1 }}>
                <div className="nav-label" style={{ marginBottom: 8 }}>Affected nodes (by depth)</div>
                {result.affected_nodes.map(n => (
                  <div key={n.id} className="blast-row">
                    <span
                      className="depth-badge"
                      style={{ background: `color-mix(in srgb, ${DEPTH_COLORS[Math.min(n.depth - 1, 3)]} 15%, transparent)`, color: DEPTH_COLORS[Math.min(n.depth - 1, 3)], borderColor: `color-mix(in srgb, ${DEPTH_COLORS[Math.min(n.depth - 1, 3)]} 30%, transparent)` }}
                    >
                      d{n.depth}
                    </span>
                    <span style={{ flex: 1, fontSize: 13, fontWeight: 500 }}>{n.name}</span>
                    <span className="svc-env">{n.type}</span>
                  </div>
                ))}
              </div>
            )}
          </div>
        )}
      </div>
    </div>
  )
}

function BlastIcon() {
  return <svg style={{ width: 14, height: 14, color: 'var(--text3)' }} viewBox="0 0 16 16" fill="none"><circle cx="8" cy="8" r="3" stroke="currentColor" strokeWidth="1.3" /><path d="M8 1v2M8 13v2M1 8h2M13 8h2" stroke="currentColor" strokeWidth="1.3" strokeLinecap="round" /></svg>
}
