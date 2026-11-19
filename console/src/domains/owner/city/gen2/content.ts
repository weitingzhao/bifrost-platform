import { panelsOf, pickedPanel, type Panel } from './panels'
import { placeTree, type Place } from './places'

/**
 * What the frame shows, kept apart from the frame (the Owner, 2026-11-19: like a page's menu and
 * pages, what the places are called and what each holds must be easy to change without touching the
 * frame). The frame (viewport, grid, scene, corners, rail, light) knows only this shape:
 *
 *   tree    the places: town → districts → buildings → rooms, each with a name, a question and a size
 *   panels  the tables and charts beside a place (its dashboard sections)
 *   picked  the card of a picked room
 *
 * Swap the content, keep the frame. Today's content is the Console's menu (consoleContent).
 */
export type CityContent = {
  tree: Place
  panels: (place: Place, open: (child: Place) => void) => Panel[]
  picked: (item: Place) => Panel
}

export const consoleContent = (): CityContent => ({ tree: placeTree(), panels: panelsOf, picked: pickedPanel })
