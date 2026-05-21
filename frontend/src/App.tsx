import { useState, useEffect } from 'react'
import { getToken } from './api/client'
import { Login } from './pages/Login'
import { Dashboard } from './pages/Dashboard'

export default function App() {
  const [authed, setAuthed] = useState(() => Boolean(getToken()))

  useEffect(() => {
    const onLogout = () => setAuthed(false)
    window.addEventListener('auth:logout', onLogout)
    return () => window.removeEventListener('auth:logout', onLogout)
  }, [])

  if (!authed) {
    return <Login onLogin={() => setAuthed(true)} />
  }

  return <Dashboard />
}
