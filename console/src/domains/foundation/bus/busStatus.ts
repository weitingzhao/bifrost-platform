import type { DenseTagVariant } from '@bifrost/ui'
import type { BusConnection } from './useBusStream'

/**
 * How the Bus page shows a roster status and the stream's connection
 * (2026-11-17: STG's Bus page went white when the roster sent "idle", a
 * status this table did not have, and the lookup threw inside a render).
 * A status the page does not know is shown as itself, never thrown on.
 */
const CLIENT_STATUS: Record<string, { label: string; variant: DenseTagVariant }> = {
  active: { label: 'active', variant: 'success' },
  lost: { label: 'lost', variant: 'danger' },
  new: { label: 'new', variant: 'warning' },
  // A dispatcher row with nothing to do yet: quiet is normal (gate.RosterIdle).
  idle: { label: 'idle', variant: 'neutral' },
}

const CONNECTION: Record<BusConnection, { label: string; variant: DenseTagVariant }> = {
  connecting: { label: 'connecting', variant: 'neutral' },
  live: { label: 'live', variant: 'success' },
  reconnecting: { label: 'reconnecting', variant: 'warning' },
  off: { label: 'bus off', variant: 'danger' },
  denied: { label: 'not signed in', variant: 'danger' },
}

export function clientStatus(status: string): { label: string; variant: DenseTagVariant } {
  return CLIENT_STATUS[status] ?? { label: status || 'unknown', variant: 'neutral' }
}

export function connectionStatus(c: string): { label: string; variant: DenseTagVariant } {
  return (CONNECTION as Record<string, { label: string; variant: DenseTagVariant }>)[c] ?? { label: c || 'unknown', variant: 'neutral' }
}
