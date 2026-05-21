import { useState } from 'react'
import { Sidebar } from '../components/Sidebar'
import { Topbar } from '../components/Topbar'
import { StatCard } from '../components/StatCard'
import { ServiceRow } from '../components/ServiceRow'
import { InfraGraph } from '../components/InfraGraph'
import { ActivityFeed } from '../components/ActivityFeed'
import { LogStrip } from '../components/LogStrip'
import { useHealth } from '../hooks/useHealth'
import { useServices } from '../hooks/useServices'
import { useDeployments } from '../hooks/useDeployments'
import { useGraph } from '../hooks/useGraph'
import { ServicesPage } from './ServicesPage'
import { DeploymentsPage } from './DeploymentsPage'
import { ActivityPage } from './ActivityPage'
import { GraphPage } from './GraphPage'
import { BlastRadiusPage } from './BlastRadiusPage'
import { LogsPage } from './LogsPage'
import { SecretsPage } from './SecretsPage'
import { BuildsPage } from './BuildsPage'
import type { AuthUser } from '../api/types'

const PAGE_LABELS: Record<string, string> = {
  dashboard: 'dashboard',
  activity: 'activity',
  services: 'services',
  deployments: 'deployments',
  graph: 'infra graph',
  'blast-radius': 'blast radius',
  logs: 'logs',
  secrets: 'secrets',
  builds: 'builds',
}

interface Props {
  user: AuthUser | null
}

export function Dashboard({ user }: Props) {
  const [activePage, setActivePage] = useState('dashboard')
  const [env, setEnv] = useState('production')

  const renderPage = () => {
    switch (activePage) {
      case 'services':      return <ServicesPage />
      case 'deployments':   return <DeploymentsPage />
      case 'activity':      return <ActivityPage />
      case 'graph':         return <GraphPage env={env} onEnvChange={setEnv} />
      case 'blast-radius':  return <BlastRadiusPage env={env} />
      case 'logs':          return <LogsPage />
      case 'secrets':       return <SecretsPage />
      case 'builds':        return <BuildsPage />
      default:              return <DashboardHome env={env} onNavigate={setActivePage} />
    }
  }

  return (
    <>
      <Sidebar
        activePage={activePage}
        onNavigate={setActivePage}
        userName={user?.email ?? 'User'}
        userRole={user?.role ?? 'developer'}
      />
      <div className="main">
        <Topbar
          env={env}
          page={PAGE_LABELS[activePage] ?? activePage}
          onEnvChange={setEnv}
          onTimeTravelClick={() => setActivePage('graph')}
          onDeployClick={() => setActivePage('services')}
        />
        {renderPage()}
      </div>
    </>
  )
}

// ── Dashboard home (overview) ─────────────────────────────────

function isToday(iso: string): boolean {
  const d = new Date(iso)
  const now = new Date()
  return d.getDate() === now.getDate() &&
    d.getMonth() === now.getMonth() &&
    d.getFullYear() === now.getFullYear()
}

interface HomeProps {
  env: string
  onNavigate: (page: string) => void
}

function DashboardHome({ env, onNavigate }: HomeProps) {
  const health = useHealth()
  const { data: services, loading: svcLoading } = useServices()
  const { data: deployments } = useDeployments()
  const graph = useGraph('default', env)

  const runningCount = Object.values(health?.services ?? {}).filter(s => s === 'ok').length
  const totalSvcs = Object.keys(health?.services ?? {}).length || services.length

  const todayDeploys = deployments.filter(d => isToday(d.created_at)).length
  const queuedBuilds = deployments.filter(d => d.status === 'building' || d.status === 'pending').length

  const failedDeployment = deployments.find(d => d.status === 'failed')
  const failedServiceName = failedDeployment
    ? services.find(s => s.id === failedDeployment.service_id)?.name ?? 'service'
    : null

  return (
    <div className="content">
      <div className="stats-row">
        <StatCard
          label="Services running"
          value={runningCount || totalSvcs}
          meta={health?.status === 'degraded' ? '⚠ degraded' : `${totalSvcs} total`}
          metaDown={health?.status === 'degraded'}
        />
        <StatCard
          label="Uptime (30d avg)"
          value="99.7"
          unit="%"
          meta="↑ 0.3% vs prev month"
          metaUp
        />
        <StatCard
          label="Deployments today"
          value={todayDeploys}
          meta={queuedBuilds > 0 ? `${queuedBuilds} in queue` : 'no queue'}
        />
        <StatCard
          label="Avg latency"
          value="—"
          unit="ms"
          meta="no metrics yet"
        />
      </div>

      <div className="two-col">
        <div className="panel">
          <div className="panel-head">
            <svg style={{ width: 14, height: 14, color: 'var(--text3)' }} viewBox="0 0 16 16" fill="none">
              <rect x="1.5" y="4" width="5" height="4" rx="1.2" stroke="currentColor" strokeWidth="1.3" />
              <rect x="9.5" y="2" width="5" height="4" rx="1.2" stroke="currentColor" strokeWidth="1.3" />
              <rect x="9.5" y="9" width="5" height="4" rx="1.2" stroke="currentColor" strokeWidth="1.3" />
              <path d="M6.5 6H8a1 1 0 011 1v1M8 4V2.5" stroke="currentColor" strokeWidth="1.2" strokeLinecap="round" />
            </svg>
            Services
            <span className="count">{services.length} total</span>
            <button
              className="btn"
              style={{ marginLeft: 'auto', padding: '3px 10px', fontSize: 11 }}
              onClick={() => onNavigate('services')}
            >
              View all
            </button>
          </div>

          {svcLoading && <div className="empty-row">Loading…</div>}

          {!svcLoading && services.length === 0 && (
            <div className="empty-row">
              No services registered.{' '}
              <span
                style={{ color: 'var(--accent)', cursor: 'pointer' }}
                onClick={() => onNavigate('services')}
              >
                Register one →
              </span>
            </div>
          )}

          {services.slice(0, 6).map(svc => {
            const lastDeploy = deployments.find(d => d.service_id === svc.id)
            return (
              <ServiceRow
                key={svc.id}
                svc={svc}
                health={health}
                lastDeployStatus={lastDeploy?.status}
                lastCommit={lastDeploy?.git_commit}
              />
            )
          })}
        </div>

        <div style={{ display: 'flex', flexDirection: 'column', gap: 12 }}>
          <InfraGraph
            graph={graph}
            onViewFull={() => onNavigate('graph')}
            onBlastRadius={() => onNavigate('blast-radius')}
          />
          <ActivityFeed deployments={deployments} />
        </div>
      </div>

      {failedDeployment && failedServiceName && (
        <LogStrip
          deploymentId={failedDeployment.id}
          serviceName={failedServiceName}
        />
      )}
    </div>
  )
}
