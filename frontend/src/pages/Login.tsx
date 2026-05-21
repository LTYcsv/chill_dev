import { useState, type FormEvent } from 'react'
import { apiFetch, setToken } from '../api/client'
import type { LoginResponse } from '../api/types'

interface Props {
  onLogin: () => void
}

export function Login({ onLogin }: Props) {
  const [email, setEmail] = useState('')
  const [password, setPassword] = useState('')
  const [error, setError] = useState('')
  const [loading, setLoading] = useState(false)

  const submit = async (e: FormEvent) => {
    e.preventDefault()
    if (!email || !password) return
    setLoading(true)
    setError('')
    try {
      const { token } = await apiFetch<LoginResponse>('/api/v1/auth/login', {
        method: 'POST',
        body: JSON.stringify({ email, password }),
      })
      setToken(token)
      onLogin()
    } catch (err) {
      setError((err as Error).message.includes('401') ? 'Invalid email or password' : 'Could not connect to server')
    } finally {
      setLoading(false)
    }
  }

  return (
    <div className="login-page">
      <div className="login-card">
        <div className="login-logo">
          <div className="logo-icon">
            <svg viewBox="0 0 16 16" fill="none">
              <rect x="1" y="7" width="6" height="6" rx="1.5" fill="white" opacity="0.9" />
              <rect x="9" y="1" width="6" height="6" rx="1.5" fill="white" opacity="0.6" />
              <rect x="9" y="9" width="6" height="6" rx="1.5" fill="white" opacity="0.4" />
            </svg>
          </div>
          <span className="logo-text">DevPlatform</span>
        </div>

        <div className="login-title">Sign in</div>
        <div className="login-sub">Access your infrastructure dashboard</div>

        {error && <div className="login-error">{error}</div>}

        <form onSubmit={submit}>
          <div className="form-group">
            <label className="form-label">Email</label>
            <input
              className="input"
              type="email"
              placeholder="you@company.com"
              value={email}
              onChange={e => setEmail(e.target.value)}
              autoFocus
            />
          </div>

          <div className="form-group">
            <label className="form-label">Password</label>
            <input
              className="input"
              type="password"
              placeholder="••••••••"
              value={password}
              onChange={e => setPassword(e.target.value)}
            />
          </div>

          <button
            type="submit"
            className="btn btn-primary"
            style={{ width: '100%', justifyContent: 'center', marginTop: 8, padding: '9px 14px' }}
            disabled={loading}
          >
            {loading ? 'Signing in…' : 'Sign in'}
          </button>
        </form>
      </div>
    </div>
  )
}
