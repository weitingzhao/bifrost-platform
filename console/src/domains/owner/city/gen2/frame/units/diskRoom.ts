import { Draw, faces, INK, type Sprite } from '../kit'
import { encode, metaphor, rulerOf, rulersKey } from '../library/dictionary'
import { readingText } from '../library/parts'
import { CONSOLE_DEPTH, hasConsole, roomConsole, type ConsoleGauge, type ConsoleSpec } from './console'

/**
 * The disk unit's room, the warehouse (2026-11-19; details the Owner's fifteen). The drives and volumes as
 * they are now, held to the room standards (frame/units/standards.ts):
 *   storage areas   one per volume: low steel racks of standard bays (256 GB a bay, eight to a rack), whole
 *                   bays; pallets on them from the left as far as the volume is used. Written beneath: its
 *                   name, used and size. Read-only: a red chain across it; file system errors: red tape.
 *   loading docks   one per drive, along the front: a roller conveyor for solid state, a turntable for a
 *                   spinning drive (its speed written on it), plain when the kind is not read. Along its
 *                   front the lathes' four folds, lit as far as it is busy; on its plate the model, goods out
 *                   (read) and in (written) a second, loads a second and time per load; the drive's own
 *                   thermometer on its right side.
 *   array boards    one per RAID array, low at the back left: its state, a lamp per drive (active, failed,
 *                   spare) and a strip as far as it is in step. Which drives and volumes an array holds is not
 *                   reported, so a board stands apart from the docks and areas rather than fencing them.
 *   the console     how full all the volumes are together on the desk; the drives' average busy.
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
const RACK = faces('#c4ccd6', '#8f9bab', '#7a8697')
const CONCRETE = faces('#d4d0c6', '#b3aea3', '#9f9a8f')
const BOARD = faces('#e6e8ec', '#c8ccd3', '#b3b8c0')
const PALLET = '#f2b03d', SHELF = '#dde3ea', RED = '#d83a32'
const LAMP = { active: '#4caf62', failed: RED, spare: '#9aa5b1' } as const

/** A bay's length along its rack; a rack's depth (two bays back to back) and height; the rows' pitch; the line written under an area. */
export const BAY = { len: 0.3, depth: 0.6, height: 6, row: 1.0, label: 0.3 }
/** A dock's footprint and height (a loading dock stands at a truck's bed, higher than a rack). */
export const DOCK = { w: 1.3, d: 1.2, h: 7, gap: 0.15 }
const ROOMS: [number, number][] = [[4, 4], [4, 8], [8, 8]]
const FLOOR_TOP = CONSOLE_DEPTH + 0.15, RACK_GAP = 0.2

const rackLen = () => 4 * BAY.len
/** Racks a row: pairs and powers of two, as many as the width holds. */
export const racksPerRow = (w: number) => { let n = 1; while ((n * 2) * rackLen() + (n * 2 - 1) * RACK_GAP <= w - 0.7 + 1e-9) n *= 2; return n }
const docksPerRow = (w: number) => Math.max(1, Math.floor((w - 0.6 + DOCK.gap) / (DOCK.w + DOCK.gap)))

/** Whole bays for a volume, under the ruler as it stands (a shelf is whole; what is on it fills it in proportion). */
export const baysOf = (v: VolumeReading) => v.size > 0 ? Math.max(1, Math.ceil(encode('disk.bays', v.size)! - 0.02)) : 0
const rowsOfVolume = (v: VolumeReading, w: number) => Math.ceil(Math.ceil(baysOf(v) / 8) / racksPerRow(w))
/** An area's depth: its rows (the last needs only its rack), and the line written beneath. */
const areaDepth = (rows: number) => (rows - 1) * BAY.row + BAY.depth + BAY.label

function depthNeeded(r: DiskRoomReadings, [w]: [number, number]) {
  const areas = r.volumes.reduce((a, v) => a + areaDepth(rowsOfVolume(v, w)) + 0.1, 0)
  const docks = r.drives.length ? Math.ceil(r.drives.length / docksPerRow(w)) * (DOCK.d + DOCK.gap) : 0
  return FLOOR_TOP + areas + docks + 0.25
}

export function diskRoomShape(r: DiskRoomReadings): [number, number] {
  return ROOMS.find(s => depthNeeded(r, s) <= s[1] + 1e-9) ?? ROOMS[ROOMS.length - 1]
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
        const sw = W - 5, f = encode('array.sync', a.sync)!
        svg += `<rect x="2.5" y="${(D - 6).toFixed(1)}" width="${sw.toFixed(1)}" height="3" fill="#fbfaf6" stroke="${INK}" stroke-width="0.5"/><rect x="2.5" y="${(D - 6).toFixed(1)}" width="${(sw * f).toFixed(1)}" height="3" fill="#4caf62"/>`
        svg += `<text x="${(W - 2.5).toFixed(1)}" y="12.8" text-anchor="end" ${MONO} font-size="4.6" fill="${INK}">IN STEP ${readingText(a.sync, 'share')}</text>`
      }
      g.floor(x, y, 3, svg)
    } })
  })

  // The storage areas, one per volume, biggest first, each in rows of racks.
  const K = racksPerRow(fw)
  let y = FLOOR_TOP
  const docksTop = fd - 0.25 - (r.drives.length ? Math.ceil(r.drives.length / docksPerRow(fw)) * (DOCK.d + DOCK.gap) : 0)
  let cut = 0
  for (const v of r.volumes) {
    const bays = baysOf(v), rows = rowsOfVolume(v, fw)
    if (y + areaDepth(rows) > docksTop + 1e-9) { cut++; continue }
    metaphor('disk.bays')
    const share = v.used != null ? encode('vol.used', v.used / v.size) : null
    if (share != null) metaphor('vol.used')
    const filled = (share ?? 0) * bays, rowW = K * rackLen() + (K - 1) * RACK_GAP, x0 = (fw - rowW) / 2 + 0.05
    const racks = Math.ceil(bays / 8)
    for (let k = 0; k < racks; k++) {
      const n = Math.min(8, bays - k * 8), len = Math.ceil(n / 2) * BAY.len
      const x = x0 + (k % K) * (rackLen() + RACK_GAP), ry = y + Math.floor(k / K) * BAY.row
      things.push({ at: x + ry, draw: () => {
        // A low steel rack: its frame, the uprights at its ends, the shelf on top with its bays; pallets on them.
        g.shadow(x, ry, len, BAY.depth, 0.2)
        g.box(x, ry, len, BAY.depth, BAY.height, RACK, 0, 1)
        g.onLeft(x, ry + BAY.depth, 0, 0.03, 0, BAY.height, '#5d6878', 0)
        g.onLeft(x, ry + BAY.depth, len - 0.03, len, 0, BAY.height, '#5d6878', 0)
        const bw = BAY.len * U, D = BAY.depth * U
        let svg = `<rect x="0" y="0" width="${(len * U).toFixed(1)}" height="${D.toFixed(1)}" fill="${SHELF}"/>`
        // Eight bays a rack, four along and two back to back; filled a column at a time from the left.
        const h = D / 2
        for (let b = 0; b < n; b++) {
          const c = Math.floor(b / 2), side = b % 2, on = Math.max(0, Math.min(1, filled - (k * 8 + b)))
          svg += `<rect x="${(c * bw + 0.5).toFixed(1)}" y="${(side * h + 0.5).toFixed(1)}" width="${(bw - 1).toFixed(1)}" height="${(h - 1).toFixed(1)}" fill="none" stroke="#7a8697" stroke-width="0.5"/>`
          if (on > 0.02) svg += `<rect x="${(c * bw + 1.6).toFixed(1)}" y="${(side * h + 1.6).toFixed(1)}" width="${((bw - 3.2) * on).toFixed(1)}" height="${(h - 3.2).toFixed(1)}" fill="${PALLET}" stroke="#a8650f" stroke-width="0.5"/>`
        }
        svg += `<rect x="0" y="0" width="${(len * U).toFixed(1)}" height="${D.toFixed(1)}" fill="none" stroke="${INK}" stroke-width="0.6"/>`
        g.floor(x, ry, BAY.height, svg)
      } })
    }
    // Written beneath the area: its name, used and size; a red chain if read-only; red tape if it has errors.
    const ah = areaDepth(rows) - BAY.label, ly = y + ah + 0.1, aw = rowW
    const line = `${esc(volumeName(v.name))}  ${readingText(v.used, 'bytes')} OF ${readingText(v.size, 'bytes')}`
    let svg = `<text x="0" y="7" ${MONO} font-size="5.6" font-weight="700" fill="#5d5a52">${line}</text>`
    if (v.readonly) { metaphor('vol.readonly'); svg += `<text x="${(aw * U).toFixed(1)}" y="7" text-anchor="end" ${MONO} font-size="5.6" font-weight="700" fill="${RED}">READ ONLY</text>` }
    if (v.errors) { metaphor('vol.errors'); svg += `<text x="${(aw * U * (v.readonly ? 0.62 : 1)).toFixed(1)}" y="7" text-anchor="end" ${MONO} font-size="5.6" font-weight="700" fill="${RED}">${Math.round(v.errors)} ERRORS</text>` }
    things.push({ at: x0 + aw + ly, draw: () => g.floor(x0, ly, 3, svg) })
    if (v.readonly) g.floor(x0 - 0.05, y, 3.2, `<path d="M0 ${(ah * U).toFixed(1)} L${((aw + 0.1) * U).toFixed(1)} ${(ah * U).toFixed(1)}" stroke="${RED}" stroke-width="1.6" stroke-dasharray="3 2"/>`)
    if (v.errors) things.push({ at: x0 + aw + y + ah, draw: () => g.floor(x0 - 0.05, y, BAY.height + 0.2, `<path d="M0 0 L${((aw + 0.1) * U).toFixed(1)} ${(ah * U).toFixed(1)}" stroke="${RED}" stroke-width="3" opacity="0.85"/>`) })
    y += areaDepth(rows) + 0.1
  }
  if (cut) g.text('left', 0.4, 0.18, 47, `${cut} VOLUME${cut > 1 ? 'S' : ''} DO NOT FIT — SET MORE GB A BAY`, 5.5, '#b8433f', 700)

  // The loading docks along the front, one per drive.
  const P = docksPerRow(fw)
  r.drives.forEach((d, i) => {
    metaphor('drive.dock')
    const inRow = Math.min(P, r.drives.length - Math.floor(i / P) * P), span = inRow * DOCK.w + (inRow - 1) * DOCK.gap
    const x = (fw - span) / 2 + 0.05 + (i % P) * (DOCK.w + DOCK.gap), dy = docksTop + Math.floor(i / P) * (DOCK.d + DOCK.gap)
    things.push({ at: x + dy, draw: () => drawDock(g, x, dy, d) })
  })

  things.sort((a, b) => a.at - b.at).forEach(t => t.draw())
  return g.done()
}

function drawDock(g: Draw, x: number, y: number, d: DriveReading) {
  const W = DOCK.w * U, D = DOCK.d * U
  g.shadow(x, y, DOCK.w, DOCK.d, 0.25)
  g.box(x, y, DOCK.w, DOCK.d, DOCK.h, CONCRETE, 0, 1.1)
  // The bumper along its front.
  g.onLeft(x, y + DOCK.d, 0.1, DOCK.w - 0.1, DOCK.h - 2.2, DOCK.h - 0.6, '#3b3f48', 0.5)
  let svg = ''
  // What it is: a roller conveyor (solid state), a turntable (spinning), nothing when not read.
  if (d.kind === 'ssd') {
    svg += `<rect x="3" y="2.5" width="${(W - 6).toFixed(1)}" height="7" fill="#b9c0c9" stroke="${INK}" stroke-width="0.5"/>`
    for (let k = 1; k * 3.2 < W - 6; k++) svg += `<line x1="${(3 + k * 3.2).toFixed(1)}" y1="2.5" x2="${(3 + k * 3.2).toFixed(1)}" y2="9.5" stroke="#6b7280" stroke-width="0.7"/>`
  } else if (d.kind === 'hdd') {
    svg += `<circle cx="9" cy="6.5" r="4.8" fill="#b9c0c9" stroke="${INK}" stroke-width="0.6"/><circle cx="9" cy="6.5" r="1" fill="#6b7280"/>`
    for (let k = 0; k < 4; k++) { const a = k * Math.PI / 4; svg += `<line x1="${(9 - 4.3 * Math.cos(a)).toFixed(1)}" y1="${(6.5 - 4.3 * Math.sin(a)).toFixed(1)}" x2="${(9 + 4.3 * Math.cos(a)).toFixed(1)}" y2="${(6.5 + 4.3 * Math.sin(a)).toFixed(1)}" stroke="#6b7280" stroke-width="0.5"/>` }
    if (d.rpm != null) { metaphor('drive.rpm'); svg += `<text x="16" y="8.3" ${MONO} font-size="4.6" font-weight="700" fill="${INK}">${Math.round(d.rpm)} RPM</text>` }
  }
  // How busy: the lathes' four folds, lit from the left.
  if (d.busy != null) {
    metaphor('drive.busy')
    const b = encode('drive.busy', d.busy)!, N = 4, lit = b * N, glow = b > 0.85 ? '#ff7a45' : '#ffc94d', fw = W - 6
    for (let k = 0; k < N; k++) {
      const u0 = 3 + (k / N) * fw, w = fw / N, on = Math.max(0, Math.min(1, lit - k))
      svg += `<rect x="${u0.toFixed(1)}" y="11" width="${w.toFixed(1)}" height="3.5" fill="${k % 2 ? '#4a564f' : '#55625a'}"/>`
      if (on > 0.02) svg += `<rect x="${u0.toFixed(1)}" y="11" width="${(w * on).toFixed(1)}" height="3.5" fill="${glow}"/>`
    }
    svg += `<rect x="3" y="11" width="${fw.toFixed(1)}" height="3.5" fill="none" stroke="${INK}" stroke-width="0.5"/>`
  }
  // The plate: the model, goods out and in, loads a second and time per load.
  const name = (d.model ?? d.device).toUpperCase(), short = name.length > 18 ? name.slice(0, 17) + '…' : name
  const lines = [short]
  if (d.read != null || d.write != null) { metaphor('drive.traffic'); lines.push(`OUT ${readingText(d.read, 'bytes/s')}`, `IN ${readingText(d.write, 'bytes/s')}`) }
  const ops = d.ops != null ? (metaphor('drive.ops'), readingText(d.ops, 'per second')) : null
  const lat = d.latency != null ? (metaphor('drive.latency'), readingText(d.latency, 'seconds')) : null
  if (ops || lat) lines.push([ops, lat].filter(Boolean).join(' · '))
  svg += `<rect x="2" y="16" width="${(W - 4).toFixed(1)}" height="${(D - 17.5).toFixed(1)}" fill="#fbfaf6" stroke="${INK}" stroke-width="0.5"/>`
  lines.forEach((l, k) => { svg += `<text x="3.5" y="${(21.5 + k * 5.6).toFixed(1)}" ${MONO} font-size="${k ? 4.2 : 4.3}" font-weight="${k ? 600 : 700}" fill="${INK}">${esc(l)}</text>` })
  g.floor(x, y, DOCK.h, svg)
  // The drive's own temperature: the lathes' small thermometer, on the dock's right side.
  if (d.temp != null) {
    metaphor('drive.temp')
    const t = d.temp
    g.onRight(x + DOCK.w, y, 0.04, 0.12, 1, DOCK.h - 1, '#fbfaf6', 0.5)
    g.onRight(x + DOCK.w, y, 0.05, 0.11, 1, 1 + (DOCK.h - 3) * encode('drive.temp', t / 100)!, t >= 80 ? '#e0533d' : '#e88a3a', 0)
  }
}

/** The key the room's picture is kept under: every reading to the step it is drawn at. */
export const diskRoomKey = (r: DiskRoomReadings) =>
  `g2:diskroom:${rulersKey()}:${rulerOf('disk.bays')}:${JSON.stringify(r).length}:${r.drives.map(d => [d.device, Math.round((d.busy ?? -1) * 100), Math.round((d.read ?? -1) / 1e4), Math.round((d.write ?? -1) / 1e4)].join('/')).join(',')}:${r.volumes.map(v => Math.round((v.used ?? -1) / 1e9)).join(',')}`
