import type { Shape } from '../frame/grid'
import { TOWN_ID, type Place } from '../frame/places'
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

/**
 * Greybox shapes until each place is designed (the Owner, 2026-11-19: a thing's footprint follows its
 * weight, so they differ). These are samples to see the rule at work, not decisions: each is set
 * when its place is designed. Rooms show the whole range, 4×8 down to 1×1.
 */
const ROOM_SHAPES: readonly [string, Shape][] = [['A', '4x8'], ['B', '2x4'], ['C', '4x4'], ['D', '2x2'], ['E', '1x2'], ['F', '1x1']]
const DISTRICT_SHAPES: Record<string, Shape> = { command: '8x8', work: '4x8', crew: '4x4', services: '2x4', system: '2x2' }
const BUILDING_SHAPES: readonly Shape[] = ['4x4', '2x4', '2x2', '4x8', '2x2', '2x4']

/** Rooms are the sections of a page; until a building is designed they are greybox, and say so. */
const GREYBOX_ROOMS = (owner: string): Place[] =>
  ROOM_SHAPES.map(([r, shape]) => ({ id: `room-${r.toLowerCase()}`, kind: 'room' as const, shape, label: `Room ${r}`, question: `A greybox room of ${owner}: what it holds is designed with the building.`, dashboard: null, children: [] }))

/** The Console's functions: its menu entries. Each district and building serves the entry it sits under. */
export const consoleFunctions = () => {
  const nav = SHELL_NAV as readonly { id: ShellRouteId; label: string }[]
  return ENTRY_ROUTE_IDS.map(id => ({ id, label: nav.find(x => x.id === id)!.label }))
}

export function placeTree(): Place {
  const nav = SHELL_NAV as readonly { id: ShellRouteId; label: string; question: string; group: string }[]
  const buildingsOf = (entry: string) =>
    nav.filter(n => n.group === 'detail' && n.id !== 'city' && entryOf({ kind: 'shell', id: n.id }) === entry)
      .map((n, i) => ({ id: n.id, function: entry, kind: 'building' as const, shape: BUILDING_SHAPES[i % BUILDING_SHAPES.length], label: n.label, question: n.question, dashboard: formatShellHash(n.id), children: GREYBOX_ROOMS(n.label) }))
  return {
    id: TOWN_ID,
    kind: 'town',
    shape: '8x8',
    label: 'City',
    question: 'The whole system, one district per part of the Console.',
    dashboard: formatShellHash('command'),
    children: ENTRY_ROUTE_IDS.map(id => {
      const n = nav.find(x => x.id === id)!
      return { id, function: id, kind: 'district' as const, shape: DISTRICT_SHAPES[id] ?? '4x4', label: n.label, question: n.question, dashboard: formatShellHash(id), children: buildingsOf(id) }
    }),
  }
}
