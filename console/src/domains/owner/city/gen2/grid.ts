/**
 * The ground of every place (the Owner, 2026-11-19): a square of 16 × 16
 * cells, and everything on it is a square of 1, 2, 4 or 8 cells a side. The
 * ground is the budget of one page: what does not fit is too much for this
 * level and goes one level down, or is drawn more abstractly here.
 *
 * Who decides what:
 *   - the SIZE of a thing is a design decision, made in code with the thing
 *     (what it is, how much it holds); the page never changes it
 *   - the POSITION is the Owner's: he may drag things around; that changes
 *     nothing but where they are drawn
 *
 * Because sizes are powers of two and the side is 16, any set whose areas
 * add up to 256 or less always packs (largest first, each on a multiple of
 * its own size): a place that passes the budget can always be drawn.
 */

export const GRID = 16
export const SIZES = [1, 2, 4, 8] as const
export type Size = (typeof SIZES)[number]

/** The smallest thing a finger must hit: a 1×1 is a marker, not a door. */
export const TAPPABLE: Size = 2

export type Cell = { x: number; y: number }
export type Item = { id: string; size: Size }
export type Placed = Item & Cell
/** The Owner's positions, by item id (display only). */
export type Arrangement = Record<string, Cell>

export const area = (items: readonly Item[]) => items.reduce((a, i) => a + i.size * i.size, 0)
export const fitsBudget = (items: readonly Item[]) => area(items) <= GRID * GRID

const overlap = (a: Placed, b: Placed) => a.x < b.x + b.size && b.x < a.x + a.size && a.y < b.y + b.size && b.y < a.y + a.size
const onGround = (p: Placed) => p.x >= 0 && p.y >= 0 && p.x + p.size <= GRID && p.y + p.size <= GRID

/** Can `p` stand where it is, given the others (itself excluded by id)? */
export function canStand(p: Placed, others: readonly Placed[]): boolean {
  return Number.isInteger(p.x) && Number.isInteger(p.y) && onGround(p) && others.every(o => o.id === p.id || !overlap(p, o))
}

/**
 * The default arrangement: largest first, each on the first free spot that
 * is a multiple of its size, scanning from the back corner. Keeps the given
 * order among equals, so the menu's order reads front to back.
 */
export function pack(items: readonly Item[], fixed: readonly Placed[] = [], gap = 0): Placed[] {
  const out: Placed[] = [...fixed]
  // With a gap, two things must stand at least `gap` cells apart on one axis or the other.
  const apart = (p: Placed, o: Placed) => p.x >= o.x + o.size + gap || o.x >= p.x + p.size + gap || p.y >= o.y + o.size + gap || o.y >= p.y + p.size + gap
  const order = items.map((it, i) => ({ it, i })).sort((a, b) => b.it.size - a.it.size || a.i - b.i)
  for (const { it } of order) {
    let spot: Placed | null = null
    // Aligned spots first (the guarantee); any free cell after, for when the Owner's moves left gaps unaligned.
    // With a gap, the lattice is size + gap, so things line up with air between them.
    for (const step of [it.size + gap, 1]) {
      for (let y = 0; y + it.size <= GRID && !spot; y += step)
        for (let x = 0; x + it.size <= GRID && !spot; x += step) {
          const p = { ...it, x, y }
          if (canStand(p, out) && (gap === 0 || out.every(o => o.id === p.id || apart(p, o)))) spot = p
        }
      if (spot) break
    }
    if (!spot) throw new Error(`no room for ${it.id} (${it.size}×${it.size}) on a ${GRID}×${GRID} ground`)
    out.push(spot)
  }
  return out.slice(fixed.length)
}

/** The default for a place nobody arranged: a cell of air between things when the ground allows, the group centred. */
export function spread(items: readonly Item[]): Placed[] {
  let out: Placed[]
  try { out = pack(items, [], 1) } catch { return pack(items) }
  if (out.length === 0) return out
  const w = Math.max(...out.map(p => p.x + p.size)), h = Math.max(...out.map(p => p.y + p.size))
  const dx = Math.floor((GRID - w) / 2), dy = Math.floor((GRID - h) / 2)
  return out.map(p => ({ ...p, x: p.x + dx, y: p.y + dy }))
}

/**
 * Where everything stands: the Owner's positions where they are still valid
 * (the item exists, is on the ground, overlaps nothing kept before it), the
 * default packing for the rest. A saved position never breaks the page: one
 * that no longer fits (a thing grew, a new thing came) falls back to packing.
 */
export function arrange(items: readonly Item[], saved: Arrangement = {}): Placed[] {
  if (!items.some(it => saved[it.id])) return spread(items)
  const kept: Placed[] = []
  for (const it of items) {
    const at = saved[it.id]
    if (!at) continue
    const p = { ...it, x: at.x, y: at.y }
    if (canStand(p, kept)) kept.push(p)
  }
  let all: Placed[]
  try {
    const rest = items.filter(it => !kept.some(k => k.id === it.id))
    let more: Placed[]
    try { more = pack(rest, kept, 1) } catch { more = pack(rest, kept) }
    all = [...kept, ...more]
  } catch {
    // The Owner's positions left the free room in pieces too small for a newcomer: start from the default.
    all = pack(items)
  }
  const byId = new Map(all.map(p => [p.id, p]))
  return items.map(it => byId.get(it.id)!)
}

/** Move one thing to a cell, if it can stand there; the arrangement after, or null. */
export function move(placed: readonly Placed[], id: string, to: Cell): Placed[] | null {
  const it = placed.find(p => p.id === id)
  if (!it) return null
  const p = { ...it, x: Math.round(to.x), y: Math.round(to.y) }
  if (!canStand(p, placed)) return null
  return placed.map(q => q.id === id ? p : q)
}

export const toArrangement = (placed: readonly Placed[]): Arrangement => Object.fromEntries(placed.map(p => [p.id, { x: p.x, y: p.y }]))
