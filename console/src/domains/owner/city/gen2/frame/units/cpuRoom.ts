import { Draw, faces, INK, type Sprite } from '../kit'
import { encode, metaphor, rulerOf, rulersKey } from '../library/dictionary'
import { INSTRUMENT, INSTRUMENT_H, instrument, readingText } from '../library/parts'

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
  const chip = [...r.temps].filter(t => t.core == null).sort((a, b) => b.c - a.c)
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
  const all = avgOf(r.cores.map(c => c.busy))
  const clock = avgOf(r.cores.map(c => c.freq)), top = avgOf(r.cores.map(c => c.max))
  const t = chip[0]
  type Gauge = { row: string; name: string; share: number | null; redFrom: number; reading: string }
  const gauges: Gauge[] = []
  if (top) gauges.push({ row: 'core.freq', name: 'CLOCK', share: clock != null ? encode('core.freq', clock / top) : null, redFrom: 0.92, reading: readingText(clock, 'Hz') })
  if (t) gauges.push({ row: 'sensor.temp', name: 'CHIP TEMP', share: encode('sensor.temp', t.c / 100), redFrom: 0.8, reading: readingText(t.c, '°C') })
  const cw = Math.max(1.7, gauges.length * INSTRUMENT + 0.2), cx = fw - 0.3 - cw, cy = 0.22, cd = 0.75, bank = 0.22
  const STEEL = faces('#d9dbe0', '#bfc3cb', '#a9aeb8'), DARK = faces('#4a5263', '#353c4a', '#2b313d')
  const zb = 15, zf = 8, BANK_H = zb + INSTRUMENT_H + 4
  const drawConsole = () => {
    g.shadow(cx, cy, cw, cd, 0.35)
    // The instrument bank: upright behind the desk, a hood on top, a cheek at the end.
    // No instrument read (the Mac minis report no clock and no sensor): no bank standing empty, the desk alone.
    if (gauges.length) {
      g.box(cx, cy, cw, bank, BANK_H, DARK, 0, 1.2)
      g.box(cx - 0.06, cy - 0.04, cw + 0.12, bank + 0.16, 3, STEEL, BANK_H, 1)
      g.box(cx + cw - 0.04, cy, 0.08, bank + 0.06, BANK_H, STEEL, 0, 0.9)
    } else g.box(cx, cy, cw, bank, zb, STEEL, 0, 1.1)
    const P = cy + bank
    gauges.forEach((m, k) => { metaphor(m.row); if (m.row === 'core.freq') metaphor('core.freq.max'); instrument(g, cx + 0.1, P, k, zb + 2, m) })
    // The desk: sloped, high at the bank, low at the front edge; its name plate on the front edge.
    const y0 = P, y1 = cy + cd
    g.poly(STEEL.left, [[cx, y1, 0], [cx + cw, y1, 0], [cx + cw, y1, zf], [cx, y1, zf]], 1.2)
    g.poly(STEEL.right, [[cx + cw, y0, 0], [cx + cw, y1, 0], [cx + cw, y1, zf], [cx + cw, y0, zb]], 1.2)
    g.poly(STEEL.top, [[cx, y0, zb], [cx + cw, y0, zb], [cx + cw, y1, zf], [cx, y1, zf]], 1.2)
    // On the desk's slope, the whole CPU's load in the same folds as every lathe's (the Owner, 2026-11-19:
    // like each machine's, so it reads the same way): eight folds lit from the left as far as all the
    // cores are busy together, the last in part; its reading on the plate at the front edge.
    if (cores > 0) {
      metaphor('core.busy.all')
      const N = 8, lit = (encode('core.busy.all', all) ?? 0) * N, glow = all != null && all > 0.85 ? '#ff7a45' : '#ffc94d'
      const a0 = cx + 0.1, span = cw - 0.2, za = zb - 1, zz = zf + 1, ya = y0 + 0.06, yz = y1 - 0.08
      for (let k = 0; k < N; k++) {
        const u0 = a0 + (k / N) * span, u1 = a0 + ((k + 1) / N) * span
        g.poly(k % 2 ? '#4a564f' : '#55625a', [[u0, ya, za], [u1, ya, za], [u1, yz, zz], [u0, yz, zz]], 0.4)
        const on = Math.max(0, Math.min(1, lit - k))
        if (on > 0.02) { const um = u0 + (u1 - u0) * on; g.poly(glow, [[u0, ya, za], [um, ya, za], [um, yz, zz], [u0, yz, zz]], 0) }
      }
      g.poly('none', [[a0, ya, za], [a0 + span, ya, za], [a0 + span, yz, zz], [a0, yz, zz]], 0.6)
    }
    g.sign('left', cx, y1, 0.1, Math.min(cw - 0.1, 1.5), 1.5, cores > 0 ? `CPU BUSY ${readingText(all, 'share')}` : 'CPU CONSOLE', '#f4efe2', 4.5)
  }

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
  if (gauges.length || cores > 0) things.push({ at: -1, draw: drawConsole })
  things.sort((a, b) => a.at - b.at).forEach(t => t.draw())
  return g.done()
}

/** The key the room's picture is kept under: every reading to the step it is drawn at. */
export const cpuRoomKey = (r: CpuRoomReadings) =>
  `g2:cpuroom:${rulersKey()}:${r.cores.map(c => `${c.busy == null ? 'n' : Math.round(c.busy * 26)}.${c.freq == null || !c.max ? 'n' : Math.round(c.freq / c.max * 20)}`).join(',')}:${r.temps.map(t => Math.round(t.c)).join(',')}`
