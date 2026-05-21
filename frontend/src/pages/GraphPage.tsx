import { useState } from 'react'
import { useGraph } from '../hooks/useGraph'
import type { GraphNode, GraphEdge, InfraGraph } from '../api/types'

const W = 680
const H = 400
const NODE_W = 90
const NODE_H = 36

type Col = 0 | 1 | 2

function column(node: GraphNode): Col {
  const n = node.name.toLowerCase()
  if (node.type === 'database' || node.type === 'cache' || node.type === 'queue') return 2
  if (n.includes('gateway') || n.includes('proxy')) return 0
  return 1
}

function nodeColor(node: GraphNode) {
  switch (node.status) {
    case 'running':  return { fill: '#18181c', stroke: 'rgba(35,209,139,0.5)',  text: '#23d18b' }
    case 'degraded': return { fill: '#1e1c14', stroke: 'rgba(244,162,74,0.5)', text: '#f4a24a' }
    case 'down':     return { fill: '#1e1218', stroke: 'rgba(240,82,82,0.8)',   text: '#f05252' }
    default:         return { fill: '#18181c', stroke: 'rgba(255,255,255,0.08)', text: '#8a8a96' }
  }
}

function edgeColor(edge: GraphEdge): string {
  if ((edge.error_pct ?? 0) > 5) return 'rgba(240,82,82,0.4)'
  if ((edge.p99_ms ?? 0) > 500) return 'rgba(244,162,74,0.35)'
  return 'rgba(91,106,249,0.3)'
}

function layoutNodes(nodes: GraphNode[]): Map<string, { x: number; y: number }> {
  const cols: GraphNode[][] = [[], [], []]
  nodes.forEach(n => cols[column(n)].push(n))
  const positions = new Map<string, { x: number; y: number }>()
  const xPositions = [W * 0.15, W * 0.5, W * 0.85]
  cols.forEach((col, ci) => {
    const spacing = H / (col.length + 1)
    col.forEach((n, i) => positions.set(n.id, { x: xPositions[ci], y: spacing * (i + 1) }))
  })
  return positions
}

interface Props {
  env: string
  onEnvChange: (env: string) => void
}

const ENVS = ['production', 'staging', 'development']

export function GraphPage({ env, onEnvChange }: Props) {
  const graph = useGraph('default', env)
  const [selectedId, setSelectedId] = useState<string | null>(null)
  const [envOpen, setEnvOpen] = useState(false)

  const nodes = graph?.nodes ?? []
  const edges = graph?.edges ?? []
  const positions = layoutNodes(nodes)
  const selected = nodes.find(n => n.id === selectedId) ?? null

  return (
    <div className="content">
      <div className="panel" style={{ overflow: 'visible' }}>
        <div className="panel-head">
          <GraphIcon />
          Infra Graph
          <span className="count">{nodes.length} nodes · {edges.length} edges</span>

          <div style={{ marginLeft: 'auto', position: 'relative' }}>
            <div className="env-tag" onClick={() => setEnvOpen(v => !v)}>
              <span className="env-dot" />
              {env}
              <svg style={{ width: 11, height: 11, marginLeft: 2 }} viewBox="0 0 12 12" fill="none">
                <path d="M3 5l3 3 3-3" stroke="currentColor" strokeWidth="1.3" strokeLinecap="round" strokeLinejoin="round" />
              </svg>
            </div>
            {envOpen && (
              <div style={{ position: 'absolute', top: '100%', right: 0, marginTop: 4, background: 'var(--bg3)', border: '1px solid var(--border)', borderRadius: 8, overflow: 'hidden', zIndex: 10, minWidth: 130 }}>
                {ENVS.map(e => (
                  <div
                    key={e}
                    onClick={() => { onEnvChange(e); setEnvOpen(false) }}
                    style={{ padding: '8px 14px', fontSize: 13, color: e === env ? 'var(--accent)' : 'var(--text2)', cursor: 'pointer', background: e === env ? 'var(--accent2)' : 'transparent' }}
                  >
                    {e}
                  </div>
                ))}
              </div>
            )}
          </div>
        </div>

        <div style={{ display: 'flex' }}>
          <div className="graph-canvas" style={{ flex: 1, minHeight: 420, padding: 24 }}>
            {nodes.length === 0 ? (
              <EmptyGraph />
            ) : (
              <svg
                className="graph-svg"
                viewBox={`0 0 ${W} ${H}`}
                style={{ cursor: 'default' }}
              >
                {edges.map(edge => {
                  const from = positions.get(edge.from)
                  const to = positions.get(edge.to)
                  if (!from || !to) return null
                  const color = edgeColor(edge)
                  const mx = (from.x + NODE_W / 2 + to.x - NODE_W / 2) / 2
                  return (
                    <g key={edge.id}>
                      <path
                        d={`M${from.x + NODE_W / 2},${from.y} C${mx},${from.y} ${mx},${to.y} ${to.x - NODE_W / 2},${to.y}`}
                        stroke={color}
                        strokeWidth="1.2"
                        fill="none"
                      />
                      {(edge.rps != null || edge.p99_ms != null) && (
                        <text x={mx} y={(from.y + to.y) / 2 - 4} textAnchor="middle" fill={color} fontSize="8" fontFamily="DM Mono" opacity="0.9">
                          {edge.rps != null ? `${edge.rps}rps` : ''}{edge.p99_ms != null ? ` p99:${edge.p99_ms}ms` : ''}
                        </text>
                      )}
                    </g>
                  )
                })}

                {nodes.map(node => {
                  const pos = positions.get(node.id)
                  if (!pos) return null
                  const { fill, stroke, text } = nodeColor(node)
                  const isSelected = node.id === selectedId
                  const isFailed = node.status === 'down'
                  const rx = pos.x - NODE_W / 2
                  const ry = pos.y - NODE_H / 2

                  return (
                    <g key={node.id} style={{ cursor: 'pointer' }} onClick={() => setSelectedId(isSelected ? null : node.id)}>
                      <rect
                        className={isFailed ? 'fail-node' : undefined}
                        x={rx} y={ry}
                        width={NODE_W} height={NODE_H}
                        rx="8"
                        fill={fill}
                        stroke={isSelected ? 'var(--accent)' : stroke}
                        strokeWidth={isSelected ? 1.5 : 1}
                      />
                      <text x={pos.x} y={ry + 13} textAnchor="middle" fill={text} fontSize="9" fontFamily="DM Mono" opacity="0.8">
                        {node.name.length > 12 ? node.name.slice(0, 11) + '…' : node.name}
                      </text>
                      <text x={pos.x} y={ry + 27} textAnchor="middle" fill={text} fontSize="10.5" fontWeight="500" fontFamily="DM Sans">
                        {node.status === 'down' ? 'down' : node.metadata?.port ? `:${node.metadata.port}` : node.type}
                      </text>
                    </g>
                  )
                })}
              </svg>
            )}
          </div>

          {selected && <NodeDetail node={selected} graph={graph} onClose={() => setSelectedId(null)} />}
        </div>
      </div>
    </div>
  )
}

function NodeDetail({ node, graph, onClose }: { node: GraphNode; graph: InfraGraph | null; onClose: () => void }) {
  const { fill: _, stroke, text } = {
    fill: '', stroke: node.status === 'running' ? 'rgba(35,209,139,0.5)' : node.status === 'down' ? 'rgba(240,82,82,0.5)' : 'rgba(255,255,255,0.1)',
    text: node.status === 'running' ? 'var(--green)' : node.status === 'down' ? 'var(--red)' : 'var(--text3)',
  }

  const inEdges = graph?.edges.filter(e => e.to === node.id) ?? []
  const outEdges = graph?.edges.filter(e => e.from === node.id) ?? []
  const nodeById = (id: string) => graph?.nodes.find(n => n.id === id)

  return (
    <div style={{ width: 220, borderLeft: '1px solid var(--border)', padding: '16px 14px', flexShrink: 0, display: 'flex', flexDirection: 'column', gap: 10 }}>
      <div style={{ display: 'flex', alignItems: 'center', justifyContent: 'space-between' }}>
        <span style={{ fontSize: 13, fontWeight: 600, color: 'var(--text)' }}>{node.name}</span>
        <button onClick={onClose} style={{ background: 'none', border: 'none', cursor: 'pointer', color: 'var(--text3)', fontSize: 14, padding: 0 }}>✕</button>
      </div>

      <div style={{ display: 'flex', gap: 6, flexWrap: 'wrap' }}>
        <span className="svc-env">{node.type}</span>
        <span className="svc-env">{node.environment}</span>
        <span style={{ fontSize: 11, padding: '2px 7px', borderRadius: 5, border: `1px solid ${stroke}`, color: text }}>
          ● {node.status}
        </span>
      </div>

      {node.metadata && Object.keys(node.metadata).length > 0 && (
        <div style={{ display: 'flex', flexDirection: 'column', gap: 4 }}>
          <div className="nav-label">Metadata</div>
          {Object.entries(node.metadata).map(([k, v]) => (
            <div key={k} style={{ display: 'flex', justifyContent: 'space-between', fontSize: 11.5 }}>
              <span style={{ color: 'var(--text3)' }}>{k}</span>
              <span style={{ color: 'var(--text2)', fontFamily: 'var(--mono)' }}>{v}</span>
            </div>
          ))}
        </div>
      )}

      {inEdges.length > 0 && (
        <div>
          <div className="nav-label" style={{ marginBottom: 4 }}>Incoming</div>
          {inEdges.map(e => (
            <div key={e.id} style={{ fontSize: 11.5, color: 'var(--text2)', padding: '2px 0' }}>
              ← {nodeById(e.from)?.name ?? e.from}
              {e.rps != null && <span style={{ color: 'var(--text3)' }}> {e.rps}rps</span>}
            </div>
          ))}
        </div>
      )}

      {outEdges.length > 0 && (
        <div>
          <div className="nav-label" style={{ marginBottom: 4 }}>Outgoing</div>
          {outEdges.map(e => (
            <div key={e.id} style={{ fontSize: 11.5, color: 'var(--text2)', padding: '2px 0' }}>
              → {nodeById(e.to)?.name ?? e.to}
              {e.p99_ms != null && <span style={{ color: 'var(--text3)' }}> p99:{e.p99_ms}ms</span>}
            </div>
          ))}
        </div>
      )}
    </div>
  )
}

function EmptyGraph() {
  return (
    <svg className="graph-svg" viewBox={`0 0 ${W} ${H}`}>
      <text x={W / 2} y={H / 2} textAnchor="middle" fill="var(--text3)" fontSize="13" fontFamily="DM Sans">
        No graph data
      </text>
      <text x={W / 2} y={H / 2 + 20} textAnchor="middle" fill="var(--text3)" fontSize="10.5" fontFamily="DM Mono">
        POST /api/v1/graph/nodes to populate
      </text>
    </svg>
  )
}

function GraphIcon() {
  return <svg style={{ width: 14, height: 14, color: 'var(--text3)' }} viewBox="0 0 16 16" fill="none"><circle cx="4" cy="8" r="2.2" stroke="currentColor" strokeWidth="1.3" /><circle cx="12" cy="4" r="2.2" stroke="currentColor" strokeWidth="1.3" /><circle cx="12" cy="12" r="2.2" stroke="currentColor" strokeWidth="1.3" /><path d="M6.2 7l3.8-2M6.2 9l3.8 2" stroke="currentColor" strokeWidth="1.2" strokeLinecap="round" /></svg>
}
