import type { CityLane, SystemItem } from './catalog'
import type { CityReading, Glance } from './CityBuilder'
import { bayReading, gateReading, padReading, shipPadReading, supplierReading, rocketCiReading, rocketStgReading, runsAt, satellitesReading, waitingReading, type Contexts, type Reading } from './cityReadings'
import { conflicts, CONFLICT_WORD, demand, supplies, type Demand, type Reads } from './cityUtilities'
import { DICTIONARY } from './worldDictionary'
import type { Machine } from './cityMachines'

/**
 * Every building's reading from the reads the page made: the release lines,
 * what waits for the Owner, and the utilities (supply on each node, demand on
 * each building, the cables between). One place, so the page and the bench
 * show the same thing.
 */
const source = (id: string) => DICTIONARY.find(e => e.id === id)?.source ?? ''
const PLATFORM_SHARED = 'platform-api runs the platform\'s domains in one process; their use is not split'

export type CityInputs = {
  contexts: Contexts; lanes: readonly CityLane[]; items: readonly SystemItem[]
  util: Reads | null; approvals: number | null; questions: number | null; now: number
  /** The machines outside the cluster, from the topology: off the grid. */
  offGrid?: readonly Machine[]
}

export function cityReadings(i: CityInputs): Map<string, CityReading> {
  const out = new Map<string, CityReading>()
  const put = (binds: string, r: Reading, id: string, d?: Demand) => out.set(binds, {
    ...r, source: d ? `${source(id)}; utilities: GET /api/v1/telemetry/promql (kube-state-metrics, cAdvisor)` : source(id),
    lines: d ? [...r.lines, ...d.lines] : r.lines,
    meters: d ? { power: d.power, water: d.water, gas: d.gas } : undefined,
    links: d?.nodes.map(n => `node:${n}`),
  })
  const u = i.util
  const draw = (x: Parameters<typeof demand>[1]) => u ? demand(u, x) : undefined
  put('stage:rocket-ci', rocketCiReading(i.contexts, i.lanes), 'hif', draw({ runs: runsAt(i.contexts, i.lanes, 'rocket', ['ci']) }))
  put('stage:rocket-stg', rocketStgReading(i.contexts, i.lanes, i.now), 'vab', draw({ namespaces: ['bifrost-platform-stg'], runs: runsAt(i.contexts, i.lanes, 'rocket', ['stg']) }))
  put('stage:ship-pad', shipPadReading(i.contexts, i.lanes, i.now), 'pad')
  put('stage:satellites', satellitesReading(i.contexts, i.lanes), 'satellite-hangar', draw({ runs: runsAt(i.contexts, i.lanes, 'satellite', ['ci', 'stg', 'build']) }))
  const platform = draw({ namespaces: ['bifrost-platform-prod'], shared: PLATFORM_SHARED })
  for (const it of i.items) {
    if (it.binds.startsWith('lane:')) put(it.binds, padReading(i.contexts, i.lanes, i.now), 'pad', platform)
    else if (it.type === 'tenant') put(it.binds, bayReading(i.contexts, i.lanes, it.binds.slice('tenant:'.length), i.now), 'bay')
    else if (it.type === 'plugin') put(it.binds, supplierReading(i.contexts, it.binds.slice('plugin:'.length), i.now), 'supplier')
    else if (it.type === 'domain' && platform && it.binds !== 'domain:owner') put(it.binds, { word: platform.word, tone: platform.tone, lines: [] }, 'utilities', platform)
  }
  put('domain:owner', waitingReading(i.approvals, i.questions), 'waiting', platform)
  put('gate', gateReading(i.contexts, i.lanes), 'gate')
  if (u) {
    const drawn = [...out].filter(([, r]) => r.links?.length)
    const sets = supplies(u), found = conflicts(sets)
    for (const s of sets) {
      const mine = found.filter(c => c.nodes.includes(s.node)).map(c => `${CONFLICT_WORD[c.kind]}: ${c.line}`)
      out.set(`node:${s.node}`, {
        word: s.word, tone: s.tone, lines: [...s.lines, ...mine], source: source('genset'),
        meters: { power: s.power, water: s.water, gas: s.gas },
        links: drawn.filter(([, r]) => r.links!.includes(`node:${s.node}`)).map(([b]) => b),
      })
    }
  }
  for (const m of i.offGrid ?? []) {
    out.set(`node:${m.id}`, {
      word: m.status === 'ok' ? 'off the grid' : 'off the grid · unknown', tone: m.status === 'ok' ? 'calm' : 'unknown',
      lines: [`${m.os === 'unknown' ? 'system not told' : m.os} machine outside the cluster`, m.roles.length ? `roles: ${m.roles.join(', ')}` : 'roles not told', 'its capacity and use are not read (no monitoring reaches it): its bars are hatched'],
      source: 'GET /api/v1/topology (status and roles)',
      meters: { power: { used: null, reserved: null, of: null }, water: { used: null, reserved: null, of: null }, gas: { used: null, reserved: null, of: null } },
    })
  }
  return out
}

/** The utilities in one chip: the conflicts across the sets, or "balanced"; unknown when not read. */
export function utilityGlance(util: Reads | null): Glance {
  if (!util) return { text: 'Utilities: unknown', tone: 'unknown', href: '#cluster' }
  const sets = supplies(util)
  if (!sets.length) return { text: 'Utilities: unknown', tone: 'unknown', href: '#cluster' }
  const kinds = [...new Set(conflicts(sets).map(c => c.kind))]
  const down = sets.filter(s => s.ready === false).length
  const words = [...kinds.map(k => CONFLICT_WORD[k]), ...(down ? [`${down} set${down > 1 ? 's' : ''} down`] : [])]
  return words.length ? { text: `Utilities: ${words.join(', ')}`, tone: kinds.includes('short') ? 'alert' : 'busy', href: '#cluster' } : { text: 'Utilities: balanced', tone: 'calm', href: '#cluster' }
}
