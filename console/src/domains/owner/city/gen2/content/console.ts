import type { CityContent } from '@bifrost/ui/city/content'
import type { Place } from '@bifrost/ui/city/places'
import { consoleFunctions } from './consolePlaces'
import { domainTree } from './domainPlaces'
import { domainPanel, domainsPanel, type DomainsRead } from './domainsPanel'
import { ownsPanel, type DomainHoldings, type ReadDomain } from './domainOwns'
import { districtTechTree, domainTechTree, type GraphRead } from './techTree'
import { trafficPanel, type TrafficRead } from './traffic'
import { overflowOf } from './districtBuildings'
import { alertsRead, type DeviceKey, type PromAlert } from './alerts'
import type { CounterRoute } from '@bifrost/ui/city/units/counterRoom'
import type { OffContractRead } from '@bifrost/ui/city/offContract'
import { districtLinesPanel, linesPanel, type LinesRead, type RosterRead } from './lines'

/**
 * Today's content, a plug-in of the frame: the districts are the domains the map reads (domainPlaces.ts),
 * the hardware stands in compute's, and the menu along the bottom is the Console's functions (the other
 * dimension, the Owner 2026-11-20). Who stands on whom is the tech tree (techTree.ts), opened from the
 * levels, each node with its domain's light; the town's panel keeps what the map counts off it, a district
 * its own lights and, read when it is entered, what it owns (the map's domain read).
 */
export const consoleContent = (domains: DomainsRead = {}, hardware: Place | null = null, graph: GraphRead = {}, readDomain?: ReadDomain, traffic?: TrafficRead, alerts?: { raw?: PromAlert[]; error?: unknown; devices: DeviceKey[] }, holdings: Record<string, DomainHoldings> = {}, lines?: { read: LinesRead; roster: RosterRead; now: Date; focus?: string }, routes?: Map<string, CounterRoute[]>, offContract?: OffContractRead): CityContent => {
  const why = domains.error instanceof Error ? domains.error.message : domains.error ? String(domains.error) : ''
  const tree = domainTree(domains.data, hardware, why, holdings, routes)
  const ids = new Set((domains.data?.domains ?? []).map(d => d.id))
  // A district whose buildings go past its ground (16 things): cut too fine, to decouple or merge (the Owner, 2026-11-22).
  const overflowPanel = (id: string) => {
    const o = overflowOf(holdings[id])
    return o.over > 0 ? [{ id: 'overflow', title: 'Too fine to fit', dock: 'right' as const, cards: [{ kind: 'note' as const, tone: 'warn' as const, text: `${o.buildings} buildings, ${o.over} past the 16 a level holds: the rest stand in the Other site. Cut too fine: decouple or merge them.` }] }] : []
  }
  // Only this content's own data; the engine adds its own cards (a facility's key and rulers, what a place holds, the picked card).
  const panels = (place: Place) =>
    place.id === tree.id ? [...(lines ? [linesPanel(lines.read, lines.roster, lines.now, lines.focus)] : []), domainsPanel(domains), ...(traffic ? [trafficPanel(traffic)] : [])]
      : place.kind === 'district' && ids.has(place.id) ? [domainPanel(domains, place.id), ...(lines ? [districtLinesPanel(lines.read, lines.roster, place.id, lines.now)] : []), ...overflowPanel(place.id), ...(readDomain ? [ownsPanel(readDomain, place.id)] : [])]
        : []
  return { version: 2, tree, panels, functions: consoleFunctions(), techTree: domainTechTree(domains.data, graph), techTreeIn: (d: Place) => districtTechTree(d, ids, graph.data, holdings[d.id]), trips: traffic?.trips, ...(offContract ? { offContract } : {}), ...(alerts ? { alerts: alertsRead(alerts.raw, alerts.error, alerts.devices, tree) } : {}) }
}
