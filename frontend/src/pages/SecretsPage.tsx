import { useState, useEffect, useCallback } from 'react'
import { useServices } from '../hooks/useServices'
import { apiFetch } from '../api/client'
import type { Secret } from '../api/types'

function formatDate(iso: string): string {
  return new Date(iso).toLocaleString(undefined, { month: 'short', day: 'numeric', hour: '2-digit', minute: '2-digit' })
}

export function SecretsPage() {
  const { data: services } = useServices()
  const [serviceId, setServiceId] = useState('')
  const [envId, setEnvId] = useState('production')
  const [secrets, setSecrets] = useState<Secret[]>([])
  const [loading, setLoading] = useState(false)
  const [showForm, setShowForm] = useState(false)
  const [newKey, setNewKey] = useState('')
  const [newValue, setNewValue] = useState('')
  const [saving, setSaving] = useState(false)
  const [error, setError] = useState('')

  const load = useCallback(async () => {
    if (!serviceId) return
    setLoading(true)
    try {
      const list = await apiFetch<Secret[]>(
        `/api/v1/secrets?service_id=${encodeURIComponent(serviceId)}&env_id=${encodeURIComponent(envId)}`
      )
      setSecrets(list ?? [])
    } catch {
      setSecrets([])
    } finally {
      setLoading(false)
    }
  }, [serviceId, envId])

  useEffect(() => { load() }, [load])

  const handleSave = async (e: React.FormEvent) => {
    e.preventDefault()
    if (!newKey || !newValue || !serviceId) return
    setSaving(true)
    setError('')
    try {
      await apiFetch('/api/v1/secrets', {
        method: 'PUT',
        body: JSON.stringify({ service_id: serviceId, environment_id: envId, key: newKey, value: newValue }),
      })
      setNewKey('')
      setNewValue('')
      setShowForm(false)
      await load()
    } catch (err) {
      setError((err as Error).message)
    } finally {
      setSaving(false)
    }
  }

  const handleDelete = async (id: string, key: string) => {
    if (!confirm(`Delete secret "${key}"?`)) return
    try {
      await apiFetch(`/api/v1/secrets/${id}`, { method: 'DELETE' })
      setSecrets(prev => prev.filter(s => s.id !== id))
    } catch (err) {
      alert((err as Error).message)
    }
  }

  return (
    <div className="content">
      <div className="filter-bar">
        <select
          className="input"
          style={{ width: 'auto', minWidth: 180 }}
          value={serviceId}
          onChange={e => { setServiceId(e.target.value); setShowForm(false) }}
        >
          <option value="">Select service…</option>
          {services.map(s => <option key={s.id} value={s.id}>{s.name}</option>)}
        </select>
        <select
          className="input"
          style={{ width: 'auto', minWidth: 140 }}
          value={envId}
          onChange={e => setEnvId(e.target.value)}
        >
          <option>production</option>
          <option>staging</option>
          <option>development</option>
        </select>
      </div>

      <div className="panel">
        <div className="panel-head">
          <SecretsIcon />
          Secrets
          {serviceId && <span className="count">{secrets.length}</span>}
          {serviceId && (
            <button
              className={showForm ? 'btn' : 'btn btn-primary'}
              style={{ marginLeft: 'auto', padding: '5px 12px', fontSize: 12 }}
              onClick={() => { setShowForm(v => !v); setError('') }}
            >
              {showForm ? 'Cancel' : '+ Add Secret'}
            </button>
          )}
        </div>

        {!serviceId && (
          <div className="empty-row">Select a service to manage its secrets</div>
        )}

        {serviceId && showForm && (
          <form className="register-form" onSubmit={handleSave} style={{ display: 'flex', flexDirection: 'column', gap: 10 }}>
            {error && <div className="login-error">{error}</div>}
            <div className="register-grid">
              <div className="form-group">
                <label className="form-label">Key</label>
                <input
                  className="input"
                  placeholder="DATABASE_URL"
                  value={newKey}
                  onChange={e => setNewKey(e.target.value.toUpperCase().replace(/\s/g, '_'))}
                  required
                />
              </div>
              <div className="form-group">
                <label className="form-label">Value</label>
                <input
                  className="input"
                  type="password"
                  placeholder="••••••••"
                  value={newValue}
                  onChange={e => setNewValue(e.target.value)}
                  required
                />
              </div>
            </div>
            <button type="submit" className="btn btn-primary" disabled={saving} style={{ width: '100%', justifyContent: 'center' }}>
              {saving ? 'Saving…' : 'Save Secret'}
            </button>
          </form>
        )}

        {serviceId && loading && <div className="empty-row">Loading…</div>}

        {serviceId && !loading && secrets.length === 0 && !showForm && (
          <div className="empty-row">
            No secrets for this service.{' '}
            <span style={{ color: 'var(--accent)', cursor: 'pointer' }} onClick={() => setShowForm(true)}>
              Add one →
            </span>
          </div>
        )}

        {secrets.map(s => (
          <div key={s.id} className="secret-row">
            <span style={{ fontFamily: 'var(--mono)', fontSize: 12.5, fontWeight: 500, color: 'var(--text)', flex: 1 }}>
              {s.key}
            </span>
            <span className="svc-env">{s.environment_id || envId}</span>
            <span style={{ fontSize: 11, color: 'var(--text3)', fontFamily: 'var(--mono)' }}>
              ••••••••
            </span>
            <span style={{ fontSize: 11, color: 'var(--text3)', flexShrink: 0 }}>
              {formatDate(s.updated_at)}
            </span>
            <button
              className="svc-deploy-btn"
              style={{ color: 'var(--red)', borderColor: 'rgba(240,82,82,0.25)', padding: '4px 8px' }}
              onClick={() => handleDelete(s.id, s.key)}
            >
              Delete
            </button>
          </div>
        ))}
      </div>
    </div>
  )
}

function SecretsIcon() {
  return <svg style={{ width: 14, height: 14, color: 'var(--text3)' }} viewBox="0 0 16 16" fill="none"><rect x="3" y="7" width="10" height="7" rx="1.5" stroke="currentColor" strokeWidth="1.3" /><path d="M5 7V5a3 3 0 016 0v2" stroke="currentColor" strokeWidth="1.3" strokeLinecap="round" /><circle cx="8" cy="10.5" r="1" fill="currentColor" /></svg>
}
