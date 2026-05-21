import { useState, useCallback } from 'react'
import { useServices } from '../hooks/useServices'
import { useDeployments } from '../hooks/useDeployments'
import { useHealth } from '../hooks/useHealth'
import { ServiceRow } from '../components/ServiceRow'
import { apiFetch } from '../api/client'

const EMPTY_FORM = {
  name: '', git_repo: '', git_branch: 'main',
  port: '8080', environment: 'production', project_id: 'default',
}

export function ServicesPage() {
  const health = useHealth()
  const { data: services, loading } = useServices()
  const { data: deployments } = useDeployments()
  const [showForm, setShowForm] = useState(false)
  const [form, setForm] = useState(EMPTY_FORM)
  const [submitting, setSubmitting] = useState(false)
  const [error, setError] = useState('')

  const submit = async (e: React.FormEvent) => {
    e.preventDefault()
    setSubmitting(true)
    setError('')
    try {
      await apiFetch('/api/v1/services', {
        method: 'POST',
        body: JSON.stringify({ ...form, port: parseInt(form.port) }),
      })
      setShowForm(false)
      setForm(EMPTY_FORM)
    } catch (err) {
      setError((err as Error).message)
    } finally {
      setSubmitting(false)
    }
  }

  const handleDelete = useCallback(async (id: string) => {
    if (!confirm('Delete this service?')) return
    try {
      await apiFetch(`/api/v1/services/${id}`, { method: 'DELETE' })
    } catch (err) {
      alert((err as Error).message)
    }
  }, [])

  const f = (k: keyof typeof form) => (e: React.ChangeEvent<HTMLInputElement | HTMLSelectElement>) =>
    setForm(prev => ({ ...prev, [k]: e.target.value }))

  return (
    <div className="content">
      <div className="panel">
        <div className="panel-head">
          <SvcIcon />
          Services
          <span className="count">{services.length} total</span>
          <button
            className={showForm ? 'btn' : 'btn btn-primary'}
            style={{ marginLeft: 'auto', padding: '5px 12px', fontSize: 12 }}
            onClick={() => { setShowForm(v => !v); setError('') }}
          >
            {showForm ? 'Cancel' : '+ Register'}
          </button>
        </div>

        {showForm && (
          <form className="register-form" onSubmit={submit}>
            {error && <div className="login-error">{error}</div>}
            <div className="register-grid">
              <div className="form-group">
                <label className="form-label">Name</label>
                <input className="input" placeholder="api-service" value={form.name} onChange={f('name')} required />
              </div>
              <div className="form-group">
                <label className="form-label">Environment</label>
                <select className="input" value={form.environment} onChange={f('environment')}>
                  <option>production</option>
                  <option>staging</option>
                  <option>development</option>
                </select>
              </div>
              <div className="form-group">
                <label className="form-label">Git Repo</label>
                <input className="input" placeholder="github.com/org/repo" value={form.git_repo} onChange={f('git_repo')} required />
              </div>
              <div className="form-group">
                <label className="form-label">Branch</label>
                <input className="input" placeholder="main" value={form.git_branch} onChange={f('git_branch')} />
              </div>
              <div className="form-group">
                <label className="form-label">Port</label>
                <input className="input" type="number" placeholder="8080" value={form.port} onChange={f('port')} required />
              </div>
              <div className="form-group">
                <label className="form-label">Project ID</label>
                <input className="input" placeholder="default" value={form.project_id} onChange={f('project_id')} />
              </div>
            </div>
            <button type="submit" className="btn btn-primary" disabled={submitting} style={{ width: '100%', justifyContent: 'center' }}>
              {submitting ? 'Registering…' : 'Register Service'}
            </button>
          </form>
        )}

        {loading && <div className="empty-row">Loading…</div>}

        {!loading && services.length === 0 && !showForm && (
          <div className="empty-row">
            No services yet.{' '}
            <span style={{ color: 'var(--accent)', cursor: 'pointer' }} onClick={() => setShowForm(true)}>
              Register one →
            </span>
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
              onDelete={handleDelete}
            />
          )
        })}
      </div>
    </div>
  )
}

function SvcIcon() {
  return (
    <svg style={{ width: 14, height: 14, color: 'var(--text3)' }} viewBox="0 0 16 16" fill="none">
      <rect x="1.5" y="4" width="5" height="4" rx="1.2" stroke="currentColor" strokeWidth="1.3" />
      <rect x="9.5" y="2" width="5" height="4" rx="1.2" stroke="currentColor" strokeWidth="1.3" />
      <rect x="9.5" y="9" width="5" height="4" rx="1.2" stroke="currentColor" strokeWidth="1.3" />
      <path d="M6.5 6H8a1 1 0 011 1v1M8 4V2.5" stroke="currentColor" strokeWidth="1.2" strokeLinecap="round" />
    </svg>
  )
}
