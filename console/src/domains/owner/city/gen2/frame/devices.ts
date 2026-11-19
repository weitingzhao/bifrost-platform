import { Draw, faces, INK, type Sprite } from './kit'
import type { Light } from './light'
import type { Look, State } from './places'
import { beacon, dock, doorRow, gatehouse, gauge, grille, hall, plaque, shade, signBand, smoke, tank, toneOf, windowStrip, type Tone } from './library/parts'
import { OS_FACES, type MachineOs } from './library/landmarks'

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

export function deviceSprite(form: Form, fw: number, fd: number, hue: string, light: Light, lv: Levels, name = ''): Sprite {
  const T = toneOf(hue, light, lv.state)
  const run = lv.state === 'online'
  const tall = form === 'station' ? 96 : form === 'plant' ? 84 : form === 'tower' ? 70 : 52
  const g = new Draw(fw, fd, tall + 24)
  const i = 0.24, x0 = i, y0 = i, w = fw - 2 * i, d = fd - 2 * i
  const disks = lv.disks.length ? lv.disks : [null]
  g.shadow(x0, y0, w, d, 0.4)
  const water = light === 'night' ? '#3f7fd6' : '#5aa9f0', fuel = '#c98a3a'
  if (form === 'plant') {
    powerPlant(g, x0, y0, w, d, T, lv, hue, light, name, 'linux')
    return g.done()
  }

  if (form === 'station') {
    // The turbine hall at the back, its windows lit by how busy the CPU is; the cooling tower steams when it runs.
    const hw = w * 0.55, hd = d * 0.62, H = 32
    g.box(x0, y0, hw, hd, H, T.wall, 0, 1.4)
    g.poly(T.roof, [[x0, y0, H], [x0 + hw, y0, H], [x0 + hw, y0 + hd, H], [x0, y0 + hd, H]], 1)
    windowStrip(g, 'left', x0, y0 + hd, hw, 8, H - 6, Math.max(4, Math.round(hw * 2.4)), run ? lv.cpu : null, T)
    windowStrip(g, 'right', x0 + hw, y0, hd, 8, H - 6, Math.max(3, Math.round(hd * 1.4)), run ? lv.cpu : null, T)
    // Memory: the cooling water; disk: the fuel. Tanks in the yard in front of the hall.
    const r = Math.min(13, 6 + Math.min(w, d) * 1.6)
    tank(g, x0 + hw * 0.3, y0 + d * 0.84, r, 26, lv.mem, water)
    tank(g, x0 + hw * 0.75, y0 + d * 0.84, r, 26, disks[0], fuel)
    const cx = x0 + hw + (w - hw) / 2, cy = y0 + d * 0.35, R = Math.min(30, 10 + Math.min(w, d) * 4)
    g.cylinder(cx, cy, R, 0, 70, T.metal.left, T.metal.right, T.metal.top)
    g.cylinder(cx, cy, R * 0.82, 70, 72, '#9aa0b0', '#8a90a0', '#6a7080')
    smoke(g, cx, cy, 72, lv.cpu ?? (run ? 0.6 : null), run)
    beacon(g, cx, cy, 72, lv.state)
    tank(g, cx, y0 + d * 0.84, r, 26, disks[1] ?? null, fuel)
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

/**
 * A power plant (the Owner, 2026-11-19: a Linux node is a power plant; built on the first generation's
 * machine room). Every Linux node is drawn by this one function from the same readings.
 *   the turbine hall   in its system's colours (Linux orange) with the district's band under the eaves;
 *                      its name on the sign; its row of tall windows lit as busy as it is (CPU)
 *   the side wall      the radiator grille, and the system's mark on a plaque
 *   the stacks         smoke by how busy (CPU); none when it is not running; a red beacon when down
 *   the yard           the cooling water tank (memory) and the fuel tank (disk), filled to their levels
 * Standby: cold, dark windows, no smoke. Unread: grey, every gauge empty.
 */
function powerPlant(g: Draw, x0: number, y0: number, w: number, d: number, T: Tone, lv: Levels, hue: string, light: Light, name: string, os: MachineOs) {
  const run = lv.state === 'online', night = light === 'night'
  const base = lv.state === 'unread' ? OS_FACES.unknown : OS_FACES[os]
  const C = night ? faces(shade(base.top, -0.45), shade(base.left, -0.5), shade(base.right, -0.55)) : base
  const band = night ? shade(hue, -0.35) : hue
  // The plant's own ground: a concrete slab.
  g.box(x0, y0, w, d, 4, faces(night ? '#4a4d58' : '#e9e6dd', night ? '#3e414c' : '#d2cec2', night ? '#34363f' : '#c1bcae'), 0, 1)
  const Z = 4, H = 48
  const hx = x0 + 0.18, hy = y0 + 0.18, hw = w * 0.6, hd = d * 0.56
  g.shadow(hx, hy, hw, hd, 0.35)
  g.box(hx, hy, hw, hd, H, C, Z, 1.4)
  // The band under the eaves, in the district's hue; the name on its sign; the windows (CPU).
  g.onLeft(hx, hy + hd, 0, hw, Z + H - 6, Z + H - 1, band, 0.8)
  g.onRight(hx + hw, hy, 0, hd, Z + H - 6, Z + H - 1, band, 0.8)
  if (name) signBand(g, 'left', hx, hy + hd, hw, Z + H - 8, name)
  windowStrip(g, 'left', hx, hy + hd, hw, Z + 8, Z + H - 22, 8, run ? lv.cpu : null, T)
  // The side wall: the radiator grille low, the system's mark above it.
  grille(g, 'right', hx + hw, hy + hd, hd, Z + 4, Z + 16)
  plaque(g, 'right', hx + hw, hy + hd, hd / 2, Z + H - 20, os, 8)
  // A parapet round the roof.
  g.poly(shade(C.top, -0.06), [[hx + 0.06, hy + 0.06, Z + H + 3], [hx + hw - 0.06, hy + 0.06, Z + H + 3], [hx + hw - 0.06, hy + hd - 0.06, Z + H + 3], [hx + 0.06, hy + hd - 0.06, Z + H + 3]], 0.8)
  // The stacks at the back of the roof.
  for (const k of [0.3, 0.68]) {
    const sx = hx + hw * k, sy = hy + hd * 0.28, top = Z + H + 44
    g.cylinder(sx, sy, 5.5, Z + H, top, '#9aa3b0', '#6d7584', '#59606b')
    g.cylinder(sx, sy, 5.9, top - 10, top - 5, '#d9534f', '#b84440')
    smoke(g, sx, sy, top, run ? lv.cpu : null, run)
    if (k === 0.3) beacon(g, sx, sy, top, lv.state)
  }
  // The yard: the cooling water (memory) and the fuel (disk).
  tank(g, x0 + w * 0.3, y0 + d * 0.84, 15, 34, run ? lv.mem : null, night ? '#3f7fd6' : '#5aa9f0')
  tank(g, x0 + w * 0.84, y0 + d * 0.42, 15, 34, run ? lv.disks[0] ?? null : null, '#c98a3a')
}

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
  const bw = w - 0.6, fx = x0 + bw
  dock(g, fx, y0, 0.55, d, light)
  // Halls share the park's length by how many disks each volume has, the biggest at the back; the gatehouse keeps the front.
  const gate = Math.min(1.1, d * 0.15), park = d - gate - 0.2
  const sizes = vols.map(v => Math.max(1, v.units.length)), total = sizes.reduce((a, b) => a + b, 0)
  let y = y0
  vols.forEach((v, k) => {
    const len = park * sizes[k] / total - 0.15, H = k === 0 ? 54 : 38
    const { mid, ridge } = hall(g, x0, y, bw, len, H, T, hue, light)
    doorRow(g, fx, y, len, H, v.units, v.f, run, T)
    if (k === 0) beacon(g, mid, y + len / 2, ridge, lv.state)
    y += len + 0.15
  })
  const gy = y0 + d - gate
  gatehouse(g, x0 + bw * 0.35, gy, bw * 0.65, gate, T, run ? lv.cpu : null)
  tank(g, x0 + bw * 0.16, gy + gate * 0.55, 8, 22, run ? lv.mem : null, light === 'night' ? '#3f7fd6' : '#5aa9f0')
}

/** The key a facility's picture is kept under: its form, size, hue, light, state and every level to a tenth. */
export const deviceKey = (form: Form, fw: number, fd: number, hue: string, light: Light, lv: Levels, name = '') => {
  const q = (f: number | null) => f == null ? 'n' : String(Math.round(f * 10))
  return `g2:fac:${form}:${name}:${fw}x${fd}:${hue}:${light}:${lv.state}:${q(lv.cpu)}:${q(lv.mem)}:${lv.disks.map(q).join('.')}:${(lv.vols ?? []).map(v => `${q(v.f)}${v.units.join('')}`).join('|')}`
}
