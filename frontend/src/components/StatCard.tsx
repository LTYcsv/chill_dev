interface Props {
  label: string
  value: string | number
  unit?: string
  meta?: string
  metaUp?: boolean
  metaDown?: boolean
}

export function StatCard({ label, value, unit, meta, metaUp, metaDown }: Props) {
  return (
    <div className="stat-card">
      <div className="stat-label">{label}</div>
      <div className="stat-value">
        {value}
        {unit && <span className="unit">{unit}</span>}
      </div>
      {meta && (
        <div className="stat-meta">
          <span className={metaUp ? 'up' : metaDown ? 'down' : undefined}>{meta}</span>
        </div>
      )}
    </div>
  )
}
