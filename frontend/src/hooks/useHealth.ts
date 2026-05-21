import { useState, useEffect } from 'react'
import { apiFetch } from '../api/client'
import type { HealthStatus } from '../api/types'

export function useHealth(intervalMs = 5000) {
  const [data, setData] = useState<HealthStatus | null>(null)

  useEffect(() => {
    let alive = true

    const poll = async () => {
      try {
        const h = await apiFetch<HealthStatus>('/healthz')
        if (alive) setData(h)
      } catch {
        // gateway unreachable — keep stale data
      }
    }

    poll()
    const id = setInterval(poll, intervalMs)
    return () => { alive = false; clearInterval(id) }
  }, [intervalMs])

  return data
}
