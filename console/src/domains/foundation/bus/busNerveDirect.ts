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

/** Height between tiers, and a tier disc's radius when it has more than one domain. */
export const TIER_HEIGHT = 110
export const TIER_RADIUS = 150

/** A domain as the ownership table declares it; tier absent when the table gives none. */
export type DomainTier = { id: string; tier?: number }

/**
 * Where each domain sits: y is its tier's height; on its tier's disc the
 * domains are spread evenly in name order (deterministic), a lone one at the
 * disc's centre. A domain with no tier stands on the tier just above the
 * highest declared, so it shows apart instead of being put somewhere wrong.
 */
export function layerPositions(domains: readonly DomainTier[]): Map<string, Vec> {
  const declared = domains.filter(d => d.tier != null).map(d => d.tier as number)
  const undeclared = declared.length > 0 ? Math.max(...declared) + 1 : 0
  const byTier = new Map<number, string[]>()
  for (const d of domains) {
    const t = d.tier ?? undeclared
    byTier.set(t, [...(byTier.get(t) ?? []), d.id])
  }
  const out = new Map<string, Vec>()
  const tiers = [...byTier.keys()].sort((a, b) => a - b)
  for (const t of tiers) {
    const ids = [...(byTier.get(t) ?? [])].sort()
    // Each tier turned a little from the one below, so a domain does not sit straight over another.
    const turn = t * 0.6
    ids.forEach((id, i) => {
      const r = ids.length === 1 ? 0 : TIER_RADIUS
      const a = turn + (2 * Math.PI * i) / ids.length
      out.set(id, { x: Math.cos(a) * r, y: t * TIER_HEIGHT, z: Math.sin(a) * r })
    })
  }
  return out
}

/** The tiers present, each with its height: the rings that show the floors. */
export function tierRings(positions: Map<string, Vec>): { y: number; radius: number }[] {
  const byY = new Map<number, number>()
  for (const p of positions.values()) byY.set(p.y, Math.max(byY.get(p.y) ?? 0, Math.hypot(p.x, p.z)))
  return [...byY.entries()].sort((a, b) => a[0] - b[0]).map(([y, r]) => ({ y, radius: Math.max(70, r + 60) }))
}

/**
 * One way a topic is taken: a reader the code runs (the register's
 * consumers, each declared with its domain in the clients' list) or a
 * subscription row (its consumer is sub-<row>, its domain the row's target).
 * The Bus page itself (console) watches every topic and is not a reader.
 */
export type Route = { topic: string; reader: string; domain: string }

export const ROW_CONSUMER_PREFIX = 'sub-'

export function routesOf(c: BusContract): Route[] {
  const domainOfClient = new Map(c.clients.filter(x => x.domain).map(x => [x.name, x.domain as string]))
  const topics = new Set(c.topics.map(t => t.type))
  const out: Route[] = []
  for (const t of c.topics) {
    for (const r of t.consumers ?? []) {
      const d = domainOfClient.get(r)
      if (d) out.push({ topic: t.type, reader: r, domain: d })
    }
  }
  for (const row of c.subscriptions) {
    if (!row.domain) continue
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
