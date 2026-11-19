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

/** `shows`: the part as a viewer names it, for the key a facility shows when opened. */
export type Metaphor = { id: string; metric: string; part: string; shows: string; encoding: Encoding; meaning: string }

const GB = 1e9, TB = 1e12

export const DICTIONARY: readonly Metaphor[] = [
  { id: 'cpu.busy', metric: 'CPU busy, as a share of its cores', part: 'windowStrip', shows: 'Hall windows', encoding: { kind: 'count', n: 8 }, meaning: 'as many windows lit as it is busy' },
  { id: 'cpu.smoke', metric: 'CPU busy (the same metric, seen from far off)', part: 'smoke', shows: 'Smoke from the stacks', encoding: { kind: 'count', n: 3 }, meaning: 'as many puffs from each stack as it is busy; none when it does not run' },
  { id: 'cores', metric: 'CPU cores', part: 'hall (height, px)', shows: 'Height of the hall', encoding: { kind: 'size', from: 8, to: 64, min: 34, max: 72 }, meaning: 'the more cores, the taller the hall' },
  { id: 'mem.used', metric: 'memory used, as a share', part: 'tank (cooling water)', shows: 'Water in the tank', encoding: { kind: 'level' }, meaning: 'the water stands as high as memory is used' },
  { id: 'mem.total', metric: 'memory installed', part: 'tank (cooling water) girth, px', shows: 'Size of the water tank', encoding: { kind: 'size', from: 8 * GB, to: 256 * GB, min: 10, max: 22 }, meaning: 'the more memory, the bigger the water tank' },
  { id: 'disk.used', metric: 'disk used, as a share', part: 'tank (fuel)', shows: 'Fuel in the tank', encoding: { kind: 'level' }, meaning: 'the fuel stands as high as the disk is used' },
  { id: 'disk.total', metric: 'disk size', part: 'tank (fuel) girth, px', shows: 'Size of the fuel tank', encoding: { kind: 'size', from: 100 * GB, to: 32 * TB, min: 10, max: 22 }, meaning: 'the bigger the disk, the bigger the fuel tank' },
  { id: 'volume.used', metric: 'a NAS volume used, as a share', part: 'doorRow (crates)', shows: 'Crates behind the doors', encoding: { kind: 'level' }, meaning: 'crates stacked behind its doors as high as it is used' },
  { id: 'volume.disks', metric: 'the disks of a volume’s array and their states', part: 'doorRow (doors, lamps)', shows: 'Doors and their lamps', encoding: { kind: 'state' }, meaning: 'a door per disk, its lamp in the disk’s state' },
  { id: 'pods', metric: 'pods running on a node', part: 'tallyBoard', shows: 'Board on the side wall', encoding: { kind: 'number' }, meaning: 'how many run on it now, written on its side board' },
  { id: 'identity', metric: 'which machine (from its node name)', part: 'roofNumber', shows: 'Number on the roof', encoding: { kind: 'number' }, meaning: 'its number painted on the roof, read from far off' },
  { id: 'purpose', metric: 'what it is for (workload pool, cluster role, topology role)', part: 'signBand', shows: 'Sign over the front', encoding: { kind: 'number' }, meaning: 'written on the sign over its front' },
  // The CPU unit's room, the workshop (a computer is a factory, the Owner 2026-11-19).
  { id: 'core.machines', metric: 'CPU cores', part: 'machine tool', shows: 'Machine tools', encoding: { kind: 'number' }, meaning: 'one machine tool per core; as many machines as it has cores' },
  { id: 'core.busy', metric: 'a core busy, as a share', part: 'machine work light', shows: 'Work light on each machine', encoding: { kind: 'level' }, meaning: 'the light column stands as high as that core is busy; dark when idle' },
  { id: 'core.freq', metric: 'a core’s clock, as a share of its top', part: 'speed dial', shows: 'Speed dial on each machine', encoding: { kind: 'level' }, meaning: 'the needle at how fast that core runs now, from stopped to its red line' },
  { id: 'core.freq.max', metric: 'a core’s top clock', part: 'speed dial red line', shows: 'Red line on the speed dial', encoding: { kind: 'number' }, meaning: 'the end of the dial: the fastest that core can run' },
  { id: 'sensor.temp', metric: 'a temperature sensor, °C', part: 'wall thermometer', shows: 'Thermometers on the wall', encoding: { kind: 'level' }, meaning: 'one per sensor, the column as high as it reads (0–100 °C); red from 80 °C' },
  { id: 'load', metric: 'load (tasks running or waiting), against its cores', part: 'order dial', shows: 'Order dials, 1 · 5 · 15 min', encoding: { kind: 'level' }, meaning: 'the needle at the work asked of it; past the middle (as many as it has cores) into red, more is asked than it can do' },
  { id: 'pressure', metric: 'share of time some work waited for a core', part: 'waiting line', shows: 'People waiting at the office', encoding: { kind: 'count', n: 10 }, meaning: 'as many in line as the share of time something waited; nobody when nothing waits' },
  { id: 'state', metric: 'how it stands: online, standby, down, unread', part: 'lights, beacon', shows: 'Lights and beacon', encoding: { kind: 'state' }, meaning: 'lit and running; cold and dark; a red beacon; grey and empty' },
]

const byId = new Map(DICTIONARY.map(m => [m.id, m]))
let seen: Set<string> | null = null

/** What a drawing looked up in the table: the rows a facility really draws, for its key (and its test). */
export function rowsDrawn<T>(draw: () => T): { out: T; ids: string[] } {
  const before = seen
  seen = new Set()
  try { const out = draw(); return { out, ids: [...seen] } } finally { seen = before }
}

/** A metaphor by its metric; drawing a metric the dictionary does not have is an error. */
export function metaphor(id: string): Metaphor {
  const m = byId.get(id)
  if (!m) throw new Error(`metaphor dictionary: no row for "${id}" — add it before drawing it`)
  seen?.add(id)
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
