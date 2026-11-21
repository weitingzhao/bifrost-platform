import type { CityContent } from '@bifrost/ui/city/content'
import type { Place } from '@bifrost/ui/city/places'
import { consoleFunctions } from './consolePlaces'
import { domainTree } from './domainPlaces'
import { domainPanel, domainsPanel, type DomainsRead } from './domainsPanel'
import { panelsOf, pickedPanel } from './panels'

/**
 * Today's content, a plug-in of the frame: the districts are the domains the map reads (domainPlaces.ts),
 * the hardware stands in compute's, and the menu along the bottom is the Console's functions (the other
 * dimension, the Owner 2026-11-20). The town carries every domain's lights; a district its own.
 */
export const consoleContent = (domains: DomainsRead = {}, hardware: Place | null = null): CityContent => {
  const why = domains.error instanceof Error ? domains.error.message : domains.error ? String(domains.error) : ''
  const tree = domainTree(domains.data, hardware, why)
  const ids = new Set((domains.data?.domains ?? []).map(d => d.id))
  const panels = (place: Place, open: (child: Place) => void) =>
    place.id === tree.id ? [...panelsOf(place, open), domainsPanel(domains)]
      : place.kind === 'district' && ids.has(place.id) ? [...panelsOf(place, open), domainPanel(domains, place.id)]
        : panelsOf(place, open)
  return { tree, panels, picked: pickedPanel, functions: consoleFunctions() }
}
