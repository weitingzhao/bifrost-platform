import { Draw, faces, INK, type Sprite } from './kit'
import type { Light } from './light'
import type { Look, State } from './places'
import { shade } from './sprites'

/**
 * Devices as city facilities (the Owner, 2026-11-19: this is a city; the hardware is its utilities.
 * A Linux node is a power plant, the NAS a warehouse — a city has no graphics cards). Each facility
 * shows how full it is natively, so a glance says it all with no numbers; every part is driven by a
 * reading (nothing made up), and a gauge nothing reads stays empty, never filled.
 *
 *   power plant (Linux node)      CPU: lit windows of the turbine hall, and smoke from the stack
 *                                 memory: the cooling water in its tank   disk: the fuel in its tank
 *   power station (the GPU box)   the same, with a cooling tower that steams when it runs
 *   warehouse (NAS)               one loading door per volume, crates stacked to how full it is
 *   exchange (Windows TWS)        the gateway to the market: its ticker board is lit when read
 *   dispatch centre (Mac mini)    the glass control room is lit
 *   office (laptop)               its windows are lit
 *   telecom tower (network)       its lights
 * How it stands: lit and running online; cold and dark on standby; a red beacon when down; grey when unread.
 */

export type Levels = { state: State; cpu: number | null; mem: number | null; disks: (number | null)[]; vols?: { f: number | null; units: State[] }[] }
export type Form = 'plant' | 'station' | 'warehouse' | 'exchange' | 'dispatch' | 'office' | 'tower'

export function formOf(look: Look, gpu: boolean, laptop: boolean): Form {
  if (look === 'linux') return gpu ? 'station' : 'plant'
  if (look === 'mac') return laptop ? 'office' : 'dispatch'
  if (look === 'storage') return 'warehouse'
  if (look === 'windows') return 'exchange'
  return 'tower'
}

const gauge = (f: number) => f < 0.7 ? '#5cc06c' : f < 0.9 ? '#f2b84b' : '#e5534b'
const EMPTY = '#d9d5cc'
const lit = (f: number | null, n: number) => f == null ? 0 : Math.max(f > 0.005 ? 1 : 0, Math.round(f * n))

type Tone = { wall: ReturnType<typeof faces>; roof: string; glass: string; glassLit: string; metal: ReturnType<typeof faces> }
function toneOf(hue: string, light: Light, state: State): Tone {
  const night = light === 'night', grey = state === 'unread'
  const base = grey ? '#b9b6ae' : hue
  const wall = night ? faces(shade(base, -0.45), shade(base, -0.55), shade(base, -0.65)) : faces(shade(base, 0.7), shade(base, 0.45), shade(base, 0.25))
  return {
    wall,
    roof: night ? '#4a4f6a' : '#efece5',
    glass: night ? '#20223a' : '#c9d6e3',
    glassLit: state === 'online' ? '#ffd76a' : night ? '#20223a' : '#c9d6e3',
    metal: night ? faces('#5a5f7a', '#4a4f6a', '#3c4058') : faces('#e4e2dc', '#c9c6be', '#b3afa6'),
  }
}

/** A tank (a vertical cylinder) filled from the bottom to its level; nothing read, it stays empty. */
function tank(g: Draw, x: number, y: number, r: number, h: number, f: number | null, liquid: string) {
  g.cylinder(x, y, r, 0, h, EMPTY, '#bdb8ad', '#ece9e2')
  if (f != null && f > 0.005) g.cylinder(x, y, r - 0.5, 0, Math.max(2, h * Math.min(1, f)), liquid, shade(liquid, -0.25), shade(liquid, 0.2))
}

/** Smoke from a stack: one puff for a quiet machine, up to three for a busy one; none when it is not running. */
function smoke(g: Draw, x: number, y: number, z: number, f: number | null, on: boolean) {
  if (!on || f == null) return
  const n = Math.min(3, 1 + Math.floor(f * 3))
  for (let k = 0; k < n; k++) {
    const [cx, cy] = g.p(x + k * 0.12, y - k * 0.08, z + 8 + k * 9)
    g.add(`<circle cx="${cx.toFixed(1)}" cy="${cy.toFixed(1)}" r="${(5 + k * 1.6).toFixed(1)}" fill="#ffffff" opacity="${(0.85 - k * 0.2).toFixed(2)}" stroke="#b8b8c0" stroke-width="0.8"/>`)
  }
}

/** A red beacon on top of whatever is down. */
const beacon = (g: Draw, x: number, y: number, z: number, state: State) => {
  if (state !== 'down') return
  const [cx, cy] = g.p(x, y, z + 4)
  g.add(`<circle cx="${cx.toFixed(1)}" cy="${cy.toFixed(1)}" r="5" fill="#ff4d4d" stroke="${INK}" stroke-width="1"/><circle cx="${cx.toFixed(1)}" cy="${cy.toFixed(1)}" r="10" fill="#ff4d4d" opacity="0.25"/>`)
}

/** A hall's windows along a face: of n, how many are lit tells how busy it is. */
function hallWindows(g: Draw, face: 'left' | 'right', x: number, y: number, span: number, z0: number, z1: number, n: number, f: number | null, T: Tone) {
  const on = lit(f, n)
  g.windows(face, x, y, span, z0, z1, 1, n, { fill: T.glass, lit: (_r, c) => c < on, litFill: '#ffd76a', tall: 0.7 })
}

export function deviceSprite(form: Form, fw: number, fd: number, hue: string, light: Light, lv: Levels): Sprite {
  const T = toneOf(hue, light, lv.state)
  const run = lv.state === 'online'
  const tall = form === 'station' ? 96 : form === 'plant' ? 84 : form === 'tower' ? 70 : 52
  const g = new Draw(fw, fd, tall + 24)
  const i = 0.24, x0 = i, y0 = i, w = fw - 2 * i, d = fd - 2 * i
  const disks = lv.disks.length ? lv.disks : [null]
  g.shadow(x0, y0, w, d, 0.4)
  const water = light === 'night' ? '#3f7fd6' : '#5aa9f0', fuel = '#c98a3a'

  if (form === 'plant' || form === 'station') {
    // The turbine hall at the back, its windows lit by how busy the CPU is; the stack smokes when it runs.
    const hw = form === 'station' ? w * 0.55 : w * 0.62, hd = d * 0.62, H = 32
    g.box(x0, y0, hw, hd, H, T.wall, 0, 1.4)
    g.poly(T.roof, [[x0, y0, H], [x0 + hw, y0, H], [x0 + hw, y0 + hd, H], [x0, y0 + hd, H]], 1)
    hallWindows(g, 'left', x0, y0 + hd, hw, 8, H - 6, Math.max(4, Math.round(hw * 2.4)), run ? lv.cpu : null, T)
    hallWindows(g, 'right', x0 + hw, y0, hd, 8, H - 6, Math.max(3, Math.round(hd * 1.4)), run ? lv.cpu : null, T)
    // Memory: the cooling water; disk: the fuel. Tanks in the yard in front of the hall.
    const r = Math.min(13, 6 + Math.min(w, d) * 1.6)
    tank(g, x0 + hw * 0.3, y0 + d * 0.84, r, 26, lv.mem, water)
    tank(g, x0 + hw * 0.75, y0 + d * 0.84, r, 26, disks[0], fuel)
    if (form === 'station') {
      // The cooling tower: steam when it runs, cold on standby.
      const cx = x0 + hw + (w - hw) / 2, cy = y0 + d * 0.35, R = Math.min(30, 10 + Math.min(w, d) * 4)
      g.cylinder(cx, cy, R, 0, 70, T.metal.left, T.metal.right, T.metal.top)
      g.cylinder(cx, cy, R * 0.82, 70, 72, '#9aa0b0', '#8a90a0', '#6a7080')
      smoke(g, cx, cy, 72, lv.cpu ?? (run ? 0.6 : null), run)
      beacon(g, cx, cy, 72, lv.state)
      tank(g, cx, y0 + d * 0.84, r, 26, disks[1] ?? null, fuel)
    } else {
      // The stack.
      const sx = x0 + hw + (w - hw) * 0.5, sy = y0 + d * 0.25
      g.cylinder(sx, sy, 5, 0, 64, '#c9c3b8', '#a9a397', '#8a857b')
      g.cylinder(sx, sy, 5.4, 56, 60, '#d9534f', '#b84440')
      smoke(g, sx, sy, 64, lv.cpu, run)
      beacon(g, sx, sy, 64, lv.state)
    }
  }

  if (form === 'warehouse' && lv.vols && lv.vols.length > 0) {
    warehousePark(g, x0, y0, w, d, T, lv, hue, light)
    return g.done()
  }

  if (form === 'warehouse') {
    // A long shed with a pitched roof; one loading door per volume, the crates behind it stacked to how full it is.
    const H = 24, mid = y0 + d / 2
    g.box(x0, y0, w, d, H, T.wall, 0, 1.4)
    g.poly(shade(T.roof, -0.05), [[x0, y0, H], [x0 + w, y0, H], [x0 + w, mid, H + 14], [x0, mid, H + 14]], 1.2)
    g.poly(shade(T.roof, -0.15), [[x0, mid, H + 14], [x0 + w, mid, H + 14], [x0 + w, y0 + d, H], [x0, y0 + d, H]], 1.2)
    g.poly(T.wall.right, [[x0 + w, y0, H], [x0 + w, mid, H + 14], [x0 + w, y0 + d, H]], 1.2)
    const doors = [...disks, ...Array(Math.max(0, 3 - disks.length)).fill(null)]
    const span = w / doors.length
    doors.forEach((f, k) => {
      const a = k * span + span * 0.14, b = (k + 1) * span - span * 0.14
      g.onLeft(x0, y0 + d, a, b, 0, 18, '#3a3d48', 1)
      if (f != null && f > 0.005) {
        const rows = Math.max(1, Math.round(Math.min(1, f) * 4))
        for (let q = 0; q < rows; q++) g.onLeft(x0, y0 + d, a + 0.04, b - 0.04, 1 + q * 4.2, 4.6 + q * 4.2, q === rows - 1 ? gauge(f) : '#c98a3a', 0.6)
      }
    })
    beacon(g, x0 + w * 0.5, mid, H + 14, lv.state)
  }

  if (form === 'exchange') {
    // The exchange (the gateway to the market): columns and a pediment; its ticker board is lit when read.
    const H = 30, fy = y0 + d * 0.85
    g.box(x0, y0 + d * 0.15, w, d * 0.7, H, T.wall, 0, 1.4)
    g.poly(T.roof, [[x0, fy, H], [x0 + w, fy, H], [x0 + w / 2, fy, H + 14]], 1.2)
    for (let k = 1; k < 6; k++) g.line([x0 + w * k / 6, fy, 2], [x0 + w * k / 6, fy, H - 8], T.wall.right, 3)
    g.onLeft(x0, fy, w * 0.08, w * 0.92, H - 7, H - 1, run ? '#1d2a3a' : '#5a5d66', 1)
    if (run) for (let k = 0; k < 5; k++) g.onLeft(x0, fy, w * (0.12 + k * 0.16), w * (0.2 + k * 0.16), H - 5, H - 3, k % 2 ? '#7be08f' : '#f29b8f', 0.4)
    beacon(g, x0 + w / 2, fy, H + 14, lv.state)
  }

  if (form === 'dispatch') {
    // The dispatch centre: a low block, a glass control room on top lit when it runs, a radar dish.
    const H = 16
    g.box(x0 + w * 0.1, y0 + d * 0.1, w * 0.8, d * 0.8, H, T.wall, 0, 1.3)
    g.box(x0 + w * 0.25, y0 + d * 0.25, w * 0.5, d * 0.5, 10, faces(T.roof, T.glassLit, shade(T.glassLit, -0.15)), H, 1.1)
    const [cx, cy] = g.p(x0 + w * 0.5, y0 + d * 0.5, H + 18)
    g.line([x0 + w * 0.5, y0 + d * 0.5, H + 10], [x0 + w * 0.5, y0 + d * 0.5, H + 16], INK, 2)
    g.add(`<ellipse cx="${cx.toFixed(1)}" cy="${cy.toFixed(1)}" rx="9" ry="4" fill="${T.metal.top}" stroke="${INK}" stroke-width="1.1" transform="rotate(-20 ${cx.toFixed(1)} ${cy.toFixed(1)})"/>`)
    beacon(g, x0 + w * 0.5, y0 + d * 0.5, H + 20, lv.state)
  }

  if (form === 'office') {
    // A small office with a pitched roof: its windows are lit when anything reads it.
    const H = 18, mid = y0 + d / 2
    g.box(x0 + w * 0.12, y0 + d * 0.12, w * 0.76, d * 0.76, H, T.wall, 0, 1.3)
    g.poly(T.roof, [[x0 + w * 0.12, y0 + d * 0.12, H], [x0 + w * 0.88, y0 + d * 0.12, H], [x0 + w * 0.88, mid, H + 10], [x0 + w * 0.12, mid, H + 10]], 1.1)
    g.poly(shade(T.roof, -0.12), [[x0 + w * 0.12, mid, H + 10], [x0 + w * 0.88, mid, H + 10], [x0 + w * 0.88, y0 + d * 0.88, H], [x0 + w * 0.12, y0 + d * 0.88, H]], 1.1)
    g.windows('left', x0 + w * 0.12, y0 + d * 0.88, w * 0.76, 4, H - 4, 1, 3, { fill: T.glass, lit: () => run, litFill: '#ffd76a' })
    beacon(g, x0 + w * 0.5, mid, H + 10, lv.state)
  }

  if (form === 'tower') {
    // A telecom tower: a lattice and a light in its state's colour.
    const cx = x0 + w / 2, cy = y0 + d / 2
    for (const [dx, dy] of [[-0.25, -0.25], [0.25, -0.25], [0.25, 0.25], [-0.25, 0.25]]) g.line([cx + dx, cy + dy, 0], [cx, cy, 64], INK, 1.4)
    for (let z = 12; z < 60; z += 12) g.line([cx - 0.2 * (1 - z / 64), cy, z], [cx + 0.2 * (1 - z / 64), cy, z], INK, 1)
    const [lx, ly] = g.p(cx, cy, 64)
    g.add(`<circle cx="${lx.toFixed(1)}" cy="${ly.toFixed(1)}" r="3.5" fill="${run ? '#5ce07a' : lv.state === 'down' ? '#ff4d4d' : '#a4a8b2'}" stroke="${INK}" stroke-width="1"/>`)
  }
  return g.done()
}

const UNIT: Record<State, string> = { online: '#5ce07a', standby: '#f2b84b', down: '#ff4d4d', unread: '#a4a8b2' }

/**
 * The NAS as a logistics park (the Owner, 2026-11-19: a warehouse, tall, its physical disks seen).
 * Along the park, back to front: a storage hall per volume, the biggest first, each with one roller
 * door per disk of its array (the lamp over the door: that disk's state) and crates stacked behind the
 * doors to how full the volume is; a loading dock along the doors; at the front the gatehouse (its
 * windows lit by CPU) and its water tank (memory).
 */
function warehousePark(g: Draw, x0: number, y0: number, w: number, d: number, T: Tone, lv: Levels, hue: string, light: Light) {
  const vols = lv.vols!
  const run = lv.state === 'online'
  const dock = 0.55, bw = w - dock - 0.05, fx = x0 + bw
  const night = light === 'night'
  // The loading dock: a concrete apron along the doors, its edge striped.
  g.box(fx, y0, dock, d, 5, faces(night ? '#5a5d6a' : '#e4e1da', night ? '#4a4d58' : '#cfcbc2', night ? '#3e414c' : '#bdb9b0'), 0, 1.1)
  for (let k = 0; k < d / 0.5; k++) g.onRight(fx + dock, y0, k * 0.5, k * 0.5 + 0.25, 1, 4, '#f2c94c', 0.3)
  // Halls share the park's length by how big each volume is, the biggest at the back; the gatehouse keeps the front.
  const gate = Math.min(1.1, d * 0.15), park = d - gate - 0.2
  const sizes = vols.map(v => Math.max(1, v.units.length))
  const total = sizes.reduce((a, b) => a + b, 0)
  let y = y0
  vols.forEach((v, k) => {
    const len = park * sizes[k] / total - 0.15
    const H = k === 0 ? 54 : 38
    const wall = faces(T.wall.top, T.wall.left, T.wall.right)
    g.box(x0, y, bw, len, H, wall, 0, 1.4)
    // Ribbed metal siding on the two faces we see.
    for (let q = 0.18; q < len - 0.05; q += 0.18) g.onRight(fx, y, q, q + 0.02, 2, H - 8, shade(T.wall.right, -0.08), 0.2)
    for (let q = 0.18; q < bw - 0.05; q += 0.18) g.onLeft(x0, y + len, q, q + 0.02, 2, H - 8, shade(T.wall.left, -0.08), 0.2)
    // The band under the eaves, in the park's hue.
    g.onRight(fx, y, 0, len, H - 7, H - 2, night ? shade(hue, -0.35) : hue, 0.8)
    g.onLeft(x0, y + len, 0, bw, H - 7, H - 2, night ? shade(hue, -0.35) : hue, 0.8)
    // A shallow gable along the hall, the ridge in the middle, skylights on the slope we see.
    const mid = x0 + bw / 2, ridge = H + 9
    g.poly(shade(T.roof, -0.04), [[x0, y, H], [mid, y, ridge], [mid, y + len, ridge], [x0, y + len, H]], 1.1)
    g.poly(shade(T.roof, -0.14), [[mid, y, ridge], [fx, y, H], [fx, y + len, H], [mid, y + len, ridge]], 1.1)
    g.poly(T.wall.left, [[x0, y + len, H], [mid, y + len, ridge], [fx, y + len, H]], 1.1)
    for (let q = 0.25; q < len - 0.35; q += 0.55) g.poly(night ? '#3a4a6a' : '#bfe0f5', [[mid + 0.15, y + q, ridge - 1.5], [fx - 0.2, y + q, H + 1.5], [fx - 0.2, y + q + 0.3, H + 1.5], [mid + 0.15, y + q + 0.3, ridge - 1.5]], 0.6)
    // A roller door per disk; behind it the crates of the volume; over it the disk's lamp.
    const n = Math.max(1, v.units.length), span = len / n, dh = Math.min(30, H - 14)
    for (let u = 0; u < n; u++) {
      const a = u * span + span * 0.16, b = (u + 1) * span - span * 0.16
      g.onRight(fx, y, a - 0.05, b + 0.05, 5, dh + 3, shade(T.wall.right, -0.25), 0.8)
      g.onRight(fx, y, a, b, 5, dh, '#2c2e38', 0.8)
      const f = v.f
      if (run && f != null && f > 0.002) {
        const rows = Math.max(1, Math.round(Math.min(1, f) * 6))
        for (let r = 0; r < rows; r++) {
          const z = 6 + r * ((dh - 6) / 6)
          g.onRight(fx, y, a + 0.05, (a + b) / 2 - 0.02, z, z + (dh - 6) / 6 - 0.6, '#e0a24e', 0.9)
          g.onRight(fx, y, (a + b) / 2 + 0.02, b - 0.05, z, z + (dh - 6) / 6 - 0.6, '#d08f3c', 0.9)
        }
      }
      // The disk's lamp over its door: big enough to read from across the district.
      g.onRight(fx, y, (a + b) / 2 - 0.16, (a + b) / 2 + 0.16, dh + 5, dh + 12, UNIT[v.units[u] ?? 'unread'], 1.2)
    }
    if (k === 0) beacon(g, mid, y + len / 2, ridge, lv.state)
    y += len + 0.15
  })
  // The gatehouse at the front, its windows lit by CPU, and its water tank (memory).
  const gy = y0 + d - gate
  g.box(x0 + bw * 0.35, gy, bw * 0.65, gate, 20, faces(T.wall.top, T.wall.left, T.wall.right), 0, 1.2)
  g.poly(T.roof, [[x0 + bw * 0.35, gy, 20], [fx, gy, 20], [fx, gy + gate, 20], [x0 + bw * 0.35, gy + gate, 20]], 1)
  const on = lit(run ? lv.cpu : null, 4)
  g.windows('left', x0 + bw * 0.35, gy + gate, bw * 0.65, 6, 16, 1, 4, { fill: T.glass, lit: (_r, c) => c < on, litFill: '#ffd76a' })
  g.windows('right', fx, gy, gate, 6, 16, 1, 2, { fill: T.glass, lit: (_r, c) => c < on, litFill: '#ffd76a' })
  tank(g, x0 + bw * 0.16, gy + gate * 0.55, 8, 22, run ? lv.mem : null, night ? '#3f7fd6' : '#5aa9f0')
}

/** The key a facility's picture is kept under: its form, size, hue, light, state and every level to a tenth. */
export const deviceKey = (form: Form, fw: number, fd: number, hue: string, light: Light, lv: Levels) => {
  const q = (f: number | null) => f == null ? 'n' : String(Math.round(f * 10))
  return `g2:fac:${form}:${fw}x${fd}:${hue}:${light}:${lv.state}:${q(lv.cpu)}:${q(lv.mem)}:${lv.disks.map(q).join('.')}:${(lv.vols ?? []).map(v => `${q(v.f)}${v.units.join('')}`).join('|')}`
}
