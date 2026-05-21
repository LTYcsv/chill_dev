interface Props {
  activePage: string
  onNavigate: (page: string) => void
  userName?: string
  userRole?: string
}

const NAV_ITEMS = [
  {
    section: 'Overview',
    items: [
      { id: 'dashboard', label: 'Dashboard', icon: <DashboardIcon /> },
      { id: 'activity', label: 'Activity', icon: <ActivityIcon /> },
    ],
  },
  {
    section: 'Infra',
    items: [
      { id: 'services', label: 'Services', icon: <ServicesIcon />, dot: 'green' },
      { id: 'deployments', label: 'Deployments', icon: <DeployIcon /> },
      { id: 'graph', label: 'Infra Graph', icon: <GraphIcon /> },
      { id: 'blast-radius', label: 'Blast Radius', icon: <BlastIcon /> },
    ],
  },
  {
    section: 'Dev',
    items: [
      { id: 'logs', label: 'Logs', icon: <LogsIcon /> },
      { id: 'secrets', label: 'Secrets', icon: <SecretsIcon /> },
      { id: 'builds', label: 'Builds', icon: <BuildsIcon /> },
    ],
  },
]

export function Sidebar({ activePage, onNavigate, userName = 'User', userRole = 'Developer' }: Props) {
  const initials = userName
    .split(' ')
    .map(w => w[0])
    .join('')
    .slice(0, 2)
    .toUpperCase()

  return (
    <div className="sidebar">
      <div className="logo">
        <div className="logo-icon">
          <svg viewBox="0 0 16 16" fill="none">
            <rect x="1" y="7" width="6" height="6" rx="1.5" fill="white" opacity="0.9" />
            <rect x="9" y="1" width="6" height="6" rx="1.5" fill="white" opacity="0.6" />
            <rect x="9" y="9" width="6" height="6" rx="1.5" fill="white" opacity="0.4" />
          </svg>
        </div>
        <span className="logo-text">DevPlatform</span>
      </div>

      <div className="nav-section">
        {NAV_ITEMS.map(({ section, items }) => (
          <div key={section}>
            <div className="nav-label">{section}</div>
            {items.map(({ id, label, icon, dot }) => (
              <div
                key={id}
                className={`nav-item${activePage === id ? ' active' : ''}`}
                onClick={() => onNavigate(id)}
              >
                <span className="nav-icon">{icon}</span>
                {label}
                {dot && <span className="dot" style={{ background: 'var(--green)' }} />}
              </div>
            ))}
            <div className="nav-sep" />
          </div>
        ))}
      </div>

      <div className="nav-bottom">
        <div className="user-row">
          <div className="avatar">{initials}</div>
          <div className="user-info">
            <div className="user-name">{userName}</div>
            <div className="user-role">{userRole}</div>
          </div>
          <svg style={{ width: 14, height: 14, color: 'var(--text3)', flexShrink: 0 }} viewBox="0 0 16 16" fill="none">
            <path d="M4 7l4-4 4 4M4 10l4 4 4-4" stroke="currentColor" strokeWidth="1.3" strokeLinecap="round" strokeLinejoin="round" />
          </svg>
        </div>
      </div>
    </div>
  )
}

function DashboardIcon() {
  return <svg viewBox="0 0 16 16" fill="none"><rect x="1" y="1" width="6" height="6" rx="1.5" fill="currentColor" /><rect x="9" y="1" width="6" height="6" rx="1.5" fill="currentColor" opacity="0.4" /><rect x="1" y="9" width="6" height="6" rx="1.5" fill="currentColor" opacity="0.4" /><rect x="9" y="9" width="6" height="6" rx="1.5" fill="currentColor" opacity="0.4" /></svg>
}

function ActivityIcon() {
  return <svg viewBox="0 0 16 16" fill="none"><circle cx="8" cy="8" r="5.5" stroke="currentColor" strokeWidth="1.5" /><path d="M8 5v3.5l2 1" stroke="currentColor" strokeWidth="1.3" strokeLinecap="round" /></svg>
}

function ServicesIcon() {
  return <svg viewBox="0 0 16 16" fill="none"><rect x="1.5" y="4" width="5" height="4" rx="1.2" stroke="currentColor" strokeWidth="1.3" /><rect x="9.5" y="2" width="5" height="4" rx="1.2" stroke="currentColor" strokeWidth="1.3" /><rect x="9.5" y="9" width="5" height="4" rx="1.2" stroke="currentColor" strokeWidth="1.3" /><path d="M6.5 6H8a1 1 0 011 1v1M8 4V2.5" stroke="currentColor" strokeWidth="1.2" strokeLinecap="round" /></svg>
}

function DeployIcon() {
  return <svg viewBox="0 0 16 16" fill="none"><path d="M3 13V6l5-4 5 4v7" stroke="currentColor" strokeWidth="1.3" strokeLinejoin="round" /><rect x="6" y="9" width="4" height="4" rx="0.8" stroke="currentColor" strokeWidth="1.2" /></svg>
}

function GraphIcon() {
  return <svg viewBox="0 0 16 16" fill="none"><circle cx="4" cy="8" r="2.2" stroke="currentColor" strokeWidth="1.3" /><circle cx="12" cy="4" r="2.2" stroke="currentColor" strokeWidth="1.3" /><circle cx="12" cy="12" r="2.2" stroke="currentColor" strokeWidth="1.3" /><path d="M6.2 7l3.8-2M6.2 9l3.8 2" stroke="currentColor" strokeWidth="1.2" strokeLinecap="round" /></svg>
}

function BlastIcon() {
  return <svg viewBox="0 0 16 16" fill="none"><circle cx="8" cy="8" r="3" stroke="currentColor" strokeWidth="1.3" /><path d="M8 1v2M8 13v2M1 8h2M13 8h2" stroke="currentColor" strokeWidth="1.3" strokeLinecap="round" /></svg>
}

function LogsIcon() {
  return <svg viewBox="0 0 16 16" fill="none"><path d="M2 4h12M2 8h8M2 12h10" stroke="currentColor" strokeWidth="1.3" strokeLinecap="round" /></svg>
}

function SecretsIcon() {
  return <svg viewBox="0 0 16 16" fill="none"><rect x="2" y="2" width="12" height="12" rx="2" stroke="currentColor" strokeWidth="1.3" /><path d="M5 8h6M8 5v6" stroke="currentColor" strokeWidth="1.3" strokeLinecap="round" /></svg>
}

function BuildsIcon() {
  return <svg viewBox="0 0 16 16" fill="none"><path d="M8 2L2 5v4c0 3 3 5 6 5s6-2 6-5V5L8 2z" stroke="currentColor" strokeWidth="1.3" strokeLinejoin="round" /></svg>
}
