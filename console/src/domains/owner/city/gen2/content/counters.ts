import type { Place } from '@bifrost/ui/city/places'
import type { Shape } from '@bifrost/ui/city/grid'
import { counterRoom, counterRoomKey, counterRoomShape, type CounterRoomReadings, type CounterRoute } from '@bifrost/ui/city/units/counterRoom'
import type { DomainHoldings } from './domainOwns'
import type { PromPoint } from './hardware'
import QUERIES from './counterQueries.json'

/**
 * The counter rooms (箭10 2026-11-23; the engine's counter room, bifrost-ui 0.41.0): a software building's rooms
 * are its Go packages, each a drive-through with a kiosk per route it handles. Everything is read, nothing matched
 * by hand:
 *   rooms      a building's packages, as the map groups them (/catalog/domain buildings[].packages)
 *   routes     every domain's contract request_response, put in the package its handler lives in (the part
 *              before the last dot); a route another domain promises carries that domain (promisedBy): the
 *              mismatch shows where the work is done (verification's routes served in assembly/server, as it is)
 *   readings   requests a minute and 5xx a minute over the last hour, per (handler, method): the metrics line's
 *              recording rules bifrost_http:requests_per_minute_1h and bifrost_http:errors_5xx_per_minute_1h
 *              (in PROD 2026-11-23, card #709; counterQueries.json names them), matched exactly to the route's
 *              method and path. A route with traffic but no 5xx series had no 5xx: 0 (the rule's own reading)
 * A route with no series is shuttered (not asked since its process started: not measured, not zero); a route the
 * metric cannot name (a wildcard proxy) is shuttered with why. A package with no route is a room that says so.
 */
export const COUNTER_QUERIES = QUERIES as { perMin: string; failPerMin: string }

/** Per "METHOD path": the reading, from the two queries; null when a query was not read. */
export type CounterReads = { perMin: Map<string, number> | null; failPerMin: Map<string, number> | null }
const keyOf = (method: string, path: string) => `${method.toUpperCase()} ${path}`

export function readingsOf(perMin: PromPoint[] | null, failPerMin: PromPoint[] | null): CounterReads {
  const map = (pts: PromPoint[] | null) => pts ? new Map(pts.filter(p => p.labels?.handler && p.labels?.method).map(p => [keyOf(p.labels!.method, p.labels!.handler), p.value] as const)) : null
  return { perMin: map(perMin), failPerMin: map(failPerMin) }
}

type ContractRoute = { method?: string; path?: string; handler?: string }
const packageOfHandler = (handler: string) => handler.includes('.') ? handler.slice(0, handler.lastIndexOf('.')) : handler

/** Every route of every domain's contract, by the package whose handler serves it. */
export function routesByPackage(holdings: Record<string, DomainHoldings>, reads: CounterReads): Map<string, CounterRoute[]> {
  const owner = new Map<string, string>()
  for (const h of Object.values(holdings)) for (const b of h.buildings ?? []) for (const p of b.packages) owner.set(p, h.id)
  const out = new Map<string, CounterRoute[]>()
  for (const h of Object.values(holdings)) {
    const rr = (h.contract?.request_response ?? []) as ContractRoute[]
    for (const r of rr) {
      if (!r.method || !r.path || !r.handler) continue
      const pkg = packageOfHandler(r.handler)
      const k = keyOf(r.method, r.path)
      const wildcard = /\*/.test(r.path)
      const perMin = wildcard || !reads.perMin ? null : reads.perMin.get(k) ?? null
      // A 5xx series exists only once a 5xx happened since the process started: none, with traffic read, is 0.
      const failPerMin = perMin == null || !reads.failPerMin ? null : reads.failPerMin.get(k) ?? 0
      const route: CounterRoute = {
        method: r.method.toUpperCase(), path: r.path, perMin, failPerMin,
        ...(owner.get(pkg) && owner.get(pkg) !== h.id ? { promisedBy: h.id } : {}),
        ...(wildcard ? { unreadable: 'a wildcard proxy route: the metric does not name it' } : !reads.perMin ? { unreadable: 'the request readings were not read' } : {}),
      }
      out.set(pkg, [...(out.get(pkg) ?? []), route])
    }
  }
  return out
}

const shapeOf = ([w, d]: [number, number]) => (w <= d ? `${w}x${d}` : `${d}x${w}`) as Shape

/** A package's room: its counter, drawn by the engine from its routes. */
export function counterPlace(pkg: string, routes: CounterRoute[], state: Place['state']): Place {
  const r: CounterRoomReadings = { routes }
  const [w, d] = counterRoomShape(routes.length)
  const name = pkg.split('/').at(-1) ?? pkg
  return {
    id: pkg, kind: 'room', shape: shapeOf([w, d]), label: name, dashboard: null, state, children: [],
    question: `The ${pkg} package: ${routes.length} route${routes.length === 1 ? '' : 's'} it serves, how often each is asked, and its 5xx.`,
    picture: { key: `counter:${pkg}:${counterRoomKey(r)}`, w, d, make: () => counterRoom(r) },
  }
}

const UNITS: Record<string, number> = { '4x4': 1, '4x8': 2, '8x8': 4 }
const MOST = 16

/**
 * A building's rooms as they fit its level (the frame's rule, as for buildings in a district: at most 16 things,
 * 16 units of 4×4): the packages with the most routes first, one slot kept for Other; the rest in one Other room
 * that names them, drawn without a picture (not drawn here, never dropped).
 */
export function counterRooms(packages: string[], routes: Map<string, CounterRoute[]>, state: Place['state']): Place[] {
  const rooms = packages.map(p => counterPlace(p, routes.get(p) ?? [], state))
  const units = (xs: Place[]) => xs.reduce((u, r) => u + (UNITS[r.shape] ?? 1), 0)
  if (rooms.length <= MOST && units(rooms) <= MOST) return rooms
  const order = [...rooms].sort((a, b) => (routes.get(b.id)?.length ?? 0) - (routes.get(a.id)?.length ?? 0))
  const kept: Place[] = []
  for (const r of order) if (kept.length + 2 <= MOST && units([...kept, r]) + 1 <= MOST) kept.push(r)
  const rest = rooms.filter(r => !kept.includes(r))
  return [...kept, {
    id: 'other', kind: 'room', shape: '4x4', label: `Other · ${rest.length}`, dashboard: null, state, children: [],
    question: `${rest.length} more packages do not fit this building's level: ${rest.map(r => r.id).join(', ')}.`,
  }]
}
