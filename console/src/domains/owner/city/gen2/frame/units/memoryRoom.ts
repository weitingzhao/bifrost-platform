import { Draw, faces, type Sprite } from '../kit'
import { encode, metaphor, rulersKey } from '../library/dictionary'
import { readingText } from '../library/parts'
import { CONSOLE_DEPTH, hasConsole, roomConsole, type ConsoleSpec } from './console'

/**
 * The memory unit's room, the cold store (the Owner, 2026-11-19: chest freezers, not a wooden bench —
 * memory empties when the power goes, as a freezer spoils; and three kinds, not seven, each readable
 * apart and in the key). The memory as it is now, held to the room standards (frame/units/standards.ts):
 *   the freezers     a chest freezer per 4 GiB of modules (the ruler), whole ones only, in pairs, low; a
 *                    vent grille, a handle, a hinge. Through the glass lids, filling them in proportion from
 *                    the left: in use (amber), can be cleared (pale blue), empty (the frosted floor). The three add up to what is installed. Damage taped
 *                    off in red across the last lids.
 *   the overflow     a corner marked on the floor at the front, as long as the swap in the same parts;
 *                    crates in it as far as the swap is used
 *   the console      the room console: in use on the desk; in use by kind on a split bar (system,
 *                    compressed, programs and the rest); the overflow traffic
 * Low and flat-topped: nothing hides anything.
 */

export type MemoryRoomReadings = {
  total: number | null; available: number | null; free: number | null; kernel?: number | null; compressed?: number | null
  swapTotal: number | null; swapUsed: number | null; swapIO: number | null; corrupted: number | null
}

/** What the freezers hold now, in order from the left, in bytes; the three add up to what is installed. */
export type Stretch = { id: 'held' | 'clearable' | 'empty'; bytes: number }
export function freezersOf(r: MemoryRoomReadings): Stretch[] {
  if (!r.total || r.available == null) return []
  const avail = Math.max(0, Math.min(r.total, r.available)), empty = Math.max(0, Math.min(avail, r.free ?? 0))
  return ([{ id: 'held', bytes: r.total - avail }, { id: 'clearable', bytes: avail - empty }, { id: 'empty', bytes: empty }] as Stretch[]).filter(s => s.bytes > 0)
}

/**
 * What is in use, by kind: what the system keeps and what is compressed, read exactly on Linux and macOS;
 * programs and the rest is what remains of in use. The three add up to in use; null when nothing is read.
 */
export function inUseOf(r: MemoryRoomReadings): { id: 'system' | 'compressed' | 'rest'; bytes: number }[] | null {
  if (!r.total || r.available == null || (r.kernel == null && r.compressed == null)) return null
  const held = r.total - Math.max(0, Math.min(r.total, r.available))
  const sys = Math.min(held, Math.max(0, r.kernel ?? 0)), cmp = Math.min(held - sys, Math.max(0, r.compressed ?? 0))
  return [{ id: 'system', bytes: sys }, { id: 'compressed', bytes: cmp }, { id: 'rest', bytes: held - sys - cmp }]
}

const ROW_OF: Record<Stretch['id'], string> = { held: 'mem.held', clearable: 'mem.clearable', empty: 'mem.empty' }
const KIND_LABEL = { system: 'SYSTEM', compressed: 'COMPRESSED', rest: 'PROGRAMS ETC' } as const
const KIND_COLOUR = { system: '#a8650f', compressed: '#d98a1e', rest: '#f6c66a' } as const

// Plain fills, three only (the Owner, 2026-11-19: the little blocks were noise; what makes it a freezer is
// its body, not a pattern inside).
const FILL: Record<Stretch['id'], string> = { held: '#f2b03d', clearable: '#9cc5da', empty: 'none' }

const U = 35.8
const WALL = faces('#efe9dc', '#e2dac8', '#cfc6b2')
const BODY = faces('#f4f6f8', '#dfe4ea', '#c9d0d8')
/** A freezer's footprint and height, and the rows' pitch: every freezer the same height, flat on top. */
export const SECTION = { len: 0.66, depth: 0.45, height: 10, pairGap: 0.16, row: 0.75 }
const ROOMS: [number, number][] = [[4, 4], [4, 8], [8, 8]]
const CORNER_D = 0.8

const perRow = (w: number) => { let pairs = 1; while ((pairs * 2) * (2 * SECTION.len + SECTION.pairGap) <= w - 0.6 + 1e-9) pairs *= 2; return pairs * 2 }
const rowsFit = (d: number, corner: boolean) => Math.floor((d - CONSOLE_DEPTH - 0.4 - (corner ? CORNER_D : 0)) / SECTION.row)

/**
 * How many freezers a memory of these readings is laid in, under the ruler as it stands: whole freezers,
 * never part of one (the Owner, 2026-11-19: a freezer cannot be half). Memory comes in modules counted in
 * GiB, and the system reports a little less than is installed (24 GB of modules reads as 22.8 GiB), so the
 * count rounds up to the modules; what is in them fills them in proportion.
 */
export const sectionsOf = (r: MemoryRoomReadings) => r.total ? Math.max(1, Math.ceil(encode('mem.sections', r.total)! - 0.02)) : 0

/** Whether the largest room holds this memory's freezers, under the ruler as it stands. */
export function memoryRoomFits(r: MemoryRoomReadings): boolean {
  const n = sectionsOf(r), [w, d] = ROOMS[ROOMS.length - 1]
  return Math.ceil(n / perRow(w)) <= rowsFit(d, !!r.swapTotal)
}
export function memoryRoomShape(r: MemoryRoomReadings): [number, number] {
  const n = sectionsOf(r), corner = !!r.swapTotal
  return ROOMS.find(([w, d]) => Math.ceil(n / perRow(w)) <= rowsFit(d, corner)) ?? ROOMS[ROOMS.length - 1]
}

export function memoryRoom(r: MemoryRoomReadings): Sprite {
  const [fw, fd] = memoryRoomShape(r)
  const g = new Draw(fw, fd, 56)
  g.box(0, 0, fw, fd, 3, faces('#d8d6cf', '#bdbab1', '#aaa79e'), 0, 1.2)
  g.box(0, 0, fw, 0.18, 46, WALL, 0, 1.2)
  g.box(0, 0.18, 0.18, fd - 0.18, 46, WALL, 0, 1.2)

  // The console: what is in use, as a share of what is installed, on the desk; the overflow traffic.
  const share = r.total && r.available != null ? (r.total - Math.min(r.total, r.available)) / r.total : null
  const io = r.swapIO, kinds = inUseOf(r)
  const spec: ConsoleSpec = {
    name: 'MEMORY CONSOLE',
    desk: share != null ? { row: 'mem.inuse', share: encode('mem.inuse', share), plate: `IN USE ${readingText(share, 'share')}` } : undefined,
    gauges: io != null && r.swapTotal ? [{ row: 'swap.io', name: 'TRAFFIC', share: encode('swap.io', io / 10e6), redFrom: 0.5, reading: readingText(io, 'bytes/s') }] : [],
    splits: kinds ? [{ row: 'mem.kinds', name: 'IN USE BY KIND', parts: kinds.map(k => ({ label: `${KIND_LABEL[k.id]} ${readingText(k.bytes, 'bytes')}`, bytes: k.bytes, colour: KIND_COLOUR[k.id] })) }] : [],
  }
  if (hasConsole(spec)) roomConsole(g, fw, spec)

  type Thing = { at: number; draw: () => void }
  const things: Thing[] = []
  const count = sectionsOf(r)
  if (count > 0) {
    metaphor('mem.sections')
    const each = r.total! / count
    const stretches = freezersOf(r)
    for (const s of stretches) metaphor(ROW_OF[s.id])
    if (r.corrupted) metaphor('mem.corrupted')
    const K = perRow(fw), rows = Math.ceil(count / K), corner = !!r.swapTotal
    const floorTop = CONSOLE_DEPTH + 0.25, floorBot = fd - 0.3 - (corner ? CORNER_D : 0)
    const slotD = Math.min(1.25, (floorBot - floorTop) / rows), y0 = floorTop + ((floorBot - floorTop) - rows * slotD) / 2
    const pairs = K / 2, slotW = (fw - 0.7) / pairs, pairW = 2 * SECTION.len + SECTION.pairGap
    for (let i = 0; i < count; i++) {
      const col = i % K, row = Math.floor(i / K)
      const len = SECTION.len
      const x = 0.35 + Math.floor(col / 2) * slotW + (slotW - pairW) / 2 + (col % 2) * (SECTION.len + SECTION.pairGap)
      const y = y0 + row * slotD + (slotD - SECTION.depth) / 2
      const from = i * each, to = from + each
      things.push({ at: x + y, draw: () => {
        // A chest freezer: white enamel body, a glass lid on top; through it, what is inside.
        g.shadow(x, y, len, SECTION.depth, 0.2)
        g.box(x, y, len, SECTION.depth, SECTION.height, BODY, 0, 1.1)
        // What makes it a freezer, and nothing else: the vent grille low on its front, the lid's handle.
        for (let v = 0; v < 3; v++) g.line([x + len * 0.55, y + SECTION.depth, 2 + v * 1.4], [x + len * 0.9, y + SECTION.depth, 2 + v * 1.4], '#9aa5b1', 0.7)
        g.onLeft(x, y + SECTION.depth, len * 0.3, len * 0.7, SECTION.height - 2.2, SECTION.height - 0.8, '#8c98a6', 0.5)
        const W = len * U, D = SECTION.depth * U, m = 2.2, inner = W - 2 * m
        let svg = `<rect x="${m}" y="${m}" width="${inner.toFixed(2)}" height="${(D - 2 * m).toFixed(2)}" rx="1.2" fill="#eef7fb"/>`
        let at = 0
        for (const s of stretches) {
          const a = Math.max(at, from), b = Math.min(at + s.bytes, to)
          if (b > a && s.id !== 'empty') {
            const u0 = m + (a - from) / (to - from) * inner, u1 = m + (b - from) / (to - from) * inner
            svg += `<rect x="${u0.toFixed(2)}" y="${(m + 0.6).toFixed(2)}" width="${(u1 - u0).toFixed(2)}" height="${(D - 2 * m - 1.2).toFixed(2)}" fill="${FILL[s.id]}"/>`
          }
          at += s.bytes
        }
        // The glass: a sheen across the lid, the hinge along its back edge, and its frame.
        svg += `<line x1="${m}" y1="${(m - 1).toFixed(1)}" x2="${(W - m).toFixed(1)}" y2="${(m - 1).toFixed(1)}" stroke="#9aa5b1" stroke-width="1"/>`
        svg += `<path d="M${(W * 0.15).toFixed(1)} ${(D - m).toFixed(1)} L${(W * 0.35).toFixed(1)} ${m} M${(W * 0.45).toFixed(1)} ${(D - m).toFixed(1)} L${(W * 0.58).toFixed(1)} ${m}" stroke="#ffffff" stroke-width="2.2" opacity="0.55"/>`
        svg += `<rect x="${m}" y="${m}" width="${inner.toFixed(2)}" height="${(D - 2 * m).toFixed(2)}" rx="1.2" fill="none" stroke="#6f8597" stroke-width="0.9"/>`
        if (r.corrupted && r.total && to > r.total - r.corrupted) svg += `<path d="M0 0L${W.toFixed(1)} ${D.toFixed(1)}M0 ${D.toFixed(1)}L${W.toFixed(1)} 0" stroke="#d83a32" stroke-width="2.4"/>`
        g.floor(x, y, SECTION.height, svg)
      } })
    }
    // The overflow corner: marked floor at the front, as long as the swap in the same parts; crates in it
    // as far as the swap is used. Crates are low, so they hide nothing.
    if (corner) {
      metaphor('swap.room')
      const sec = encode('mem.sections', r.swapTotal)!, cl = Math.min(fw - 0.6, sec * SECTION.len), cy = fd - 0.3 - CORNER_D + 0.15
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
