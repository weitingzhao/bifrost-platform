import { Draw, faces, INK, type Sprite } from './kit'

/** Face units to a cell (see Draw.face). */
const U = 35.8

/** Each system's colour, for the band under a facility's eaves: Ubuntu's orange, a Mac's silver-white, Windows' blue. */
const OS_BAND: Record<MachineOs, string> = { linux: '#e95420', mac: '#dfe3ea', win: '#0078d4', unknown: '#b9b6c4' }
import type { Light } from './light'
import type { Look, Place, State } from './places'
import { beacon, dock, doorRow, gatehouse, gauge, hall, lit, plaque, shade, smoke, tank, toneOf, windowStrip, type Tone } from './library/parts'
import type { MachineOs } from './library/landmarks'
import { encode, metaphor } from './library/dictionary'

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

export type Levels = {
  state: State; cpu: number | null; mem: number | null; disks: (number | null)[]; vols?: { f: number | null; units: State[] }[]
  /** How much it has (cores, memory bytes, disk bytes), for the sizes (dictionary: cores, mem.total, disk.total); null when not read. */
  cap?: { cores: number | null; mem: number | null; disk: number | null }
}
export type Form = 'plant' | 'station' | 'warehouse' | 'exchange' | 'dispatch' | 'office' | 'tower'

/** A device's levels, read off its rooms' meters (the same for its picture and its key). */
export function levelsOf(k: Place): Levels {
  const frac = (r?: Place) => r?.meter && r.meter.used != null && r.meter.of ? r.meter.used / r.meter.of : null
  const room = (id: string) => k.children.find(c => c.id === id)
  const vols = k.children.filter(c => c.id.startsWith('volume-'))
  return { state: k.state ?? 'unread', cpu: frac(room('cpu')), mem: frac(room('memory')), disks: k.children.filter(c => c.id === 'disk' || c.id.startsWith('volume-')).map(frac),
    vols: vols.length ? vols.map(v => ({ f: frac(v), units: v.units ?? [] })) : undefined,
    cap: { cores: room('cpu')?.meter?.of ?? null, mem: room('memory')?.meter?.of ?? null, disk: room('disk')?.meter?.of ?? null } }
}

/** A device's facility form, from its look and rooms. */
export const formOfPlace = (k: Place): Form => formOf(k.look ?? 'network', k.children.some(c => c.id === 'gpu'), /book/i.test(k.label))

export function formOf(look: Look, gpu: boolean, laptop: boolean): Form {
  if (look === 'linux') return gpu ? 'station' : 'plant'
  if (look === 'mac') return laptop ? 'office' : 'dispatch'
  if (look === 'storage') return 'warehouse'
  if (look === 'windows') return 'exchange'
  return 'tower'
}

export function deviceSprite(form: Form, fw: number, fd: number, hue: string, light: Light, lv: Levels, name = '', face: Place['face'] = {}): Sprite {
  const T = toneOf(hue, light, lv.state)
  const run = lv.state === 'online'
  const tall = form === 'station' ? 96 : form === 'plant' ? 84 : form === 'tower' ? 70 : 52
  const g = new Draw(fw, fd, tall + 24)
  const i = 0.24, x0 = i, y0 = i, w = fw - 2 * i, d = fd - 2 * i
  const disks = lv.disks.length ? lv.disks : [null]
  g.shadow(x0, y0, w, d, 0.4)
  const water = light === 'night' ? '#3f7fd6' : '#5aa9f0', fuel = '#c98a3a'
  if (form === 'plant') {
    powerPlant(g, x0, y0, w, d, T, lv, hue, light, name, 'linux', face)
    return g.done()
  }

  if (form === 'station') {
    // The turbine hall at the back, as tall as it has cores, its windows lit by how busy the CPU is; the cooling tower steams when it runs.
    const hw = w * 0.55, hd = d * 0.62, H = encode('cores', lv.cap?.cores ?? null)!
    g.box(x0, y0, hw, hd, H, T.wall, 0, 1.4)
    g.poly(T.roof, [[x0, y0, H], [x0 + hw, y0, H], [x0 + hw, y0 + hd, H], [x0, y0 + hd, H]], 1)
    const n = (metaphor('cpu.busy').encoding as { n: number }).n
    windowStrip(g, 'left', x0, y0 + hd, hw, 8, H - 6, n, run ? lv.cpu : null, T)
    windowStrip(g, 'right', x0 + hw, y0, hd, 8, H - 6, n, run ? lv.cpu : null, T)
    // Memory: the cooling water; disk: the fuel. Each tank as big as what it holds, filled to its level.
    const r = encode('mem.total', lv.cap?.mem ?? null)!, rf = encode('disk.total', lv.cap?.disk ?? null)!
    tank(g, x0 + hw * 0.3, y0 + d * 0.84, r, 26, run ? encode('mem.used', lv.mem) : null, water)
    tank(g, x0 + hw * 0.75, y0 + d * 0.84, rf, 26, run ? encode('disk.used', disks[0]) : null, fuel)
    const cx = x0 + hw + (w - hw) / 2, cy = y0 + d * 0.35, R = Math.min(30, 10 + Math.min(w, d) * 4)
    g.cylinder(cx, cy, R, 0, 70, T.metal.left, T.metal.right, T.metal.top)
    g.cylinder(cx, cy, R * 0.82, 70, 72, '#9aa0b0', '#8a90a0', '#6a7080')
    smoke(g, cx, cy, 72, lv.cpu ?? (run ? 0.6 : null), run)
    beacon(g, cx, cy, 72, lv.state)
    tank(g, cx, y0 + d * 0.84, rf, 26, run ? encode('disk.used', disks[1] ?? null) : null, fuel)
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
 * A power plant (the Owner, 2026-11-19: a Linux node is a power plant). Redrawn 2026-11-20 to the drawing
 * floor (RULES.md「画法」; the Owner: the orange box with two pipes and two jars looked like a toy): a typical
 * thermal plant in the first generation's palette, off-white with one band in its system's colour.
 *   the turbine hall   as tall as it has cores; its row of tall windows lit as busy as it is (CPU);
 *                      a roller door, louvres and the system's small mark on the side
 *   the chimney        red and white, smoke by how busy (CPU); none when it is not running; a red light when down
 *   the tanks          steel, in one row before the hall, memory then disk, each as big as what it
 *                      holds, its name stencilled on it, its cap in its colour, a gauge up its front at its level
 * Its words (what it is for, the pods on it, its name) are on its name plate outside the drawing (the
 * Owner, 2026-11-20: the readings go outside the picture). Standby: dark windows, no smoke. Unread: grey,
 * every gauge empty.
 */
function powerPlant(g: Draw, x0: number, y0: number, w: number, d: number, _T: Tone, lv: Levels, hue: string, light: Light, _name: string, os: MachineOs, face: Place['face'] = {}) {
  const run = lv.state === 'online', night = light === 'night', unread = lv.state === 'unread'
  const S = (c: string, k: number) => night ? shade(c, k) : c
  const n1 = (v: number) => v.toFixed(1)
  // The words go on the name plate (placeScene); the dictionary still lists them for this facility's key.
  metaphor('identity')
  if (face?.sign) metaphor('purpose')
  if (face?.tally) metaphor('pods')
  // The plant's own ground: a concrete slab.
  g.box(x0, y0, w, d, 4, faces(S('#ebe8e0', -0.45), S('#d6d2c7', -0.5), S('#c4bfb2', -0.55)), 0, 1)
  const Z = 4, H = encode('cores', lv.cap?.cores ?? null)!
  const hx = x0 + 0.14, hy = y0 + 0.16, hw = w * 0.6, hd = d * 0.5
  const wall = unread ? faces('#eeedf2', '#d9d7e0', '#c6c3cf') : faces(S('#f7f8fa', -0.45), S('#e4e9ef', -0.5), S('#cdd5df', -0.55))
  // The band in the system's own colour (the Owner, 2026-11-20: Ubuntu is orange, a Mac white, Windows blue); the walls stay off-white.
  const band = unread ? '#b9b6c4' : S(OS_BAND[os], -0.35)
  void hue

  // The chimney behind the hall, from the ground: red and white, a gallery, the light on top.
  { const sx = hx + hw + 0.22, sy = hy + 0.2, top = Z + H + 62
    const [cx, cy] = g.p(sx, sy, Z), r = 6.5, bands = 6, step = (top - Z) / bands
    for (let i = 0; i < bands; i++) {
      const yb = cy - i * step, yt = yb - step
      g.add(`<rect x="${n1(cx - r)}" y="${n1(yt)}" width="${2 * r}" height="${n1(step)}" fill="${i % 2 ? S('#ffffff', -0.45) : unread ? '#c9c6d2' : S('#e0564f', -0.4)}" stroke="${INK}" stroke-width="1.1"/>`)
    }
    const ty = cy - (top - Z)
    g.add(`<rect x="${n1(cx - r - 2)}" y="${n1(ty + step * 1.15)}" width="${2 * r + 4}" height="2.6" fill="#59606b" stroke="${INK}" stroke-width="0.7"/>`)
    g.add(`<rect x="${n1(cx - r - 1.6)}" y="${n1(ty - 3)}" width="${2 * r + 3.2}" height="4" fill="#59606b" stroke="${INK}" stroke-width="1"/>`)
    smoke(g, sx, sy, top, run ? lv.cpu : null, run)
    beacon(g, sx, sy, top, lv.state) }

  // The turbine hall.
  g.shadow(hx, hy, hw, hd, 0.35)
  g.box(hx, hy, hw, hd, H, wall, Z, 1.4)
  const W = hw * U, D = hd * U
  { // Front (+y): the band under the eaves, the tall windows (CPU), a plinth, the roller door.
    const n = (metaphor('cpu.busy').encoding as { n: number }).n, on = run ? lit(lv.cpu, n) : 0
    const door = Math.min(22, W * 0.22), span = W - door - 12, cw = span / n
    let f = `<rect x="0" y="${-H + 3}" width="${n1(W)}" height="5" fill="${band}" stroke="${INK}" stroke-width="0.8"/>`
    f += `<rect x="0" y="-5" width="${n1(W)}" height="5" fill="${S('#c3ccd8', -0.5)}" stroke="${INK}" stroke-width="0.6"/>`
    const wy = -H + 13, wh = H - 25
    for (let i = 0; i < n; i++) {
      const u = 4 + i * cw, ww = cw * 0.62
      const fill = i < on ? '#ffd76a' : unread ? '#dcdbe3' : night ? '#3a4158' : '#cfe3f7'
      f += `<rect x="${n1(u)}" y="${n1(wy)}" width="${n1(ww)}" height="${n1(wh)}" fill="${fill}" stroke="${INK}" stroke-width="0.7"/>`
      f += `<line x1="${n1(u + ww / 2)}" y1="${n1(wy)}" x2="${n1(u + ww / 2)}" y2="${n1(wy + wh)}" stroke="${INK}" stroke-width="0.4"/><line x1="${n1(u)}" y1="${n1(wy + wh * 0.5)}" x2="${n1(u + ww)}" y2="${n1(wy + wh * 0.5)}" stroke="${INK}" stroke-width="0.4"/>`
    }
    const dx = W - door - 4, dh = Math.min(22, H - 14)
    f += `<rect x="${n1(dx)}" y="${-dh}" width="${n1(door)}" height="${dh}" fill="${S('#aab4c3', -0.5)}" stroke="${INK}" stroke-width="0.9"/>`
    for (let v = -dh + 3; v < 0; v += 3) f += `<line x1="${n1(dx)}" y1="${v}" x2="${n1(dx + door)}" y2="${v}" stroke="${S('#7f8a9c', -0.4)}" stroke-width="0.5"/>`
    f += `<rect x="${n1(dx)}" y="${-dh - 2.5}" width="${n1(door)}" height="2.5" fill="#f2b234" stroke="${INK}" stroke-width="0.6"/>`
    g.face('left', hx, hy + hd, Z, f) }
  { // Side (+x): the band, louvres, the system's small mark.
    let r = `<rect x="0" y="${-H + 3}" width="${n1(D)}" height="5" fill="${band}" stroke="${INK}" stroke-width="0.8"/>`
    r += `<rect x="0" y="-5" width="${n1(D)}" height="5" fill="${S('#c3ccd8', -0.5)}" stroke="${INK}" stroke-width="0.6"/>`
    const lw = Math.min(16, D * 0.35), lx = 5, lh = Math.min(20, H - 24)
    r += `<rect x="${lx}" y="${-10 - lh}" width="${n1(lw)}" height="${n1(lh)}" fill="${S('#c3ccd8', -0.5)}" stroke="${INK}" stroke-width="0.8"/>`
    for (let v = -10 - lh + 3; v < -10; v += 3) r += `<line x1="${lx}" y1="${n1(v)}" x2="${n1(lx + lw)}" y2="${n1(v)}" stroke="${S('#eef1f5', -0.4)}" stroke-width="0.9"/>`
    g.face('right', hx + hw, hy + hd, Z, r)
    plaque(g, 'right', hx + hw, hy + hd, hd * 0.72, Z + H - 18, os, 6) }
  // The roof: a parapet, a skylight, the air handlers.
  g.poly(S('#eef1f5', -0.45), [[hx + 0.07, hy + 0.07, Z + H], [hx + hw - 0.07, hy + 0.07, Z + H], [hx + hw - 0.07, hy + hd - 0.07, Z + H], [hx + 0.07, hy + hd - 0.07, Z + H]], 0.8, S('#9aa6b6', -0.3))
  g.poly(night ? '#3a4158' : '#bfe0f7', [[hx + 0.2, hy + hd * 0.42, Z + H], [hx + hw * 0.55, hy + hd * 0.42, Z + H], [hx + hw * 0.55, hy + hd * 0.62, Z + H], [hx + 0.2, hy + hd * 0.62, Z + H]], 0.8)
  g.unit(hx + hw * 0.66, hy + hd * 0.2, Z + H, Math.min(0.42, hd * 0.3))
  g.unit(hx + hw * 0.3, hy + hd * 0.12, Z + H, Math.min(0.32, hd * 0.24))

  // The yard: two steel tanks in one row before the hall, always memory then disk, each as big as what it
  // holds; its name stencilled on it, its cap in its colour, a wide gauge up its front filled to its level.
  const steel = (tx: number, ty: number, r: number, h: number, f: number | null, liquid: string, word: string) => {
    g.cylinder(tx, ty, r, Z, Z + h, S('#f4f6f9', -0.45), S('#aab4c1', -0.45), unread ? '#d9d7e0' : S(liquid, -0.3))
    const [cx, cy] = g.p(tx, ty, Z)
    // A ring round the shell near the top.
    g.add(`<path d="M${n1(cx - r)},${n1(cy - h + 6)} A${r},${r / 2} 0 0 0 ${n1(cx + r)},${n1(cy - h + 6)}" fill="none" stroke="${S('#c3ccd8', -0.4)}" stroke-width="0.9"/>`)
    // The gauge on the front of the shell, ticks at a quarter, a half, three quarters, and what is in it.
    const gx = cx - r * 0.55, base = cy + r / 2 - 4, gh = h - 12, gw = 6
    g.add(`<rect x="${n1(gx - gw / 2)}" y="${n1(base - gh)}" width="${gw}" height="${n1(gh)}" rx="1.5" fill="${night ? '#2a3044' : '#ffffff'}" stroke="${INK}" stroke-width="0.8"/>`)
    if (f != null && f > 0.005) { const lh = Math.max(1.5, (gh - 1.6) * Math.min(1, f)); g.add(`<rect x="${n1(gx - gw / 2 + 0.8)}" y="${n1(base - 0.8 - lh)}" width="${gw - 1.6}" height="${n1(lh)}" rx="1" fill="${liquid}"/>`) }
    for (const q of [0.25, 0.5, 0.75]) g.add(`<line x1="${n1(gx + gw / 2)}" y1="${n1(base - gh * q)}" x2="${n1(gx + gw / 2 + 2.5)}" y2="${n1(base - gh * q)}" stroke="${INK}" stroke-width="0.6"/>`)
    // The stencil, across the shell beside the gauge.
    const sx = cx + r * 0.28, fs = Math.min(6.4, r * 0.4)
    g.add(`<text x="${n1(sx)}" y="${n1(base - gh * 0.55)}" text-anchor="middle" font-family="'JetBrains Mono',ui-monospace,Menlo,monospace" font-size="${n1(fs)}" font-weight="800" fill="${unread ? '#a4a8b2' : S(liquid, -0.3)}">${word}</text>`)
  }
  const mr = encode('mem.total', lv.cap?.mem ?? null)!, dr = encode('disk.total', lv.cap?.disk ?? null)!
  const ty = hy + hd + (y0 + d - hy - hd) * 0.55
  // A pipe from each tank back to the hall.
  for (const k of [0.28, 0.6]) g.line([x0 + w * k, ty, Z + 10], [x0 + w * k, hy + hd, Z + 10], S('#8d97a6', -0.3), 2.2)
  steel(x0 + w * 0.28, ty, mr, 34, run ? encode('mem.used', lv.mem) : null, night ? '#3f7fd6' : '#3b8fe0', 'MEM')
  steel(x0 + w * 0.6, ty, dr, 34, run ? encode('disk.used', lv.disks[0] ?? null) : null, '#d98f2b', 'DISK')
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
  tank(g, x0 + bw * 0.16, gy + gate * 0.55, 8, 22, run ? encode('mem.used', lv.mem) : null, light === 'night' ? '#3f7fd6' : '#5aa9f0')
}

/** The key a facility's picture is kept under: its form, size, hue, light, state and every level to a tenth. */
export const deviceKey = (form: Form, fw: number, fd: number, hue: string, light: Light, lv: Levels, name = '', face: Place['face'] = {}) => {
  const q = (f: number | null) => f == null ? 'n' : String(Math.round(f * 10))
  const c = (v: number | null | undefined) => v == null ? 'n' : String(Math.round(Math.log2(v) * 4))
  return `g2:fac:${form}:${name}:${c(lv.cap?.cores)}.${c(lv.cap?.mem)}.${c(lv.cap?.disk)}:${face?.roof ?? ''}:${face?.sign ?? ''}:${face?.tally?.n ?? ''}:${fw}x${fd}:${hue}:${light}:${lv.state}:${q(lv.cpu)}:${q(lv.mem)}:${lv.disks.map(q).join('.')}:${(lv.vols ?? []).map(v => `${q(v.f)}${v.units.join('')}`).join('|')}`
}
