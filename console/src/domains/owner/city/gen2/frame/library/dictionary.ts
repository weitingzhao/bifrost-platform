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
 *   parts   counted out in standard parts of one fixed size, the same everywhere in the city (the Owner,
 *           2026-11-19: a standard part, aligned, so 16 GB and 24 GB, 16 cores and 32, read at a glance and
 *           compare across machines): a machine per core, a bench section per 4 GB, a bay per 256 GB.
 *           A part partly filled is drawn partly: 17.2 GB is four sections and a little.
 *   number  written out (a tally, a roof number), read, never set
 *   state   the four states as lights (lamps, beacons, dark windows)
 */

export type Encoding =
  | { kind: 'count'; n: number }
  | { kind: 'level' }
  | { kind: 'size'; from: number; to: number; min: number; max: number }
  | { kind: 'parts'; each: number; unit: string; group?: { n: number; name: string } }
  | { kind: 'number' }
  | { kind: 'state' }

/** `shows`: the part as a viewer names it, for the key a facility shows when opened. */
/** `scope`: what the reading is measured over, where that matters to where it is drawn (each core on every machine; the whole CPU once, on the workshop). */
export type Metaphor = { id: string; metric: string; part: string; shows: string; encoding: Encoding; meaning: string; scope?: string }

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
  { id: 'core.machines', metric: 'CPU cores', part: 'machine tool', shows: 'Lathes', encoding: { kind: 'parts', each: 1, unit: 'core' }, meaning: 'the standard part of compute: one lathe per core (bed, headstock and chuck, tailstock), the same in every factory', scope: 'whole CPU' },
  // The standard parts of memory and storage (proposed 2026-11-19, for the Owner to confirm; drawn when
  // those units are made). Measured that day: the cluster nodes 24 GB (one 16, one 32) → 6 sections,
  // the Mac minis 17.2 → 4.3, the NAS 7.8 → 1.9, the GPU box 134 → 33.5; disks 500 GB → 2 bays, the Mac
  // minis 245 → 1, NAS volume 2 984 → 3.8, volume 1 24 TB → 94 bays (12 racks).
  { id: 'mem.sections', metric: 'memory installed', part: 'chest freezer', shows: 'Chest freezers', encoding: { kind: 'parts', each: 4e9, unit: 'GB' }, meaning: 'the standard part of memory: one chest freezer per 4 GB' },
  { id: 'disk.bays', metric: 'disk size', part: 'rack bay', shows: 'Rack bays', encoding: { kind: 'parts', each: 256e9, unit: 'GB', group: { n: 8, name: 'rack' } }, meaning: 'the standard part of storage: one rack bay per 256 GB; eight bays make a rack (2 TB)' },
  { id: 'core.busy', metric: 'a core busy, as a share', part: 'lathe bellows cover', shows: 'Lit folds on each lathe', encoding: { kind: 'level' }, meaning: 'the bellows cover along the lathe’s bed, four folds lit from the headstock on as far as that core is busy (the last in part), amber, orange above 85%; dark when idle', scope: 'each core' },
  { id: 'core.busy.all', metric: 'all cores busy, the average of each core', part: 'console desk folds', shows: 'Lit folds on the console desk', encoding: { kind: 'level' }, meaning: 'how busy the cores are together (the average of the lathes’ folds): the same folds on the desk’s slope, eight lit from the left as far as that, amber, orange above 85%; the percent on the plate', scope: 'whole CPU' },
  { id: 'core.freq', metric: 'the cores’ clock (their average), as a share of their top', part: 'console instrument', shows: 'CLOCK on the console', encoding: { kind: 'level' }, meaning: 'how fast the cores run now, in GHz beneath; the dial ends at their top clock, its last 8% the red band', scope: 'whole CPU' },
  { id: 'core.freq.max', metric: 'the cores’ top clock', part: 'the CLOCK dial’s end', shows: 'End of the CLOCK dial', encoding: { kind: 'number' }, meaning: 'the dial runs from stopped to the fastest the cores can run', scope: 'whole CPU' },
  { id: 'sensor.temp', metric: 'a CPU temperature sensor, °C', part: 'console instrument (whole chip) or thermometer (a core)', shows: 'CHIP TEMP on the console', encoding: { kind: 'level' }, meaning: 'a sensor for the whole chip: CHIP TEMP on the console, the needle across 0–100 °C, °C beneath, the red band from 80 °C; a sensor per core: a small thermometer on its machine', scope: 'as the sensor measures: whole CPU or each core' },
  // The memory unit's room, the cold store (the Owner, 2026-11-19: chest freezers, not a wooden bench;
  // three kinds, not seven — each one readable apart and in the key). A freezer per 4 GB (mem.sections).
  { id: 'mem.held', metric: 'memory in use (installed less available)', part: 'goods in the freezer', shows: 'In use (amber goods)', encoding: { kind: 'level' }, meaning: 'goods packed from the left of the freezers, as much as is in use and cannot be cleared', scope: 'whole unit' },
  { id: 'mem.clearable', metric: 'memory that can be cleared (available less empty: cache)', part: 'goods that can go', shows: 'Can be cleared (pale goods)', encoding: { kind: 'level' }, meaning: 'goods kept while there is room, next after those in use; the first to go when room is needed', scope: 'whole unit' },
  { id: 'mem.empty', metric: 'memory empty', part: 'cold floor of the freezer', shows: 'Empty (bare, frosted)', encoding: { kind: 'level' }, meaning: 'nothing in it: the rest of the freezers', scope: 'whole unit' },
  { id: 'mem.corrupted', metric: 'memory the hardware reports damaged', part: 'red tape on a lid', shows: 'Taped off (red)', encoding: { kind: 'level' }, meaning: 'red tape across the last freezers’ lids, as much as is damaged; none when nothing is', scope: 'whole unit' },
  { id: 'swap.room', metric: 'swap space', part: 'overflow corner', shows: 'Overflow corner (marked floor)', encoding: { kind: 'number' }, meaning: 'floor marked off at the front for what does not fit in the freezers, as long as the swap in the same 4 GB parts', scope: 'whole unit' },
  { id: 'swap.used', metric: 'swap in use, as a share of the swap', part: 'crates in the corner', shows: 'Crates in the corner', encoding: { kind: 'level' }, meaning: 'crates filling the marked floor as far as the swap is used', scope: 'whole unit' },
  { id: 'mem.inuse', metric: 'memory in use, as a share of the installed', part: 'console desk folds', shows: 'Lit folds on the console desk', encoding: { kind: 'level' }, meaning: 'how full the freezers are with goods in use, in the same folds as the CPU console, the percent on the plate', scope: 'whole unit' },
  { id: 'swap.io', metric: 'traffic to and from swap, per second', part: 'console instrument', shows: 'TRAFFIC on the console', encoding: { kind: 'level' }, meaning: 'how fast things move between the freezers and the corner: the needle across 0–10 MB/s, the rate beneath, the red band from 5 MB/s', scope: 'whole unit' },
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
  if (e.kind === 'parts') return v == null || v < 0 ? null : v / rulerOf(id)
  return v
}

/**
 * The rulers (the Owner, 2026-11-19: because there is a standard, it can be adjusted — a different
 * ruler draws a different picture of the same reality; this is what keeps the city maintainable and
 * elastic). A standard part's size is how the city measures, not what the system is: a viewer may
 * set another size for a while (the workshop's Rulers panel), and every drawing that counts that part
 * redraws; the readings do not change. The table holds the defaults; an override lasts until reset.
 */
const rulers = new Map<string, number>()

/** The size of one standard part now: the viewer's ruler if set, else the table's default. */
export function rulerOf(id: string): number {
  const e = metaphor(id).encoding
  if (e.kind !== 'parts') throw new Error(`"${id}" is not a standard part`)
  return rulers.get(id) ?? e.each
}

/** Set a ruler (a size above zero), or put it back to the table's default (null). */
export function setRuler(id: string, each: number | null) {
  rulerOf(id)
  if (each == null) rulers.delete(id)
  else if (!(each > 0)) throw new Error(`a ruler is a size above zero, not ${each}`)
  else rulers.set(id, each)
}

/** The rulers as they stand, for the keys pictures are cached under: another ruler is another picture. */
export const rulersKey = () => [...rulers.entries()].sort().map(([k, v]) => `${k}=${v}`).join(',')

/** Every standard part, from the table: one size each, the same everywhere. */
export const STANDARD_PARTS = () => DICTIONARY.filter(m => m.encoding.kind === 'parts')
