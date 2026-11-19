/**
 * The metaphor dictionary (the Owner, 2026-11-19: the point is that a domain's metrics show
 * naturally; which metaphor stands for which metric must be standard, written down, and changeable
 * as data, not buried in the drawing). One row per metric: the part that shows it, how, and what it
 * means. Facilities take their encodings from here (`encode`); a metric not in the table cannot be
 * drawn (it throws), so nothing shows on the city without a row here.
 *
 * Encodings, and only these:
 *   count   as many lit of n as its share (windows, smoke puffs)
 *   level   filled to its share (a tank, crates)
 *   size    as big as its amount, on a log scale between two bounds (a hall's height, a tank's girth):
 *           of two of a kind, the bigger one looks bigger
 *   number  written out (a tally, a roof number), read, never set
 *   state   the four states as lights (lamps, beacons, dark windows)
 */

export type Encoding =
  | { kind: 'count'; n: number }
  | { kind: 'level' }
  | { kind: 'size'; from: number; to: number; min: number; max: number }
  | { kind: 'number' }
  | { kind: 'state' }

export type Metaphor = { id: string; metric: string; part: string; encoding: Encoding; meaning: string }

const GB = 1e9, TB = 1e12

export const DICTIONARY: readonly Metaphor[] = [
  { id: 'cpu.busy', metric: 'CPU busy, as a share of its cores', part: 'windowStrip', encoding: { kind: 'count', n: 8 }, meaning: 'as many windows lit as it is busy' },
  { id: 'cpu.smoke', metric: 'CPU busy (the same metric, seen from far off)', part: 'smoke', encoding: { kind: 'count', n: 3 }, meaning: 'as many puffs from each stack as it is busy; none when it does not run' },
  { id: 'cores', metric: 'CPU cores', part: 'hall (height, px)', encoding: { kind: 'size', from: 8, to: 64, min: 34, max: 72 }, meaning: 'the more cores, the taller the hall' },
  { id: 'mem.used', metric: 'memory used, as a share', part: 'tank (cooling water)', encoding: { kind: 'level' }, meaning: 'the water stands as high as memory is used' },
  { id: 'mem.total', metric: 'memory installed', part: 'tank (cooling water) girth, px', encoding: { kind: 'size', from: 8 * GB, to: 256 * GB, min: 10, max: 22 }, meaning: 'the more memory, the bigger the water tank' },
  { id: 'disk.used', metric: 'disk used, as a share', part: 'tank (fuel)', encoding: { kind: 'level' }, meaning: 'the fuel stands as high as the disk is used' },
  { id: 'disk.total', metric: 'disk size', part: 'tank (fuel) girth, px', encoding: { kind: 'size', from: 100 * GB, to: 32 * TB, min: 10, max: 22 }, meaning: 'the bigger the disk, the bigger the fuel tank' },
  { id: 'volume.used', metric: 'a NAS volume used, as a share', part: 'doorRow (crates)', encoding: { kind: 'level' }, meaning: 'crates stacked behind its doors as high as it is used' },
  { id: 'volume.disks', metric: 'the disks of a volume’s array and their states', part: 'doorRow (doors, lamps)', encoding: { kind: 'state' }, meaning: 'a door per disk, its lamp in the disk’s state' },
  { id: 'pods', metric: 'pods running on a node', part: 'tallyBoard', encoding: { kind: 'number' }, meaning: 'how many run on it now, written on its side board' },
  { id: 'identity', metric: 'which machine (from its node name)', part: 'roofNumber', encoding: { kind: 'number' }, meaning: 'its number painted on the roof, read from far off' },
  { id: 'purpose', metric: 'what it is for (workload pool, cluster role, topology role)', part: 'signBand', encoding: { kind: 'number' }, meaning: 'written on the sign over its front' },
  { id: 'state', metric: 'how it stands: online, standby, down, unread', part: 'lights, beacon', encoding: { kind: 'state' }, meaning: 'lit and running; cold and dark; a red beacon; grey and empty' },
]

const byId = new Map(DICTIONARY.map(m => [m.id, m]))

/** A metaphor by its metric; drawing a metric the dictionary does not have is an error. */
export function metaphor(id: string): Metaphor {
  const m = byId.get(id)
  if (!m) throw new Error(`metaphor dictionary: no row for "${id}" — add it before drawing it`)
  return m
}

/**
 * A metric's value as its metaphor shows it: for count, how many are lit (a share in, a whole number
 * out; 0 when nothing is read); for level, the share (null when nothing is read); for size, the
 * dimension (its lower bound when nothing is read).
 */
export function encode(id: string, v: number | null): number | null {
  const e = metaphor(id).encoding
  if (e.kind === 'count') return v == null ? 0 : Math.max(v > 0.005 ? 1 : 0, Math.round(Math.min(1, v) * e.n))
  if (e.kind === 'level') return v == null ? null : Math.max(0, Math.min(1, v))
  if (e.kind === 'size') {
    if (v == null || v <= 0) return e.min
    const t = (Math.log(v) - Math.log(e.from)) / (Math.log(e.to) - Math.log(e.from))
    return e.min + (e.max - e.min) * Math.max(0, Math.min(1, t))
  }
  return v
}
