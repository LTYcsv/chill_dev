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
import { useState } from 'react'

interface Props {
  userName?: string
  userRole?: string
}

function isToday(iso: string): boolean {
  const d = new Date(iso)
  const now = new Date()
  return d.getDate() === now.getDate() && d.getMonth() === now.getMonth() && d.getFullYear() === now.getFullYear()
}

export function Dashboard({ userName, userRole }: Props) {
  const [activePage, setActivePage] = useState('dashboard')
  const [env, setEnv] = useState('production')

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
    <>
      <Sidebar
        activePage={activePage}
        onNavigate={setActivePage}
        userName={userName}
        userRole={userRole}
      />

      <div className="main">
        <Topbar env={env} onEnvChange={setEnv} />

        <div className="content">
          {/* Stats */}
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

          {/* Two-col */}
          <div className="two-col">
            {/* Services list */}
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
              </div>

              {svcLoading && <div className="empty-row">Loading…</div>}

              {!svcLoading && services.length === 0 && (
                <div className="empty-row">
                  No services registered.{' '}
                  <span style={{ color: 'var(--accent)' }}>POST /api/v1/services</span>
                </div>
              )}

              {services.map(svc => {
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

            {/* Right col */}
            <div style={{ display: 'flex', flexDirection: 'column', gap: 12 }}>
              <InfraGraph graph={graph} />
              <ActivityFeed deployments={deployments} />
            </div>
          </div>

          {/* Log strip — only when there's a failed deployment */}
          {failedDeployment && failedServiceName && (
            <LogStrip
              deploymentId={failedDeployment.id}
              serviceName={failedServiceName}
            />
          )}
        </div>
      </div>
    </>
  )
}
