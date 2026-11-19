import { Draw, faces, INK, type Sprite } from '../kit'
import { encode, metaphor, rulerOf, rulersKey } from '../library/dictionary'

/**
 * The CPU unit's room, the workshop (a computer is a factory, the Owner 2026-11-19; RULES.md, 由内向外).
 * The hardware only — how many cores, how busy each, how fast, how hot, whether more is asked than it can do:
 *   the workshop floor   one machine tool per core, in rows: its work light as high as the core is busy,
 *                        its speed dial's needle at the core's clock, the dial's red line its top clock
 *   the left wall        one thermometer per sensor, red from 80 °C
 *   the production board three dials on the back wall (load over 1, 5 and 15 minutes against the cores:
 *                        past the middle into red, more is asked than it can do)
 *   the staging area     by the door, bins of work waiting for a free machine (as many as the share of
 *                        time something waited for a core)
 * Everything is a reading; what is not read is not drawn (no machines when the cores are not read, no
 * needle on a dial nothing reads).
 */

export type CpuRoomReadings = {
  /** Per core, in core order: busy share, clock and top clock (Hz); null where not read. */
  cores: { busy: number | null; freq: number | null; max: number | null }[]
  temps: { name: string; c: number }[]
  load: [number | null, number | null, number | null]
  pressure: number | null
}

const U = 35.8
const MACHINE = faces('#b7cfc0', '#94b3a1', '#7d9c8b'), IDLE = faces('#c9cec9', '#adb3ad', '#9aa09a')
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
  // The floor: a workshop slab; the aisle painted between the machines and the staging area.
  g.box(0, 0, fw, fd, 3, faces('#d8d6cf', '#bdbab1', '#aaa79e'), 0, 1.2)
  const sy = fd - STAGING - 0.2
  g.floor(0, 0, 3, `<rect x="${(0.3 * U).toFixed(1)}" y="${((sy - 0.12) * U).toFixed(1)}" width="${((fw - 0.6) * U).toFixed(1)}" height="${(0.1 * U).toFixed(1)}" fill="#f2c94c"/>`)
  // The back walls.
  g.box(0, 0, fw, 0.18, 46, WALL, 0, 1.2)
  g.box(0, 0.18, 0.18, fd - 0.18, 46, WALL, 0, 1.2)

  // Thermometers on the left wall, one per sensor (the hottest first), red from 80 °C.
  const temps = [...r.temps].sort((a, b) => b.c - a.c)
  if (temps.length) metaphor('sensor.temp')
  const span = (fd - 0.8) / Math.max(1, temps.length)
  temps.forEach((t, k) => {
    const a = 0.5 + k * span + span * 0.3, b = a + Math.min(0.14, span * 0.4), z0 = 20, z1 = 42
    const f = encode('sensor.temp', t.c / 100)!, hot = t.c >= 80, liquid = hot ? '#e0533d' : '#e88a3a'
    g.onRight(0.18, 0, a, b, z0, z1, '#fbfaf6', 0.7)
    g.onRight(0.18, 0, a + 0.02, b - 0.02, z0, z0 + (z1 - z0) * f, liquid, 0)
    g.face('right', 0.18, a + (b - a) / 2, z0 - 2, `<circle cx="0" cy="0" r="2.6" fill="${liquid}" stroke="${INK}" stroke-width="0.7"/>`)
    g.text('right', 0.18, b + 0.06, 12, `${Math.round(t.c)}°`, 5.5)
  })

  // The production board on the back wall: three dials of the work asked of the workshop over 1, 5 and
  // 15 minutes, against its cores (past the middle, into red, more is asked than it can do).
  const bw = 2, bx = Math.max(0.4, fw - 0.3 - bw)
  g.onLeft(0, 0.18, bx, bx + bw, 12, 44, '#26303d', 1)
  g.text('left', bx + 0.12, 0.18, 36, 'LOAD', 6.5, '#f2f0e8', 700)
  metaphor('load')
  const cores = r.cores.length
  r.load.forEach((l, k) => {
    const f = l == null || cores === 0 ? null : encode('load', (l / cores) / 2)
    g.face('left', bx, 0.18, 22, dial(8, (0.2 + k * 0.3) * bw * U + 4, f, 0.5, ['1m', '5m', '15m'][k]))
  })
  // The staging area by the door, marked on the floor: work waiting for a free machine waits here.
  const sx = 0.3, sw = fw - 0.6, sd = STAGING
  g.floor(sx, sy, 3, `<rect x="0" y="0" width="${(sw * U).toFixed(1)}" height="${(sd * U).toFixed(1)}" fill="none" stroke="#f2c94c" stroke-width="2.5" stroke-dasharray="6 4"/>` +
    `<text x="${(sw * U / 2).toFixed(1)}" y="${(sd * U - 6).toFixed(1)}" text-anchor="middle" font-family="'JetBrains Mono',ui-monospace,Menlo,monospace" font-size="8" font-weight="700" fill="#c9a43a">WAITING</text>`)

  // The machines and the waiting bins, drawn back to front.
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
    const avg = (v: (number | null)[]) => { const k = v.filter((x): x is number => x != null); return k.length ? k.reduce((a, b) => a + b, 0) / k.length : null }
    const MW = 0.52, md = 0.42
    groups.forEach((grp, i) => {
      const mw = Math.max(0.3, MW * grp.length / per)
      const c = { busy: avg(grp.map(x => x.busy)), freq: avg(grp.map(x => x.freq)), max: grp.reduce<number | null>((a, x) => x.max == null ? a : Math.max(a ?? 0, x.max), null) }
      const col = i % cols, x = 0.35 + col * PITCH + Math.floor(col / 2) * GAP + (PITCH - MW) / 2, y = 0.55 + Math.floor(i / cols) * ROW + (ROW - md) / 2
      things.push({ at: x + y, draw: () => {
        const busy = encode('core.busy', c.busy)
        const idle = busy == null || busy < 0.01
        g.shadow(x, y, mw, md, 0.25)
        g.box(x, y, mw, md, 12, idle ? IDLE : MACHINE, 0, 1.1)
        // The work light: a column on the cabinet at the machine's back corner.
        const kx = x + mw - 0.2
        g.box(kx, y, 0.2, 0.2, 22, faces('#5b5f6b', '#474a55', '#3a3d47'), 12, 0.9)
        g.onLeft(kx, y + 0.2, 0.03, 0.17, 14, 32, '#2a2c35', 0.5)
        if (busy != null && busy > 0.005) g.onLeft(kx, y + 0.2, 0.03, 0.17, 14, 14 + 18 * busy, busy > 0.85 ? '#ff7a45' : '#ffc94d', 0)
        // The speed dial on its front: the needle at its clock, the red line at its top.
        if (c.max) g.face('left', x, y + md, 6, dial(Math.min(6, mw * U * 0.3), mw * U * 0.38, c.freq != null ? encode('core.freq', c.freq / c.max) : null, 0.92))
      } })
    })
  } else {
    g.text('left', 0.6, 0.18, 30, 'CORES NOT READ', 7, '#8a857a')
  }
  // Bins of work waiting in the staging area, as many as the share of time something waited; only
  // when the waiting is read (looking the row up is what marks it drawn).
  const waiting = r.pressure == null ? 0 : encode('pressure', r.pressure)!
  for (let k = 0; k < waiting; k++) {
    const x = sx + 0.2 + k * 0.55, y = sy + 0.45
    if (x < fw - 0.8) things.push({ at: x + y, draw: () => g.box(x, y, 0.36, 0.36, 8, faces('#8fb0dc', '#6b8fc0', '#56789f'), 0, 0.9) })
  }
  things.sort((a, b) => a.at - b.at).forEach(t => t.draw())
  return g.done()
}

/** The key the room's picture is kept under: every reading to the step it is drawn at. */
export const cpuRoomKey = (r: CpuRoomReadings) =>
  `g2:cpuroom:${rulersKey()}:${r.cores.map(c => `${c.busy == null ? 'n' : Math.round(c.busy * 26)}.${c.freq == null || !c.max ? 'n' : Math.round(c.freq / c.max * 20)}`).join(',')}:${r.temps.map(t => Math.round(t.c)).join(',')}:${r.load.map(l => l == null ? 'n' : l.toFixed(1)).join(',')}:${r.pressure == null ? 'n' : Math.round(r.pressure * 10)}`
