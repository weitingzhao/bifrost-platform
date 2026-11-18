import { arcsOf, halosOf, type Route } from '../bus/busNerveDirect'
import type { BusListening } from '../bus/busNerve'

/**
 * The Ops home's launch site (the Owner via the coordinator, 2026-11-17):
 * the platform is a rocket, the tenant (Trade) its payload, CI builds the
 * rockets, each release is a launch, what is up is watched by telemetry,
 * the seats are the engineers, the base and compute the ground support, the
 * lineage the archive, the Owner and the plan mission control. Each domain
 * is a facility on the site and opens its page. Which facility a domain is
 * and which page it opens come from the domains' table (served with them);
 * where each kind of facility stands on the site is the site plan below (a
 * drawing, like a map's legend, not a reading). Every light, arc and pulse
 * is a reading: the same bus contract, listening and stream as the nerve map.
 */

export type FacilityKind =
  | 'mission-control' | 'planning-office' | 'comms-tower' | 'assembly-building' | 'launch-pad'
  | 'payload-hangar' | 'archive' | 'ground-support' | 'server-hall'

/** A domain as the table declares it, with its facility when it has one. */
export type DomainFacility = { id: string; tier?: number; facility?: { kind: string; page: string } }

export type Facility = {
  domain: string
  kind: FacilityKind
  page: string
  /** Where it stands on the ground (x, z), from the site plan. */
  at: { x: number; z: number }
  /** Its height, for arcs and the beacon. */
  height: number
  state: FacilityState
  note: string
}

/**
 * What a facility's beacon says: quiet (its domain published nothing in the
 * window), heard (what it published was taken), or sending what nobody takes
 * (a halo, as on the nerve map).
 */
export type FacilityState = 'quiet' | 'heard' | 'unheard'

/** The site plan: where each kind of facility stands, and how tall it is. */
export const SITE_PLAN: Record<FacilityKind, { x: number; z: number; height: number; label: string }> = {
  'ground-support': { x: -85, z: -25, height: 16, label: 'ground support' },
  'server-hall': { x: -60, z: -65, height: 9, label: 'server hall' },
  'comms-tower': { x: 0, z: 0, height: 34, label: 'comms tower' },
  'archive': { x: -30, z: -45, height: 12, label: 'archive' },
  'assembly-building': { x: 40, z: -25, height: 36, label: 'assembly building' },
  'launch-pad': { x: 95, z: 10, height: 40, label: 'launch pad' },
  'payload-hangar': { x: 60, z: 50, height: 14, label: 'payload hangar' },
  'planning-office': { x: -40, z: 60, height: 12, label: 'planning office' },
  'mission-control': { x: -70, z: 35, height: 14, label: 'mission control' },
}

const KINDS = new Set(Object.keys(SITE_PLAN))

function isKind(k: string): k is FacilityKind {
  return KINDS.has(k)
}

/**
 * The facilities: each domain the table gives a known facility, at its place
 * on the plan, with its beacon from the listening reading. A domain with no
 * facility, or a kind the plan does not know, is left off (and listed, so
 * the page can say so).
 */
export function facilitiesOf(domains: readonly DomainFacility[], routes: readonly Route[], domainOfType: Map<string, string>, listening?: BusListening): { facilities: Facility[]; missing: string[] } {
  const halos = halosOf(routes, domainOfType, listening)
  const published = new Map<string, number>()
  for (const t of listening?.topics ?? []) {
    const d = domainOfType.get(t.type)
    if (d) published.set(d, (published.get(d) ?? 0) + t.published)
  }
  const facilities: Facility[] = []
  const missing: string[] = []
  const taken = new Set<FacilityKind>()
  for (const d of [...domains].sort((a, b) => a.id.localeCompare(b.id))) {
    const f = d.facility
    if (!f || !isKind(f.kind) || taken.has(f.kind)) {
      missing.push(d.id)
      continue
    }
    taken.add(f.kind)
    const plan = SITE_PLAN[f.kind]
    const n = published.get(d.id) ?? 0
    const h = halos.get(d.id)
    const state: FacilityState = h ? 'unheard' : n > 0 ? 'heard' : 'quiet'
    const hours = listening?.hours ?? 24
    facilities.push({
      domain: d.id, kind: f.kind, page: f.page, at: { x: plan.x, z: plan.z }, height: plan.height, state,
      note: `${plan.label} · domain ${d.id} · ${n} events in ${hours}h${h ? ` · sends what nobody takes: ${h.topics.join(', ')} (${h.count})` : ''} · click to open`,
    })
  }
  return { facilities, missing }
}

/** A cable run between two facilities: one domain's topics taken by another's reader. */
export type SiteArc = { id: string; from: string; to: string; count: number; label: string }

export function siteArcs(routes: readonly Route[], domainOfType: Map<string, string>, facilities: readonly Facility[], listening?: BusListening): SiteArc[] {
  const here = new Set(facilities.map(f => f.domain))
  return arcsOf(routes, domainOfType, listening)
    .filter(a => a.from !== a.to && here.has(a.from) && here.has(a.to))
    .map(a => ({ id: a.id, from: a.from, to: a.to, count: a.count, label: `${a.from} → ${a.to}: ${a.topics.join(', ')} · read by ${a.readers.join(', ')} · ${a.count} events` }))
}

export type Ground = { x: number; z: number }

/** Where a cable leaves or reaches a facility: its front door, on the ground. */
export function doorOf(f: Facility): Ground {
  return { x: f.at.x, z: f.at.z + 16 }
}

/**
 * A cable run on the ground, laid as on a real site (the Owner, 2026-11-17:
 * no arcs in the sky; trays and ducts, square, turning at right angles): from
 * the sender's door along x to the reader's column, then along z to its door.
 * The turn is the junction box, where a pulse waits in transit for the
 * reader's receipt. Each run is shifted a little sideways by its id, so runs
 * that share a stretch lie side by side, not on one another.
 */
export function cableRun(from: Ground, to: Ground, id: string): { points: Ground[]; junction: number } {
  let h = 2166136261
  for (let i = 0; i < id.length; i++) h = Math.imul(h ^ id.charCodeAt(i), 16777619)
  const lane = (((h >>> 0) % 7) - 3) * 2.2
  const a = { x: from.x, z: from.z + lane }
  const b = { x: to.x + lane, z: to.z }
  const corner = { x: b.x, z: a.z }
  return { points: [from, a, corner, b, to], junction: 2 }
}

/** The length of a run up to each of its points. */
export function runLengths(points: readonly Ground[]): number[] {
  const out = [0]
  for (let i = 1; i < points.length; i++) out.push(out[i - 1] + Math.hypot(points[i].x - points[i - 1].x, points[i].z - points[i - 1].z))
  return out
}

/** The point at a distance along a run. */
export function pointAt(points: readonly Ground[], lengths: readonly number[], d: number): Ground {
  const total = lengths[lengths.length - 1]
  const at = Math.max(0, Math.min(total, d))
  for (let i = 1; i < points.length; i++) {
    if (at <= lengths[i] || i === points.length - 1) {
      const seg = lengths[i] - lengths[i - 1] || 1
      const k = (at - lengths[i - 1]) / seg
      return { x: points[i - 1].x + (points[i].x - points[i - 1].x) * k, z: points[i - 1].z + (points[i].z - points[i - 1].z) * k }
    }
  }
  return points[points.length - 1]
}

/**
 * A launch: a release finished and succeeded (release.deploy.finished, its
 * subject "<env>/<app>@<sha>"). The platform's (app "platform") goes up as
 * the rocket, a tenant's as its payload; a failed one launches nothing.
 */
export function launchOf(type: string, subject: string, change?: string): { what: 'rocket' | 'payload'; env: string; app: string } | null {
  if (type !== 'release.deploy.finished' || (change != null && change !== 'succeeded')) return null
  const m = /^([^/]+)\/([^@]+)@/.exec(subject)
  if (!m) return null
  return { what: m[2] === 'platform' ? 'rocket' : 'payload', env: m[1], app: m[2] }
}
