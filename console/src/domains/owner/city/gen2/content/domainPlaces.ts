import { DISTRICT } from '@bifrost/ui/city/grid'
import { TOWN_ID, type Place } from '@bifrost/ui/city/places'
import { formatShellHash } from '@/lib/shell/consoleRoutes'
import type { MapStatus } from './domainsPanel'

/**
 * The city's districts are the system's domains (the Owner, 2026-11-20: "the 2.5D is there to show the
 * system's domain design; the Console's menu is another dimension, look at the two apart"). So the
 * districts are read from the map line (GET /api/v1/catalog/status), in the order it serves them, and
 * nothing here lists them by hand; the Console's menu stays the bar along the bottom (by function).
 *
 * The hardware is compute's: its computers stand in the compute district (its nuclear town kept).
 * The other domains hold nothing drawn yet: each is a district under its own name, see-through until
 * something real in it is read onto it. When the map cannot be read, the town says so and holds only
 * what was read itself (the hardware), never a guessed list.
 */
export const COMPUTE = 'compute'

/** A domain's name as the map serves it, else its id (never one written down here). */
type Named = { id: string; name?: string }
const nameOf = (d: Named) => d.name || d.id

export function domainTree(status: MapStatus | undefined, hardware: Place | null, why = ''): Place {
  const domains = (status?.domains ?? []) as Named[]
  const children: Place[] = domains.map(d => d.id === COMPUTE && hardware
    ? { ...hardware, id: d.id, label: nameOf(d) }
    : { id: d.id, kind: 'district', shape: DISTRICT, label: nameOf(d), question: `The ${nameOf(d)} domain: what it holds and how it stands, as the map sees it.`, dashboard: null, children: [] })
  // The hardware was read even when the map was not: it stands on its own rather than vanish.
  if (hardware && !domains.some(d => d.id === COMPUTE)) children.unshift(hardware)
  return {
    id: TOWN_ID,
    kind: 'town',
    shape: '8x8',
    label: 'City',
    question: status ? 'The whole system, one district per domain.' : `The whole system. The domains are not known${why ? `: ${why}` : ''}.`,
    dashboard: formatShellHash('command'),
    children,
  }
}
