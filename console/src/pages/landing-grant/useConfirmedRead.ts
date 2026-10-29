import { useSyncExternalStore } from 'react'
import { onlineManager } from '@tanstack/react-query'

type Network = { online: boolean; generation: number }
let network: Network = { online: onlineManager.isOnline(), generation: 0 }
const listeners = new Set<() => void>()
const confirmations = new WeakMap<object, number>()

function observe(online: boolean): void {
  if (online === network.online) return
  network = { online, generation: network.generation + 1 }
  listeners.forEach(changed => changed())
}
// Application lifetime, independent of mounted pages or QueryClients. This
// also keeps onlineManager's browser event listeners alive with no consumers.
onlineManager.subscribe(observe)
function snapshot(): Network {
  return network
}
function subscribe(changed: () => void): () => void {
  listeners.add(changed)
  return () => { listeners.delete(changed) }
}

/** Bind a successful response to the network generation in which it started.
 * Keep its object identity in the query cache: structural sharing could reuse
 * an old response's confirmation when two generations return identical JSON.
 */
export function confirmedReadOptions<T extends object>(read: (context: { signal: AbortSignal }) => Promise<T>) {
  return {
    structuralSharing: false as const,
    queryFn: async (context: { signal: AbortSignal }): Promise<T> => {
      const started = snapshot()
      const data = { ...await read(context) }
      if (started.online && network.online && started.generation === network.generation && !context.signal.aborted) {
        confirmations.set(data, started.generation)
      }
      return data
    },
  }
}

export type Read = { isSuccess: boolean; isPaused: boolean; data?: unknown }

/** Only a read started and completed in this online generation is current.
 * Remounts and later dataUpdatedAt values cannot confirm an old response.
 */
export function useConfirmedReads(reads: readonly Read[]): boolean[] {
  const n = useSyncExternalStore(subscribe, snapshot, snapshot)
  return reads.map(r => r.isSuccess && !r.isPaused && n.online && typeof r.data === 'object' && r.data !== null
    && confirmations.get(r.data) === n.generation)
}

/** One read: useConfirmedReads([q])[0]. */
export function useConfirmedRead(read: Read): boolean {
  return useConfirmedReads([read])[0]
}
