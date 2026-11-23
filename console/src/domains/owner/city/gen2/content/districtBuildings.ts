import type { Place, State } from '@bifrost/ui/city/places'
import type { Shape } from '@bifrost/ui/city/grid'
import type { DomainHoldings } from './domainOwns'

/**
 * A domain's buildings (the Owner, 2026-11-22: the DAG must show at every level; the map line, f2c674e8): the map
 * groups each domain's Go packages into buildings by the first directory under a Go root, and GET
 * /api/v1/catalog/domain serves them, the biggest first. City draws each as a building in its district, as read:
 * nothing grouped or named here.
 *
 * A building's size is its weight in packages, by the city's standard parts (RULES.md: sizes from standard parts,
 * three classes): up to 7 packages one unit (4×4), up to 19 two (4×8), 20 or more four (8×8). Its state is its
 * district's (read when the map serves the domain's lights); what is inside it (its packages) is on its card.
 */
const sizeOf = (n: number): Shape => (n >= 20 ? '8x8' : n >= 8 ? '4x8' : '4x4')
const UNITS: Record<string, number> = { '4x4': 1, '4x8': 2, '8x8': 4 }
/** A level's budget (the frame's rule: at most 16 things on a 16×16 ground, 16 units of 4×4). */
const MOST = 16
export const OTHER = 'other'

/**
 * The buildings as they fit (the Owner, 2026-11-20: fixed slots and Other; the mess shown, bounded): the biggest
 * first while they fit the ground, one slot kept for Other; the rest go into one Other building that says how
 * many it holds. Nothing is dropped: Other's card lists them.
 */
export function buildingsOf(h: DomainHoldings | undefined, state: State, roomsOf?: (packages: string[]) => Place[]): Place[] {
  const all = h?.buildings ?? []
  const make = (id: string, label: string, n: number, question: string, shape = sizeOf(n), children: Place[] = []): Place =>
    ({ id, kind: 'building', shape, label, question, dashboard: null, state, children })
  // Its rooms, when the content gives them (the counters: one room per package, counters.ts).
  const one = (b: { id: string; packages: string[] }) => make(b.id, b.id, b.packages.length,
    `The ${b.id} building of ${h!.id}: ${b.packages.length} Go package${b.packages.length === 1 ? '' : 's'}.`, sizeOf(b.packages.length), roomsOf?.(b.packages) ?? [])
  const fits = (xs: { packages: string[] }[], extra: number) => xs.length + extra <= MOST && xs.reduce((u, b) => u + UNITS[sizeOf(b.packages.length)], 0) + extra <= MOST
  if (fits(all, 0)) return all.map(one)
  const kept: typeof all = []
  for (const b of all) if (fits([...kept, b], 1)) kept.push(b)
  const rest = all.filter(b => !kept.includes(b))
  const pkgs = rest.reduce((n, b) => n + b.packages.length, 0)
  // Drawn as a construction site: more than a level holds is cut too fine, work to do (the Owner, 2026-11-22:
  // what does not fit should be decoupled or merged), never hidden.
  const other = make(OTHER, `Other · ${rest.length}`, pkgs,
    `${rest.length} more buildings of ${h!.id} (${pkgs} packages) do not fit its ground: cut too fine, to decouple or merge: ${rest.map(b => b.id).join(', ')}.`, '4x4')
  return [...kept.map(one), { ...other, landmark: 'site' as const }]
}

/** How far a domain's buildings go past its ground (0 when they fit): the district card says it. */
export function overflowOf(h: DomainHoldings | undefined): { buildings: number; over: number } {
  const n = h?.buildings?.length ?? 0
  return { buildings: n, over: Math.max(0, n - MOST) }
}
