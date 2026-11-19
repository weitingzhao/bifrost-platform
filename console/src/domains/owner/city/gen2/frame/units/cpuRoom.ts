import { Draw, faces, INK, type Sprite } from '../kit'
import { encode, metaphor, rulerOf, rulersKey } from '../library/dictionary'
import { readingText } from '../library/parts'
import { hasConsole, roomConsole, type ConsoleGauge, type ConsoleSpec } from './console'

/**
 * The CPU unit's room, the workshop (a computer is a factory, the Owner 2026-11-19; RULES.md, 由内向外).
 * The hardware only — how many cores, how busy each, how fast, how hot:
 *   the workshop floor   one machine tool per core, in pairs: a load bar across its top filled as far as
 *                        the core is busy — the only reading on a machine
 *   the CPU console      against the back wall, for the whole CPU, each on the standard instrument: how busy all
 *                        the cores are, how fast they run against their top, how hot the chip is
 *   a core's own sensor  where one reports per core, a small thermometer on its machine
 * Everything is a reading; what is not read is not drawn (no machines when the cores are not read, no
 * needle on a dial nothing reads).
 */

export type CpuRoomReadings = {
  /** Per core, in core order: busy share, clock and top clock (Hz); null where not read. */
  cores: { busy: number | null; freq: number | null; max: number | null }[]
  /** The CPU's own sensors: a sensor for one core carries its core (Intel's "Core N"); one without measures the whole chip (AMD's Tctl). */
  temps: { name: string; c: number; core?: number }[]
}

const U = 35.8
// Colours: structure is one colour each and never changes (walls, floor, machines, cabinets, the console);
// only the readings' parts change colour, each change written in the key (the Owner, 2026-11-19: every
// colour stands for something). Machines were grey when idle and green when not — the work light's
// reading said twice, and not in the key: taken out.
const MACHINE = faces('#b7cfc0', '#94b3a1', '#7d9c8b'), HEAD = faces('#8eaa9a', '#6f8c7c', '#5d796a')
const WALL = faces('#efe9dc', '#e2dac8', '#cfc6b2')


/**
 * One design for every CPU, scaled out by its standard parts (the Owner, 2026-11-19: the more cores, the
 * bigger the room; however many, it is the same room, configured, so one design draws every CPU). A
 * machine keeps its size and its cell; the room is the smallest of the allowed shapes that holds
 * them, one way round, so a room twice the size holds about twice the machines. A CPU of more machines than the largest room
 * holds says so on its wall, and the ruler (more cores to a machine) is how it fits.
 */
// Cores come in pairs and powers of two, so machines stand in pairs, 2, 4 or 8 to a row, an aisle
// between pairs, every row full (the Owner, 2026-11-19: three to a row, or one left over, looks wrong).
// A strip along the back wall holds the console; the machines fill the floor in front of it, a walkway
// between rows. A room holds 8, 24 or 48: 8 cores the small room, 16 the middle, 32 the large.
const PITCH = 0.8, ROW = 1.0, GAP = 0.2, STAGING = 1.2, MD = 0.5, MW = 0.55, ROOMS: [number, number][] = [[4, 4], [4, 8], [8, 8]]
const rowWidth = (cols: number) => cols * PITCH + Math.max(0, cols / 2 - 1) * GAP
const rowsOf = (d: number) => Math.floor((d - 0.6 - STAGING) / ROW)
/** The most machines to a row that a room this wide holds and that divide the machines evenly. */
const colsOf = (w: number, machines: number) => {
  let best = 1
  for (const c of [2, 4, 8]) if (rowWidth(c) <= w - 0.6 + 1e-9 && (machines <= 1 || machines % c === 0)) best = c
  return best
}
const fitsIn = ([w, d]: [number, number], n: number) => Math.ceil(n / colsOf(w, n)) <= rowsOf(d)
export function cpuRoomShape(machines: number): [number, number] {
  return ROOMS.find(s => fitsIn(s, machines)) ?? ROOMS[ROOMS.length - 1]
}
/**
 * A lathe's heights (px above the floor): the bed's top, and how far its headstock and tailstock rise
 * above it. Nothing may hide another machine (the Owner, 2026-11-19): a row stands ROW cells behind the
 * next and a machine is MD deep, so the rows are (ROW − MD) × 16 px apart on screen; nothing on a machine
 * may rise more than that above its bed, or it covers the bed of the machine behind.
 */
export const LATHE = { bed: 9, head: 4, tail: 3 }
export const ROW_GAP_PX = (ROW - MD) * 16
/** The same between neighbours in a row: (PITCH − MW) cells apart, so the same bound side to side. */
export const COL_GAP_PX = (PITCH - MW) * 16

/** How machines stand in a room: its shape, how many to a row, how many rows. */
export function cpuRoomLayout(machines: number) {
  const shape = cpuRoomShape(machines), cols = colsOf(shape[0], machines)
  return { shape, cols, rows: Math.ceil(machines / cols) }
}
/**
 * The CPU's summary: what its console shows and what it passes up to its building (stage 3; the console
 * is the room summing itself up, so the two are one computation and cannot drift apart). Made from the
 * room's readings only: how many cores (count); how busy all together (the average of each core); how
 * fast (the average clock, against the average top); how hot (the hottest sensor for the whole chip).
 * Nothing read, null — never a guess.
 */
export type CpuSummary = { cores: number; busy: number | null; clock: number | null; top: number | null; temp: { c: number; name: string } | null }
export function cpuSummaryOf(r: CpuRoomReadings): CpuSummary {
  const avg = (v: (number | null)[]) => { const k = v.filter((x): x is number => x != null); return k.length ? k.reduce((a, b) => a + b, 0) / k.length : null }
  const chip = r.temps.filter(t => t.core == null).sort((a, b) => b.c - a.c)[0]
  return { cores: r.cores.length, busy: avg(r.cores.map(c => c.busy)), clock: avg(r.cores.map(c => c.freq)), top: avg(r.cores.map(c => c.max)), temp: chip ? { c: chip.c, name: chip.name } : null }
}

/** How many machines a CPU of these readings is drawn with, under the ruler as it stands. */
export const machinesOf = (r: CpuRoomReadings) => r.cores.length ? Math.ceil(encode('core.machines', r.cores.length)! - 1e-9) : 0

export function cpuRoom(r: CpuRoomReadings): Sprite {
  const count = machinesOf(r), [fw, fd] = cpuRoomShape(count)
  const g = new Draw(fw, fd, 56)
  // The floor: a workshop slab.
  g.box(0, 0, fw, fd, 3, faces('#d8d6cf', '#bdbab1', '#aaa79e'), 0, 1.2)
  // The console stands against the back wall, nothing behind it to hide; the machines fill the floor in
  // front of it (low, they cover at most its desk, never its instruments). Machines start at `fy`.
  const fy = STAGING + 0.25
  // The back walls.
  g.box(0, 0, fw, 0.18, 46, WALL, 0, 1.2)
  g.box(0, 0.18, 0.18, fd - 0.18, 46, WALL, 0, 1.2)

  // Temperature: a sensor for one core stands on that core's machine; a sensor for the whole chip is on
  // the console. Readings for the whole CPU are on the console at the front, where nothing hides them
  // (the Owner, 2026-11-19: on the back wall the machines hid them; a whole CPU's load belongs on a console).
  const perCore = new Map(r.temps.filter(t => t.core != null).map(t => [t.core!, t.c]))
  if (r.temps.length) metaphor('sensor.temp')
  const liquidOf = (c: number) => c >= 80 ? '#e0533d' : '#e88a3a'
  const cores = r.cores.length
  const avgOf = (v: (number | null)[]) => { const k = v.filter((x): x is number => x != null); return k.length ? k.reduce((a, b) => a + b, 0) / k.length : null }

  // The console: a control desk against the back wall for what is measured over the whole CPU, each on the
  // standard instrument (library/parts.ts), one slot each: how fast the cores run against their top, how
  // hot the chip is; and on the desk's slope, how busy all the cores are, in the lathes' own folds. Only instruments with a reading; no console when nothing is read. (The Owner, 2026-11-19:
  // the whole CPU's percent belongs here; each machine keeps its load bar and nothing that looks like a
  // second one; instruments stood outside the desk and must not.)
  const sum = cpuSummaryOf(r), all = sum.busy, clock = sum.clock, top = sum.top, t = sum.temp
  const gauges: ConsoleGauge[] = []
  if (top) gauges.push({ row: 'core.freq', also: ['core.freq.max'], name: 'CLOCK', share: clock != null ? encode('core.freq', clock / top) : null, redFrom: 0.92, reading: readingText(clock, 'Hz') })
  if (t) gauges.push({ row: 'sensor.temp', name: 'CHIP TEMP', share: encode('sensor.temp', t.c / 100), redFrom: 0.8, reading: readingText(t.c, '°C') })
  const consoleSpec: ConsoleSpec = { name: 'CPU CONSOLE', gauges, desk: cores > 0 ? { row: 'core.busy.all', share: all, plate: `CPU BUSY ${readingText(all, 'share')}` } : undefined }
  const drawConsole = () => roomConsole(g, fw, consoleSpec)

  // The machines, then the console in front of them, drawn back to front.
  type Thing = { at: number; draw: () => void }
  const things: Thing[] = []
  if (cores > 0) {
    metaphor('core.busy')
    // As many machines as the ruler counts (one per core by default): each stands for its group of
    // cores, the last one narrower when not full; every machine the same size in every room.
    const per = Math.max(1, Math.round(rulerOf('core.machines')))
    const cols = colsOf(fw, count), fits = cols * rowsOf(fd)
    const groups = Array.from({ length: Math.min(count, fits) }, (_, m) => r.cores.slice(m * per, (m + 1) * per))
    if (count > fits) g.text('left', 0.4, 0.18, 47, `${fits} OF ${count} MACHINES FIT — SET MORE CORES A MACHINE`, 5.5, '#b8433f', 700)
    const md = MD
    groups.forEach((grp, i) => {
      const mw = Math.max(0.3, MW * grp.length / per)
      const c = { busy: avgOf(grp.map(x => x.busy)) }
      // Spread over the floor (the Owner, 2026-11-19: crowded in a corner with the rest of the room empty is
      // neither tidy nor needed): each pair centred in an equal share of the floor's width, each row in an
      // equal share of its depth before the console. A machine keeps its size; only the spacing grows.
      const col = i % cols, row = Math.floor(i / cols), pairs = Math.max(1, cols / 2)
      const slotW = (fw - 0.7) / pairs, rowsN = Math.ceil(groups.length / cols), slotD = Math.min(1.25, (fd - 0.3 - fy) / rowsN)
      // Rows no further apart than 1.25 cells (the Owner, 2026-11-19: across the room the rows stood too far
      // apart): the block of rows centred in the floor's depth.
      const y0 = fy + ((fd - 0.3 - fy) - rowsN * slotD) / 2
      const inPair = cols === 1 ? 0 : (col % 2) * PITCH, pairW = cols === 1 ? MW : PITCH + MW
      const x = 0.35 + Math.floor(col / 2) * slotW + (slotW - pairW) / 2 + inPair, y = y0 + row * slotD + (slotD - md) / 2
      things.push({ at: x + y, draw: () => {
        const busy = encode('core.busy', c.busy)
        // A lathe, low (the Owner, 2026-11-19: it must look like a machine tool, hide no other machine, and
        // show its load at a glance): the bed; the headstock at the left with its chuck facing in; the
        // tailstock at the right; between them, along the bed, the ways' bellows cover in four folds.
        // The load lights the folds from the headstock on, as far as the core is busy (the last fold in
        // part): amber, orange above 85%; dark when idle. It is the machine's own part, not a gauge on it.
        const BED = LATHE.bed, hw = Math.min(0.13, mw * 0.25), tw = 0.08
        g.shadow(x, y, mw, md, 0.25)
        g.box(x, y, mw, md, BED, MACHINE, 0, 1.1)
        g.box(x, y + 0.03, hw, md - 0.06, LATHE.head, HEAD, BED, 1)
        const fx = x + hw + 0.02, fl = mw - hw - tw - 0.04, FW = fl * U, FD = (md - 0.18) * U, N = 4
        const lit = busy == null ? 0 : busy * N, glow = busy != null && busy > 0.85 ? '#ff7a45' : '#ffc94d'
        let folds = ''
        for (let k = 0; k < N; k++) {
          const u0 = (k / N) * FW, w = FW / N
          folds += `<rect x="${u0.toFixed(1)}" y="0" width="${w.toFixed(1)}" height="${FD.toFixed(1)}" fill="${k % 2 ? '#4a564f' : '#55625a'}"/>`
          const on = Math.max(0, Math.min(1, lit - k))
          if (on > 0.02) folds += `<rect x="${u0.toFixed(1)}" y="0" width="${(w * on).toFixed(1)}" height="${FD.toFixed(1)}" fill="${glow}" opacity="${k % 2 ? 0.88 : 1}"/>`
        }
        g.floor(fx, y + 0.09, BED, folds + `<rect x="0" y="0" width="${FW.toFixed(1)}" height="${FD.toFixed(1)}" fill="none" stroke="${INK}" stroke-width="0.6"/>`)
        g.face('right', x + hw, y + md / 2 + 0.06, BED + 2.2, `<circle cx="0" cy="0" r="2.6" fill="#c5cad3" stroke="${INK}" stroke-width="0.7"/><circle cx="0" cy="0" r="0.9" fill="#6b7280"/>`)
        g.box(x + mw - tw, y + md * 0.3, tw, md * 0.4, LATHE.tail, HEAD, BED, 0.9)
        // A core's own temperature, where its sensor reports one: a small thermometer on its right side.
        const hot = grp.map((_, k) => perCore.get(i * per + k)).filter((v): v is number => v != null)
        if (hot.length) {
          const tc = Math.max(...hot)
          g.onRight(x + mw, y, 0.04, 0.12, 1, LATHE.bed - 1, '#fbfaf6', 0.5)
          g.onRight(x + mw, y, 0.05, 0.11, 1, 1 + (LATHE.bed - 3) * encode('sensor.temp', tc / 100)!, liquidOf(tc), 0)
        }
      } })
    })
  } else {
    g.text('left', 0.6, 0.18, 30, 'CORES NOT READ', 7, '#8a857a')
  }
  if (hasConsole(consoleSpec)) things.push({ at: -1, draw: drawConsole })
  things.sort((a, b) => a.at - b.at).forEach(t => t.draw())
  return g.done()
}

/** The key the room's picture is kept under: every reading to the step it is drawn at. */
export const cpuRoomKey = (r: CpuRoomReadings) =>
  `g2:cpuroom:${rulersKey()}:${r.cores.map(c => `${c.busy == null ? 'n' : Math.round(c.busy * 26)}.${c.freq == null || !c.max ? 'n' : Math.round(c.freq / c.max * 20)}`).join(',')}:${r.temps.map(t => Math.round(t.c)).join(',')}`
