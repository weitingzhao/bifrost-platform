import type { Place } from '@bifrost/ui/city/places'
import type { Shape } from '@bifrost/ui/city/grid'
import { counterBuildingSummaryOf, counterRoom, counterRoomKey, counterRoomShape, type CounterRoomReadings, type CounterRoute } from '@bifrost/ui/city/units/counterRoom'
import type { DomainHoldings } from './domainOwns'
import type { PromPoint } from './hardware'
import QUERIES from './counterQueries.json'
import type { OffContractRead } from '@bifrost/ui/city/offContract'

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
export type CounterQueries = { perMin: string; failPerMin: string; namespace: string }
const Q = QUERIES as { perMin: string; failPerMin: string; namespaces: Record<string, string> }

/**
 * The two readings for the platform of the environment this Console runs in (箭10 2026-11-23: STG showed PROD's
 * traffic, the namespace was written as bifrost-platform-prod). null for an environment with no platform namespace
 * (dev, dev-local): the counters then say they were not read, never another environment's numbers.
 */
export function counterQueriesFor(env: string | null | undefined): CounterQueries | null {
  const namespace = env ? Q.namespaces[env] : undefined
  if (!namespace) return null
  const at = (q: string) => q.replace('{namespace}', namespace)
  return { perMin: at(Q.perMin), failPerMin: at(Q.failPerMin), namespace }
}

/** Per "METHOD path": the reading, from the two queries; null when a query was not read. */
export type CounterReads = { perMin: Map<string, number> | null; failPerMin: Map<string, number> | null; source?: string; why?: string }
const keyOf = (method: string, path: string) => `${method.toUpperCase()} ${path}`

export function readingsOf(perMin: PromPoint[] | null, failPerMin: PromPoint[] | null, source?: string): CounterReads {
  const map = (pts: PromPoint[] | null) => pts ? new Map(pts.filter(p => p.labels?.handler && p.labels?.method).map(p => [keyOf(p.labels!.method, p.labels!.handler), p.value] as const)) : null
  return { perMin: map(perMin), failPerMin: map(failPerMin), ...(source ? { source } : {}) }
}

/** Not read, and why (an environment with no platform namespace). */
export const readingsNotRead = (why: string): CounterReads => ({ perMin: null, failPerMin: null, why })

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
        ...(wildcard ? { unreadable: 'a wildcard proxy route: the metric does not name it' } : !reads.perMin ? { unreadable: reads.why ?? 'the request readings were not read' } : {}),
      }
      out.set(pkg, [...(out.get(pkg) ?? []), route])
    }
  }
  return out
}

const shapeOf = ([w, d]: [number, number]) => (w <= d ? `${w}x${d}` : `${d}x${w}`) as Shape

/** A package's room: its counter, drawn by the engine from its routes. */
/**
 * A room's id: the package path with its slashes turned to dots. The engine refuses an id with / # ? or spaces
 * (rules.ts: an id is one step of an address); a package path is a path, so taken as is it refused the whole City
 * on PROD once the buildings were read (2026-11-23 23:16Z).
 */
export const roomIdOf = (pkg: string) => pkg.replace(/[/#?\s]/g, '.')

export function counterPlace(pkg: string, routes: CounterRoute[], state: Place['state']): Place {
  const r: CounterRoomReadings = { routes }
  const [w, d] = counterRoomShape(routes.length)
  const name = pkg.split('/').at(-1) ?? pkg
  return {
    id: roomIdOf(pkg), kind: 'room', shape: shapeOf([w, d]), label: name, dashboard: null, state, children: [],
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
  const routesOf = new Map(packages.map(p => [roomIdOf(p), routes.get(p)?.length ?? 0]))
  const units = (xs: Place[]) => xs.reduce((u, r) => u + (UNITS[r.shape] ?? 1), 0)
  if (rooms.length <= MOST && units(rooms) <= MOST) return rooms
  const order = [...rooms].sort((a, b) => (routesOf.get(b.id) ?? 0) - (routesOf.get(a.id) ?? 0))
  const kept: Place[] = []
  for (const r of order) if (kept.length + 2 <= MOST && units([...kept, r]) + 1 <= MOST) kept.push(r)
  const rest = rooms.filter(r => !kept.includes(r))
  return [...kept, {
    id: 'other', kind: 'room', shape: '4x4', label: `Other · ${rest.length}`, dashboard: null, state, children: [],
    question: `${rest.length} more packages do not fit this building's level: ${packages.filter(p => rest.some(r => r.id === roomIdOf(p))).join(', ')}.`,
  }]
}

/**
 * A building's one reading outside (the engine's stage 4, 0.48.0): its requests a minute, summed over its packages'
 * counters by the engine's own counterBuildingSummaryOf (never a second sum here), read from the requests rule.
 * Not read: perMin null, no cars at the door.
 */
export function buildingFlow(packages: string[], routes: Map<string, CounterRoute[]>, source: string): NonNullable<Place['flow']> {
  const s = counterBuildingSummaryOf(packages.map(p => ({ routes: routes.get(p) ?? [] })))
  return { perMin: s.perMin, source }
}

/**
 * Requests to routes no domain's contract names (the engine's off-contract card, bifrost-ui 0.52.0): from the same
 * requests reading the counters use (never a second read), the (method, handler) pairs that match no contract's
 * (method, path). A wildcard proxy route in a contract matches no label, so its traffic lands here, as it is (the
 * card says the map cannot tell which package serves it). contractPerMin is the matched part, the counters' own sum.
 * The readings not read: not "none", the card says why.
 */
export function offContractOf(holdings: Record<string, DomainHoldings>, reads: CounterReads): OffContractRead {
  const source = reads.source ?? 'bifrost_http:requests_per_minute_1h{namespace=?}'
  if (!reads.perMin) return { source, why: reads.why ?? 'the request readings were not read' }
  const promised = new Set<string>()
  for (const h of Object.values(holdings)) for (const r of (h.contract?.request_response ?? []) as ContractRoute[]) if (r.method && r.path) promised.add(keyOf(r.method, r.path))
  if (promised.size === 0) return { source, why: 'no domain\'s contract was read (the map\'s domains)' }
  const routes: { method: string; path: string; perMin: number }[] = []
  let contractPerMin = 0
  for (const [k, perMin] of reads.perMin) {
    if (promised.has(k)) { contractPerMin += perMin; continue }
    const i = k.indexOf(' ')
    routes.push({ method: k.slice(0, i), path: k.slice(i + 1), perMin })
  }
  return { routes: routes.sort((a, b) => b.perMin - a.perMin), contractPerMin, source }
}
