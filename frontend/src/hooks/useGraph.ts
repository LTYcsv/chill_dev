import { useState, useEffect } from 'react'
import { apiFetch } from '../api/client'
import type { InfraGraph } from '../api/types'

export function useGraph(projectId: string, env: string, intervalMs = 20000) {
  const [data, setData] = useState<InfraGraph | null>(null)

  useEffect(() => {
    let alive = true

    const poll = async () => {
      try {
        const g = await apiFetch<InfraGraph>(
          `/api/v1/graph?project_id=${encodeURIComponent(projectId)}&env=${encodeURIComponent(env)}`
        )
        if (alive) setData(g)
      } catch {
        // keep stale
      }
    }

    poll()
    const id = setInterval(poll, intervalMs)
    return () => { alive = false; clearInterval(id) }
  }, [projectId, env, intervalMs])

  return data
}
