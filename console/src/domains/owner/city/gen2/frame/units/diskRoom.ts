import { Draw, faces, INK, type Sprite } from '../kit'
import { encode, metaphor, rulerOf, rulersKey } from '../library/dictionary'
import { readingText } from '../library/parts'
import { CONSOLE_DEPTH, hasConsole, roomConsole, type ConsoleGauge, type ConsoleSpec } from './console'

/**
 * The disk unit's room, the warehouse (2026-11-19; details the Owner's fifteen; shelves the Owner's word the
 * same day: "改成这个货架…尽量往上面靠"). The drives and volumes as they are now, held to the room standards
 * (frame/units/standards.ts):
 *   a shelf per drive   its form is its kind. Solid state: a flow rack, low lanes of rollers sloping to the
 *                       front, goods sliding down to the pick face (no waiting). Spinning: a horizontal
 *                       carousel, bins on a loop that must come round to the window (the platter turning; its
 *                       speed on its front). Kind not read: a plain shelf.
 *   bays and goods      the shelf's bays are the standard part (256 GB a bay, two deep), whole bays; goods
 *                       (amber) on them as far as the volume is used. Written beneath: the volume's name, used
 *                       and size; read-only a red chain, file system errors red tape.
 *   the end plate       beside each shelf: the model, the lathes' four folds lit as far as it is busy, goods
 *                       out (read) and in (written) a second, loads a second and time per load; the drive's own
 *                       thermometer on the shelf's end.
 *   staging floor       where a machine has several drives, which volume lies on which drive is not reported:
 *                       the shelves stand empty of goods and the volumes' bays lie on a marked floor at the
 *                       front, "SHELF NOT REPORTED", rather than on a guessed shelf.
 *   array boards        one per RAID array, low at the back left: its state, a lamp per drive, a strip as far
 *                       as it is in step; apart from the shelves, membership not being reported.
 *   the console         how full all the volumes are together on the desk; the drives' average busy.
 * Low and flat-topped: nothing hides anything.
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
const FRAME = faces('#c4ccd6', '#8f9bab', '#7a8697')
const BOARD = faces('#e6e8ec', '#c8ccd3', '#b3b8c0')
const PALLET = '#f2b03d', PALLET_EDGE = '#a8650f', RED = '#d83a32', LANE = '#b9c0c9', ROLLER = '#6b7280', BIN = '#d7dce2'
const LAMP = { active: '#4caf62', failed: RED, spare: '#9aa5b1' } as const

/**
 * A bay column's length along the shelf (two bays deep); the shelf's depth; the rows' pitch, which holds the
 * shelf, the line written beneath and the next row's rise (no shelf hides the line or the row behind).
 */
export const SHELF = { col: 0.4, depth: 0.6, label: 0.3, row: 1.35 }
/** Heights by form: back and front (a flow rack slopes to its pick face). */
export const FORM_HEIGHT = { flow: [7, 4], carousel: [5, 5], plain: [7, 7] } as const
/** The end plate beside each shelf; a shelf's length where its goods are not placed on it. */
export const PLATE = { w: 1.6, gap: 0.15, h: 3 }
const BARE_COLS = 3
/** A bay on the staging floor. */
const CELL = 0.25
const ROOMS: [number, number][] = [[4, 4], [4, 8], [8, 8]]
const FLOOR_TOP = CONSOLE_DEPTH + 0.15, LEFT = 0.35

type Form = keyof typeof FORM_HEIGHT
const formOf = (d: DriveReading | null): Form => d?.kind === 'ssd' ? 'flow' : d?.kind === 'hdd' ? 'carousel' : 'plain'

/** Whole bays for a volume, under the ruler as it stands (a bay is whole; what is on it fills it in proportion). */
export const baysOf = (v: VolumeReading) => v.size > 0 ? Math.max(1, Math.ceil(encode('disk.bays', v.size)! - 0.02)) : 0
const colsPerRow = (w: number) => Math.max(1, Math.floor((w - LEFT - 0.15 - PLATE.w - PLATE.gap) / SHELF.col))
const unitW = BARE_COLS * SHELF.col + PLATE.gap + PLATE.w
const unitsPerRow = (w: number) => Math.max(1, Math.floor((w - LEFT - 0.15 + 0.3) / (unitW + 0.3)))
const cellsPerRow = (w: number) => Math.max(1, Math.floor((w - LEFT - 0.2) / CELL))
const stagingDepth = (v: VolumeReading, w: number) => Math.ceil(baysOf(v) / cellsPerRow(w)) * CELL + SHELF.label + 0.1

/** One drive with its goods on its shelf: a row piece per run of bay columns. */
type Piece = { y: number; drive: DriveReading | null; cols: number; volume: VolumeReading | null; from: number; plate: boolean; x: number; labelled: boolean }
type Staged = { y: number; volume: VolumeReading }
type Plan = { pieces: Piece[]; staged: Staged[]; stagingTop: number | null; cut: number; depth: number }

function plan(r: DiskRoomReadings, [w, d]: [number, number]): Plan {
  const pieces: Piece[] = [], staged: Staged[] = []
  let y = FLOOR_TOP, cut = 0, stagingTop: number | null = null
  const limit = d - 0.2
  if (r.drives.length === 1 && r.volumes.length > 0) {
    // One drive: every volume read is on it. Its goods go on its shelf, in rows of whole bay columns.
    const K = colsPerRow(w)
    let first = true
    for (const v of r.volumes) {
      const cols = Math.ceil(baysOf(v) / 2), rows = Math.ceil(cols / K)
      if (y + rows * SHELF.row > limit + 1e-9) { cut++; continue }
      for (let k = 0; k < rows; k++) {
        // Its name is written under the last row of its bays.
        pieces.push({ y, drive: r.drives[0], cols: Math.min(K, cols - k * K), volume: v, from: k * K * 2, plate: first, x: LEFT, labelled: k === rows - 1 })
        first = false
        y += SHELF.row
      }
    }
  } else {
    // Several drives (or none read): the shelves bare of goods, the volumes on the staging floor.
    const P = unitsPerRow(w)
    r.drives.forEach((dr, i) => {
      const row = Math.floor(i / P)
      pieces.push({ y: y + row * SHELF.row, drive: dr, cols: BARE_COLS, volume: null, from: 0, plate: true, x: LEFT + (i % P) * (unitW + 0.3), labelled: false })
    })
    if (r.drives.length) y += Math.ceil(r.drives.length / P) * SHELF.row
    if (r.volumes.length) {
      stagingTop = y
      y += 0.3
      for (const v of r.volumes) {
        const sd = stagingDepth(v, w)
        if (y + sd > limit + 1e-9) { cut++; continue }
        staged.push({ y, volume: v })
        y += sd
      }
    }
  }
  return { pieces, staged, stagingTop, cut, depth: y + 0.2 }
}

export function diskRoomShape(r: DiskRoomReadings): [number, number] {
  return ROOMS.find(s => { const p = plan(r, s); return p.cut === 0 && p.depth <= s[1] + 1e-9 }) ?? ROOMS[ROOMS.length - 1]
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

  // The array boards, low at the back left, beside the console.
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

  // The shelves: one per drive (a row piece per run of bay columns when its goods are on it).
  for (const pc of p.pieces) {
    metaphor('drive.shelf')
    const v = pc.volume
    if (v) metaphor('disk.bays')
    const bays = v ? baysOf(v) : 0
    const share = v && v.used != null ? encode('vol.used', v.used / v.size) : null
    if (share != null) metaphor('vol.used')
    const filled = (share ?? 0) * bays - pc.from
    const n = v ? Math.min(pc.cols * 2, bays - pc.from) : null
    const L = pc.cols * SHELF.col
    things.push({ at: pc.x + pc.y, draw: () => drawShelf(g, pc.x, pc.y, pc.cols, formOf(pc.drive), n, filled, pc.drive) })
    if (pc.plate && pc.drive) { const d = pc.drive, px = pc.x + L + PLATE.gap; things.push({ at: px + pc.y, draw: () => drawPlate(g, px, pc.y, d) }) }
    if (v && pc.labelled) {
      const ly = pc.y + SHELF.depth + 0.06
      things.push({ at: pc.x + ly + 0.5, draw: () => volumeLine(g, pc.x, ly, v) })
    }
    if (v?.readonly) { metaphor('vol.readonly'); things.push({ at: pc.x + pc.y + 0.6, draw: () => g.floor(pc.x - 0.05, pc.y, FORM_HEIGHT[formOf(pc.drive)][0] + 0.3, `<path d="M0 ${(SHELF.depth * U).toFixed(1)} L${((L + 0.1) * U).toFixed(1)} 0" stroke="${RED}" stroke-width="1.6" stroke-dasharray="3 2"/>`) }) }
    if (v?.errors) { metaphor('vol.errors'); things.push({ at: pc.x + pc.y + 0.6, draw: () => g.floor(pc.x - 0.05, pc.y, FORM_HEIGHT[formOf(pc.drive)][0] + 0.3, `<path d="M0 0 L${((L + 0.1) * U).toFixed(1)} ${(SHELF.depth * U).toFixed(1)}" stroke="${RED}" stroke-width="3" opacity="0.85"/>`) }) }
  }

  // The staging floor: volumes whose drive is not reported, their bays laid out on a marked floor.
  if (p.stagingTop != null) {
    const top = p.stagingTop
    things.push({ at: top, draw: () => g.floor(LEFT, top, 3, `<text x="0" y="6" ${MONO} font-size="5.2" font-weight="700" fill="#8a857a">VOLUMES · SHELF NOT REPORTED</text>`) })
  }
  const C = cellsPerRow(fw)
  for (const s of p.staged) {
    const v = s.volume, bays = baysOf(v)
    metaphor('disk.bays')
    const share = v.used != null ? encode('vol.used', v.used / v.size) : null
    if (share != null) metaphor('vol.used')
    const filled = (share ?? 0) * bays, rows = Math.ceil(bays / C), c = CELL * U
    let svg = ''
    for (let b = 0; b < bays; b++) {
      const cx = (b % C) * c, cy = Math.floor(b / C) * c, on = clamp(filled - b)
      svg += `<rect x="${(cx + 0.5).toFixed(1)}" y="${(cy + 0.5).toFixed(1)}" width="${(c - 1).toFixed(1)}" height="${(c - 1).toFixed(1)}" fill="#e9e6de" stroke="#9f9a8f" stroke-width="0.5" stroke-dasharray="1.5 1"/>`
      if (on > 0.02) svg += `<rect x="${(cx + 1.4).toFixed(1)}" y="${(cy + 1.4).toFixed(1)}" width="${((c - 2.8) * on).toFixed(1)}" height="${(c - 2.8).toFixed(1)}" fill="${PALLET}" stroke="${PALLET_EDGE}" stroke-width="0.5"/>`
    }
    const W = Math.min(bays, C) * CELL, ly = s.y + rows * CELL + 0.04
    if (v.readonly) { metaphor('vol.readonly'); svg += `<path d="M0 ${(rows * c).toFixed(1)} L${(W * U).toFixed(1)} 0" stroke="${RED}" stroke-width="1.6" stroke-dasharray="3 2"/>` }
    if (v.errors) { metaphor('vol.errors'); svg += `<path d="M0 0 L${(W * U).toFixed(1)} ${(rows * c).toFixed(1)}" stroke="${RED}" stroke-width="3" opacity="0.85"/>` }
    things.push({ at: s.y, draw: () => { g.floor(LEFT, s.y, 3.4, svg); volumeLine(g, LEFT, ly, v) } })
  }
  if (p.cut) g.text('left', 0.4, 0.18, 47, `${p.cut} VOLUME${p.cut > 1 ? 'S' : ''} DO NOT FIT — SET MORE GB A BAY`, 5.5, '#b8433f', 700)

  things.sort((a, b) => a.at - b.at).forEach(t => t.draw())
  return g.done()
}

/** Written beneath a volume's bays: its name, used and size; READ ONLY and the error count in red. */
function volumeLine(g: Draw, x: number, y: number, v: VolumeReading) {
  const line = `${volumeName(v.name)}  ${readingText(v.used, 'bytes')} OF ${readingText(v.size, 'bytes')}`
  let svg = `<text x="0" y="7" ${MONO} font-size="5.6" font-weight="700" fill="#5d5a52">${esc(line)}</text>`
  // Red tags right after the line (a mono character is 0.6 of the size wide).
  const tags = [v.readonly ? 'READ ONLY' : null, v.errors ? `${Math.round(v.errors)} ERRORS` : null].filter(Boolean)
  if (tags.length) svg += `<text x="${((line.length + 2) * 5.6 * 0.6).toFixed(1)}" y="7" ${MONO} font-size="5.6" font-weight="700" fill="${RED}">${tags.join(' · ')}</text>`
  g.floor(x, y, 3, svg)
}

/**
 * A shelf of `cols` bay columns, two bays deep, in its form. `n` bays on it (null: the shelf drawn bare, its
 * goods not placed on it), `filled` of them holding goods in the form's order.
 */
function drawShelf(g: Draw, x: number, y: number, cols: number, form: Form, n: number | null, filled: number, d: DriveReading | null) {
  const L = cols * SHELF.col, D = SHELF.depth, [zb, zf] = FORM_HEIGHT[form]
  const z = (yy: number) => zb + (zf - zb) * (yy - y) / D
  const quad = (x0: number, x1: number, y0: number, y1: number, fill: string, sw = 0.5, stroke = INK, lift = 0) =>
    g.poly(fill, [[x0, y0, z(y0) + lift], [x1, y0, z(y0) + lift], [x1, y1, z(y1) + lift], [x0, y1, z(y1) + lift]], sw, stroke)
  g.shadow(x, y, L, D, 0.2)
  // The frame: front face, end (a trapezoid where it slopes), top.
  g.poly(FRAME.left, [[x, y + D, 0], [x + L, y + D, 0], [x + L, y + D, zf], [x, y + D, zf]], 1)
  g.poly(FRAME.right, [[x + L, y, 0], [x + L, y + D, 0], [x + L, y + D, zf], [x + L, y, zb]], 1)
  g.poly(form === 'carousel' ? '#cdd3da' : FRAME.top, [[x, y, zb], [x + L, y, zb], [x + L, y + D, zf], [x, y + D, zf]], 1)
  // Where bay b sits: its column and its slot (0 back, 1 front), in the order goods fill it.
  const at = (b: number): [number, number] => {
    if (form === 'flow') return [Math.floor(b / 2), b % 2 === 0 ? 1 : 0] // slid to the pick face first
    return [Math.floor(b / 2), b % 2]
  }
  const slotY = (s: number): [number, number] => s === 0 ? [y + 0.04, y + D / 2 - 0.02] : [y + D / 2 + 0.02, y + D - 0.04]
  if (form === 'flow') {
    // Lanes of rollers from back to front, one per bay column.
    for (let c = 0; c < cols; c++) {
      const xa = x + c * SHELF.col + 0.04, xb = x + (c + 1) * SHELF.col - 0.04
      quad(xa, xb, y + 0.02, y + D - 0.02, LANE, 0.5)
      for (let yy = y + 0.07; yy < y + D - 0.03; yy += 0.07) g.line([xa, yy, z(yy)], [xb, yy, z(yy)], ROLLER, 0.6)
    }
  } else if (form === 'carousel') {
    // The loop: a track round the shelf (a ring with rounded ends), the bins riding on it in a ring, filled
    // round the loop; the window they come round to at the front middle.
    const rc = Math.max(0.06, Math.min(D, L) / 2 - 0.15), cx0 = x + Math.max(D / 2, L / 2 > D / 2 ? D / 2 : L / 2), cx1 = x + L - (cx0 - x), my = y + D / 2
    const stadium = (r: number) => {
      const q: [number, number, number][] = []
      for (let k = 0; k <= 12; k++) { const t = -Math.PI / 2 + Math.PI * k / 12; q.push([cx1 + r * Math.cos(t), my + r * Math.sin(t), zb]) }
      for (let k = 0; k <= 12; k++) { const t = Math.PI / 2 + Math.PI * k / 12; q.push([cx0 + r * Math.cos(t), my + r * Math.sin(t), zb]) }
      return q
    }
    g.poly('#7d8794', stadium(rc + 0.1), 0.6)
    g.poly('#cdd3da', stadium(Math.max(0.01, rc - 0.1)), 0.5)
    const straight = cx1 - cx0, P = 2 * straight + 2 * Math.PI * rc
    const along = (sd: number): [number, number] => {
      if (sd < straight) return [cx0 + sd, my - rc]
      sd -= straight
      if (sd < Math.PI * rc) { const t = -Math.PI / 2 + sd / rc; return [cx1 + rc * Math.cos(t), my + rc * Math.sin(t)] }
      sd -= Math.PI * rc
      if (sd < straight) return [cx1 - sd, my + rc]
      sd -= straight
      const t = Math.PI / 2 + sd / rc; return [cx0 + rc * Math.cos(t), my + rc * Math.sin(t)]
    }
    const N = n ?? 2 * cols + 2, hs = Math.min(0.075, P / N / 2.6)
    for (let k = 0; k < N; k++) {
      const [bx, by] = along((k + 0.5) * P / N)
      g.poly(BIN, [[bx - hs, by - hs, zb + 0.3], [bx + hs, by - hs, zb + 0.3], [bx + hs, by + hs, zb + 0.3], [bx - hs, by + hs, zb + 0.3]], 0.5, n == null ? '#9aa5b1' : '#5d6878')
      const on = n == null ? 0 : clamp(filled - k)
      if (on > 0.02) g.poly(PALLET, [[bx - hs + 0.015, by - hs + 0.015, zb + 1.6], [bx - hs + 0.015 + (2 * hs - 0.03) * on, by - hs + 0.015, zb + 1.6], [bx - hs + 0.015 + (2 * hs - 0.03) * on, by + hs - 0.015, zb + 1.6], [bx - hs + 0.015, by + hs - 0.015, zb + 1.6]], 0.5, PALLET_EDGE)
    }
    // The window at the front middle, where a bin must come round to be picked.
    g.poly('#3b3f48', [[x + L / 2 - 0.06, y + D - 0.015, zb], [x + L / 2 + 0.06, y + D - 0.015, zb], [x + L / 2 + 0.06, y + D - 0.015, zb + 2.5], [x + L / 2 - 0.06, y + D - 0.015, zb + 2.5]], 0.5)
  }
  // The bays and their goods (a carousel's ride on its loop, above).
  const N = form === 'carousel' ? 0 : n ?? (form === 'flow' ? 0 : cols * 2)
  for (let b = 0; b < N; b++) {
    const [c, s] = at(b), [ya, yb] = slotY(s)
    const xa = x + c * SHELF.col + 0.05, xb = x + (c + 1) * SHELF.col - 0.05
    if (form !== 'flow') quad(xa, xb, ya, yb, '#dde3ea', 0.5, n == null ? '#9aa5b1' : '#7a8697', 0.2)
    const on = n == null ? 0 : clamp(filled - b)
    if (on > 0.02) quad(xa + 0.02, xa + 0.02 + (xb - xa - 0.04) * on, ya + 0.02, yb - 0.02, PALLET, 0.5, PALLET_EDGE, 1.5)
  }
  // A spinning drive's speed on the carousel's front.
  if (form === 'carousel' && d?.rpm != null && L >= 0.8) { metaphor('drive.rpm'); g.text('left', x + 0.06, y + D, 0.6, `${Math.round(d.rpm)} RPM`, 3.6, '#fbfaf6', 700) }
  // The drive's own temperature: the lathes' small thermometer on the shelf's end.
  if (d?.temp != null) {
    metaphor('drive.temp')
    const t = d.temp, top = zb - 1
    g.onRight(x + L, y, 0.05, 0.13, 0.8, top, '#fbfaf6', 0.5)
    g.onRight(x + L, y, 0.06, 0.12, 0.8, 0.8 + (top - 0.8) * encode('drive.temp', t / 100)!, t >= 80 ? '#e0533d' : '#e88a3a', 0)
  }
}

/** The end plate beside a shelf: the model (and a spinning drive's speed), how busy, goods out and in, loads a second and time per load. */
function drawPlate(g: Draw, x: number, y: number, d: DriveReading) {
  const W = PLATE.w * U
  g.shadow(x, y, PLATE.w, SHELF.depth, 0.15)
  g.box(x, y, PLATE.w, SHELF.depth, PLATE.h, BOARD, 0, 1)
  const name = (d.model ?? d.device).toUpperCase(), short = name.length > 20 ? name.slice(0, 19) + '…' : name
  let svg = `<text x="2.5" y="5.4" ${MONO} font-size="4" font-weight="700" fill="${INK}">${esc(short)}</text>`
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
