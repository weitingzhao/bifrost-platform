import type { CityContent } from '../frame/content'
import type { Place } from '../frame/places'
import { consoleFunctions, placeTree } from './consolePlaces'
import { panelsOf, pickedPanel } from './panels'

/** Today's content: the Console's menu as places, with greybox panels. A plug-in of the frame. */
export const consoleContent = (extra: Place[] = []): CityContent => {
  const tree = placeTree()
  // Domains that come in through the entry rules (RULES.md), ahead of the Console's districts: today the hardware.
  return { tree: { ...tree, children: [...extra, ...tree.children] }, panels: panelsOf, picked: pickedPanel, functions: consoleFunctions() }
}
