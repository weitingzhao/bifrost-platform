import { Draw, faces, hash, INK, type Sprite } from './kit'

/** Face units to a cell (see Draw.face). */
const U = 35.8

/** Each system's colour, for the band under a facility's eaves: Ubuntu's orange, a Mac's silver-white, Windows' blue. */
const OS_BAND: Record<MachineOs, string> = { linux: '#e95420', mac: '#dfe3ea', win: '#0078d4', unknown: '#b9b6c4' }
import type { Light } from './light'
import type { Look, Place, State } from './places'
import { beacon, dock, doorRow, gatehouse, hall, lit, plaque, shade, smoke, tank, toneOf, type Tone } from './library/parts'
import { coolingTower, type MachineOs } from './library/landmarks'
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
  const T0 = toneOf(hue, light, lv.state)
  // Every facility here stands in off-white with a grey roof (2026-11-20, the drawing floor: the hue tinted
  // whole buildings); colour is kept for bands and marks.
  const T: Tone = lv.state === 'unread' ? T0 : { ...T0, wall: light === 'night' ? faces(shade('#f7f8fa', -0.45), shade('#e4e9ef', -0.5), shade('#cdd5df', -0.55)) : faces('#f7f8fa', '#e4e9ef', '#cdd5df'), roof: light === 'night' ? shade('#d3dae3', -0.4) : '#d3dae3' }
  const run = lv.state === 'online'
  const tall = form === 'station' ? 96 : form === 'plant' ? 84 : form === 'tower' ? 70 : 52
  const g = new Draw(fw, fd, tall + 24)
  const i = 0.24, x0 = i, y0 = i, w = fw - 2 * i, d = fd - 2 * i
  const disks = lv.disks.length ? lv.disks : [null]
  g.shadow(x0, y0, w, d, 0.4)
  if (form === 'plant') {
    powerPlant(g, x0, y0, w, d, T, lv, hue, light, name, 'linux', face)
    return g.done()
  }

  if (form === 'station') {
    powerPlant(g, x0, y0, w, d, T, lv, hue, light, name, 'linux', face, 'station')
    return g.done()
  }

  if (form === 'warehouse') {
    // With no arrays read, its disks stand as one volume (each disk a door, in the box's own state).
    const vols = lv.vols && lv.vols.length > 0 ? lv.vols : [{ f: disks[0] ?? null, units: disks.map(() => lv.state) }]
    warehousePark(g, x0, y0, w, d, T, { ...lv, vols }, hue, light)
    return g.done()
  }

  if (form === 'exchange') {
    exchange(g, x0, y0, w, d, lv, light)
    return g.done()
  }

  if (form === 'dispatch' || form === 'office') {
    macHouse(g, x0, y0, w, d, lv, light, form === 'office')
    return g.done()
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
function powerPlant(g: Draw, x0: number, y0: number, w: number, d: number, _T: Tone, lv: Levels, hue: string, light: Light, _name: string, os: MachineOs, face: Place['face'] = {}, kind: 'plant' | 'station' = 'plant') {
  const run = lv.state === 'online', night = light === 'night', unread = lv.state === 'unread'
  const S = (c: string, k: number) => night ? shade(c, k) : c
  const n1 = (v: number) => v.toFixed(1)
  // The words go on the name plate (placeScene); the dictionary still lists them for this facility's key.
  metaphor('identity')
  // Its gauges are in its key whatever its state: dark when it does not run, never left out.
  metaphor('cpu.smoke'); metaphor('mem.used'); metaphor('disk.used')
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
  if (kind === 'plant') { const sx = hx + hw + 0.22, sy = hy + 0.2, top = Z + H + 62
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
  // The GPU box is a power station: a cooling tower beside the hall (drawn after it: it stands in front of its side), steaming as busy as it is (CPU), its light on top when down.
  if (kind === 'station') {
    const tx = hx + hw + (x0 + w - hx - hw) / 2, ty = hy + hd * 0.55, th = H + 56
    coolingTower(g, tx, ty, th, `ct-${hash(_name) % 9973}`, Math.min(1.15, (x0 + w - hx - hw) * 0.75), false)
    smoke(g, tx, ty, Z + th, run ? lv.cpu : null, run)
    beacon(g, tx, ty, Z + th, lv.state)
  }

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
  // Memory first, then each disk it has (none read: one empty disk tank), evenly along the row.
  const tanks = [
    { r: encode('mem.total', lv.cap?.mem ?? null)!, f: run ? encode('mem.used', lv.mem) : null, c: night ? '#3f7fd6' : '#3b8fe0', word: 'MEM' },
    ...(lv.disks.length ? lv.disks : [null]).map((f, k, all) => ({ r: encode('disk.total', lv.cap?.disk ?? null)!, f: run ? encode('disk.used', f) : null, c: '#d98f2b', word: all.length > 1 ? `DISK${k + 1}` : 'DISK' })),
  ]
  const ty = hy + hd + (y0 + d - hy - hd) * 0.55
  tanks.forEach((t, k) => {
    const tx = x0 + w * (0.28 + k * Math.min(0.32, 0.55 / Math.max(1, tanks.length - 1)))
    g.line([tx, ty, Z + 10], [tx, hy + hd, Z + 10], S('#8d97a6', -0.3), 2.2)
    steel(tx, ty, t.r, 34, t.f, t.c, t.word)
  })
}

/** Shared by the facilities below: a colour darkened at night, a number to one place. */
const nightly = (light: Light) => (c: string, k: number) => light === 'night' ? shade(c, k) : c
const f1 = (v: number) => v.toFixed(1)

/**
 * The exchange (a Windows host running TWS: the gateway to the market), redrawn 2026-11-20 to the drawing
 * floor: a stone hall behind a portico of six columns under a pediment, steps up to it, the system's blue on
 * the entablature, its windows and the ticker board over the door lit while it is read (dark when not).
 */
function exchange(g: Draw, x0: number, y0: number, w: number, d: number, lv: Levels, light: Light) {
  const run = lv.state === 'online', unread = lv.state === 'unread', S = nightly(light)
  const stone = faces(S('#f8f1e1', -0.45), S('#eee2ca', -0.5), S('#dccdb0', -0.55))
  const blue = unread ? '#b9b6c4' : S(OS_BAND.win, -0.35)
  const glass = run ? '#ffd76a' : light === 'night' ? '#3a4158' : '#cfe3f7'
  // The plinth and the steps across the front.
  g.box(x0, y0, w, d, 5, faces(S('#ebe4d2', -0.45), S('#d9cfb8', -0.5), S('#c8bc9f', -0.55)), 0, 1)
  for (let k = 0; k < 3; k++) g.box(x0 + w * 0.12 + k * 0.05, y0 + d - 0.42 + k * 0.12, w * 0.76 - k * 0.1, 0.12, 5 - k * 1.6, stone, 0, 0.6)
  // The hall.
  const bx = x0 + 0.15, by = y0 + 0.18, bw = w - 0.3, bd = d * 0.6, H = 46
  g.shadow(bx, by, bw, bd, 0.35)
  g.box(bx, by, bw, bd, H, stone, 5, 1.4)
  const W = bw * U, D = bd * U
  let f = `<rect x="0" y="${-H + 2}" width="${f1(W)}" height="6" fill="${S('#e6dbc2', -0.5)}" stroke="${INK}" stroke-width="0.7"/>`
  // The ticker board over the door and the door.
  f += `<rect x="${f1(W * 0.2)}" y="${-H + 12}" width="${f1(W * 0.6)}" height="8" rx="1" fill="#1d2230" stroke="${INK}" stroke-width="0.9"/>`
  if (run) for (let k = 0; k < 7; k++) f += `<rect x="${f1(W * 0.23 + k * W * 0.078)}" y="${-H + 14.5}" width="${f1(W * 0.05)}" height="3" fill="${k % 3 === 1 ? '#f29b8f' : '#7be08f'}"/>`
  f += `<rect x="${f1(W / 2 - 7)}" y="-20" width="14" height="20" fill="${S('#8a6a48', -0.4)}" stroke="${INK}" stroke-width="0.9"/><line x1="${f1(W / 2)}" y1="-20" x2="${f1(W / 2)}" y2="0" stroke="${INK}" stroke-width="0.6"/>`
  for (const u of [0.1, 0.72]) f += `<rect x="${f1(W * u)}" y="${-H + 24}" width="${f1(W * 0.16)}" height="16" fill="${glass}" stroke="${INK}" stroke-width="0.7"/><line x1="${f1(W * (u + 0.08))}" y1="${-H + 24}" x2="${f1(W * (u + 0.08))}" y2="${-H + 40}" stroke="${INK}" stroke-width="0.4"/>`
  g.face('left', bx, by + bd, 5, f)
  let r = `<rect x="0" y="${-H + 2}" width="${f1(D)}" height="6" fill="${S('#e6dbc2', -0.5)}" stroke="${INK}" stroke-width="0.7"/>`
  const n = Math.max(2, Math.floor(D / 14))
  for (let k = 0; k < n; k++) { const u = 5 + k * (D - 10) / n; r += `<rect x="${f1(u)}" y="${-H + 14}" width="${f1((D - 10) / n - 5)}" height="22" rx="${f1(((D - 10) / n - 5) / 2)}" fill="${glass}" stroke="${INK}" stroke-width="0.7"/>` }
  g.face('right', bx + bw, by + bd, 5, r)
  plaque(g, 'right', bx + bw, by + bd, bd * 0.85, 5 + H - 30, 'win', 5)
  // The roof: a parapet and an air handler.
  g.poly(S('#efe6d2', -0.45), [[bx + 0.07, by + 0.07, 5 + H], [bx + bw - 0.07, by + 0.07, 5 + H], [bx + bw - 0.07, by + bd - 0.07, 5 + H], [bx + 0.07, by + bd - 0.07, 5 + H]], 0.8)
  g.unit(bx + bw * 0.62, by + bd * 0.2, 5 + H, Math.min(0.4, bd * 0.3))
  // The portico: six columns, the entablature in the system's blue, the pediment.
  const py = by + bd + 0.42, pz = 5, ph = H - 10, px0 = bx + bw * 0.1, px1 = bx + bw * 0.9
  for (let k = 0; k < 6; k++) {
    const cx = px0 + (px1 - px0) * k / 5
    g.cylinder(cx, py - 0.08, 3.4, pz, pz + ph, S('#fbf7ee', -0.45), S('#d6c9ad', -0.5), S('#efe6d2', -0.45))
  }
  g.box(px0 - 0.12, by + bd, px1 - px0 + 0.24, 0.5, 7, faces(S('#f3ead6', -0.45), blue, shade(blue, -0.15)), pz + ph, 1)
  const ez = pz + ph + 7
  g.poly(S('#f8f1e1', -0.45), [[px0 - 0.12, py + 0.08, ez], [px1 + 0.12, py + 0.08, ez], [(px0 + px1) / 2, py + 0.08, ez + 16]], 1.2)
  g.poly(S('#e9ddc2', -0.5), [[px0 + 0.15, py + 0.08, ez + 2], [px1 - 0.15, py + 0.08, ez + 2], [(px0 + px1) / 2, py + 0.08, ez + 12]], 0.6)
  g.poly(S('#dccdb0', -0.55), [[px1 + 0.12, by + bd, ez], [px1 + 0.12, py + 0.08, ez], [(px0 + px1) / 2, py + 0.08, ez + 16], [(px0 + px1) / 2, by + bd, ez + 16]], 1)
  beacon(g, (px0 + px1) / 2, py, ez + 16, lv.state)
}

/**
 * A Mac (the Owner, 2026-11-20: a Mac is white), redrawn to the drawing floor. A Mac mini is the dispatch
 * centre: a white pavilion, a glass curtain wall across its front, a glass control room on the roof; a
 * laptop is a small white office under a pitched roof. Their glass is lit while they are read, dark when not.
 */
function macHouse(g: Draw, x0: number, y0: number, w: number, d: number, lv: Levels, light: Light, laptop: boolean) {
  const run = lv.state === 'online', unread = lv.state === 'unread', S = nightly(light)
  const white = unread ? faces('#eeedf2', '#d9d7e0', '#c6c3cf') : faces(S('#ffffff', -0.45), S('#eef1f5', -0.5), S('#d8dee6', -0.55))
  const silver = unread ? '#b9b6c4' : S('#9aa3ae', -0.3)
  const glass = run ? (light === 'night' ? '#ffd76a' : '#fff0c2') : light === 'night' ? '#3a4158' : '#cfe3f7'
  g.box(x0, y0, w, d, 3, faces(S('#ebe8e0', -0.45), S('#d6d2c7', -0.5), S('#c4bfb2', -0.55)), 0, 1)
  const bx = x0 + w * 0.14, by = y0 + d * 0.14, bw = w * 0.72, bd = d * 0.66, H = laptop ? 22 : 26, Z = 3
  g.shadow(bx, by, bw, bd, 0.3)
  g.box(bx, by, bw, bd, H, white, Z, 1.3)
  const W = bw * U, D = bd * U
  let f = `<rect x="0" y="-4" width="${f1(W)}" height="4" fill="${silver}" stroke="${INK}" stroke-width="0.6"/>`
  if (laptop) {
    for (const u of [0.1, 0.4, 0.7]) f += `<rect x="${f1(W * u)}" y="${-H + 6}" width="${f1(W * 0.2)}" height="10" fill="${glass}" stroke="${INK}" stroke-width="0.7"/><line x1="${f1(W * (u + 0.1))}" y1="${-H + 6}" x2="${f1(W * (u + 0.1))}" y2="${-H + 16}" stroke="${INK}" stroke-width="0.4"/>`
  } else {
    f += `<rect x="4" y="${-H + 4}" width="${f1(W - 8)}" height="${H - 9}" fill="${glass}" stroke="${INK}" stroke-width="0.9"/>`
    for (let u = 4 + (W - 8) / 5; u < W - 5; u += (W - 8) / 5) f += `<line x1="${f1(u)}" y1="${-H + 4}" x2="${f1(u)}" y2="-5" stroke="${INK}" stroke-width="0.6"/>`
    f += `<line x1="4" y1="${f1(-H / 2)}" x2="${f1(W - 4)}" y2="${f1(-H / 2)}" stroke="${INK}" stroke-width="0.5"/>`
    f += `<path d="M${f1(W * 0.18)},${-H + 5} l10,0 l-${f1(H * 0.5)},${f1(H - 11)} l-10,0 Z" fill="#ffffff" opacity="0.35"/>`
  }
  f += `<rect x="${f1(W * 0.45)}" y="-12" width="9" height="12" fill="${S('#59606b', -0.3)}" stroke="${INK}" stroke-width="0.8"/>`
  g.face('left', bx, by + bd, Z, f)
  g.face('right', bx + bw, by + bd, Z, `<rect x="0" y="-4" width="${f1(D)}" height="4" fill="${silver}" stroke="${INK}" stroke-width="0.6"/><rect x="${f1(D * 0.15)}" y="${-H + 6}" width="${f1(D * 0.3)}" height="9" fill="${glass}" stroke="${INK}" stroke-width="0.7"/>`)
  plaque(g, 'right', bx + bw, by + bd, bd * 0.72, Z + H - 10, 'mac', 5)
  if (laptop) {
    const mid = by + bd / 2
    g.poly(S('#e3e8ef', -0.45), [[bx, by, Z + H], [bx + bw, by, Z + H], [bx + bw, mid, Z + H + 12], [bx, mid, Z + H + 12]], 1)
    g.poly(S('#c9d1db', -0.5), [[bx, mid, Z + H + 12], [bx + bw, mid, Z + H + 12], [bx + bw, by + bd, Z + H], [bx, by + bd, Z + H]], 1)
    g.poly(white.right, [[bx + bw, by, Z + H], [bx + bw, by + bd, Z + H], [bx + bw, mid, Z + H + 12]], 1)
    beacon(g, bx + bw / 2, mid, Z + H + 12, lv.state)
    return
  }
  g.poly(S('#f4f6f9', -0.45), [[bx + 0.06, by + 0.06, Z + H], [bx + bw - 0.06, by + 0.06, Z + H], [bx + bw - 0.06, by + bd - 0.06, Z + H], [bx + 0.06, by + bd - 0.06, Z + H]], 0.8)
  // The control room on the roof, all glass.
  const cx = bx + bw * 0.3, cy = by + bd * 0.25, cw = bw * 0.45, cd = bd * 0.5
  g.box(cx, cy, cw, cd, 12, faces(S('#f4f6f9', -0.45), glass, shade(glass, -0.12)), Z + H, 1.1)
  const mull = (span: number) => { let m = ''; for (let u = span / 4; u < span - 1; u += span / 4) m += `<line x1="${f1(u)}" y1="-12" x2="${f1(u)}" y2="0" stroke="${INK}" stroke-width="0.6"/>`; return m + `<line x1="0" y1="-2" x2="${f1(span)}" y2="-2" stroke="${INK}" stroke-width="0.5"/>` }
  g.face('left', cx, cy + cd, Z + H, mull(cw * U))
  g.face('right', cx + cw, cy + cd, Z + H, mull(cd * U))
  g.poly(silver, [[cx, cy + cd, Z + H + 12], [cx + cw, cy + cd, Z + H + 12], [cx + cw, cy + cd, Z + H + 10], [cx, cy + cd, Z + H + 10]], 0.6)
  beacon(g, cx + cw / 2, cy + cd / 2, Z + H + 12, lv.state)
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
