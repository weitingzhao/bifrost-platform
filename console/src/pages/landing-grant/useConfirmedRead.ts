import { useRef, useSyncExternalStore } from 'react'
import { onlineManager } from '@tanstack/react-query'

// Review 698 R-04: a landing read that succeeded before the browser went
// offline is not a current fact. Offline, every authorization conclusion is
// unconfirmed at once (no fetch needs to fail first); back online, it is
// confirmed again only by a read that succeeded after the network returned.
type Network = { online: boolean; drops: number }
let network: Network = { online: onlineManager.isOnline(), drops: onlineManager.isOnline() ? 0 : 1 }

function observe(online: boolean): Network {
  if (online !== network.online) network = { online, drops: online ? network.drops : network.drops + 1 }
  return network
}
function subscribe(changed: () => void): () => void {
  const fromManager = onlineManager.subscribe(online => { observe(online); changed() })
  const fromWindow = () => { observe(onlineManager.isOnline() && navigator.onLine !== false); changed() }
  window.addEventListener('offline', fromWindow)
  window.addEventListener('online', fromWindow)
  return () => { fromManager(); window.removeEventListener('offline', fromWindow); window.removeEventListener('online', fromWindow) }
}
const snapshot = () => observe(onlineManager.isOnline())

export type Read = { isSuccess: boolean; isPaused: boolean; dataUpdatedAt: number }

/**
 * For each read: may it still be shown as a current fact and acted on? A
 * drop seen while mounted makes every earlier read unconfirmed; a read
 * that succeeded after it (online again) confirms it.
 */
export function useConfirmedReads(reads: readonly Read[]): boolean[] {
  const n = useSyncExternalStore(subscribe, snapshot, snapshot)
  const seen = useRef({ drops: n.drops, at: 0 })
  if (seen.current.drops !== n.drops) seen.current = { drops: n.drops, at: Date.now() }
  const droppedAt = seen.current.at
  return reads.map(r => r.isSuccess && !r.isPaused && n.online && r.dataUpdatedAt > droppedAt)
}

/** One read: useConfirmedReads([q])[0]. */
export function useConfirmedRead(read: Read): boolean {
  return useConfirmedReads([read])[0]
}
