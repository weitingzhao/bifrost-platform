import type { CityContent, Panel } from '@bifrost/ui/city/content'
import type { Place } from '@bifrost/ui/city/places'
import { consoleFunctions, placeTree } from './consolePlaces'
import { panelsOf, pickedPanel } from './panels'

/** Today's content: the Console's menu as places, with greybox panels. A plug-in of the frame. */
export const consoleContent = (extra: Place[] = [], townPanels: Panel[] = []): CityContent => {
  const tree = placeTree()
  // Domains that come in through the entry rules (RULES.md), ahead of the Console's districts: today the hardware.
  // Panels that belong to the whole city (today the map's domain lights) ride with the town's own.
  const panels = (place: Place, open: (child: Place) => void) =>
    place.id === tree.id ? [...panelsOf(place, open), ...townPanels] : panelsOf(place, open)
  return { tree: { ...tree, children: [...extra, ...tree.children] }, panels, picked: pickedPanel, functions: consoleFunctions() }
}
