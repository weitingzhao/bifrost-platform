import { Draw, faces, INK, type Sprite } from '../kit'
import { encode, metaphor } from '../library/dictionary'

/**
 * The CPU unit's room, the workshop (a computer is a factory, the Owner 2026-11-19; RULES.md, 由内向外).
 * The hardware only — how many cores, how busy each, how fast, how hot, whether more is asked than it can do:
 *   the workshop floor   one machine tool per core, in rows: its work light as high as the core is busy,
 *                        its speed dial's needle at the core's clock, the dial's red line its top clock
 *   the back wall        one thermometer per sensor, red from 80 °C
 *   the order office     three order dials (load over 1, 5 and 15 minutes against the cores: past the
 *                        middle into red, more is asked than it can do); outside it, the waiting line
 *                        (as long as the share of time something waited for a core)
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
const WALL = faces('#efe9dc', '#e2dac8', '#cfc6b2'), OFFICE = faces('#f6f1e6', '#e8dfcb', '#d6ccb5')

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

function person(g: Draw, x: number, y: number) {
  g.cylinder(x, y, 2.6, 0, 9, '#4a6fa5', '#36507a')
  const [hx, hy] = g.p(x, y, 12.5)
  g.add(`<circle cx="${hx.toFixed(1)}" cy="${hy.toFixed(1)}" r="2.6" fill="#f1c9a5" stroke="${INK}" stroke-width="0.8"/>`)
}

export function cpuRoom(r: CpuRoomReadings, fw = 8, fd = 8): Sprite {
  const g = new Draw(fw, fd, 56)
  // The floor: a workshop slab, the aisle painted on it.
  g.box(0, 0, fw, fd, 3, faces('#d8d6cf', '#bdbab1', '#aaa79e'), 0, 1.2)
  g.floor(0, 0, 3, `<rect x="${(5.55 * U).toFixed(1)}" y="${(0.3 * U).toFixed(1)}" width="${(0.12 * U).toFixed(1)}" height="${((fd - 0.6) * U).toFixed(1)}" fill="#f2c94c"/>`)
  // The back walls.
  g.box(0, 0, fw, 0.18, 46, WALL, 0, 1.2)
  g.box(0, 0.18, 0.18, fd - 0.18, 46, WALL, 0, 1.2)

  // Thermometers on the back wall, one per sensor (the hottest first), red from 80 °C.
  const temps = [...r.temps].sort((a, b) => b.c - a.c)
  if (temps.length) metaphor('sensor.temp')
  const span = 5.2 / Math.max(1, temps.length)
  temps.forEach((t, k) => {
    const a = 0.35 + k * span + span * 0.3, b = a + Math.min(0.14, span * 0.4), z0 = 20, z1 = 42
    const f = encode('sensor.temp', t.c / 100)!, hot = t.c >= 80
    g.onLeft(0, 0.18, a, b, z0, z1, '#fbfaf6', 0.7)
    g.onLeft(0, 0.18, a + 0.02, b - 0.02, z0, z0 + (z1 - z0) * f, hot ? '#e0533d' : '#e88a3a', 0)
    g.face('left', a + (b - a) / 2, 0.18, z0 - 2, `<circle cx="0" cy="0" r="2.6" fill="${hot ? '#e0533d' : '#e88a3a'}" stroke="${INK}" stroke-width="0.7"/>`)
    g.text('left', a - 0.04, 0.18, 12, `${Math.round(t.c)}°`, 5.5)
  })

  // The order office at the back right, its three order dials on its front.
  const ox = 5.9, oy = 0.4, ow = Math.min(1.8, fw - 6.1), od = 2.3, oh = 28
  g.box(ox, oy, ow, od, oh, OFFICE, 0, 1.3)
  g.sign('left', ox, oy + od, ow * 0.1, ow * 0.9, oh - 9, 'ORDERS', '#fbfaf6', 6)
  metaphor('load')
  const cores = r.cores.length
  r.load.forEach((l, k) => {
    const f = l == null || cores === 0 ? null : encode('load', (l / cores) / 2)
    g.face('left', ox, oy + od, 7.5, dial(7, (0.2 + k * 0.3) * ow * U / 0.9 + 4, f, 0.5, ['1m', '5m', '15m'][k]))
  })

  // The machines, and the waiting line in front of the office, drawn back to front.
  type Thing = { at: number; draw: () => void }
  const things: Thing[] = []
  if (cores > 0) {
    metaphor('core.machines'); metaphor('core.busy')
    // A speed dial only where the top clock is read (the Mac minis report none): no dial is drawn to sit empty.
    if (r.cores.some(c => c.max)) { metaphor('core.freq'); metaphor('core.freq.max') }
    const cols = Math.ceil(Math.sqrt(cores)), rows = Math.ceil(cores / cols)
    const W = 4.6, D = fd - 1.0, cw = W / cols, cd = D / rows
    const mw = Math.min(cw * 0.6, 0.85), md = Math.min(cd * 0.42, 0.62)
    r.cores.forEach((c, i) => {
      const x = 0.55 + (i % cols) * cw + (cw - mw) / 2, y = 0.75 + Math.floor(i / cols) * cd + (cd - md) / 2
      things.push({ at: x + y, draw: () => {
        const busy = encode('core.busy', c.busy)
        const idle = busy == null || busy < 0.01
        g.shadow(x, y, mw, md, 0.25)
        g.box(x, y, mw, md, 12, idle ? IDLE : MACHINE, 0, 1.1)
        // The work light: a column on the cabinet at the machine's back corner.
        const kx = x + mw - 0.24
        g.box(kx, y, 0.24, 0.24, 30, faces('#5b5f6b', '#474a55', '#3a3d47'), 12, 0.9)
        g.onLeft(kx, y + 0.24, 0.04, 0.2, 14, 40, '#2a2c35', 0.5)
        if (busy != null && busy > 0.005) g.onLeft(kx, y + 0.24, 0.04, 0.2, 14, 14 + 26 * busy, busy > 0.85 ? '#ff7a45' : '#ffc94d', 0)
        // The speed dial on its front: the needle at its clock, the red line at its top.
        if (c.max) g.face('left', x, y + md, 6, dial(Math.min(6, mw * U * 0.3), mw * U * 0.38, c.freq != null ? encode('core.freq', c.freq / c.max) : null, 0.92))
      } })
    })
  } else {
    g.text('left', 1.2, 0.18, 30, 'CORES NOT READ', 7, '#8a857a')
  }
  // The line only when the waiting is read (looking the row up is what marks it drawn).
  const waiting = r.pressure == null ? 0 : encode('pressure', r.pressure)!
  for (let k = 0; k < waiting; k++) {
    const x = ox + ow * 0.5, y = oy + od + 0.45 + k * 0.42
    if (y < fd - 0.2) things.push({ at: x + y, draw: () => person(g, x, y) })
  }
  things.sort((a, b) => a.at - b.at).forEach(t => t.draw())
  return g.done()
}

/** The key the room's picture is kept under: every reading to the step it is drawn at. */
export const cpuRoomKey = (r: CpuRoomReadings, fw = 8, fd = 8) =>
  `g2:cpuroom:${fw}x${fd}:${r.cores.map(c => `${c.busy == null ? 'n' : Math.round(c.busy * 26)}.${c.freq == null || !c.max ? 'n' : Math.round(c.freq / c.max * 20)}`).join(',')}:${r.temps.map(t => Math.round(t.c)).join(',')}:${r.load.map(l => l == null ? 'n' : l.toFixed(1)).join(',')}:${r.pressure == null ? 'n' : Math.round(r.pressure * 10)}`
