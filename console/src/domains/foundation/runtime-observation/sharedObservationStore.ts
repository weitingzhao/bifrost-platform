import { useSyncExternalStore } from 'react'
import type { RuntimeWorldControl } from '@bifrost/ui/city/observation'

/**
 * The one observation context above the Console's routes (箭10 2026-11-23, World's gap table item 9: a Signals pin
 * was lost on every change of place). The host (sharedObservationHost.tsx) mounts the readers once Signals has been
 * opened and keeps them; this store hands its context to whoever asks, so the Console page itself does not re-render
 * on the context's clock. off: Signals not opened yet in this tab; reading: the composite has not arrived.
 */
export type SharedObservation =
  | { state: 'off' }
  | { state: 'reading'; error?: unknown }
  | { state: 'ready'; observation: RuntimeWorldControl }

let current: SharedObservation = { state: 'off' }
const subscribers = new Set<() => void>()

export function publishSharedObservation(next: SharedObservation): void {
  current = next
  for (const notify of subscribers) notify()
}

export function useSharedObservation(): SharedObservation {
  return useSyncExternalStore(
    notify => {
      subscribers.add(notify)
      return () => subscribers.delete(notify)
    },
    () => current,
  )
}
