import { useSyncExternalStore } from 'react'
import { formatShellHash } from '@/lib/shell/consoleRoutes'
import type { ContextAdapter, WorldContext } from '@/lib/shell/sharedContext'

/*
 * Signals' adapter for the shared context (箭10 2026-11-25; design cockpit/ui-console/shared-context-design.md).
 * The address holds the control room's opened group: #observation?open=<item id>. A device's group is the focus
 * node:<device> (the topology's device id, as City's buildings); a domain's group is the scope. Back the other way: a
 * node opens that device's group, a scope opens that domain's group (which devices serve a domain no source says,
 * so the domain's own lamps are what Signals shows of it). Any other focus travels as from=.
 * Time is not here: Signals' pin is its only writer and travels through the shared observation (sharedObservationStore).
 */
const DEVICE = 'device:', DOMAIN = 'domain:'
const queryOf = (hash: string) => new URLSearchParams(hash.replace(/^#/, '').split('?')[1] ?? '')

export const signalsContext: ContextAdapter = {
  toContext(hash: string): WorldContext {
    const open = queryOf(hash).get('open') ?? ''
    if (open.startsWith(DEVICE)) return { focus: { kind: 'node', id: open.slice(DEVICE.length) } }
    if (open.startsWith(DOMAIN)) return { scope: open.slice(DOMAIN.length) }
    return {}
  },
  hrefFor(ctx: WorldContext) {
    if (ctx.focus?.kind === 'node') return { href: openHref(DEVICE + ctx.focus.id), shown: true }
    if (ctx.scope) return { href: openHref(DOMAIN + ctx.scope), shown: ctx.focus == null }
    return null
  },
}

/** The address that opens one item of the control room (null: the room with nothing open). */
export const openHref = (id: string | null) => formatShellHash('observation', id ? new URLSearchParams({ open: id }).toString() : '')

const subscribe = (l: () => void) => { window.addEventListener('hashchange', l); return () => window.removeEventListener('hashchange', l) }
const hash = () => window.location.hash
/** The opened item from the address, and a setter that writes it there (replace: opening a group adds no history). */
export function useOpenedSignal(): [string | null, (id: string | null) => void] {
  const opened = queryOf(useSyncExternalStore(subscribe, hash, () => '')).get('open')
  return [opened, id => window.location.replace(openHref(id))]
}
