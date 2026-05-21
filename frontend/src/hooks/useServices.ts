import { useState, useEffect } from 'react'
import { apiFetch } from '../api/client'
import type { ServiceConfig } from '../api/types'

export function useServices(intervalMs = 10000) {
  const [data, setData] = useState<ServiceConfig[]>([])
  const [loading, setLoading] = useState(true)

  useEffect(() => {
    let alive = true

    const poll = async () => {
      try {
        const svcs = await apiFetch<ServiceConfig[]>('/api/v1/services')
        if (alive) setData(svcs ?? [])
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
