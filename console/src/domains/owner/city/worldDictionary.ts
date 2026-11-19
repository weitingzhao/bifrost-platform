/**
 * The world's dictionary (charter 10 §5): each metaphor on the city page, the
 * one real thing it stands for, its fixed tiers and what enters each, where its
 * data is read, and whether a person acts through it. One metaphor, one
 * meaning. The thresholds live here, versioned; no building is tiered by hand.
 */

export const DICTIONARY_VERSION = 6

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
    id: 'orbit', metaphor: 'A satellite in orbit', means: 'a tenant on PROD (Trade, Research), one each; the plugins are vendors on the ground, their docks',
    tiers: [{ name: 'in orbit', enters: 'its PROD version is read' }, { name: 'launching', enters: 'its PROD or image run is in flight' }, { name: 'unknown', enters: 'its PROD version is not read' }],
    source: 'GET /api/v1/delivery/context?repo=<tenant>: what PROD runs and runs_in_flight; the tenants from GET /api/v1/delivery/pipelines', person: '',
  },
  {
    id: 'genset', metaphor: 'A generator set (a big one is the power station)', means: 'a node of the cluster: the supply side',
    tiers: [
      { name: 'normal', enters: 'power (CPU) and water (memory) used are both under 60 % of capacity' },
      { name: 'tight', enters: 'the worse of the two is 60 % to 85 %' },
      { name: 'overloaded', enters: 'the worse of the two is above 85 %' },
      { name: 'blackout', enters: 'the node is not Ready' },
    ],
    source: 'GET /api/v1/telemetry/promql: kube_node_status_capacity, kube_node_status_condition, container_cpu_usage_seconds_total, container_memory_working_set_bytes, live pods\' requests; unknown where not read',
    person: '',
  },
  {
    id: 'utilities', metaphor: 'Three meters by a roof, and cables to the sets', means: 'a building\'s demand: power is CPU, water is memory, gas is disk; fill is used against the limit, the tick what is reserved',
    tiers: [{ name: 'drawing', enters: 'its pods run and stay under their limits' }, { name: 'near its limit', enters: 'power or water used is above 85 % of its limit' }, { name: 'idle', enters: 'no pod of it runs' }, { name: 'unknown', enters: 'the use is not read' }],
    source: 'GET /api/v1/telemetry/promql by namespace and pod; the platform\'s domains share one meter (one process); no pod declares a disk need, so gas is unknown',
    person: '',
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
  {
    id: 'bay', metaphor: 'A satellite campus: code factories (a repository each, smoking while its CI runs), a conveyor to the payload bay, and a block per module its PROD runs', means: 'a tenant (Trade, Research, Finance…), the payload the system exists to build; its size the Owner\'s, what stands inside read',
    tiers: [
      { name: 'building', enters: 'a run of one of its lanes is in flight (the bay lit)' },
      { name: 'ready', enters: 'nothing in flight' },
      { name: 'unknown', enters: 'its repositories\' release contexts were not read' },
    ],
    source: 'GET /api/v1/delivery/context?repo= (runs in flight, PROD release)', person: 'none',
  },
  {
    id: 'supplier', metaphor: "A supplier's dock: a warehouse with a truck in the vendor's colour", means: 'a plugin, an outside vendor: its repository and the runs that deliver it',
    tiers: [
      { name: 'delivering', enters: 'a run of its repository is in flight' },
      { name: 'quiet', enters: 'nothing in flight: its tag shows the version PROD runs' },
      { name: 'unknown', enters: 'its release context was not read' },
    ],
    source: 'GET /api/v1/delivery/context?repo=<plugin repository>', person: 'none',
  },
  {
    id: 'machine', metaphor: 'A power plant with its stacks: its colour and side mark its system, its name on the front wall, its number painted on the roof, three front windows its gauges (yellow power, blue cooling water, grey nitrogen), a lamp on its roof', means: 'a computer that supplies work: Linux orange, a Mac silver, Windows blue; a window fills with what is used of what there is; the lamp is green online, red down, grey not known; off the grid when outside the cluster',
    tiers: [
      { name: 'small', enters: '8 cores or fewer; memory under 20 GiB; disk under 750 GB' },
      { name: 'medium', enters: '16 cores or fewer; memory under 48 GiB; disk under 1.5 TB' },
      { name: 'large', enters: '32 cores or fewer; memory under 96 GiB; disk under 4 TB' },
      { name: 'very large', enters: 'more (hatched when not read)' },
    ],
    source: 'kube_node_info and kube_node_status_capacity (the cluster); GET /api/v1/topology (the machines outside it)', person: 'none',
  },
  {
    id: 'lamp', metaphor: 'A name plate at a building\'s foot, with a coloured dot', means: 'the building\'s name and its state at a glance (green calm, amber busy, red alert, grey unknown); the full tag shows when pointed at, picked, or on alert',
    tiers: [{ name: 'calm', enters: 'green' }, { name: 'busy', enters: 'amber' }, { name: 'alert', enters: 'red, with its tag' }, { name: 'unknown', enters: 'grey' }],
    source: 'the building\'s own reading', person: 'none',
  },
  {
    id: 'gate', metaphor: 'The traffic light where the crawlerway meets the pad', means: 'the one release window: whether a release may start',
    tiers: [
      { name: 'red', enters: 'someone holds the release window' },
      { name: 'amber', enters: 'a launch is going' },
      { name: 'green', enters: 'the window is free' },
      { name: 'grey', enters: 'the window was not read' },
    ],
    source: 'GET /api/v1/delivery/context (window, runs_in_flight)', person: 'none',
  },
  {
    id: 'worker', metaphor: 'A coloured worker with a speech bubble, walking from the dorm to a building', means: 'a seat (an agent line) and its latest status: it stands at the building of the domain its charter names',
    tiers: [
      { name: 'working', enters: 'a status in the last 120 min, waiting on nothing' },
      { name: 'waiting', enters: 'a status in the last 120 min that waits on something (yellow bubble)' },
      { name: 'home', enters: 'no status in 120 min: grey, asleep in the dorm' },
    ],
    source: 'GET /api/v1/agentrt/seats (each seat\'s report_status)', person: 'none: seats report themselves',
  },
]

/** A tier only changes after the same new tier was read twice in a row, so a count at a threshold does not flicker. */
export function steady<T>(shown: T | undefined, pending: { tier: T; times: number } | undefined, read: T): { shown: T; pending?: { tier: T; times: number } } {
  if (shown === undefined || read === shown) return { shown: read }
  const times = pending && pending.tier === read ? pending.times + 1 : 1
  return times >= 2 ? { shown: read } : { shown, pending: { tier: read, times } }
}
