import { queryOptions, useQuery } from '@tanstack/react-query'
import { authedFetch } from '@/api/client'
import { fetchSelfHealth } from '@/api/core'
import { captureReads, snapshotOf } from '../runtime-observation/read'
import { capturedFromEngine, engineHardwareOn, ENGINE_HARDWARE_PATH, type EngineHardware } from './engineHardware'

/** Host adapter only. The engine receives ObservationSnapshot v1, never credentials or HTTP payloads.
 * viaEngine: the same snapshot from the UI engine's hardware read (engineHardware.ts; off everywhere until compared). */
export function hardwareObservationOptions({ viaEngine = false }: { viaEngine?: boolean } = {}) {
  return queryOptions({
    // Compatible with the deployed Signals reader. Share a host QueryClient, not a second store.
    // Through the engine is another key: one cache never holds the two sources' captures mixed.
    queryKey: viaEngine ? ['runtime-observation', 'hardware', 'engine'] as const : ['runtime-observation', 'hardware'] as const,
    queryFn: async ({ signal }) => {
      const reads = viaEngine
        ? capturedFromEngine(await (await authedFetch('Runtime observation', ENGINE_HARDWARE_PATH, { signal })).json() as EngineHardware)
        : await captureReads(async path => (await authedFetch('Runtime observation', path, { signal })).json())
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
  // The shell's self-health query (same key, so one read): only its viewer_env is needed here. Not imported from the
  // shell, so a page mounting the hardware does not load the shell's module (2026-11-24: that import broke
  // domainMigration.test, whose @/api/core mock has no fetchSelfHealth, before any test in it ran).
  const health = useQuery({ queryKey: ['shell', 'self-health'], queryFn: () => fetchSelfHealth(), staleTime: 10_000 })
  // Until self-health answers the environment is unknown: the direct read.
  return useQuery(hardwareObservationOptions({ viaEngine: engineHardwareOn(health.data?.viewer_env) }))
}

export { domainStatusObservationOptions, useDomainStatusObservation } from './domainStatus'
