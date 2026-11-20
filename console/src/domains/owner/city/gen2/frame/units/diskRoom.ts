import { Draw, faces, INK, type Sprite } from '../kit'
import { encode, metaphor, rulerOf, rulersKey } from '../library/dictionary'
import { readingText } from '../library/parts'
import { CONSOLE_DEPTH, hasConsole, roomConsole, type ConsoleGauge, type ConsoleSpec } from './console'

/**
 * The disk unit's room, the factory's warehouse (2026-11-19; details the Owner's fifteen). Drawn with what a
 * factory warehouse really has, not with how a drive looks (the Owner the same day: "我们这个是工厂…硬盘的话，
 * 它是仓库"), goods stacked so how full it is shows at a glance, as in the cold store. Held to the room
 * standards (frame/units/standards.ts):
 *   high-bay racks    a spinning drive: tall pallet racking along the left wall, five levels, a stacker crane
 *                     on its rail in front (a load is fetched by the crane driving over and climbing: slow).
 *   line-side racks   a solid state drive: a low rack of blue parts bins at the front by the line, reached by
 *                     hand (fast). Kind not read: a plain low rack of pallets.
 *   bays and goods    a pallet place (or bin) per 256 GB, the standard part, whole bays; goods stacked on them
 *                     (amber cartons) from the bottom level up, as far as the volume is used. Written beneath:
 *                     the volume's name, used and size; read-only a red chain, file system errors red tape.
 *   the end plate     beside each rack: the model (a spinning drive's speed), the lathes' four folds lit as far
 *                     as it is busy, goods out (read) and in (written) a second, loads a second and time per
 *                     load; the drive's own thermometer on the rack's upright.
 *   receiving floor   where a machine has several drives, which volume lies on which drive is not reported:
 *                     the racks stand empty and the volumes' pallets wait on the receiving floor, "SHELF NOT
 *                     REPORTED", rather than on a guessed rack.
 *   array boards      one per RAID array, low at the back: its state, a lamp per drive, a strip as far as it is
 *                     in step; apart from the racks, membership not being reported.
 *   the console       how full all the volumes are together on the desk; the drives' average busy.
 * Nothing hides anything: the tall racks stand against the wall with nothing behind them; the low racks at the
 * front, a row's rise no more than the gap to the line written behind it.
 */

export type DriveReading = {
  device: string; kind: 'ssd' | 'hdd' | null; model: string | null; rpm: number | null
  read: number | null; write: number | null; busy: number | null; ops: number | null; latency: number | null; temp: number | null
}
export type VolumeReading = { name: string; size: number; used: number | null; readonly: boolean | null; errors: number | null }
export type ArrayReading = { name: string; state: string | null; active: number | null; failed: number | null; spare: number | null; sync: number | null }
export type DiskRoomReadings = { drives: DriveReading[]; volumes: VolumeReading[]; arrays: ArrayReading[] }

const U = 35.8
const MONO = `font-family="'JetBrains Mono',ui-monospace,Menlo,monospace"`
const WALL = faces('#efe9dc', '#e2dac8', '#cfc6b2')
const BOARD = faces('#e6e8ec', '#c8ccd3', '#b3b8c0')
const UPRIGHT = faces('#5b86c7', '#3d6fb6', '#2f5c9e')
const BEAM = faces('#c9ced6', '#e8833a', '#d0702b')
const WOOD = faces('#d8b98a', '#b8925c', '#a07c4a')
const CARTON = faces('#f6c25a', '#e0a238', '#c98a26')
const BINF = faces('#6fa0e0', '#3f7fd1', '#3369b3')
const CRANE = faces('#f0d24a', '#d9b62c', '#c19f1e')
const RED = '#d83a32'
const LAMP = { active: '#4caf62', failed: RED, spare: '#9aa5b1' } as const

/** High-bay racking along the left wall: depth, a bay's length along the wall, levels and their height. */
export const HIGH_BAY = { x: 0.22, depth: 0.6, bay: 0.32, levels: 5, level: 7.5, base: 1.5 }
const HB_H = HIGH_BAY.base + HIGH_BAY.levels * HIGH_BAY.level + 1
/** A low rack at the front: depth, a bay's length, levels and their height by kind; the rows' pitch. */
export const LOW_RACK = { depth: 0.5, bay: 0.3, label: 0.3, row: 1.8, line: { levels: 3, level: 5 }, plain: { levels: 2, level: 5.5 } }
const lowH = (k: 'line' | 'plain') => 1 + LOW_RACK[k].levels * LOW_RACK[k].level
/** The end plate beside each rack. */
export const PLATE = { w: 1.6, d: 0.6, gap: 0.15, h: 3 }
/** Bays a rack shows where its goods are not placed on it. */
const BARE_COLS = 3
/** A pallet place on the receiving floor. */
const CELL = 0.25
const ROOMS: [number, number][] = [[4, 4], [4, 8], [8, 8]]
const FLOOR_TOP = CONSOLE_DEPTH + 0.15, LEFT = 0.35
const PLATE_X = HIGH_BAY.x + HIGH_BAY.depth + 0.33

type Form = 'high' | 'line' | 'plain'
const formOf = (d: DriveReading | null): Form => d?.kind === 'hdd' ? 'high' : d?.kind === 'ssd' ? 'line' : 'plain'

/** Whole bays for a volume, under the ruler as it stands (a bay is whole; what is on it fills it in proportion). */
export const baysOf = (v: VolumeReading) => v.size > 0 ? Math.max(1, Math.ceil(encode('disk.bays', v.size)! - 0.02)) : 0
const levelsOf = (f: Form) => f === 'high' ? HIGH_BAY.levels : LOW_RACK[f].levels
const bayOf = (f: Form) => f === 'high' ? HIGH_BAY.bay : LOW_RACK.bay

/** A rack: its drive, the volume whose goods are on it (null: bare), the first of the volume's bays it holds. */
type Rack = { form: Form; drive: DriveReading | null; volume: VolumeReading | null; bays: number | null; from: number; cols: number; x: number; y: number; plate: boolean; labelled: boolean }
type Staged = { y: number; volume: VolumeReading }
type Plan = { racks: Rack[]; staged: Staged[]; stagingTop: number | null; xs: number; cut: number; fits: boolean }

function plan(r: DiskRoomReadings, [w, d]: [number, number]): Plan {
  const racks: Rack[] = [], staged: Staged[] = []
  let cut = 0, fits = true, stagingTop: number | null = null
  const one = r.drives.length === 1 && r.volumes.length > 0
  // What each drive holds: on a lone drive, every volume read; with several, nothing placed.
  type Want = { drive: DriveReading; volume: VolumeReading | null }
  const wants: Want[] = one ? r.volumes.map(v => ({ drive: r.drives[0], volume: v })) : r.drives.map(dr => ({ drive: dr, volume: null }))
  const high = wants.filter(x => formOf(x.drive) === 'high'), low = wants.filter(x => formOf(x.drive) !== 'high')
  // High-bay sections along the left wall, from the back.
  let y = FLOOR_TOP + 0.15
  const limit = d - 0.2
  let firstPlate = true
  for (const h of high) {
    const n = h.volume ? baysOf(h.volume) : null
    const cols = Math.max(3, n != null ? Math.ceil(n / HIGH_BAY.levels) : BARE_COLS)
    const len = cols * HIGH_BAY.bay
    if (y + len > limit + 1e-9) { cut++; continue }
    racks.push({ form: 'high', drive: h.drive, volume: h.volume, bays: n, from: 0, cols, x: HIGH_BAY.x, y, plate: one ? firstPlate : true, labelled: !!h.volume })
    firstPlate = false
    y += len + 0.12
  }
  const hbEnd = y
  const xs = high.length ? PLATE_X + PLATE.w + 0.3 : LEFT
  // Low racks in rows from the front, left to right (from the left wall where the high-bay racks have ended);
  // a long volume over several racks.
  const rise = lowH('line') / 16
  const rowStart = (ry: number) => ry - rise >= hbEnd ? LEFT : xs
  let rowY = d - 0.2 - LOW_RACK.label - LOW_RACK.depth, x = rowStart(rowY)
  const room = w - xs - 0.15
  const place = (len: number) => {
    if (x > rowStart(rowY) && x + len > w - 0.15 + 1e-9) { rowY -= LOW_RACK.row; x = rowStart(rowY) }
    const at = { x, y: rowY }; x += len + 0.3; return at
  }
  for (const l of low) {
    const f = formOf(l.drive), L = levelsOf(f)
    const n = l.volume ? baysOf(l.volume) : null
    const maxCols = Math.max(1, Math.floor((room - PLATE.gap - PLATE.w) / LOW_RACK.bay))
    const all = n != null ? Math.ceil(n / L) : BARE_COLS
    for (let k = 0, from = 0; k < Math.ceil(all / maxCols); k++) {
      const cols = Math.min(maxCols, all - k * maxCols), plate = one ? firstPlate : k === 0
      const at = place(cols * LOW_RACK.bay + (plate ? PLATE.gap + PLATE.w : 0))
      racks.push({ form: f, drive: l.drive, volume: l.volume, bays: n, from, cols, x: at.x, y: at.y, plate, labelled: !!l.volume && k === Math.ceil(all / maxCols) - 1 })
      from += cols * L
      firstPlate = false
    }
  }
  const lowTop = racks.some(k => k.form !== 'high') ? rowY - (lowH('line') / 16) : limit
  if (lowTop < FLOOR_TOP + 0.15 - 1e-9) fits = false
  // The receiving floor, behind the low racks: the volumes whose drive is not reported.
  if (!one && r.volumes.length) {
    const C = Math.max(1, Math.floor((w - xs - 0.2) / CELL))
    let sy = FLOOR_TOP + 0.15
    stagingTop = sy
    sy += 0.3
    for (const v of r.volumes) {
      // The line written beneath, and room for the next volume's cartons to rise without covering it.
      const sd = Math.ceil(baysOf(v) / C) * CELL + LOW_RACK.label + 0.35
      if (sy + sd > lowTop + 1e-9) { cut++; continue }
      staged.push({ y: sy, volume: v })
      sy += sd
    }
  }
  return { racks, staged, stagingTop, xs, cut, fits }
}

export function diskRoomShape(r: DiskRoomReadings): [number, number] {
  return ROOMS.find(s => { const p = plan(r, s); return p.cut === 0 && p.fits }) ?? ROOMS[ROOMS.length - 1]
}

const avg = (v: (number | null)[]) => { const k = v.filter((x): x is number => x != null); return k.length ? k.reduce((a, b) => a + b, 0) / k.length : null }

/** The room's summary, what its console shows: how full the volumes are together, and the drives' average busy. */
export function diskSummaryOf(r: DiskRoomReadings) {
  const read = r.volumes.filter(v => v.used != null)
  const size = read.reduce((a, v) => a + v.size, 0), used = read.reduce((a, v) => a + v.used!, 0)
  return { used: size > 0 ? used / size : null, size: r.volumes.reduce((a, v) => a + v.size, 0) || null, busy: avg(r.drives.map(d => d.busy)) }
}

/** A volume by the last part of where it is mounted: VOLUME1, DATA; the root ROOT. */
export const volumeName = (mount: string) => (mount.split('/').filter(Boolean).pop() ?? 'root').toUpperCase()

const esc = (s: string) => s.replace(/&/g, '&amp;').replace(/</g, '&lt;')
const clamp = (v: number) => Math.max(0, Math.min(1, v))

export function diskRoom(r: DiskRoomReadings): Sprite {
  const [fw, fd] = diskRoomShape(r)
  const g = new Draw(fw, fd, 56)
  g.box(0, 0, fw, fd, 3, faces('#d8d6cf', '#bdbab1', '#aaa79e'), 0, 1.2)
  g.box(0, 0, fw, 0.18, 46, WALL, 0, 1.2)
  g.box(0, 0.18, 0.18, fd - 0.18, 46, WALL, 0, 1.2)

  const sum = diskSummaryOf(r)
  const gauges: ConsoleGauge[] = sum.busy != null ? [{ row: 'drive.busy.all', name: 'BUSY', share: encode('drive.busy.all', sum.busy), redFrom: 0.85, reading: readingText(sum.busy, 'share') }] : []
  const spec: ConsoleSpec = {
    name: 'DISK CONSOLE', gauges,
    desk: sum.used != null ? { row: 'disk.used.all', share: encode('disk.used.all', sum.used), plate: `USED ${readingText(sum.used, 'share')}` } : undefined,
  }

  type Thing = { at: number; draw: () => void }
  const things: Thing[] = []
  if (hasConsole(spec)) things.push({ at: -1, draw: () => roomConsole(g, fw, spec) })

  if (!r.drives.length && !r.volumes.length) {
    g.text('left', 0.6, 0.18, 30, 'DISK NOT READ', 7, '#8a857a')
    things.sort((a, b) => a.at - b.at).forEach(t => t.draw())
    return g.done()
  }

  // The array boards, low at the back, beside the console.
  r.arrays.forEach((a, k) => {
    metaphor('array.state')
    const x = 0.35 + k * 1.35, y = 0.3, W = 1.2 * U, D = 0.6 * U
    const ok = a.state === 'active'
    things.push({ at: x + y, draw: () => {
      g.shadow(x, y, 1.2, 0.6, 0.2)
      g.box(x, y, 1.2, 0.6, 3, BOARD, 0, 1)
      let svg = `<text x="2.5" y="7" ${MONO} font-size="5.2" font-weight="700" fill="${ok ? INK : RED}">${esc(a.name.toUpperCase())} ${esc((a.state ?? '—').toUpperCase())}</text>`
      const lamps: string[] = []
      for (const s of ['active', 'failed', 'spare'] as const) for (let i = 0; i < (a[s] ?? 0); i++) lamps.push(LAMP[s])
      if (a.active != null) { metaphor('array.disks'); lamps.forEach((c, i) => { svg += `<circle cx="${(5 + i * 6).toFixed(1)}" cy="12.5" r="2.2" fill="${c}" stroke="${INK}" stroke-width="0.5"/>` }) }
      if (a.sync != null) {
        metaphor('array.sync')
        const sw = W - 19, f = encode('array.sync', a.sync)!
        svg += `<rect x="2.5" y="${(D - 6).toFixed(1)}" width="${sw.toFixed(1)}" height="3" fill="#fbfaf6" stroke="${INK}" stroke-width="0.5"/><rect x="2.5" y="${(D - 6).toFixed(1)}" width="${(sw * f).toFixed(1)}" height="3" fill="#4caf62"/>`
        svg += `<text x="${(W - 2.5).toFixed(1)}" y="${(D - 3.2).toFixed(1)}" text-anchor="end" ${MONO} font-size="4.4" font-weight="600" fill="${INK}">${readingText(a.sync, 'share')}</text>`
      }
      g.floor(x, y, 3, svg)
    } })
  })

  const p = plan(r, [fw, fd])

  for (const rk of p.racks) {
    metaphor('drive.shelf')
    const v = rk.volume
    if (v) metaphor('disk.bays')
    const share = v && v.used != null ? encode('vol.used', v.used / v.size) : null
    if (share != null) metaphor('vol.used')
    if (v?.readonly) metaphor('vol.readonly')
    if (v?.errors) metaphor('vol.errors')
    // Goods stacked from the bottom level up, along the rack.
    const filled = v ? (share ?? 0) * baysOf(v) - rk.from : 0
    const n = rk.bays != null ? Math.min(rk.cols * levelsOf(rk.form), rk.bays - rk.from) : null
    const len = rk.cols * bayOf(rk.form)
    if (rk.form === 'high') {
      things.push({ at: rk.x + rk.y, draw: () => highBay(g, rk.y, rk.cols, n, filled, rk.drive, v) })
      if (rk.plate && rk.drive) { const d = rk.drive; things.push({ at: PLATE_X + rk.y, draw: () => drawPlate(g, PLATE_X, rk.y, d) }) }
      if (v && rk.labelled) things.push({ at: PLATE_X + rk.y + 0.7, draw: () => volumeLine(g, PLATE_X, rk.y + PLATE.d + 0.05, v) })
    } else {
      things.push({ at: rk.x + rk.y, draw: () => lowRack(g, rk.x, rk.y, rk.cols, rk.form as 'line' | 'plain', n, filled, rk.drive, v) })
      if (rk.plate && rk.drive) { const d = rk.drive, px = rk.x + len + PLATE.gap; things.push({ at: px + rk.y, draw: () => drawPlate(g, px, rk.y - (PLATE.d - LOW_RACK.depth), d) }) }
      if (v && rk.labelled) things.push({ at: rk.x + rk.y + 0.6, draw: () => volumeLine(g, rk.x, rk.y + LOW_RACK.depth + 0.1, v) })
    }
  }

  // The receiving floor: volumes whose drive is not reported, their pallets waiting to go up.
  if (p.stagingTop != null) {
    const top = p.stagingTop
    things.push({ at: p.xs + top, draw: () => g.floor(p.xs, top, 3, `<text x="0" y="6" ${MONO} font-size="5.2" font-weight="700" fill="#8a857a">VOLUMES · SHELF NOT REPORTED</text>`) })
  }
  const C = Math.max(1, Math.floor((fw - p.xs - 0.2) / CELL))
  for (const s of p.staged) {
    const v = s.volume, bays = baysOf(v)
    metaphor('disk.bays')
    const share = v.used != null ? encode('vol.used', v.used / v.size) : null
    if (share != null) metaphor('vol.used')
    const filled = (share ?? 0) * bays, rows = Math.ceil(bays / C)
    for (let b = 0; b < bays; b++) {
      const cx = p.xs + (b % C) * CELL, cy = s.y + Math.floor(b / C) * CELL, on = clamp(filled - b)
      things.push({ at: cx + cy, draw: () => {
        g.box(cx + 0.02, cy + 0.02, CELL - 0.04, CELL - 0.04, 1, WOOD, 0, 0.5)
        if (on > 0.02) g.box(cx + 0.04, cy + 0.04, CELL - 0.08, CELL - 0.08, 1 + 4 * on, CARTON, 1, 0.5)
      } })
    }
    const W = Math.min(bays, C) * CELL
    if (v.readonly) metaphor('vol.readonly')
    if (v.errors) metaphor('vol.errors')
    if (v.readonly || v.errors) things.push({ at: p.xs + W + s.y + rows * CELL, draw: () => {
      let svg = ''
      if (v.readonly) svg += `<path d="M0 ${(rows * CELL * U).toFixed(1)} L${(W * U).toFixed(1)} 0" stroke="${RED}" stroke-width="1.6" stroke-dasharray="3 2"/>`
      if (v.errors) svg += `<path d="M0 0 L${(W * U).toFixed(1)} ${(rows * CELL * U).toFixed(1)}" stroke="${RED}" stroke-width="3" opacity="0.85"/>`
      g.floor(p.xs, s.y, 7, svg)
    } })
    const ly = s.y + rows * CELL + 0.04
    things.push({ at: p.xs + ly + 0.5, draw: () => volumeLine(g, p.xs, ly, v) })
  }
  if (p.cut) g.text('left', 0.4, 0.18, 47, `${p.cut} VOLUME${p.cut > 1 ? 'S' : ''} DO NOT FIT — SET MORE GB A BAY`, 5.5, '#b8433f', 700)

  things.sort((a, b) => a.at - b.at).forEach(t => t.draw())
  return g.done()
}

/** Written beneath a volume's bays: its name, used and size; READ ONLY and the error count in red after it. */
function volumeLine(g: Draw, x: number, y: number, v: VolumeReading) {
  const line = `${volumeName(v.name)}  ${readingText(v.used, 'bytes')} OF ${readingText(v.size, 'bytes')}`
  let svg = `<text x="0" y="7" ${MONO} font-size="5.6" font-weight="700" fill="#5d5a52">${esc(line)}</text>`
  // What is wrong, in red on the line below.
  const tags = [v.readonly ? 'READ ONLY' : null, v.errors ? `${Math.round(v.errors)} ERRORS` : null].filter(Boolean)
  if (tags.length) svg += `<text x="0" y="13.5" ${MONO} font-size="5.6" font-weight="700" fill="${RED}">${tags.join(' · ')}</text>`
  g.floor(x, y, 3, svg)
}

/** A stack of goods on a bay: a pallet and cartons as high as the bay is full (a bin with parts on a line-side rack). */
function goods(g: Draw, x: number, y: number, w: number, d: number, z: number, room: number, on: number, bin: boolean) {
  if (bin) {
    // A blue parts bin, open-topped; its parts (amber) rise inside as far as it is full.
    const bh = room * 0.75
    g.box(x, y, w, d, bh, BINF, z, 0.5)
    // Its open picking front shows the parts, as high as it is full (the deck above hides its top).
    if (on > 0.02) g.onLeft(x, y + d, 0.015, w - 0.015, z + 0.4, z + 0.4 + (bh - 0.8) * on, CARTON.left, 0.3)
    if (on > 0.02) g.poly(CARTON.top, [[x + 0.02, y + 0.02, z + room * 0.75 * on], [x + w - 0.02, y + 0.02, z + room * 0.75 * on], [x + w - 0.02, y + d - 0.02, z + room * 0.75 * on], [x + 0.02, y + d - 0.02, z + room * 0.75 * on]], 0.4)
    return
  }
  g.box(x, y, w, d, 0.8, WOOD, z, 0.5)
  if (on <= 0.02) return
  const h = (room - 1.6) * on
  g.box(x + 0.02, y + 0.02, w - 0.04, d - 0.04, h, CARTON, z + 0.8, 0.5)
  // The cartons' seams: a stack, not a block.
  if (h > 2.5) g.line([x + 0.02, y + d - 0.02, z + 0.8 + h / 2], [x + w - 0.02, y + d - 0.02, z + 0.8 + h / 2], '#a8650f', 0.5)
  g.line([x + w / 2, y + 0.02, z + 0.8 + h], [x + w / 2, y + d - 0.02, z + 0.8 + h], '#a8650f', 0.5)
}

/** A section of high-bay racking along the left wall: five levels of pallet places, the stacker crane in front. */
function highBay(g: Draw, y0: number, cols: number, n: number | null, filled: number, d: DriveReading | null, v: VolumeReading | null) {
  const { x: x0, depth: D, bay: B, levels, level, base } = HIGH_BAY, L = cols * B
  g.shadow(x0, y0, D, L, 0.3)
  // The back uprights against the wall.
  for (let c = 0; c <= cols; c++) g.box(x0, y0 + c * B - (c === cols ? 0.04 : 0), 0.05, 0.04, HB_H, UPRIGHT, 0, 0.5)
  for (let k = 0; k < levels; k++) {
    const z = base + k * level
    g.box(x0, y0, D, L, 0.9, BEAM, z - 0.9, 0.6)
    for (let c = 0; c < cols; c++) {
      const b = k * cols + c
      if (n == null || b >= n) continue
      goods(g, x0 + 0.06, y0 + c * B + 0.04, D - 0.12, B - 0.08, z, level, clamp(filled - b), false)
    }
  }
  g.box(x0, y0, D, L, 0.9, BEAM, base + levels * level - 0.9, 0.6)
  // The front uprights.
  for (let c = 0; c <= cols; c++) g.box(x0 + D - 0.05, y0 + c * B - (c === cols ? 0.04 : 0), 0.05, 0.04, HB_H, UPRIGHT, 0, 0.5)
  // Read-only: a red chain across its face; errors: red tape.
  const fx = x0 + D + 0.01
  if (v?.readonly) g.line([fx, y0, HB_H * 0.55], [fx, y0 + L, HB_H * 0.55], RED, 1.8, 'stroke-dasharray="3 2"')
  if (v?.errors) g.line([fx, y0, HB_H - 2], [fx, y0 + L, 2], RED, 3, 'opacity="0.85"')
  // The drive's own temperature: the lathes' small thermometer on the front upright.
  if (d?.temp != null) {
    metaphor('drive.temp')
    const t = d.temp
    g.onRight(fx, y0, 0.06, 0.12, 4, HB_H - 4, '#fbfaf6', 0.5)
    g.onRight(fx, y0, 0.07, 0.11, 4, 4 + (HB_H - 8) * encode('drive.temp', t / 100)!, t >= 80 ? '#e0533d' : '#e88a3a', 0)
  }
  // The stacker crane on its rail: a load waits for it to drive over and climb.
  const rx = x0 + D + 0.1
  g.box(rx, y0, 0.06, L, 0.6, faces('#8f9bab', '#6b7280', '#5d6878'), 0, 0.5)
  const cy = y0 + L / 2 - 0.08
  g.box(rx - 0.02, cy, 0.1, 0.16, 1.6, CRANE, 0.6, 0.6)
  g.box(rx + 0.01, cy + 0.05, 0.04, 0.06, HB_H + 1, CRANE, 2.2, 0.6)
  g.box(rx - 0.06, cy - 0.02, 0.14, 0.2, 1.4, CRANE, 2.2 + level * 1.2, 0.6)
}

/** A low rack at the front: a few levels of parts bins (line-side) or pallets (plain). */
function lowRack(g: Draw, x0: number, y0: number, cols: number, k: 'line' | 'plain', n: number | null, filled: number, d: DriveReading | null, v: VolumeReading | null) {
  const { depth: D, bay: B } = LOW_RACK, { levels, level } = LOW_RACK[k], L = cols * B, H = lowH(k)
  g.shadow(x0, y0, L, D, 0.25)
  for (let c = 0; c <= cols; c++) g.box(x0 + c * B - (c === cols ? 0.04 : 0), y0, 0.04, 0.05, H, UPRIGHT, 0, 0.5)
  for (let lv = 0; lv < levels; lv++) {
    const z = 1 + lv * level
    g.box(x0, y0, L, D, 0.7, BEAM, z - 0.7, 0.5)
    for (let c = 0; c < cols; c++) {
      const b = lv * cols + c
      if (n == null || b >= n) continue
      goods(g, x0 + c * B + 0.04, y0 + 0.06, B - 0.08, D - 0.12, z, level, clamp(filled - b), k === 'line')
    }
  }
  g.box(x0, y0, L, D, 0.7, BEAM, H - 0.7, 0.5)
  for (let c = 0; c <= cols; c++) g.box(x0 + c * B - (c === cols ? 0.04 : 0), y0 + D - 0.05, 0.04, 0.05, H, UPRIGHT, 0, 0.5)
  const fy = y0 + D + 0.01
  if (v?.readonly) g.line([x0, fy, H * 0.55], [x0 + L, fy, H * 0.55], RED, 1.6, 'stroke-dasharray="3 2"')
  if (v?.errors) g.line([x0, fy, H - 1], [x0 + L, fy, 1], RED, 3, 'opacity="0.85"')
  if (d?.temp != null) {
    metaphor('drive.temp')
    const t = d.temp
    g.onRight(x0 + L + 0.01, y0, 0.15, 0.23, 1.5, H - 1.5, '#fbfaf6', 0.5)
    g.onRight(x0 + L + 0.01, y0, 0.16, 0.22, 1.5, 1.5 + (H - 3) * encode('drive.temp', t / 100)!, t >= 80 ? '#e0533d' : '#e88a3a', 0)
  }
}

/** The end plate beside a rack: the model (a spinning drive's speed), how busy, goods out and in, loads a second and time per load. */
function drawPlate(g: Draw, x: number, y: number, d: DriveReading) {
  const W = PLATE.w * U
  g.shadow(x, y, PLATE.w, PLATE.d, 0.15)
  g.box(x, y, PLATE.w, PLATE.d, PLATE.h, BOARD, 0, 1)
  const name = (d.model ?? d.device).toUpperCase(), rpm = d.rpm != null ? (metaphor('drive.rpm'), ` ${Math.round(d.rpm)} RPM`) : ''
  const room = 22 - rpm.length, short = name.length > room ? name.slice(0, room - 1) + '…' : name
  let svg = `<text x="2.5" y="5.4" ${MONO} font-size="4" font-weight="700" fill="${INK}">${esc(short + rpm)}</text>`
  // How busy: the lathes' four folds, lit from the left.
  if (d.busy != null) {
    metaphor('drive.busy')
    const b = encode('drive.busy', d.busy)!, N = 4, lit = b * N, glow = b > 0.85 ? '#ff7a45' : '#ffc94d', fw = W - 5
    for (let k = 0; k < N; k++) {
      const u0 = 2.5 + (k / N) * fw, w = fw / N, on = clamp(lit - k)
      svg += `<rect x="${u0.toFixed(1)}" y="7.2" width="${w.toFixed(1)}" height="3" fill="${k % 2 ? '#4a564f' : '#55625a'}"/>`
      if (on > 0.02) svg += `<rect x="${u0.toFixed(1)}" y="7.2" width="${(w * on).toFixed(1)}" height="3" fill="${glow}"/>`
    }
    svg += `<rect x="2.5" y="7.2" width="${fw.toFixed(1)}" height="3" fill="none" stroke="${INK}" stroke-width="0.5"/>`
  }
  const lines: string[] = []
  if (d.read != null || d.write != null) { metaphor('drive.traffic'); lines.push(`OUT ${readingText(d.read, 'bytes/s')} · IN ${readingText(d.write, 'bytes/s')}`) }
  const ops = d.ops != null ? (metaphor('drive.ops'), readingText(d.ops, 'per second')) : null
  const lat = d.latency != null ? (metaphor('drive.latency'), readingText(d.latency, 'seconds')) : null
  if (ops || lat) lines.push([ops, lat].filter(Boolean).join(' · '))
  lines.forEach((l, k) => { svg += `<text x="2.5" y="${(14.6 + k * 4.8).toFixed(1)}" ${MONO} font-size="3.6" font-weight="600" fill="${INK}">${esc(l)}</text>` })
  g.floor(x, y, PLATE.h, svg)
}

/** The key the room's picture is kept under: every reading to the step it is drawn at. */
export const diskRoomKey = (r: DiskRoomReadings) =>
  `g2:diskroom:${rulersKey()}:${rulerOf('disk.bays')}:${JSON.stringify(r).length}:${r.drives.map(d => [d.device, Math.round((d.busy ?? -1) * 100), Math.round((d.read ?? -1) / 1e4), Math.round((d.write ?? -1) / 1e4)].join('/')).join(',')}:${r.volumes.map(v => Math.round((v.used ?? -1) / 1e9)).join(',')}`
