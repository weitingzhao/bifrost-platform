import { ENTRY_ROUTE_IDS, SHELL_NAV, entryOf, formatShellHash, type ShellRouteId } from '@/lib/shell/consoleRoutes'

/**
 * The second generation's frame (the Owner, 2026-11-19): the game style and
 * the dashboard style are two looks of one Console, switchable. So the city
 * is not a separate map: its places ARE the Console's menu. The town is the
 * home; each entry of the menu (Command, Crew, Work, Services, System) is a
 * district; each detail page that sits under an entry is a building in that
 * district; a building's rooms are its sections. Entering a place is opening
 * a page; the place you came from shrinks to the corner.
 *
 * Nothing here is hand-kept: the tree is read from the Console's routes.
 */

export type PlaceKind = 'town' | 'district' | 'building' | 'room'

export type Place = {
  /** The path segment: a route id, or a room id inside a building. */
  id: string
  kind: PlaceKind
  label: string
  /** The business question the place answers (the Console's page question). */
  question: string
  /** The same place in the dashboard style, or null when it has none of its own yet. */
  dashboard: string | null
  children: Place[]
}

export type Path = string[]

export const TOWN_ID = 'town'

/** Rooms are the sections of a page; until a building is designed they are greybox, and say so. */
const GREYBOX_ROOMS = (owner: string): Place[] =>
  ['A', 'B', 'C'].map(r => ({ id: `room-${r.toLowerCase()}`, kind: 'room' as const, label: `Room ${r}`, question: `A greybox room of ${owner}: what it holds is designed with the building.`, dashboard: null, children: [] }))

export function placeTree(): Place {
  const nav = SHELL_NAV as readonly { id: ShellRouteId; label: string; question: string; group: string }[]
  const buildingsOf = (entry: string) =>
    nav.filter(n => n.group === 'detail' && n.id !== 'city' && entryOf({ kind: 'shell', id: n.id }) === entry)
      .map(n => ({ id: n.id, kind: 'building' as const, label: n.label, question: n.question, dashboard: formatShellHash(n.id), children: GREYBOX_ROOMS(n.label) }))
  return {
    id: TOWN_ID,
    kind: 'town',
    label: 'City',
    question: 'The whole system, one district per part of the Console.',
    dashboard: formatShellHash('command'),
    children: ENTRY_ROUTE_IDS.map(id => {
      const n = nav.find(x => x.id === id)!
      return { id, kind: 'district' as const, label: n.label, question: n.question, dashboard: formatShellHash(id), children: buildingsOf(id) }
    }),
  }
}

/** The places along a path, town first; stops at the first segment that is not there. */
export function trail(tree: Place, path: Path): Place[] {
  const out = [tree]
  for (const seg of path) {
    const next = out[out.length - 1].children.find(c => c.id === seg)
    if (!next) break
    out.push(next)
  }
  return out
}

/** A path made valid: the part of it that exists. */
export const resolve = (tree: Place, path: Path): Path => trail(tree, path).slice(1).map(p => p.id)

export const here = (tree: Place, path: Path): Place => trail(tree, path).at(-1)!

/** `#/system/infrastructure` ⇄ ['system', 'infrastructure'] — every place has an address, so Back works and a link can be sent. */
export const parsePath = (hash: string): Path => hash.replace(/^#\/?/, '').split('/').filter(Boolean).map(decodeURIComponent)
export const formatPath = (path: Path): string => `#/${path.map(encodeURIComponent).join('/')}`

/**
 * Where the children of a place stand on its ground, centred on the origin.
 * Wide screens: a square-ish grid on the ground. Tall screens (a phone held
 * upright): two columns as seen on screen, running down, so the place fills
 * the height instead of a band across the middle.
 */
export function slots(n: number, tall = false, gap = 4): { x: number; y: number }[] {
  if (tall && n > 2) {
    const cols = 2, rows = Math.ceil(n / cols)
    const U = 6, V = 9 // across and down the screen, in cells: room for a block, its height and its name
    return Array.from({ length: n }, (_, i) => {
      const u = ((i % cols) - (cols - 1) / 2) * U
      const v = (Math.floor(i / cols) - (rows - 1) / 2) * V
      return { x: (u + v) / 2, y: (v - u) / 2 }
    })
  }
  const cols = Math.max(1, Math.ceil(Math.sqrt(n)))
  const rows = Math.max(1, Math.ceil(n / cols))
  return Array.from({ length: n }, (_, i) => ({
    x: (i % cols - (cols - 1) / 2) * gap,
    y: (Math.floor(i / cols) - (rows - 1) / 2) * gap,
  }))
}
