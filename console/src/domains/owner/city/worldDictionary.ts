/**
 * The world's dictionary (charter 10 §5): each metaphor on the city page, the
 * one real thing it stands for, its fixed tiers and what enters each, where its
 * data is read, and whether a person acts through it. One metaphor, one
 * meaning. The thresholds live here, versioned; no building is tiered by hand.
 */

export const DICTIONARY_VERSION = 1

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
    id: 'crawlerway', metaphor: 'The crawlerway', means: 'the release path from CI to the launch pads',
    tiers: [{ name: 'laid', enters: 'an assembly building and a pad both stand on the land' }],
    source: 'worked out from where the buildings stand; never saved', person: '',
  },
  {
    id: 'pad', metaphor: 'A launch pad', means: 'a release lane: one delivery pipeline',
    tiers: [
      { name: 'launching', enters: 'a delivery run of this pipeline has not finished' },
      { name: 'window held', enters: 'the release window is held for this lane\'s repository and no run of it is going' },
      { name: 'quiet', enters: 'neither' },
      { name: 'unknown', enters: 'the release context of its repository could not be read' },
    ],
    source: 'GET /api/v1/delivery/context?repo=<repository>: runs_in_flight, window, the last release of its environment',
    person: 'none for the platform\'s rockets; Trade\'s production launch stays the Owner\'s, through the release page',
  },
  {
    id: 'ci-queue', metaphor: 'Crates at the assembly building\'s door', means: 'CI and landing runs not finished',
    tiers: [
      { name: 'clear', enters: 'no run in flight' },
      { name: 'busy', enters: '1 to 3 runs in flight' },
      { name: 'jammed', enters: '4 runs or more in flight' },
      { name: 'unknown', enters: 'a release context could not be read' },
    ],
    source: 'GET /api/v1/delivery/context: runs_in_flight of bifrost-ci-* and bifrost-land, platform and Trade; whether a run waits or runs is not read',
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
]

/** A tier only changes after the same new tier was read twice in a row, so a count at a threshold does not flicker. */
export function steady<T>(shown: T | undefined, pending: { tier: T; times: number } | undefined, read: T): { shown: T; pending?: { tier: T; times: number } } {
  if (shown === undefined || read === shown) return { shown: read }
  const times = pending && pending.tier === read ? pending.times + 1 : 1
  return times >= 2 ? { shown: read } : { shown, pending: { tier: read, times } }
}
