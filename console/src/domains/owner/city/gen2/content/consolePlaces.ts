import { shapeByCount, type Shape } from '../frame/grid'
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
 * Sizes and rooms (the Owner, 2026-11-19: nothing made up). A district's size comes from how many
 * pages it holds; a page's weight is not measured yet, so every page is the same 2×2 and has no rooms
 * until its sections are read from something real. The sample sizes and the greybox rooms A–F that
 * stood here were taken out the day that rule was set.
 */
const PAGE: Shape = '2x2'

/** The Console's functions: its menu entries. Each district and building serves the entry it sits under. */
export const consoleFunctions = () => {
  const nav = SHELL_NAV as readonly { id: ShellRouteId; label: string }[]
  return ENTRY_ROUTE_IDS.map(id => ({ id, label: nav.find(x => x.id === id)!.label }))
}

export function placeTree(): Place {
  const nav = SHELL_NAV as readonly { id: ShellRouteId; label: string; question: string; group: string }[]
  const buildingsOf = (entry: string) =>
    nav.filter(n => n.group === 'detail' && n.id !== 'city' && entryOf({ kind: 'shell', id: n.id }) === entry)
      .map(n => ({ id: n.id, function: entry, kind: 'building' as const, shape: PAGE, label: n.label, question: n.question, dashboard: formatShellHash(n.id), children: [] }))
  return {
    id: TOWN_ID,
    kind: 'town',
    shape: '8x8',
    label: 'City',
    question: 'The whole system, one district per part of the Console.',
    dashboard: formatShellHash('command'),
    children: ENTRY_ROUTE_IDS.map(id => {
      const n = nav.find(x => x.id === id)!
      const pages = buildingsOf(id)
      return { id, function: id, kind: 'district' as const, shape: shapeByCount(pages.length), label: n.label, question: n.question, dashboard: formatShellHash(id), children: pages }
    }),
  }
}
