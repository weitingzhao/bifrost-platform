import { Draw, faces, INK, type Sprite } from '../kit'
import { encode, metaphor, rulerOf, rulersKey } from '../library/dictionary'
import { readingText } from '../library/parts'
import { CONSOLE_DEPTH, hasConsole, roomConsole, type ConsoleSpec } from './console'

/**
 * The memory unit's room, the workbench (a computer is a factory, the Owner 2026-11-19). The memory as it
 * is now, held to the room standards (frame/units/standards.ts):
 *   the bench        laid in the standard part, a section per 4 GB (the ruler), in pairs, rows spread over
 *                    the floor; the last section as long as its part. Its top shows what is on it now, side
 *                    by side from the left: the system's fixtures, work in hand, packed boxes, copies that
 *                    can be cleared, parts waiting for the warehouse, what the readings do not break down
 *                    (hatched, never guessed), and the bare top. Damage taped off in red.
 *   the overflow     a corner marked on the floor at the front, as long as the swap in the same sections;
 *                    crates in it as far as the swap is used
 *   the console      the room console: the bench's share in use on the desk; the overflow traffic
 * Flat on the tops: nothing rises above a bench but its top; nothing hides anything.
 */

export type MemoryRoomReadings = {
  total: number | null; used: number | null; free: number | null; cache: number | null; compressed: number | null
  kernel: number | null; dirty: number | null; swapTotal: number | null; swapUsed: number | null; swapIO: number | null; corrupted: number | null
}

/** What lies on the bench now, in order from the left, in bytes; clipped to what is installed. */
export type Stretch = { id: 'kernel' | 'work' | 'compressed' | 'cache' | 'dirty' | 'unbroken' | 'free'; bytes: number }
export function benchOf(r: MemoryRoomReadings): Stretch[] {
  if (!r.total) return []
  const z = (v: number | null) => Math.max(0, v ?? 0)
  const kernel = z(r.kernel), compressed = z(r.compressed), dirty = z(r.dirty)
  const work = r.used == null ? 0 : Math.max(0, r.used - kernel - compressed)
  const cache = Math.max(0, z(r.cache) - dirty), free = z(r.free)
  const named: Stretch[] = [{ id: 'kernel', bytes: kernel }, { id: 'work', bytes: work }, { id: 'compressed', bytes: compressed }, { id: 'cache', bytes: cache }, { id: 'dirty', bytes: dirty }]
  const out: Stretch[] = []
  let left = r.total
  for (const s of named) { const b = Math.min(s.bytes, left); if (b > 0) { out.push({ id: s.id, bytes: b }); left -= b } }
  const freeB = Math.min(free, left)
  if (left - freeB > 0.001 * r.total) out.push({ id: 'unbroken', bytes: left - freeB })
  if (freeB > 0) out.push({ id: 'free', bytes: freeB })
  return out
}

const ROW_OF: Record<Stretch['id'], string> = { kernel: 'mem.kernel', work: 'mem.work', compressed: 'mem.compressed', cache: 'mem.cache', dirty: 'mem.dirty', unbroken: 'mem.unbroken', free: 'mem.free' }
const FILL: Record<Stretch['id'], string> = { kernel: 'url(#mk)', work: 'url(#mw)', compressed: 'url(#mc)', cache: 'url(#mh)', dirty: 'url(#md)', unbroken: 'url(#mu)', free: 'none' }
const PATTERNS = '<defs>' +
  '<pattern id="mk" width="8" height="8" patternUnits="userSpaceOnUse"><rect width="8" height="8" fill="#8b929c"/><rect x="1.5" y="2" width="5" height="4" fill="#5d636c"/></pattern>' +
  '<pattern id="mw" width="7" height="7" patternUnits="userSpaceOnUse"><rect width="7" height="7" fill="#f2b03d"/><circle cx="3.5" cy="3.5" r="2" fill="#c98a1e"/></pattern>' +
  '<pattern id="mc" width="6" height="6" patternUnits="userSpaceOnUse"><rect width="6" height="6" fill="#6f9bd1"/><path d="M0 0H6V6" fill="none" stroke="#4d77ab" stroke-width="0.8"/></pattern>' +
  '<pattern id="mh" width="6" height="8" patternUnits="userSpaceOnUse"><rect width="6" height="8" fill="#efe6cf"/><path d="M1 2H5M1 4H5M1 6H4" stroke="#c9bfa4" stroke-width="0.6"/></pattern>' +
  '<pattern id="md" width="6" height="6" patternUnits="userSpaceOnUse"><rect width="6" height="6" fill="#79b37d"/><circle cx="3" cy="3" r="1.2" fill="#4e8a53"/></pattern>' +
  '<pattern id="mu" width="5" height="5" patternUnits="userSpaceOnUse"><rect width="5" height="5" fill="#cfc3ad"/><path d="M0 5L5 0" stroke="#9a8f7a" stroke-width="0.7"/></pattern>' +
  '</defs>'

const U = 35.8
const WALL = faces('#efe9dc', '#e2dac8', '#cfc6b2')
const BENCH = faces('#d2a76c', '#a98352', '#8f6c44'), LEG = faces('#6b6f78', '#555962', '#474a52')
/** A bench section's footprint and height, and the rows' pitch: a bench rises BENCH_H, flat on top. */
export const SECTION = { len: 0.66, depth: 0.45, height: 10, pairGap: 0.16, row: 0.75 }
const ROOMS: [number, number][] = [[4, 4], [4, 8], [8, 8]]
const CORNER_D = 0.8

const perRow = (w: number) => { let pairs = 1; while ((pairs * 2) * (2 * SECTION.len + SECTION.pairGap) <= w - 0.6 + 1e-9) pairs *= 2; return pairs * 2 }
const rowsFit = (d: number, corner: boolean) => Math.floor((d - CONSOLE_DEPTH - 0.4 - (corner ? CORNER_D : 0)) / SECTION.row)

/** How many sections a memory of these readings is laid in, under the ruler as it stands (the last may be part of one). */
export const sectionsOf = (r: MemoryRoomReadings) => r.total ? encode('mem.sections', r.total)! : 0

export function memoryRoomShape(r: MemoryRoomReadings): [number, number] {
  const n = Math.ceil(sectionsOf(r) - 1e-9), corner = !!r.swapTotal
  return ROOMS.find(([w, d]) => Math.ceil(n / perRow(w)) <= rowsFit(d, corner)) ?? ROOMS[ROOMS.length - 1]
}

export function memoryRoom(r: MemoryRoomReadings): Sprite {
  const [fw, fd] = memoryRoomShape(r)
  const g = new Draw(fw, fd, 56)
  g.add(PATTERNS)
  g.box(0, 0, fw, fd, 3, faces('#d8d6cf', '#bdbab1', '#aaa79e'), 0, 1.2)
  g.box(0, 0, fw, 0.18, 46, WALL, 0, 1.2)
  g.box(0, 0.18, 0.18, fd - 0.18, 46, WALL, 0, 1.2)

  // The console: what is in use, as a share of what is installed, on the desk; the overflow traffic.
  const share = r.total && r.used != null ? r.used / r.total : null
  const io = r.swapIO
  const spec: ConsoleSpec = {
    name: 'MEMORY CONSOLE',
    desk: r.total ? { row: 'mem.inuse', share: encode('mem.inuse', share), plate: `IN USE ${readingText(share, 'share')}` } : undefined,
    gauges: io != null && r.swapTotal ? [{ row: 'swap.io', name: 'TRAFFIC', share: encode('swap.io', io / 10e6), redFrom: 0.5, reading: readingText(io, 'bytes/s') }] : [],
  }
  if (hasConsole(spec)) roomConsole(g, fw, spec)

  type Thing = { at: number; draw: () => void }
  const things: Thing[] = []
  const n = sectionsOf(r), count = Math.ceil(n - 1e-9)
  if (count > 0) {
    metaphor('mem.sections')
    const each = rulerOf('mem.sections')
    const stretches = benchOf(r)
    for (const s of stretches) metaphor(ROW_OF[s.id])
    if (r.corrupted) metaphor('mem.corrupted')
    const K = perRow(fw), rows = Math.ceil(count / K), corner = !!r.swapTotal
    const floorTop = CONSOLE_DEPTH + 0.25, floorBot = fd - 0.3 - (corner ? CORNER_D : 0)
    const slotD = Math.min(1.25, (floorBot - floorTop) / rows), y0 = floorTop + ((floorBot - floorTop) - rows * slotD) / 2
    const pairs = K / 2, slotW = (fw - 0.7) / pairs, pairW = 2 * SECTION.len + SECTION.pairGap
    for (let i = 0; i < count; i++) {
      const col = i % K, row = Math.floor(i / K)
      const len = SECTION.len * Math.min(1, n - i)
      const x = 0.35 + Math.floor(col / 2) * slotW + (slotW - pairW) / 2 + (col % 2) * (SECTION.len + SECTION.pairGap)
      const y = y0 + row * slotD + (slotD - SECTION.depth) / 2
      const from = i * each, to = from + each * Math.min(1, n - i)
      things.push({ at: x + y, draw: () => {
        // A workbench, not a crate (crates are the warehouse's): a top on four legs, open beneath.
        g.shadow(x, y, len, SECTION.depth, 0.2)
        const L = 0.06, T = 2.5, legs: [number, number][] = [[x + 0.03, y + 0.03], [x + len - 0.03 - L, y + 0.03], [x + 0.03, y + SECTION.depth - 0.03 - L], [x + len - 0.03 - L, y + SECTION.depth - 0.03 - L]]
        for (const [lx, ly] of legs) g.box(lx, ly, L, L, SECTION.height - T, LEG, 0, 0.6)
        g.box(x, y, len, SECTION.depth, T, BENCH, SECTION.height - T, 1.1)
        // What lies on this section's top: each stretch of the bench that falls on it.
        let svg = '', at = 0
        for (const s of stretches) {
          const a = Math.max(at, from), b = Math.min(at + s.bytes, to)
          if (b > a && s.id !== 'free') svg += `<rect x="${((a - from) / each * SECTION.len * U).toFixed(2)}" y="1.5" width="${((b - a) / each * SECTION.len * U).toFixed(2)}" height="${(SECTION.depth * U - 3).toFixed(2)}" fill="${FILL[s.id]}"/>`
          at += s.bytes
        }
        if (r.corrupted && r.total) {
          const bad = r.total - r.corrupted
          if (to > bad) { const a = Math.max(from, bad); const u0 = (a - from) / each * SECTION.len * U, u1 = (to - from) / each * SECTION.len * U, h = SECTION.depth * U
            svg += `<path d="M${u0} 0L${u1} ${h}M${u0} ${h}L${u1} 0" stroke="#d83a32" stroke-width="2"/>` }
        }
        g.floor(x, y, SECTION.height, svg + `<rect x="0" y="0" width="${(len * U).toFixed(2)}" height="${(SECTION.depth * U).toFixed(2)}" fill="none" stroke="${INK}" stroke-width="0.6"/>`)
      } })
    }
    // The overflow corner: marked floor at the front, as long as the swap in the same sections; crates in
    // it as far as the swap is used. Crates are low, so they hide nothing.
    if (corner) {
      metaphor('swap.room')
      const sec = encode('mem.sections', r.swapTotal)!, cl = Math.min(fw - 0.6, sec * SECTION.len), cy = fd - 0.3 - CORNER_D + 0.25
      g.floor(0.3, cy, 3, `<rect x="0" y="0" width="${(cl * U).toFixed(1)}" height="${(0.5 * U).toFixed(1)}" fill="none" stroke="#7a7568" stroke-width="1.6" stroke-dasharray="5 3"/>` +
        `<text x="2" y="${(0.5 * U + 9).toFixed(1)}" font-family="'JetBrains Mono',ui-monospace,Menlo,monospace" font-size="6.5" font-weight="700" fill="#7a7568">OVERFLOW ${readingText(r.swapUsed, 'bytes')} / ${readingText(r.swapTotal, 'bytes')}</text>`)
      if (r.swapUsed != null) {
        metaphor('swap.used')
        const f = encode('swap.used', r.swapUsed / r.swapTotal!)!, slots = Math.max(1, Math.floor(cl / 0.24)), full = f * slots
        for (let k = 0; k < Math.ceil(full - 1e-9); k++) {
          const w = 0.2 * Math.min(1, full - k), cx = 0.34 + k * 0.24
          things.push({ at: cx + cy + 0.1, draw: () => g.box(cx, cy + 0.12, w, 0.26, 4, faces('#c9a36a', '#a5824d', '#8e6f40'), 3, 0.7) })
        }
      }
    }
  } else g.text('left', 0.6, 0.18, 30, 'MEMORY NOT READ', 7, '#8a857a')
  things.sort((a, b) => a.at - b.at).forEach(t => t.draw())
  return g.done()
}

/** The key the room's picture is kept under: every reading to the step it is drawn at. */
export const memoryRoomKey = (r: MemoryRoomReadings) =>
  `g2:memroom:${rulersKey()}:${Object.values(r).map(v => v == null ? 'n' : Math.round(v / 1e7)).join(',')}`
