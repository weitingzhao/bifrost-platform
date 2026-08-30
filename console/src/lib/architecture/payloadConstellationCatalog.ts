/**
 * Payload constellation — multi-instrument Satellite vehicle (Plan C).
 *
 * Rocket = control plane launcher. Satellite = on-orbit vehicle (display-host = Trade).
 * Payloads / instruments ride the vehicle; constellation edges decide who must/suggests
 * fly together. New payloads add a catalog row + Launch child + edges — not a Briefing Line.
 *
 * System Domain `research` stays for D13 write boundaries; domain ≠ top-level sidebar group.
 */

import type { DeliveryTargetId } from '@/lib/delivery/deliveryTargets'
import type { ShellNavItem } from '@bifrost/ui'

export const PAYLOAD_CONSTELLATION_VERSION = '2026-08-29'
export const PAYLOAD_CONSTELLATION_SOURCE =
  'console/src/lib/architecture/payloadConstellationCatalog.ts'

export type PayloadId = 'trade' | 'research'

export type PayloadVehicle = 'satellite' | 'rocket'

export type PayloadRole = 'display-host' | 'instrument'

export type CouplingKind = 'api-contract' | 'ui-surface' | 'schema'

export type CouplingStrength = 'requires' | 'suggests'

export type ConstellationEdge = {
  from: PayloadId
  to: PayloadId
  kind: CouplingKind
  strength: CouplingStrength
  note: string
}

export type PayloadInstrument = {
  id: PayloadId
  label: string
  vehicle: PayloadVehicle
  role: PayloadRole
  /** Primary delivery target for STG/default launch. */
  deliveryTargetId: DeliveryTargetId
  /** Optional prod target (Trade only today). */
  prodDeliveryTargetId?: DeliveryTargetId
  launchTab: string
  observeTab?: string
  mirrorRepos: readonly string[]
  /**
   * Path globs relative to repo root (Wave 4). Matched against Gitea compare paths.
   * Empty = no path-level lift for this instrument as companion.
   */
  companionPathGlobs: readonly string[]
}

/** Bifrost cluster constellation — Satellite vehicle with Trade + Research. */
export const SATELLITE_PAYLOADS: readonly PayloadInstrument[] = [
  {
    id: 'trade',
    label: 'Trade',
    vehicle: 'satellite',
    role: 'display-host',
    deliveryTargetId: 'trade-stg',
    prodDeliveryTargetId: 'trade-prod',
    launchTab: 'trade-release',
    observeTab: 'satellite-health',
    mirrorRepos: [
      'bifrost-trade-core',
      'bifrost-trade-worker',
      'bifrost-trade-socket',
      'bifrost-trade-api',
      'bifrost-trade-frontend',
      'bifrost-trade-infra',
      'bifrost-ui',
    ],
    companionPathGlobs: [
      'src/**/research/**',
      'src/pages/research/**',
      'src/components/research/**',
      'src/hooks/useResearch*',
      'src/api/research*',
    ],
  },
  {
    id: 'research',
    label: 'Research',
    vehicle: 'satellite',
    role: 'instrument',
    deliveryTargetId: 'research',
    launchTab: 'research-release',
    observeTab: 'research-engine',
    mirrorRepos: ['bifrost-research'],
    companionPathGlobs: [],
  },
] as const

/**
 * Research → Trade: UI surface + API contract. Research upgrades usually need Trade FE.
 * Trade → Research: no edge (Trade-only business pages skip Research unless path lift).
 */
export const CONSTELLATION_EDGES: readonly ConstellationEdge[] = [
  {
    from: 'research',
    to: 'trade',
    kind: 'ui-surface',
    strength: 'suggests',
    note: 'Research UI ships through Trade FE — suggest Trade when Research flies',
  },
  {
    from: 'research',
    to: 'trade',
    kind: 'api-contract',
    strength: 'suggests',
    note: 'Research API/contract changes often need Trade FE consumers',
  },
] as const

export const PAYLOAD_CONSTELLATION_RULES: string[] = [
  'Rocket = control-plane launcher; Satellite = on-orbit vehicle (display-host = Trade).',
  'Payloads / instruments ride the Satellite; add a catalog row + Launch child + edges — never a Briefing Line.',
  'System Domain research remains for D13 write boundaries; domain identity ≠ top-level Ops sidebar group.',
  'Constellation edges are directed: Research → Trade suggests ui-surface / api-contract; Trade → Research has no static edge.',
  'Formation flights use two independent pipelines (never merge bifrost-deliver-stg + bifrost-deliver-research).',
  'D10 BLOCKED — constellation launch never writes ib:operator:cmd or scales daemon.',
]

export function payloadById(id: PayloadId): PayloadInstrument {
  const found = SATELLITE_PAYLOADS.find(p => p.id === id)
  if (found == null) throw new Error(`Unknown payload: ${id}`)
  return found
}

export function payloadsForVehicle(vehicle: PayloadVehicle): PayloadInstrument[] {
  return SATELLITE_PAYLOADS.filter(p => p.vehicle === vehicle)
}

/** Map changed git repos → payloads that own them. */
export function payloadsForRepos(repos: readonly string[]): PayloadId[] {
  const set = new Set<PayloadId>()
  for (const repo of repos) {
    for (const p of SATELLITE_PAYLOADS) {
      if (p.mirrorRepos.includes(repo)) set.add(p.id)
    }
  }
  return [...set]
}

export function edgesFrom(origin: PayloadId): ConstellationEdge[] {
  return CONSTELLATION_EDGES.filter(e => e.from === origin)
}

export function companionsFor(origin: PayloadId): Array<{
  payload: PayloadId
  strength: CouplingStrength
  kinds: CouplingKind[]
  notes: string[]
}> {
  const byTarget = new Map<
    PayloadId,
    { strength: CouplingStrength; kinds: CouplingKind[]; notes: string[] }
  >()
  for (const e of edgesFrom(origin)) {
    const cur = byTarget.get(e.to)
    if (cur == null) {
      byTarget.set(e.to, {
        strength: e.strength,
        kinds: [e.kind],
        notes: [e.note],
      })
      continue
    }
    if (e.strength === 'requires') cur.strength = 'requires'
    if (!cur.kinds.includes(e.kind)) cur.kinds.push(e.kind)
    if (!cur.notes.includes(e.note)) cur.notes.push(e.note)
  }
  return [...byTarget.entries()].map(([payload, v]) => ({ payload, ...v }))
}

/** Flatten ShellNavItem trees (Launch Desk children) for allowed-tab / progress. */
export function flattenLaunchNav(items: readonly ShellNavItem[]): ShellNavItem[] {
  const out: ShellNavItem[] = []
  for (const item of items) {
    if (item.children != null && item.children.length > 0) {
      out.push(...flattenLaunchNav(item.children))
    } else {
      out.push(item)
    }
  }
  return out
}

/** True if path matches any glob (supports ** and * segments). */
export function pathMatchesGlob(path: string, glob: string): boolean {
  const normalized = path.replace(/^\/+/, '')
  const escaped = glob
    .replace(/[.+^${}()|[\]\\]/g, '\\$&')
    .replace(/\*\*/g, '<<<DS>>>')
    .replace(/\*/g, '[^/]*')
    .replace(/<<<DS>>>/g, '.*')
  return new RegExp(`^${escaped}$`).test(normalized)
}

export function pathMatchesAnyGlob(path: string, globs: readonly string[]): boolean {
  return globs.some(g => pathMatchesGlob(path, g))
}

/** Display-host first, then instruments — for strip ordering. */
export function orderedSatellitePayloads(): PayloadInstrument[] {
  const list = payloadsForVehicle('satellite')
  return [
    ...list.filter(p => p.role === 'display-host'),
    ...list.filter(p => p.role !== 'display-host'),
  ]
}
