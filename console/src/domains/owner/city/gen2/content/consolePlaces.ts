import type { Size } from '../frame/grid'
import { TOWN_ID, type Place, type PlaceKind } from '../frame/places'
import { ENTRY_ROUTE_IDS, SHELL_NAV, entryOf, formatShellHash, type ShellRouteId } from '@/lib/shell/consoleRoutes'

/**
 * Content: the Console's menu as places (the Owner, 2026-11-19): the game style and
 * the dashboard style are two looks of one Console, switchable. So the city
 * is not a separate map: its places ARE the Console's menu. The town is the
 * home; each entry of the menu (Command, Crew, Work, Services, System) is a
 * district; each detail page that sits under an entry is a building in that
 * district; a building's rooms are its sections. Entering a place is opening
 * a page; the place you came from shrinks to the corner.
 *
 * Nothing here is hand-kept: the tree is read from the Console's routes.
 */

/** Rooms are the sections of a page; until a building is designed they are greybox, and say so. */
const GREYBOX_ROOMS = (owner: string): Place[] =>
  ([['A', 4], ['B', 2], ['C', 2]] as const).map(([r, size]) => ({ id: `room-${r.toLowerCase()}`, kind: 'room' as const, size, label: `Room ${r}`, question: `A greybox room of ${owner}: what it holds is designed with the building.`, dashboard: null, children: [] }))

/** Greybox sizes until each is designed: a district and a building take 4×4 of the ground above them. */
const SIZE: Record<Exclude<PlaceKind, 'room'>, Size> = { town: 8, district: 4, building: 4 }

export function placeTree(): Place {
  const nav = SHELL_NAV as readonly { id: ShellRouteId; label: string; question: string; group: string }[]
  const buildingsOf = (entry: string) =>
    nav.filter(n => n.group === 'detail' && n.id !== 'city' && entryOf({ kind: 'shell', id: n.id }) === entry)
      .map(n => ({ id: n.id, kind: 'building' as const, size: SIZE.building, label: n.label, question: n.question, dashboard: formatShellHash(n.id), children: GREYBOX_ROOMS(n.label) }))
  return {
    id: TOWN_ID,
    kind: 'town',
    size: SIZE.town,
    label: 'City',
    question: 'The whole system, one district per part of the Console.',
    dashboard: formatShellHash('command'),
    children: ENTRY_ROUTE_IDS.map(id => {
      const n = nav.find(x => x.id === id)!
      return { id, kind: 'district' as const, size: SIZE.district, label: n.label, question: n.question, dashboard: formatShellHash(id), children: buildingsOf(id) }
    }),
  }
}
