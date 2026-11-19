import { Draw, faces, INK, type Sprite } from '../kit'
import { encode, metaphor, rulerOf, rulersKey } from '../library/dictionary'

/**
 * The CPU unit's room, the workshop (a computer is a factory, the Owner 2026-11-19; RULES.md, 由内向外).
 * The hardware only — how many cores, how busy each, how fast, how hot:
 *   the workshop floor   one machine tool per core, in rows: its work light as high as the core is busy,
 *                        its speed dial's needle at the core's clock, the dial's red line its top clock
 *   the CPU console      at the front, for the whole CPU: a busy dial (all cores together) and the
 *                        chip's thermometer
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
const esc = (t: string) => t.replace(/[&<>"]/g, c => ({ '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;' })[c]!)
// Colours: structure is one colour each and never changes (walls, floor, machines, cabinets, the console);
// only the readings' parts change colour, each change written in the key (the Owner, 2026-11-19: every
// colour stands for something). Machines were grey when idle and green when not — the work light's
// reading said twice, and not in the key: taken out.
const MACHINE = faces('#b7cfc0', '#94b3a1', '#7d9c8b')
const WALL = faces('#efe9dc', '#e2dac8', '#cfc6b2')

/** A half dial on a face, centred at u (face units), its needle at share f (0–1) of its sweep; none when not read. */
function dial(r: number, u: number, f: number | null, redFrom: number, label = '') {
  const arc = (a: number, b: number, c: string) => {
    const p = (t: number) => [u + r * Math.cos(Math.PI * (1 - t)), -r * Math.sin(Math.PI * (1 - t))]
    const [x0, y0] = p(a), [x1, y1] = p(b)
    return `<path d="M${x0.toFixed(1)},${y0.toFixed(1)} A${r},${r} 0 0 1 ${x1.toFixed(1)},${y1.toFixed(1)}" fill="none" stroke="${c}" stroke-width="${(r * 0.28).toFixed(1)}"/>`
  }
  let s = `<path d="M${u - r - 1},0 A${r + 1},${r + 1} 0 0 1 ${u + r + 1},0 Z" fill="#fbfaf6" stroke="${INK}" stroke-width="0.8"/>`
  s += arc(0.02, redFrom, '#9fcf9f') + arc(redFrom, 0.98, '#e27d6f')
  if (f != null) {
    const t = Math.max(0, Math.min(1, f)), a = Math.PI * (1 - t)
    s += `<line x1="${u}" y1="0" x2="${(u + r * 0.92 * Math.cos(a)).toFixed(1)}" y2="${(-r * 0.92 * Math.sin(a)).toFixed(1)}" stroke="${INK}" stroke-width="1.2" stroke-linecap="round"/>`
  }
  s += `<circle cx="${u}" cy="0" r="1.2" fill="${INK}"/>`
  if (label) s += `<text x="${u}" y="${(r * 0.55 + 4).toFixed(1)}" text-anchor="middle" font-family="'JetBrains Mono',ui-monospace,Menlo,monospace" font-size="${Math.max(4.5, r * 0.55).toFixed(1)}" font-weight="700" fill="${INK}">${label}</text>`
  return s
}

/**
 * One design for every CPU, scaled out by its standard parts (the Owner, 2026-11-19: the more cores, the
 * bigger the room; however many, it is the same room, configured, so one design draws every CPU). A
 * machine keeps its size and its cell; the room is the smallest of the allowed shapes that holds
 * them, one way round, so a room twice the size holds about twice the machines. A CPU of more machines than the largest room
 * holds says so on its wall, and the ruler (more cores to a machine) is how it fits.
 */
// Cores come in pairs and powers of two, so machines stand in pairs, 2, 4 or 8 to a row, an aisle
// between pairs, every row full (the Owner, 2026-11-19: three to a row, or one left over, looks wrong).
// The staging area is a strip across the front, by the door; the machines fill the floor behind it,
// a walkway between rows. A room holds 8, 24 or 48: 8 cores the small room, 16 the middle, 32 the large.
const PITCH = 0.7, ROW = 1.0, GAP = 0.2, STAGING = 1.2, ROOMS: [number, number][] = [[4, 4], [4, 8], [8, 8]]
const rowWidth = (cols: number) => cols * PITCH + Math.max(0, cols / 2 - 1) * GAP
const rowsOf = (d: number) => Math.floor((d - 0.6 - STAGING) / ROW)
/** The most machines to a row that a room this wide holds and that divide the machines evenly. */
const colsOf = (w: number, machines: number) => {
  let best = 1
  for (const c of [2, 4, 8]) if (rowWidth(c) <= w - 0.6 && (machines <= 1 || machines % c === 0)) best = c
  return best
}
const fitsIn = ([w, d]: [number, number], n: number) => Math.ceil(n / colsOf(w, n)) <= rowsOf(d)
export function cpuRoomShape(machines: number): [number, number] {
  return ROOMS.find(s => fitsIn(s, machines)) ?? ROOMS[ROOMS.length - 1]
}
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
  const sy = fd - STAGING - 0.2
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
  const all = avgOf(r.cores.map(c => c.busy))

  // The console: a desk at the front with an upright panel, for the whole CPU: a busy dial (all cores
  // together, the average of each machine's work light) and the chip's thermometer. Only what the CPU
  // knows as hardware (the Owner, 2026-11-19: load and waiting are counted by the scheduler, not the CPU).
  // Shaped as a plant's control desk (the Owner, 2026-11-19: it looked like a television): a sloped
  // desk, low at the front, and behind it an upright instrument bank under a hood; each instrument in its
  // bezel with a name plate. Nothing on it that is not a reading: no buttons or lamps for show.
  const cw = Math.min(2.4, fw - 0.6), cx = fw - 0.3 - cw, cy = sy + 0.15, cd = 0.75, bank = 0.22
  const STEEL = faces('#d9dbe0', '#bfc3cb', '#a9aeb8'), DARK = faces('#4a5263', '#353c4a', '#2b313d')
  const drawConsole = () => {
    g.shadow(cx, cy, cw, cd, 0.35)
    // The instrument bank: upright behind the desk, a hood on top, cheeks at the ends.
    g.box(cx, cy, cw, bank, 40, DARK, 0, 1.2)
    g.box(cx - 0.06, cy - 0.04, cw + 0.12, bank + 0.16, 3, STEEL, 40, 1)
    g.box(cx + cw - 0.04, cy, 0.08, bank + 0.06, 40, STEEL, 0, 0.9)
    const P = cy + bank
    // The desk: sloped, 15 px high at the bank, 8 at the front edge.
    const y0 = P, y1 = cy + cd, zb = 15, zf = 8
    g.poly(STEEL.left, [[cx, y1, 0], [cx + cw, y1, 0], [cx + cw, y1, zf], [cx, y1, zf]], 1.2)
    g.poly(STEEL.right, [[cx + cw, y0, 0], [cx + cw, y1, 0], [cx + cw, y1, zf], [cx + cw, y0, zb]], 1.2)
    g.poly(STEEL.top, [[cx, y0, zb], [cx + cw, y0, zb], [cx + cw, y1, zf], [cx, y1, zf]], 1.2)
    g.poly('#c9ccd3', [[cx + 0.1, y0 + 0.06, zb - 1], [cx + cw - 0.1, y0 + 0.06, zb - 1], [cx + cw - 0.1, y1 - 0.08, zf + 1], [cx + 0.1, y1 - 0.08, zf + 1]], 0.7)
    // A name plate on the desk's front edge.
    g.sign('left', cx, y1, 0.12, 1.25, 1.5, 'CPU CONSOLE', '#f4efe2', 4.5)
    // Instrument 1: the busy dial, all cores together, in a round bezel.
    const plate = (u: number, z: number, text: string) => g.face('left', cx, P, z, `<rect x="${(u - 24).toFixed(1)}" y="-3.5" width="48" height="6.5" rx="1" fill="#e8e2cf" stroke="${INK}" stroke-width="0.5"/><text x="${u.toFixed(1)}" y="1.6" text-anchor="middle" font-family="'JetBrains Mono',ui-monospace,Menlo,monospace" font-size="4.2" font-weight="700" fill="${INK}">${text}</text>`)
    if (cores > 0) {
      metaphor('core.busy.all')
      const u = 0.62 * U
      g.face('left', cx, P, 31, `<circle cx="${u}" cy="0" r="10" fill="#1f242e" stroke="#9aa0ab" stroke-width="1.8"/>`)
      g.face('left', cx, P, 30, dial(7.5, u, encode('core.busy.all', all), 0.85))
      if (all != null) g.face('left', cx, P, 30, `<text x="${u}" y="6" text-anchor="middle" font-family="'JetBrains Mono',ui-monospace,Menlo,monospace" font-size="5" font-weight="700" fill="#f2f0e8">${Math.round(all * 100)}%</text>`)
      plate(u, 18, 'BUSY · ALL CORES')
    }
    // Instrument 2: the chip's thermometer, in a tall bezel.
    const t = chip[0]
    if (t) {
      const a = 1.5, b = a + 0.12, z0 = 24, z1 = 38, f = encode('sensor.temp', t.c / 100)!
      g.onLeft(cx, P, a - 0.1, b + 0.42, z0 - 4, z1 + 2, '#1f242e', 1)
      g.onLeft(cx, P, a, b, z0, z1, '#fbfaf6', 0.6)
      g.onLeft(cx, P, a + 0.02, b - 0.02, z0, z0 + (z1 - z0) * f, liquidOf(t.c), 0)
      g.face('left', cx + a + 0.06, P, z0 - 1, `<circle cx="0" cy="0" r="2.6" fill="${liquidOf(t.c)}" stroke="${INK}" stroke-width="0.6"/>`)
      g.text('left', cx + b + 0.04, P, 31, `${Math.round(t.c)}°`, 6.5, '#f2f0e8', 700)
      plate((a + 0.22) * U, 18, `CHIP · ${esc(t.name).toUpperCase()}`)
    }
  }

  // The machines, then the console in front of them, drawn back to front.
  type Thing = { at: number; draw: () => void }
  const things: Thing[] = []
  if (cores > 0) {
    metaphor('core.busy')
    // A speed dial only where the top clock is read (the Mac minis report none): no dial is drawn to sit empty.
    if (r.cores.some(c => c.max)) { metaphor('core.freq'); metaphor('core.freq.max') }
    // As many machines as the ruler counts (one per core by default): each stands for its group of
    // cores, the last one narrower when not full; every machine the same size in every room.
    const per = Math.max(1, Math.round(rulerOf('core.machines')))
    const cols = colsOf(fw, count), fits = cols * rowsOf(fd)
    const groups = Array.from({ length: Math.min(count, fits) }, (_, m) => r.cores.slice(m * per, (m + 1) * per))
    if (count > fits) g.text('left', 0.4, 0.18, 47, `${fits} OF ${count} MACHINES FIT — SET MORE CORES A MACHINE`, 5.5, '#b8433f', 700)
    const MW = 0.52, md = 0.42
    groups.forEach((grp, i) => {
      const mw = Math.max(0.3, MW * grp.length / per)
      const c = { busy: avgOf(grp.map(x => x.busy)), freq: avgOf(grp.map(x => x.freq)), max: grp.reduce<number | null>((a, x) => x.max == null ? a : Math.max(a ?? 0, x.max), null) }
      const col = i % cols, x = 0.35 + col * PITCH + Math.floor(col / 2) * GAP + (PITCH - MW) / 2, y = 0.55 + Math.floor(i / cols) * ROW + (ROW - md) / 2
      things.push({ at: x + y, draw: () => {
        const busy = encode('core.busy', c.busy)
        g.shadow(x, y, mw, md, 0.25)
        g.box(x, y, mw, md, 12, MACHINE, 0, 1.1)
        // The work light: a column on the cabinet at the machine's back corner.
        const kx = x + mw - 0.2
        g.box(kx, y, 0.2, 0.2, 22, faces('#5b5f6b', '#474a55', '#3a3d47'), 12, 0.9)
        g.onLeft(kx, y + 0.2, 0.03, 0.17, 14, 32, '#2a2c35', 0.5)
        if (busy != null && busy > 0.005) g.onLeft(kx, y + 0.2, 0.03, 0.17, 14, 14 + 18 * busy, busy > 0.85 ? '#ff7a45' : '#ffc94d', 0)
        // A core's own temperature, where its sensor reports one: a small thermometer on its cabinet.
        const hot = grp.map((_, k) => perCore.get(i * per + k)).filter((v): v is number => v != null)
        if (hot.length) {
          const tc = Math.max(...hot)
          g.onRight(kx + 0.2, y, 0.04, 0.16, 14, 32, '#fbfaf6', 0.5)
          g.onRight(kx + 0.2, y, 0.06, 0.14, 14, 14 + 18 * encode('sensor.temp', tc / 100)!, liquidOf(tc), 0)
        }
        // The speed dial on its front: the needle at its clock, the red line at its top.
        if (c.max) g.face('left', x, y + md, 6, dial(Math.min(6, mw * U * 0.3), mw * U * 0.38, c.freq != null ? encode('core.freq', c.freq / c.max) : null, 0.92))
      } })
    })
  } else {
    g.text('left', 0.6, 0.18, 30, 'CORES NOT READ', 7, '#8a857a')
  }
  things.push({ at: cx + cy + 99, draw: drawConsole })
  things.sort((a, b) => a.at - b.at).forEach(t => t.draw())
  return g.done()
}

/** The key the room's picture is kept under: every reading to the step it is drawn at. */
export const cpuRoomKey = (r: CpuRoomReadings) =>
  `g2:cpuroom:${rulersKey()}:${r.cores.map(c => `${c.busy == null ? 'n' : Math.round(c.busy * 26)}.${c.freq == null || !c.max ? 'n' : Math.round(c.freq / c.max * 20)}`).join(',')}:${r.temps.map(t => Math.round(t.c)).join(',')}`
