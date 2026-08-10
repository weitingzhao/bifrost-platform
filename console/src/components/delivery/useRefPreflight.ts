import { useQuery } from '@tanstack/react-query'
import { useEffect, useState } from 'react'
import { fetchRefPreflight } from '@/api/delivery'
import type { RefPreflightResponse } from '@/api/deliveryTypes'
import { validateGitRevision } from '@/lib/delivery/revisionValidation'

/** Debounces a fast-changing value (e.g. a text input) by `delayMs`. */
function useDebounced<T>(value: T, delayMs: number): T {
  const [debounced, setDebounced] = useState(value)
  useEffect(() => {
    const id = window.setTimeout(() => setDebounced(value), delayMs)
    return () => window.clearTimeout(id)
  }, [value, delayMs])
  return debounced
}

export function useRefPreflight(pipeline: string, revision: string) {
  const trimmed = revision.trim()
  const debounced = useDebounced(trimmed, 400)
  const valid = validateGitRevision(debounced) == null

  return useQuery({
    queryKey: ['delivery', 'ref-preflight', pipeline, debounced],
    queryFn: () => fetchRefPreflight(pipeline, debounced),
    enabled: valid && debounced.length > 0,
    staleTime: 30_000,
  })
}

/**
 * Deploy is blocked only when probes had full visibility (reachability ok) and
 * the ref is genuinely missing somewhere. Never block on probe/network errors.
 */
export function isRefDeployBlocked(data: RefPreflightResponse | undefined): boolean {
  if (data == null) return false
  return data.reachability === 'ok' && data.missing.length > 0
}
