import { useState, useEffect } from 'react'
import { apiFetch } from '../api/client'
import type { Deployment } from '../api/types'

export function useDeployments(intervalMs = 15000) {
  const [data, setData] = useState<Deployment[]>([])
  const [loading, setLoading] = useState(true)

  useEffect(() => {
    let alive = true

    const poll = async () => {
      try {
        const deps = await apiFetch<Deployment[]>('/api/v1/deployments')
        if (alive) setData(deps ?? [])
      } catch {
        // keep stale
      } finally {
        if (alive) setLoading(false)
      }
    }

    poll()
    const id = setInterval(poll, intervalMs)
    return () => { alive = false; clearInterval(id) }
  }, [intervalMs])

  return { data, loading }
}
