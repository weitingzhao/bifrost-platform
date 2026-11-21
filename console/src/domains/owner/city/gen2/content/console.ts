import type { CityContent } from '@bifrost/ui/city/content'
import type { Place } from '@bifrost/ui/city/places'
import { consoleFunctions } from './consolePlaces'
import { domainTree } from './domainPlaces'
import { domainPanel, domainsPanel, type DomainsRead } from './domainsPanel'
import { ownsPanel } from './ownsPanel'
import type { ReadDomain } from './domainOwns'
import { panelsOf, pickedPanel } from './panels'
import { domainTechTree, type GraphRead } from './techTree'

/**
 * Today's content, a plug-in of the frame: the districts are the domains the map reads (domainPlaces.ts),
 * the hardware stands in compute's, and the menu along the bottom is the Console's functions (the other
 * dimension, the Owner 2026-11-20). Who stands on whom is the tech tree (techTree.ts), opened from the
 * levels, each node with its domain's light; the town's panel keeps what the map counts off it, a district
 * its own lights and, read when it is entered, what it owns (the map's domain read).
 */
export const consoleContent = (domains: DomainsRead = {}, hardware: Place | null = null, graph: GraphRead = {}, readDomain?: ReadDomain): CityContent => {
  const why = domains.error instanceof Error ? domains.error.message : domains.error ? String(domains.error) : ''
  const tree = domainTree(domains.data, hardware, why)
  const ids = new Set((domains.data?.domains ?? []).map(d => d.id))
  const panels = (place: Place, open: (child: Place) => void) =>
    place.id === tree.id ? [...panelsOf(place, open), domainsPanel(domains)]
      : place.kind === 'district' && ids.has(place.id) ? [...panelsOf(place, open), domainPanel(domains, place.id), ...(readDomain ? [ownsPanel(readDomain, place.id)] : [])]
        : panelsOf(place, open)
  return { tree, panels, picked: pickedPanel, functions: consoleFunctions(), techTree: domainTechTree(domains.data, graph) }
}
