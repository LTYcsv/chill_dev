import type { InfraGraph as InfraGraphData, GraphNode, GraphEdge } from '../api/types'

const W = 280
const H = 180
const NODE_W = 70
const NODE_H = 30

type Col = 0 | 1 | 2

function column(node: GraphNode): Col {
  const n = node.name.toLowerCase()
  if (node.type === 'database' || node.type === 'cache' || node.type === 'queue') return 2
  if (n.includes('gateway') || n.includes('proxy')) return 0
  return 1
}

function nodeColor(node: GraphNode): { fill: string; stroke: string; text: string } {
  switch (node.status) {
    case 'running':  return { fill: '#18181c', stroke: 'rgba(35,209,139,0.5)', text: '#23d18b' }
    case 'degraded': return { fill: '#1e1c14', stroke: 'rgba(244,162,74,0.5)',  text: '#f4a24a' }
    case 'down':     return { fill: '#1e1218', stroke: 'rgba(240,82,82,0.8)',   text: '#f05252' }
    default:         return { fill: '#18181c', stroke: 'rgba(255,255,255,0.08)', text: '#8a8a96' }
  }
}

function layoutNodes(nodes: GraphNode[]): Map<string, { x: number; y: number }> {
  const cols: GraphNode[][] = [[], [], []]
  nodes.forEach(n => cols[column(n)].push(n))

  const positions = new Map<string, { x: number; y: number }>()
  const xPositions = [44, 140, 236]

  cols.forEach((col, ci) => {
    const count = col.length
    const spacing = H / (count + 1)
    col.forEach((n, i) => {
      positions.set(n.id, { x: xPositions[ci], y: spacing * (i + 1) })
    })
  })

  return positions
}

function edgeColor(edge: GraphEdge): string {
  if ((edge.error_pct ?? 0) > 5) return 'rgba(240,82,82,0.4)'
  if ((edge.p99_ms ?? 0) > 500) return 'rgba(244,162,74,0.3)'
  return 'rgba(91,106,249,0.3)'
}

interface Props {
  graph: InfraGraphData | null
  onViewFull?: () => void
  onBlastRadius?: () => void
}

export function InfraGraph({ graph, onViewFull, onBlastRadius }: Props) {
  const nodes = graph?.nodes ?? []
  const edges = graph?.edges ?? []
  const positions = layoutNodes(nodes)

  return (
    <div className="panel graph-preview">
      <div className="panel-head">
        <svg style={{ width: 14, height: 14, color: 'var(--text3)' }} viewBox="0 0 16 16" fill="none">
          <circle cx="4" cy="8" r="2.2" stroke="currentColor" strokeWidth="1.3" />
          <circle cx="12" cy="4" r="2.2" stroke="currentColor" strokeWidth="1.3" />
          <circle cx="12" cy="12" r="2.2" stroke="currentColor" strokeWidth="1.3" />
          <path d="M6.2 7l3.8-2M6.2 9l3.8 2" stroke="currentColor" strokeWidth="1.2" strokeLinecap="round" />
        </svg>
        Infra Graph
        <span className="count" style={{ marginLeft: 'auto' }}>
          {graph ? 'live' : '—'}
        </span>
      </div>

      <div className="graph-canvas">
        {nodes.length === 0 ? (
          <EmptyGraph />
        ) : (
          <svg className="graph-svg" viewBox={`0 0 ${W} ${H}`}>
            {edges.map(edge => {
              const from = positions.get(edge.from)
              const to = positions.get(edge.to)
              if (!from || !to) return null
              return (
                <line
                  key={edge.id}
                  x1={from.x + NODE_W / 2}
                  y1={from.y}
                  x2={to.x - NODE_W / 2}
                  y2={to.y}
                  stroke={edgeColor(edge)}
                  strokeWidth="1"
                />
              )
            })}

            {nodes.map(node => {
              const pos = positions.get(node.id)
              if (!pos) return null
              const { fill, stroke, text } = nodeColor(node)
              const isFailed = node.status === 'down'
              const rx = pos.x - NODE_W / 2
              const ry = pos.y - NODE_H / 2

              return (
                <g key={node.id}>
                  <rect
                    className={isFailed ? 'fail-node' : undefined}
                    x={rx} y={ry}
                    width={NODE_W} height={NODE_H}
                    rx="7"
                    fill={fill}
                    stroke={stroke}
                    strokeWidth="1"
                  />
                  <text
                    x={pos.x} y={ry + 11}
                    textAnchor="middle"
                    fill={text}
                    fontSize="8.5"
                    fontFamily="DM Mono"
                    opacity="0.8"
                  >
                    {node.name.length > 10 ? node.name.slice(0, 9) + '…' : node.name}
                  </text>
                  <text
                    x={pos.x} y={ry + 23}
                    textAnchor="middle"
                    fill={text}
                    fontSize="9.5"
                    fontWeight="500"
                    fontFamily="DM Sans"
                  >
                    {node.status === 'down' ? 'down' : node.metadata?.port ? `:${node.metadata.port}` : node.type}
                  </text>
                </g>
              )
            })}
          </svg>
        )}
      </div>

      <div style={{ padding: '0 16px 12px', display: 'flex', gap: 8 }}>
        <button className="btn" style={{ flex: 1, justifyContent: 'center', fontSize: 12, padding: 6 }} onClick={onViewFull}>
          View full graph
        </button>
        <button className="btn btn-danger" style={{ fontSize: 12, padding: '6px 10px' }} onClick={onBlastRadius}>
          Blast radius
        </button>
      </div>
    </div>
  )
}

function EmptyGraph() {
  return (
    <svg className="graph-svg" viewBox={`0 0 ${W} ${H}`}>
      <text x={W / 2} y={H / 2} textAnchor="middle" fill="var(--text3)" fontSize="12" fontFamily="DM Sans">
        No graph data
      </text>
      <text x={W / 2} y={H / 2 + 18} textAnchor="middle" fill="var(--text3)" fontSize="10" fontFamily="DM Mono">
        POST /api/v1/graph/nodes to populate
      </text>
    </svg>
  )
}
