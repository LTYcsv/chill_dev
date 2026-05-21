import { useState, useEffect } from 'react'
import { getToken, apiFetch } from './api/client'
import { Login } from './pages/Login'
import { Dashboard } from './pages/Dashboard'
import type { AuthUser } from './api/types'

export default function App() {
  const [authed, setAuthed] = useState(() => Boolean(getToken()))
  const [user, setUser] = useState<AuthUser | null>(null)

  useEffect(() => {
    if (!authed) { setUser(null); return }
    apiFetch<AuthUser>('/api/v1/auth/validate')
      .then(u => setUser(u))
      .catch(() => {})
  }, [authed])

  useEffect(() => {
    const onLogout = () => setAuthed(false)
    window.addEventListener('auth:logout', onLogout)
    return () => window.removeEventListener('auth:logout', onLogout)
  }, [])

  if (!authed) return <Login onLogin={() => setAuthed(true)} />
  return <Dashboard user={user} />
}
