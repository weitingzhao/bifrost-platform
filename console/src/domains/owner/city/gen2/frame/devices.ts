import { Draw, faces, hash, INK, type Sprite } from './kit'

/** Face units to a cell (see Draw.face). */
const U = 35.8

/** Each system's colour, for the band under a facility's eaves: Ubuntu's orange, a Mac's aluminium silver, Windows' blue, anything else (the NAS) slate. */
const OS_BAND: Record<MachineOs, string> = { linux: '#e95420', mac: '#a9b1bc', win: '#0078d4', unknown: '#7d8796' }
import type { Light } from './light'
import type { Look, Place, State } from './places'
import { beacon, lit, plaque, shade, smoke, toneOf, UNIT, type Tone } from './library/parts'
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
export type Form = 'plant' | 'station' | 'tower'

/** A device's levels, read off its rooms' meters (the same for its picture and its key). */
export function levelsOf(k: Place): Levels {
  const frac = (r?: Place) => r?.meter && r.meter.used != null && r.meter.of ? r.meter.used / r.meter.of : null
  const room = (id: string) => k.children.find(c => c.id === id)
  const vols = k.children.filter(c => c.id.startsWith('volume-'))
  return { state: k.state ?? 'unread', cpu: frac(room('cpu')), mem: frac(room('memory')), disks: k.children.filter(c => c.id === 'disk' || c.id.startsWith('volume-')).map(frac),
    vols: vols.length ? vols.map(v => ({ f: frac(v), units: v.units ?? [] })) : undefined,
    cap: { cores: room('cpu')?.meter?.of ?? null, mem: room('memory')?.meter?.of ?? null, disk: room('disk')?.meter?.of ?? null } }
}

/**
 * A device's facility form, from its look and rooms (the Owner, 2026-11-20: every computer is a compute
 * node, one kind of thing, so one kind of building: Linux, Mac, Windows, a laptop, the NAS all stand as the
 * same power plant; only their system's style differs, and a GPU box adds its cooling tower).
 */
export const formOfPlace = (k: Place): Form => formOf(k.look ?? 'network', k.children.some(c => c.id === 'gpu'))
export function formOf(look: Look, gpu: boolean): Form {
  if (look === 'network') return 'tower'
  return gpu ? 'station' : 'plant'
}
/** A device's system, for its style (the band's colour and the mark on its side). */
export const osOfPlace = (k: Place): MachineOs => k.look === 'linux' ? 'linux' : k.look === 'mac' ? 'mac' : k.look === 'windows' ? 'win' : 'unknown'

export function deviceSprite(form: Form, fw: number, fd: number, hue: string, light: Light, lv: Levels, name = '', face: Place['face'] = {}, os: MachineOs = 'linux'): Sprite {
  const T0 = toneOf(hue, light, lv.state)
  // Every facility here stands in off-white with a grey roof (2026-11-20, the drawing floor: the hue tinted
  // whole buildings); colour is kept for bands and marks.
  const T: Tone = lv.state === 'unread' ? T0 : { ...T0, wall: light === 'night' ? faces(shade('#f7f8fa', -0.45), shade('#e4e9ef', -0.5), shade('#cdd5df', -0.55)) : faces('#f7f8fa', '#e4e9ef', '#cdd5df'), roof: light === 'night' ? shade('#d3dae3', -0.4) : '#d3dae3' }
  const run = lv.state === 'online'
  const tall = form === 'station' ? 96 : form === 'plant' ? 84 : form === 'tower' ? 70 : 52
  const g = new Draw(fw, fd, tall + 24)
  const i = 0.24, x0 = i, y0 = i, w = fw - 2 * i, d = fd - 2 * i
  g.shadow(x0, y0, w, d, 0.4)
  if (form === 'plant') {
    powerPlant(g, x0, y0, w, d, T, lv, hue, light, name, os, face)
    return g.done()
  }

  if (form === 'station') {
    powerPlant(g, x0, y0, w, d, T, lv, hue, light, name, os, face, 'station')
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
  const band = unread ? '#b9b6c4' : S(OS_BAND[os], -0.35)
  void hue
  // One family, each in its own style (the Owner, 2026-11-20: all of them computers, yet tell them apart at
  // a glance). What every one shares, so it reads as a computer: the MEM and DISK tanks in the front row, the
  // CPU as lit panes and as exhaust. What differs is the building, by its system and what it is for:
  //   Linux     a factory: a sawtooth roof with north lights, the red and white chimney
  //   Mac       the agents' studio: a low white pavilion, glass from end to end, a thin aluminium flue
  //   Windows   the trading desk: a blue glass office tower, a ticker strip round its foot
  //   storage   the warehouse: a gabled shed, a roller door per disk with its lamp, crates by how full
  //   GPU       the server hall: windowless, louvred, a field of fans on the roof, a cooling tower beside
  const style = kind === 'station' ? 'servers' : os === 'linux' ? 'factory' : os === 'mac' ? 'studio' : os === 'win' ? 'tower' : 'warehouse'
  const n = (metaphor('cpu.busy').encoding as { n: number }).n, on = run ? lit(lv.cpu, n) : 0
  const pane = (i: number, off = night ? '#3a4158' : '#cfe3f7') => i < on ? '#ffd76a' : unread ? '#dcdbe3' : off
  const greyed = (c: ReturnType<typeof faces>) => unread ? faces('#eeedf2', '#d9d7e0', '#c6c3cf') : faces(S(c.top, -0.45), S(c.left, -0.5), S(c.right, -0.55))
  // A stack standing from the ground at (sx, sy), its smoke the CPU, its light the state.
  const stack = (sx: number, sy: number, top: number, r: number, striped: boolean) => {
    const [cx, cy] = g.p(sx, sy, Z), bands = striped ? 6 : 1, step = (top - Z) / bands
    for (let i = 0; i < bands; i++) {
      const yb = cy - i * step
      const fill = !striped ? (unread ? '#d9d7e0' : S('#c9cfd8', -0.4)) : i % 2 ? S('#ffffff', -0.45) : unread ? '#c9c6d2' : S('#e0564f', -0.4)
      g.add(`<rect x="${n1(cx - r)}" y="${n1(yb - step)}" width="${2 * r}" height="${n1(step)}" fill="${fill}" stroke="${INK}" stroke-width="1.1"/>`)
    }
    if (!striped) g.add(`<rect x="${n1(cx - r + 1.2)}" y="${n1(cy - (top - Z))}" width="1.6" height="${n1(top - Z)}" fill="#ffffff" opacity="0.6"/>`)
    g.add(`<rect x="${n1(cx - r - 1.6)}" y="${n1(cy - (top - Z) - 3)}" width="${2 * r + 3.2}" height="4" fill="#59606b" stroke="${INK}" stroke-width="1"/>`)
    smoke(g, sx, sy, top, run ? lv.cpu : null, run)
    beacon(g, sx, sy, top, lv.state)
  }
  let hx = x0 + 0.14, hy = y0 + 0.16, hw = w * 0.6, hd = d * 0.5

  if (style === 'factory') {
    stack(hx + hw + 0.22, hy + 0.2, Z + H + 62, 6.5, true)
    const wall = greyed(faces('#fdf6ea', '#f3e6cf', '#e2d0b0'))
    g.shadow(hx, hy, hw, hd, 0.35)
    g.box(hx, hy, hw, hd, H, wall, Z, 1.4)
    const W = hw * U, D = hd * U
    let f = `<rect x="0" y="${-H + 3}" width="${n1(W)}" height="5" fill="${band}" stroke="${INK}" stroke-width="0.8"/><rect x="0" y="-5" width="${n1(W)}" height="5" fill="${S('#d8c6a4', -0.5)}" stroke="${INK}" stroke-width="0.6"/>`
    const door = Math.min(22, W * 0.22), cw = (W - door - 12) / n, wy = -H + 13, wh = H - 25
    for (let i = 0; i < n; i++) { const u = 4 + i * cw, ww = cw * 0.62
      f += `<rect x="${n1(u)}" y="${n1(wy)}" width="${n1(ww)}" height="${n1(wh)}" rx="${n1(ww / 2)}" fill="${pane(i)}" stroke="${INK}" stroke-width="0.7"/><line x1="${n1(u + ww / 2)}" y1="${n1(wy + 2)}" x2="${n1(u + ww / 2)}" y2="${n1(wy + wh)}" stroke="${INK}" stroke-width="0.4"/>` }
    const dx = W - door - 4, dh = Math.min(22, H - 14)
    f += `<rect x="${n1(dx)}" y="${-dh}" width="${n1(door)}" height="${dh}" fill="${S('#9aa3ae', -0.5)}" stroke="${INK}" stroke-width="0.9"/>`
    for (let v = -dh + 3; v < 0; v += 3) f += `<line x1="${n1(dx)}" y1="${v}" x2="${n1(dx + door)}" y2="${v}" stroke="${S('#6f7884', -0.4)}" stroke-width="0.5"/>`
    g.face('left', hx, hy + hd, Z, f)
    let r = `<rect x="0" y="${-H + 3}" width="${n1(D)}" height="5" fill="${band}" stroke="${INK}" stroke-width="0.8"/><rect x="0" y="-5" width="${n1(D)}" height="5" fill="${S('#d8c6a4', -0.5)}" stroke="${INK}" stroke-width="0.6"/>`
    for (let u = 6; u < D * 0.55; u += 9) r += `<rect x="${n1(u)}" y="${-H + 13}" width="5" height="${H - 25}" rx="2.5" fill="${pane(n)}" stroke="${INK}" stroke-width="0.6"/>`
    g.face('right', hx + hw, hy + hd, Z, r)
    plaque(g, 'right', hx + hw, hy + hd, hd * 0.78, Z + H - 18, os, 6)
    // The sawtooth: north lights facing back, slopes to the front.
    const teeth = 4, tw = hw / teeth
    for (let i = 0; i < teeth; i++) { const a = hx + i * tw, b = a + tw
      g.poly(night ? '#3a4a6a' : '#bfe0f7', [[a, hy, Z + H], [a, hy + hd, Z + H], [a, hy + hd, Z + H + 11], [a, hy, Z + H + 11]], 0.8)
      g.poly(S(i % 2 ? '#eadcc0' : '#f0e4cc', -0.45), [[a, hy, Z + H + 11], [a, hy + hd, Z + H + 11], [b, hy + hd, Z + H], [b, hy, Z + H]], 1)
      g.poly(wall.left, [[a, hy + hd, Z + H], [a, hy + hd, Z + H + 11], [b, hy + hd, Z + H]], 1) }
    g.poly(wall.right, [[hx + hw, hy, Z + H], [hx + hw, hy + hd, Z + H], [hx + hw, hy + hd, Z + H], [hx + hw, hy, Z + H]], 0.6)
  }

  if (style === 'studio') {
    const Hm = Math.max(26, Math.round(H * 0.62))
    hw = w * 0.66
    stack(hx + hw + 0.2, hy + 0.25, Z + Hm + 44, 4, false)
    const wall = greyed(faces('#ffffff', '#f2f4f7', '#dde2e9'))
    g.shadow(hx, hy, hw, hd, 0.3)
    g.box(hx, hy, hw, hd, Hm, wall, Z, 1.3)
    const W = hw * U, D = hd * U, cw = (W - 8) / n
    let f = ''
    for (let i = 0; i < n; i++) f += `<rect x="${n1(4 + i * cw)}" y="${-Hm + 7}" width="${n1(cw)}" height="${Hm - 10}" fill="${pane(i, night ? '#33405a' : '#d7e9f8')}" stroke="${INK}" stroke-width="0.6"/>`
    f += `<path d="M${n1(W * 0.2)},${-Hm + 7} l9,0 l-${n1(Hm * 0.45)},${Hm - 10} l-9,0 Z" fill="#ffffff" opacity="0.4"/>`
    f += `<rect x="0" y="${-Hm + 1}" width="${n1(W)}" height="4" fill="${band}" stroke="${INK}" stroke-width="0.6"/>`
    g.face('left', hx, hy + hd, Z, f)
    g.face('right', hx + hw, hy + hd, Z, `<rect x="0" y="${-Hm + 1}" width="${n1(D)}" height="4" fill="${band}" stroke="${INK}" stroke-width="0.6"/><rect x="5" y="${-Hm + 10}" width="${n1(D * 0.3)}" height="${Hm - 16}" fill="${pane(n, night ? '#33405a' : '#d7e9f8')}" stroke="${INK}" stroke-width="0.6"/>`)
    plaque(g, 'right', hx + hw, hy + hd, hd * 0.68, Z + Hm / 2, os, 9)
    // The thin roof slab overhanging all round.
    g.box(hx - 0.07, hy - 0.07, hw + 0.14, hd + 0.14, 3, greyed(faces('#ffffff', '#e9edf2', '#d3d9e1')), Z + Hm, 1)
  }

  if (style === 'tower') {
    const Ht = H + 44
    hx = x0 + 0.3; hy = y0 + 0.22; hw = w * 0.42; hd = d * 0.42
    const glass = unread ? faces('#eeedf2', '#d9d7e0', '#c6c3cf') : faces(S('#dce8f6', -0.45), S('#5b8fd0', -0.4), S('#3f6fb0', -0.45))
    g.shadow(hx, hy, hw, hd, 0.5)
    g.box(hx, hy, hw, hd, Ht, glass, Z, 1.4)
    const W = hw * U, D = hd * U, rows = Math.floor((Ht - 26) / 9), cols = 4
    const cells = rows * cols, lot = run ? Math.round(cells * on / n) : 0
    const grid = (span: number, k0: number) => { let s2 = ''
      for (let rr = 0; rr < rows; rr++) for (let c = 0; c < cols; c++) { const k = k0 + (rows - 1 - rr) * cols + c
        s2 += `<rect x="${n1(3 + c * (span - 6) / cols)}" y="${n1(-Ht + 14 + rr * 9)}" width="${n1((span - 6) / cols - 2)}" height="6.5" fill="${k < lot ? '#ffd76a' : unread ? '#dcdbe3' : night ? '#22355a' : '#9cc3ee'}" stroke="${S('#1f3f73', -0.2)}" stroke-width="0.5"/>` }
      return s2 }
    const crown = (span: number) => `<rect x="0" y="${-Ht}" width="${n1(span)}" height="9" fill="${unread ? '#b9b6c4' : S('#1f3f73', -0.2)}" stroke="${INK}" stroke-width="0.8"/>`
    // The ticker strip round its foot (the trading desk), lit while it is read.
    const ticker = (span: number) => { let t = `<rect x="0" y="-9" width="${n1(span)}" height="6" fill="#1d2230" stroke="${INK}" stroke-width="0.7"/>`
      if (run) for (let u = 2; u < span - 4; u += 6) t += `<rect x="${n1(u)}" y="-7.5" width="3.5" height="3" fill="${Math.round(u) % 18 < 6 ? '#f29b8f' : '#7be08f'}"/>`
      return t }
    g.face('left', hx, hy + hd, Z, crown(W) + grid(W, 0) + ticker(W) + `<rect x="${n1(W / 2 - 5)}" y="-3" width="10" height="3" fill="${S('#1f3f73', -0.2)}"/>`)
    g.face('right', hx + hw, hy + hd, Z, crown(D) + grid(D, 0) + ticker(D))
    plaque(g, 'left', hx, hy + hd, hw * 0.5, Z + Ht - 4.5, os, 3.4)
    g.poly(S('#cfd9e6', -0.45), [[hx + 0.05, hy + 0.05, Z + Ht], [hx + hw - 0.05, hy + 0.05, Z + Ht], [hx + hw - 0.05, hy + hd - 0.05, Z + Ht], [hx + 0.05, hy + hd - 0.05, Z + Ht]], 0.8)
    // Its exhaust: two vents on the roof, the smoke the CPU.
    for (const k of [0.3, 0.7]) g.cylinder(hx + hw * k, hy + hd * 0.4, 3.2, Z + Ht, Z + Ht + 10, S('#c9cfd8', -0.4), S('#8d97a6', -0.4), '#59606b')
    smoke(g, hx + hw * 0.3, hy + hd * 0.4, Z + Ht + 10, run ? lv.cpu : null, run)
    beacon(g, hx + hw * 0.7, hy + hd * 0.4, Z + Ht + 10, lv.state)
    hx = x0 + 0.14; hy = y0 + 0.16; hd = d * 0.5
  }

  if (style === 'warehouse') {
    const Hs = Math.max(30, Math.round(H * 0.7)), ridge = 13
    hw = w * 0.78; hd = d * 0.52
    const wall = greyed(faces('#f7f8fa', '#e4e9ef', '#cdd5df'))
    g.shadow(hx, hy, hw, hd, 0.35)
    g.box(hx, hy, hw, hd, Hs, wall, Z, 1.4)
    const W = hw * U, D = hd * U
    // Its doors: one per disk, grouped by volume; each lamp the disk's state, the crates its volume's use.
    const vols = lv.vols && lv.vols.length ? lv.vols : [{ f: lv.disks[0] ?? null, units: (lv.disks.length ? lv.disks : [null]).map(() => lv.state) }]
    if (lv.vols?.length) { metaphor('volume.disks'); metaphor('volume.used') }
    const doors = vols.flatMap(v => (v.units.length ? v.units : [lv.state]).map(u => ({ u, f: v.f })))
    const span = (W - 10) / doors.length, dh = Math.min(24, Hs - 12)
    let f = `<rect x="0" y="${-Hs + 3}" width="${n1(W)}" height="5" fill="${band}" stroke="${INK}" stroke-width="0.8"/>`
    for (let u = 3; u < W; u += 4) f += `<line x1="${n1(u)}" y1="${-Hs + 9}" x2="${n1(u)}" y2="-1" stroke="${S('#d3dae3', -0.4)}" stroke-width="0.5"/>`
    doors.forEach((dr, k) => { const a = 5 + k * span + span * 0.12, dw = span * 0.76
      f += `<rect x="${n1(a)}" y="${-dh}" width="${n1(dw)}" height="${dh}" fill="#2c2e38" stroke="${INK}" stroke-width="0.8"/>`
      if (run && dr.f != null && dr.f > 0.002) { const rowsC = Math.max(1, Math.round(Math.min(1, dr.f) * 5)), ch = (dh - 4) / 5
        for (let q = 0; q < rowsC; q++) f += `<rect x="${n1(a + 1.5)}" y="${n1(-2 - (q + 1) * ch)}" width="${n1(dw / 2 - 2)}" height="${n1(ch - 0.8)}" fill="#e0a24e" stroke="${INK}" stroke-width="0.5"/><rect x="${n1(a + dw / 2 + 0.5)}" y="${n1(-2 - (q + 1) * ch)}" width="${n1(dw / 2 - 2)}" height="${n1(ch - 0.8)}" fill="#d08f3c" stroke="${INK}" stroke-width="0.5"/>` }
      f += `<rect x="${n1(a + dw / 2 - 3)}" y="${-dh - 6}" width="6" height="4" rx="1" fill="${UNIT[dr.u ?? 'unread']}" stroke="${INK}" stroke-width="0.7"/>` })
    g.face('left', hx, hy + hd, Z, f)
    // The office at the side, its windows the CPU.
    let r = `<rect x="0" y="${-Hs + 3}" width="${n1(D)}" height="5" fill="${band}" stroke="${INK}" stroke-width="0.8"/>`
    const ow = (D - 10) / 4
    for (let i = 0; i < 4; i++) r += `<rect x="${n1(5 + i * ow)}" y="-17" width="${n1(ow - 3)}" height="9" fill="${pane(Math.floor(i * n / 4))}" stroke="${INK}" stroke-width="0.6"/>`
    g.face('right', hx + hw, hy + hd, Z, r)
    // The gabled metal roof, its ridge along the shed, a vent on it with the CPU's exhaust.
    const mid = hy + hd / 2
    g.poly(S('#dfe5ec', -0.45), [[hx, hy, Z + Hs], [hx + hw, hy, Z + Hs], [hx + hw, mid, Z + Hs + ridge], [hx, mid, Z + Hs + ridge]], 1.1)
    g.poly(S('#c3ccd7', -0.5), [[hx, mid, Z + Hs + ridge], [hx + hw, mid, Z + Hs + ridge], [hx + hw, hy + hd, Z + Hs], [hx, hy + hd, Z + Hs]], 1.1)
    for (let k = 1; k < 10; k++) { const x = hx + hw * k / 10; g.line([x, mid, Z + Hs + ridge], [x, hy + hd, Z + Hs], S('#a9b3c0', -0.3), 0.6) }
    g.poly(wall.right, [[hx + hw, hy, Z + Hs], [hx + hw, hy + hd, Z + Hs], [hx + hw, mid, Z + Hs + ridge]], 1.1)
    g.cylinder(hx + hw * 0.7, mid, 3.4, Z + Hs + ridge - 2, Z + Hs + ridge + 8, S('#c9cfd8', -0.4), S('#8d97a6', -0.4), '#59606b')
    smoke(g, hx + hw * 0.7, mid, Z + Hs + ridge + 8, run ? lv.cpu : null, run)
    beacon(g, hx + hw * 0.4, mid, Z + Hs + ridge, lv.state)
  }

  if (style === 'servers') {
    const wall = greyed(faces('#6a7080', '#4f5564', '#41464f'))
    g.shadow(hx, hy, hw, hd, 0.35)
    g.box(hx, hy, hw, hd, H, wall, Z, 1.4)
    const W = hw * U, D = hd * U
    // Louvred walls; a strip of status lights, as many lit as the CPU is busy; the GPU's name on its band.
    const louvres = (span: number) => { let l = ''
      for (let u = 4; u < span - 6; u += 11) { l += `<rect x="${n1(u)}" y="${-H + 14}" width="7" height="${H - 26}" fill="${S('#3a3f4a', -0.3)}" stroke="${INK}" stroke-width="0.6"/>`
        for (let v = -H + 17; v < -13; v += 3) l += `<line x1="${n1(u)}" y1="${v}" x2="${n1(u + 7)}" y2="${v}" stroke="${S('#7d8494', -0.3)}" stroke-width="0.6"/>` }
      return l }
    let f = `<rect x="0" y="${-H + 3}" width="${n1(W)}" height="7" fill="${S('#2b2f38', -0.2)}" stroke="${INK}" stroke-width="0.8"/><text x="5" y="${-H + 8.6}" font-family="'JetBrains Mono',ui-monospace,Menlo,monospace" font-size="5.4" font-weight="800" fill="#76e08f" letter-spacing="1.4">GPU</text>`
    f += louvres(W)
    const lw = (W - 30) / n
    for (let i = 0; i < n; i++) f += `<rect x="${n1(26 + i * lw)}" y="${-H + 5}" width="${n1(lw - 2)}" height="3" fill="${i < on ? '#76e08f' : '#20242c'}"/>`
    f += `<rect x="${n1(W - 16)}" y="-16" width="11" height="16" fill="${S('#2b2f38', -0.2)}" stroke="${INK}" stroke-width="0.8"/>`
    g.face('left', hx, hy + hd, Z, f)
    g.face('right', hx + hw, hy + hd, Z, `<rect x="0" y="${-H + 3}" width="${n1(D)}" height="7" fill="${S('#2b2f38', -0.2)}" stroke="${INK}" stroke-width="0.8"/>` + louvres(D))
    plaque(g, 'right', hx + hw, hy + hd, hd * 0.88, Z + H - 18, os, 5)
    // The roof: a field of fans.
    g.poly(S('#7a8090', -0.4), [[hx + 0.06, hy + 0.06, Z + H], [hx + hw - 0.06, hy + 0.06, Z + H], [hx + hw - 0.06, hy + hd - 0.06, Z + H], [hx + 0.06, hy + hd - 0.06, Z + H]], 0.8)
    const fc = Math.max(2, Math.floor(hw / 0.55)), fr = Math.max(2, Math.floor(hd / 0.55))
    for (let i = 0; i < fc; i++) for (let j = 0; j < fr; j++) {
      const fx = hx + (i + 0.5) * hw / fc, fy = hy + (j + 0.5) * hd / fr
      g.box(fx - 0.2, fy - 0.2, 0.4, 0.4, 4, faces(S('#9aa1af', -0.4), S('#7d8494', -0.4), S('#6a7080', -0.4)), Z + H, 0.8)
      const [cx, cy] = g.p(fx, fy, Z + H + 4)
      g.add(`<ellipse cx="${n1(cx)}" cy="${n1(cy)}" rx="9" ry="4.5" fill="#2b2f38" stroke="${INK}" stroke-width="0.7"/><path d="M${n1(cx - 7)},${n1(cy)} L${n1(cx + 7)},${n1(cy)} M${n1(cx)},${n1(cy - 3.5)} L${n1(cx)},${n1(cy + 3.5)}" stroke="#8d95a3" stroke-width="1.2"/>`)
    }
    // The cooling tower beside the hall, steaming as busy as it is.
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
    ...(style === 'warehouse' ? [] : lv.disks.length ? lv.disks : [null]).map((f, k, all) => ({ r: encode('disk.total', lv.cap?.disk ?? null)!, f: run ? encode('disk.used', f) : null, c: '#d98f2b',
      word: all.length > 1 ? `DISK${k + 1}` : 'DISK' })),
  ]
  const ty = hy + hd + (y0 + d - hy - hd) * 0.55
  tanks.forEach((t, k) => {
    const tx = x0 + w * (0.28 + k * Math.min(0.32, 0.55 / Math.max(1, tanks.length - 1)))
    g.line([tx, ty, Z + 10], [tx, hy + hd, Z + 10], S('#8d97a6', -0.3), 2.2)
    steel(tx, ty, t.r, 34, t.f, t.c, t.word)
  })
  // Its number, two digits, large on the front corner of its ground, the lowest point on screen, drawn last so
  // nothing stands over it (the Owner, 2026-11-20: on the eave it was too small; one place for all, in sight).
  if (face?.roof) {
    const pw = 0.82, ph = 20
    g.face('left', x0 + w - pw - 0.04, y0 + d, Z + 2, `<rect x="0" y="${-ph}" width="${n1(pw * U)}" height="${ph}" rx="3" fill="${night ? '#e6e3f2' : '#fbfaf6'}" stroke="${INK}" stroke-width="1.4"/><text x="${n1(pw * U / 2)}" y="-4.6" text-anchor="middle" font-family="'JetBrains Mono',ui-monospace,Menlo,monospace" font-size="15" font-weight="800" fill="${INK}">${face.roof}</text>`)
  }
}

export const deviceKey = (form: Form, fw: number, fd: number, hue: string, light: Light, lv: Levels, name = '', face: Place['face'] = {}, os: MachineOs = 'linux') => {
  const q = (f: number | null) => f == null ? 'n' : String(Math.round(f * 10))
  const c = (v: number | null | undefined) => v == null ? 'n' : String(Math.round(Math.log2(v) * 4))
  return `g2:fac:${form}:${os}:${name}:${c(lv.cap?.cores)}.${c(lv.cap?.mem)}.${c(lv.cap?.disk)}:${face?.roof ?? ''}:${face?.sign ?? ''}:${face?.tally?.n ?? ''}:${fw}x${fd}:${hue}:${light}:${lv.state}:${q(lv.cpu)}:${q(lv.mem)}:${lv.disks.map(q).join('.')}:${(lv.vols ?? []).map(v => `${q(v.f)}${v.units.join('')}`).join('|')}`
}
