/**
 * Constellation release impact — must / suggest / skip companions for formation flight.
 * Wave 2: static edges. Wave 3: repo-level. Wave 4: path-level lift (fail-soft to W3).
 */

import {
  companionsFor,
  orderedSatellitePayloads,
  pathMatchesAnyGlob,
  payloadById,
  payloadsForRepos,
  type CouplingKind,
  type CouplingStrength,
  type PayloadId,
} from '@/lib/architecture/payloadConstellationCatalog'

export type ImpactVerdict = 'must' | 'suggest' | 'skip' | 'origin'

export type PayloadImpactRow = {
  payload: PayloadId
  label: string
  role: string
  verdict: ImpactVerdict
  kinds: CouplingKind[]
  notes: string[]
  repos: string[]
}

export type ConstellationImpact = {
  origin: PayloadId
  rows: PayloadImpactRow[]
  /** Instruments (and host) to fly besides origin, ordered instrument-first then display-host. */
  flyWith: PayloadId[]
  summary: string
}

function strengthToVerdict(s: CouplingStrength): ImpactVerdict {
  return s === 'requires' ? 'must' : 'suggest'
}

/** Wave 2 — static edges only (no changed-repo signal). */
export function staticConstellationImpact(origin: PayloadId): ConstellationImpact {
  return resolveConstellationImpact({ origin, changedRepos: [] })
}

/**
 * Resolve formation impact.
 * - Origin always `origin`.
 * - Companions from catalog edges → must/suggest when origin is the from-side,
 *   OR when changedRepos include the companion's upstream (edge from changed → companion).
 * - Trade-only changes → Research stays skip unless path lift.
 * - Path lift: Trade origin + FE path hits companionPathGlobs → Research suggest.
 */
export function resolveConstellationImpact(args: {
  origin: PayloadId
  changedRepos: readonly string[]
  /** Paths relative to bifrost-trade-frontend (or other repos keyed below). */
  changedPathsByRepo?: Readonly<Record<string, readonly string[]>>
}): ConstellationImpact {
  const { origin, changedRepos } = args
  const changedPathsByRepo = args.changedPathsByRepo ?? {}
  const changedPayloads = new Set(payloadsForRepos(changedRepos))

  const verdictBy = new Map<PayloadId, PayloadImpactRow>()

  for (const p of orderedSatellitePayloads()) {
    verdictBy.set(p.id, {
      payload: p.id,
      label: p.label,
      role: p.role,
      verdict: p.id === origin ? 'origin' : 'skip',
      kinds: [],
      notes: [],
      repos: [...p.mirrorRepos],
    })
  }

  // Edges FROM origin → companions
  for (const c of companionsFor(origin)) {
    const row = verdictBy.get(c.payload)
    if (row == null || row.verdict === 'origin') continue
    row.verdict = strengthToVerdict(c.strength)
    row.kinds = [...c.kinds]
    row.notes = [...c.notes]
  }

  // If changed repos include an instrument that has edges TO others, and we're
  // launching from that instrument, companions already set. If launching from
  // Trade with only trade repos changed, Research stays skip (no Trade→Research edge).

  // When origin is research and research repo is in changed (or empty = assume static suggest), keep suggest.
  // When origin is trade and only trade repos changed → research skip (already).

  // Wave 4 path lift: Trade origin + trade-frontend paths match Research UI globs → suggest Research
  if (origin === 'trade') {
    const fePaths = changedPathsByRepo['bifrost-trade-frontend'] ?? []
    const trade = payloadById('trade')
    const hit = fePaths.some(p => pathMatchesAnyGlob(p, trade.companionPathGlobs))
    if (hit) {
      const row = verdictBy.get('research')
      if (row != null && row.verdict === 'skip') {
        row.verdict = 'suggest'
        row.kinds = ['ui-surface']
        row.notes = ['Trade FE research surface changed — suggest Research formation']
      }
    }
  }

  // If research repos changed while origin is trade (rare dual-edit) → suggest research via reverse: treat as suggest
  if (origin === 'trade' && changedPayloads.has('research')) {
    const row = verdictBy.get('research')
    if (row != null && row.verdict === 'skip') {
      row.verdict = 'suggest'
      row.kinds = ['api-contract']
      row.notes = ['Research repos also changed — suggest Research formation']
    }
  }

  const rows = orderedSatellitePayloads().map(p => verdictBy.get(p.id)!)

  // Fly order: instruments first, then display-host (plan W5)
  const flyWith = rows
    .filter(r => r.verdict === 'must' || r.verdict === 'suggest')
    .sort((a, b) => {
      const ra = payloadById(a.payload).role === 'instrument' ? 0 : 1
      const rb = payloadById(b.payload).role === 'instrument' ? 0 : 1
      return ra - rb
    })
    .map(r => r.payload)

  const summary = buildSummary(origin, rows)

  return { origin, rows, flyWith, summary }
}

function buildSummary(origin: PayloadId, rows: PayloadImpactRow[]): string {
  const originLabel = payloadById(origin).label
  const must = rows.filter(r => r.verdict === 'must').map(r => r.label)
  const suggest = rows.filter(r => r.verdict === 'suggest').map(r => r.label)
  const skip = rows.filter(r => r.verdict === 'skip').map(r => r.label)
  const parts = [`Origin ${originLabel}`]
  if (must.length > 0) parts.push(`must: ${must.join(', ')}`)
  if (suggest.length > 0) parts.push(`suggest: ${suggest.join(', ')}`)
  if (skip.length > 0) parts.push(`skip: ${skip.join(', ')}`)
  return parts.join(' · ')
}

export function impactVerdictLabel(v: ImpactVerdict): string {
  switch (v) {
    case 'origin':
      return 'ORIGIN'
    case 'must':
      return 'MUST'
    case 'suggest':
      return 'SUGGEST'
    case 'skip':
      return 'SKIP'
  }
}
