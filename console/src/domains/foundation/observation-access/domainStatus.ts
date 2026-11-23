import { queryOptions, useQuery } from '@tanstack/react-query'
import type { ReadEvidence } from '@bifrost/ui/city/observation'
import { fetchDomainStatus } from '../catalog/api/domainStatus'

/** Same raw cache shape as Console Status; no CI, data, mailbox or client health verdict. */
export function domainStatusObservationOptions() {
  return queryOptions({
    queryKey: ['status', 'domain-status'] as const,
    queryFn: async ({ signal }) => {
      const data = await fetchDomainStatus({ signal })
      signal.throwIfAborted()
      return data
    },
    staleTime: 60_000,
    refetchInterval: 60_000,
    retry: false,
  })
}

/** Read evidence is separate from the unmodified authoritative response and its four lights. */
export function useDomainStatusObservation() {
  const query = useQuery(domainStatusObservationOptions())
  const received = query.error ? query.errorUpdatedAt : query.dataUpdatedAt
  const evaluatedAt = query.data?.at
  const evaluated = evaluatedAt ? Date.parse(evaluatedAt) : NaN
  const evidence: ReadEvidence | undefined = received ? {
    source: '/api/v1/catalog/status',
    outcome: query.error ? 'error' : 'read',
    receivedAt: new Date(received).toISOString(),
    evaluatedAt,
    // Existing Signals display window, not a domain health rule. Old sources never become fresh on retry.
    expiresAt: Number.isFinite(evaluated) ? new Date(Math.min(evaluated, query.dataUpdatedAt) + 90_000).toISOString() : undefined,
    detail: query.error?.message,
  } : undefined
  return { ...query, evidence }
}
