/**
 * The world's dictionary (charter 10 §5): each metaphor on the city page, the
 * one real thing it stands for, its fixed tiers and what enters each, where its
 * data is read, and whether a person acts through it. One metaphor, one
 * meaning. The thresholds live here, versioned; no building is tiered by hand.
 */

export const DICTIONARY_VERSION = 2

export type Tier = { name: string; enters: string }
export type Entry = {
  id: string
  metaphor: string
  means: string
  tiers: readonly Tier[]
  source: string
  /** What a person may do through it; '' when nothing. */
  person: string
}

export const DICTIONARY: readonly Entry[] = [
  {
    id: 'size', metaphor: 'A building\'s footprint', means: 'which kind of facility it is; its weight once modules are measured',
    tiers: [{ name: '1×1', enters: 'a small facility' }, { name: '2×2', enters: 'a facility of one module' }, { name: '4×4', enters: 'a landmark' }, { name: '8×8', enters: 'reserved for the largest landmark' }],
    source: 'the building catalogue', person: 'the Owner places and moves buildings: arranging his map, not a request',
  },
  {
    id: 'road', metaphor: 'A road', means: 'nothing in the system: the Owner\'s own way of arranging the map',
    tiers: [{ name: 'road', enters: 'a cell the Owner paved' }],
    source: 'the saved layout', person: 'the Owner paves and erases roads',
  },
  {
    id: 'crawlerway', metaphor: 'The crawlerway', means: 'the release path: horizontal hangar to vertical assembly to pad, and satellite hangar to pad',
    tiers: [{ name: 'still', enters: 'no run moves along a leg' }, { name: 'lit', enters: 'a run of that leg\'s step has not finished: rocket STG for hangar to assembly, rocket PROD for assembly to pad, a tenant\'s PROD for satellites to pad' }],
    source: 'its legs from where the buildings stand (never saved); lit from GET /api/v1/delivery/context runs_in_flight', person: '',
  },
  {
    id: 'hif', metaphor: 'Crates at the horizontal hangar', means: 'the rocket\'s CI: the platform\'s CI and landing runs not finished',
    tiers: [
      { name: 'clear', enters: 'no run in flight' },
      { name: 'busy', enters: '1 to 3 runs in flight' },
      { name: 'jammed', enters: '4 runs or more in flight' },
      { name: 'unknown', enters: 'the platform\'s release context could not be read' },
    ],
    source: 'GET /api/v1/delivery/context?repo=bifrost-platform runs_in_flight: bifrost-ci-platform, bifrost-ci-infra, bifrost-land; whether a run waits or runs is not read',
    person: '',
  },
  {
    id: 'vab', metaphor: 'A rocket lit inside the vertical assembly building', means: 'the rocket\'s STG: a platform delivery to STG not finished',
    tiers: [{ name: 'stacking', enters: 'a bifrost-deliver-platform run is in flight' }, { name: 'quiet', enters: 'none is' }, { name: 'unknown', enters: 'the platform\'s release context could not be read' }],
    source: 'GET /api/v1/delivery/context?repo=bifrost-platform: runs_in_flight and what STG runs', person: '',
  },
  {
    id: 'satellite-hangar', metaphor: 'The satellite hangar: crates at the factory, a lit test bay', means: 'the tenants\' CI (crates) and STG or image builds (the bay)',
    tiers: [{ name: 'testing', enters: 'a tenant STG or build run is in flight' }, { name: 'in CI', enters: 'only tenant CI runs are' }, { name: 'quiet', enters: 'none' }, { name: 'unknown', enters: 'a tenant\'s release context could not be read' }],
    source: 'GET /api/v1/delivery/context for each tenant repository: runs_in_flight', person: '',
  },
  {
    id: 'pad', metaphor: 'The launch pad and its rocket', means: 'the platform, the one vehicle: its PROD release flies a new rocket; a tenant\'s PROD release rides the rocket in service',
    tiers: [
      { name: 'launching', enters: 'a platform or tenant PROD run is in flight (the payload band shows which tenant)' },
      { name: 'window held', enters: 'the release window is held for the platform and nothing launches' },
      { name: 'quiet', enters: 'neither' },
      { name: 'unknown', enters: 'the platform\'s release context could not be read' },
    ],
    source: 'GET /api/v1/delivery/context: runs_in_flight, window, and the rocket in service (what PROD runs)',
    person: 'none for the platform\'s rockets; Trade\'s production launch stays the Owner\'s, through the release page',
  },
  {
    id: 'orbit', metaphor: 'A satellite in orbit', means: 'a tenant on PROD: Trade, research, the plugins; one each',
    tiers: [{ name: 'in orbit', enters: 'its PROD version is read' }, { name: 'launching', enters: 'its PROD or image run is in flight' }, { name: 'unknown', enters: 'its PROD version is not read' }],
    source: 'GET /api/v1/delivery/context?repo=<tenant>: what PROD runs and runs_in_flight; the tenants from GET /api/v1/delivery/pipelines', person: '',
  },
  {
    id: 'waiting', metaphor: 'Workers holding signs at mission control', means: 'requests and questions waiting for the Owner',
    tiers: [
      { name: 'none', enters: 'nothing waits' },
      { name: 'one', enters: '1 waits' },
      { name: 'some', enters: '2 or 3 wait' },
      { name: 'many', enters: '4 or more wait' },
    ],
    source: 'GET /api/v1/approvals?status=pending and GET /api/v1/questions?status=open', person: 'the Owner answers on the Command page',
  },
]

/** A tier only changes after the same new tier was read twice in a row, so a count at a threshold does not flicker. */
export function steady<T>(shown: T | undefined, pending: { tier: T; times: number } | undefined, read: T): { shown: T; pending?: { tier: T; times: number } } {
  if (shown === undefined || read === shown) return { shown: read }
  const times = pending && pending.tier === read ? pending.times + 1 : 1
  return times >= 2 ? { shown: read } : { shown, pending: { tier: read, times } }
}
