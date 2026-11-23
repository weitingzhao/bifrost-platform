import { queryOptions, useQuery } from '@tanstack/react-query'
import { authedFetch } from '@/api/client'
import { captureReads, snapshotOf } from '../runtime-observation/read'

/** Host adapter only. The engine receives ObservationSnapshot v1, never credentials or HTTP payloads. */
export function hardwareObservationOptions() {
  return queryOptions({
    // Compatible with the deployed Signals reader. Share a host QueryClient, not a second store.
    queryKey: ['runtime-observation', 'hardware'] as const,
    queryFn: async ({ signal }) => {
      const reads = await captureReads(async path => (await authedFetch('Runtime observation', path, { signal })).json())
      // captureReads records ordinary failures as evidence. Cancellation must not publish a partial snapshot.
      signal.throwIfAborted()
      return snapshotOf(reads, 'live')
    },
    staleTime: 30_000,
    refetchInterval: 30_000,
  })
}

/** Opt in to hardware + monitoring alerts only; no communication, operations, menus or workflow actions. */
export function useHardwareObservation() {
  return useQuery(hardwareObservationOptions())
}

export { domainStatusObservationOptions, useDomainStatusObservation } from './domainStatus'
