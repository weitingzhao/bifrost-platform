import type { BusContract } from './busNextgen'
import type { BusListening } from './busNerve'

/**
 * The nerve map's default view (the Owner via the coordinator, 2026-11-17):
 * who listens to whom, drawn straight. A domain sits at the height of its
 * tier (the ownership table's own field: base at the bottom, the Owner on
 * top), on that tier's disc at an angle fixed by name. A topic one domain
 * publishes and another's reader takes is an arc between the two domains,
 * with no bus in the middle: the bus is the bridge, not a place. A topic
 * nobody subscribes to is a halo round its publisher's domain. Everything
 * here is read from the contract and the listening reading; nothing is
 * placed by hand.
 */

export type Vec = { x: number; y: number; z: number }

/**
 * The big bang (the Owner via the coordinator, 2026-11-17): the base is the
 * core at the origin and the body opens forward from it into a cone. A
 * domain's distance from the core along the cone's axis is its tier; the
 * tiers' sections widen as they go out. Dependence runs from the core out:
 * an arc going in calls a tier below, one going out reports up.
 */
export const AXIS: Vec = (() => { const l = Math.hypot(1, 0.45); return { x: 1 / l, y: 0.45 / l, z: 0 } })()
/** Distance from the core between tiers, the cone's widening per unit out, and its waist at the core. */
export const TIER_STEP = 100
const CONE_SLOPE = 0.32
const CONE_WAIST = 38
/** How far a domain may sit off its tier's distance, along the axis: under half a step, so tiers never mix. */
const TIER_JITTER = 0.3
/** The least distance between two domains' centres (their clouds do not run into one another). */
export const MIN_APART = 78

/** A domain as the ownership table declares it; tier absent when the table gives none. */
export type DomainTier = { id: string; tier?: number }

/** A number in [0, 1) fixed by a name. */
function hashUnit(name: string): number {
  let h = 2166136261
  for (let i = 0; i < name.length; i++) h = Math.imul(h ^ name.charCodeAt(i), 16777619)
  h = Math.imul(h ^ (h >>> 13), 3266489909)
  return ((h ^ (h >>> 16)) >>> 0) / 4294967296
}

/** Two unit vectors square to the axis and to each other: the plane of a cone's section. */
const U: Vec = { x: -AXIS.y, y: AXIS.x, z: 0 }
const V: Vec = { x: 0, y: 0, z: 1 }

const add = (a: Vec, b: Vec, k = 1): Vec => ({ x: a.x + b.x * k, y: a.y + b.y * k, z: a.z + b.z * k })
const dot = (a: Vec, b: Vec) => a.x * b.x + a.y * b.y + a.z * b.z

/** How far out along the cone a place is (0 at the core). */
export function alongAxis(p: Vec): number {
  return dot(p, AXIS)
}

/**
 * Where each domain sits: out from the core by its tier, on its tier's
 * section of the cone as on a sunflower (golden angle, in name order), a
 * little nearer or further by its name; then pushed apart, only within
 * their sections, until no two are closer than MIN_APART. Same input, same
 * place. A domain with no tier stands one step beyond the furthest declared.
 */
export function layerPositions(domains: readonly DomainTier[]): Map<string, Vec> {
  const declared = domains.filter(d => d.tier != null).map(d => d.tier as number)
  const undeclared = declared.length > 0 ? Math.max(...declared) + 1 : 0
  const byTier = new Map<number, string[]>()
  for (const d of domains) {
    const t = d.tier ?? undeclared
    byTier.set(t, [...(byTier.get(t) ?? []), d.id])
  }
  const golden = Math.PI * (3 - Math.sqrt(5))
  const ids: string[] = []
  const at: Vec[] = []
  for (const t of [...byTier.keys()].sort((a, b) => a - b)) {
    const list = [...(byTier.get(t) ?? [])].sort()
    const d = t * TIER_STEP
    const R = CONE_WAIST + d * CONE_SLOPE
    list.forEach((id, i) => {
      const a = t * 1.3 + i * golden
      const r = list.length === 1 ? R * 0.2 : R * Math.sqrt((i + 0.5) / list.length)
      const along = d + (hashUnit(`a:${id}`) - 0.5) * TIER_STEP * TIER_JITTER
      ids.push(id)
      at.push(add(add({ x: AXIS.x * along, y: AXIS.y * along, z: AXIS.z * along }, U, Math.cos(a) * r), V, Math.sin(a) * r))
    })
  }
  // Apart: push pairs that are too close away from each other within their sections (the tier's distance stays).
  for (let pass = 0; pass < 80; pass++) {
    let moved = false
    for (let i = 0; i < at.length; i++) {
      for (let j = i + 1; j < at.length; j++) {
        let dv = add(at[j], at[i], -1)
        dv = add(dv, AXIS, -dot(dv, AXIS))
        let dist = Math.hypot(dv.x, dv.y, dv.z)
        const full = Math.hypot(at[j].x - at[i].x, at[j].y - at[i].y, at[j].z - at[i].z)
        if (full >= MIN_APART) continue
        if (dist < 1e-6) { dv = { ...U }; dist = 1 }
        const k = (MIN_APART - full) / 2 / dist + 0.001
        at[i] = add(at[i], dv, -k)
        at[j] = add(at[j], dv, k)
        moved = true
      }
    }
    if (!moved) break
  }
  return new Map(ids.map((id, i) => [id, at[i]]))
}

/**
 * One way a topic is taken: a reader the code runs (the register's
 * consumers, each declared with its domain in the clients' list) or a
 * subscription row (its consumer is sub-<row>, its domain the row's target).
 * The Bus page itself (console) watches every topic and is not a reader.
 */
export type Route = { topic: string; reader: string; domain: string }

export const ROW_CONSUMER_PREFIX = 'sub-'

/** env: this platform's environment (GET /bus/clients); a reader or row declared for other environments only does not read here. */
export function routesOf(c: BusContract, env?: string): Route[] {
  const here = (envs?: string[]) => !env || !envs || envs.length === 0 || envs.includes(env)
  const domainOfClient = new Map(c.clients.filter(x => x.domain && here(x.envs)).map(x => [x.name, x.domain as string]))
  const topics = new Set(c.topics.map(t => t.type))
  const out: Route[] = []
  for (const t of c.topics) {
    for (const r of t.consumers ?? []) {
      const d = domainOfClient.get(r)
      if (d) out.push({ topic: t.type, reader: r, domain: d })
    }
  }
  for (const row of c.subscriptions) {
    if (!row.domain || !here(row.envs)) continue
    for (const on of row.on) if (topics.has(on)) out.push({ topic: on, reader: ROW_CONSUMER_PREFIX + row.id, domain: row.domain })
  }
  return out
}

/** An arc: every route from one domain's topics to another domain's readers, as one line. */
export type Arc = {
  id: string
  from: string
  to: string
  topics: string[]
  readers: string[]
  /** Events of its topics in the reading's window (the listening reading's published counts). */
  count: number
}

export function arcId(from: string, to: string): string {
  return `arc:${from}>${to}`
}

export function arcsOf(routes: readonly Route[], domainOfType: Map<string, string>, listening?: BusListening): Arc[] {
  const published = new Map((listening?.topics ?? []).map(t => [t.type, t.published]))
  const by = new Map<string, Arc>()
  for (const r of routes) {
    const from = domainOfType.get(r.topic)
    if (!from) continue
    const id = arcId(from, r.domain)
    const a = by.get(id) ?? { id, from, to: r.domain, topics: [], readers: [], count: 0 }
    if (!a.topics.includes(r.topic)) {
      a.topics.push(r.topic)
      a.count += published.get(r.topic) ?? 0
    }
    if (!a.readers.includes(r.reader)) a.readers.push(r.reader)
    by.set(id, a)
  }
  return [...by.values()].map(a => ({ ...a, topics: a.topics.sort(), readers: a.readers.sort() })).sort((a, b) => a.id.localeCompare(b.id))
}

/**
 * The halo of each domain that publishes topics nobody subscribes to: how
 * many such events it sent in the window, and which topics. A display-only
 * topic is watched, not sent to anyone, and makes no halo.
 */
export function halosOf(routes: readonly Route[], domainOfType: Map<string, string>, listening?: BusListening): Map<string, { count: number; topics: string[] }> {
  const taken = new Set(routes.map(r => r.topic))
  const out = new Map<string, { count: number; topics: string[] }>()
  for (const t of listening?.topics ?? []) {
    if (taken.has(t.type) || t.display_only || t.published === 0) continue
    const d = domainOfType.get(t.type)
    if (!d) continue
    const h = out.get(d) ?? { count: 0, topics: [] }
    h.count += t.published
    h.topics.push(t.type)
    out.set(d, h)
  }
  return out
}

/**
 * An arc's curve: a cubic from the publisher's domain to the reader's,
 * raised above the straight line by its length (and to the side, so the arc
 * back the other way does not lie on it). A domain's arc to itself is a loop
 * over it. The renderer draws it; the midpoint (t = 0.5) is where a pulse
 * waits in the bus.
 */
export function arcCurve(a: Vec, b: Vec): [Vec, Vec, Vec, Vec] {
  const dx = b.x - a.x, dy = b.y - a.y, dz = b.z - a.z
  const len = Math.hypot(dx, dy, dz)
  if (len < 1) {
    // A loop: out to one side and up, back from the other.
    return [a, { x: a.x + 60, y: a.y + 70, z: a.z + 20 }, { x: a.x - 60, y: a.y + 70, z: a.z - 20 }, a]
  }
  // Sideways: horizontal and square to the line (the line's own cross with up).
  let sx = -dz, sz = dx
  const sl = Math.hypot(sx, sz) || 1
  sx /= sl; sz /= sl
  const lift = 25 + len * 0.3
  const side = 14
  const c1 = { x: a.x + dx / 3 + sx * side, y: a.y + dy / 3 + lift, z: a.z + dz / 3 + sz * side }
  const c2 = { x: a.x + (2 * dx) / 3 + sx * side, y: a.y + (2 * dy) / 3 + lift, z: a.z + (2 * dz) / 3 + sz * side }
  return [a, c1, c2, b]
}

/** A point of a cubic at t. */
export function cubicAt([p0, p1, p2, p3]: [Vec, Vec, Vec, Vec], t: number): Vec {
  const u = 1 - t
  const k0 = u * u * u, k1 = 3 * u * u * t, k2 = 3 * u * t * t, k3 = t * t * t
  return {
    x: k0 * p0.x + k1 * p1.x + k2 * p2.x + k3 * p3.x,
    y: k0 * p0.y + k1 * p1.y + k2 * p2.y + k3 * p3.y,
    z: k0 * p0.z + k1 * p1.z + k2 * p2.z + k3 * p3.z,
  }
}

/** The median of a list of seconds, or undefined for none. */
export function median(xs: readonly number[]): number | undefined {
  if (xs.length === 0) return undefined
  const s = [...xs].sort((a, b) => a - b)
  const m = Math.floor(s.length / 2)
  return s.length % 2 ? s[m] : (s[m - 1] + s[m]) / 2
}
