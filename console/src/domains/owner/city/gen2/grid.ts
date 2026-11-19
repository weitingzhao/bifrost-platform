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
 * Each size has its own lattice (the Owner, 2026-11-19): a thing of size s
 * stands only on multiples of s, so it always fills whole squares of the
 * lines drawn for its size. Because sizes are powers of two and the side is
 * 16, any set whose areas add up to 256 or less always packs: a place that
 * passes the budget can always be drawn.
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

/** Is `p` on its own lattice: a thing of size s stands only on multiples of s (the Owner, 2026-11-19: snap). */
export const aligned = (p: Placed) => p.x % p.size === 0 && p.y % p.size === 0

/** Can `p` stand where it is, given the others (itself excluded by id)? On its lattice, on the ground, on no one. */
export function canStand(p: Placed, others: readonly Placed[]): boolean {
  return Number.isInteger(p.x) && Number.isInteger(p.y) && aligned(p) && onGround(p) && others.every(o => o.id === p.id || !overlap(p, o))
}

/** The spots a thing of size s may take, nearest the ground's centre first (ties: back to front, left to right). */
function spots(size: number): Cell[] {
  const c = GRID / 2, out: Cell[] = []
  for (let y = 0; y + size <= GRID; y += size) for (let x = 0; x + size <= GRID; x += size) out.push({ x, y })
  const d = (p: Cell) => (p.x + size / 2 - c) ** 2 + (p.y + size / 2 - c) ** 2
  return out.sort((a, b) => d(a) - d(b) || a.y - b.y || a.x - b.x)
}

/**
 * The default arrangement: largest first, each on the free spot of its own
 * lattice nearest the centre, so a place's things gather in the middle of its
 * ground. Keeps the given order among equals. Any set within the budget packs:
 * everything placed before is larger or equal and on a coarser lattice, so the
 * free room is always whole spots of the current size.
 */
export function pack(items: readonly Item[], fixed: readonly Placed[] = []): Placed[] {
  const out: Placed[] = [...fixed]
  const order = items.map((it, i) => ({ it, i })).sort((a, b) => b.it.size - a.it.size || a.i - b.i)
  for (const { it } of order) {
    const at = spots(it.size).find(c => canStand({ ...it, ...c }, out))
    if (!at) throw new Error(`no room for ${it.id} (${it.size}×${it.size}) on a ${GRID}×${GRID} ground`)
    out.push({ ...it, ...at })
  }
  return out.slice(fixed.length)
}

/**
 * Where everything stands: the Owner's positions where they are still valid
 * (the item exists, sits on its lattice and the ground, overlaps nothing kept
 * before it), the default packing for the rest. A saved position never breaks
 * the page: one that no longer fits falls back to packing.
 */
export function arrange(items: readonly Item[], saved: Arrangement = {}): Placed[] {
  const kept: Placed[] = []
  for (const it of items) {
    const at = saved[it.id]
    if (!at) continue
    const p = { ...it, x: at.x, y: at.y }
    if (canStand(p, kept)) kept.push(p)
  }
  let all: Placed[]
  try {
    all = [...kept, ...pack(items.filter(it => !kept.some(k => k.id === it.id)), kept)]
  } catch {
    // The Owner's positions left the free room in pieces too small for a newcomer: start from the default.
    all = pack(items)
  }
  const byId = new Map(all.map(p => [p.id, p]))
  return items.map(it => byId.get(it.id)!)
}

/** The spot of its own lattice nearest a fractional cell: where a dragged thing snaps. */
export const snap = (size: number, to: Cell): Cell => ({
  x: Math.min(GRID - size, Math.max(0, Math.round(to.x / size) * size)),
  y: Math.min(GRID - size, Math.max(0, Math.round(to.y / size) * size)),
})

/** Move one thing to a cell (snapped to its lattice), if it can stand there; the arrangement after, or null. */
export function move(placed: readonly Placed[], id: string, to: Cell): Placed[] | null {
  const it = placed.find(p => p.id === id)
  if (!it) return null
  const p = { ...it, ...snap(it.size, to) }
  if (!canStand(p, placed)) return null
  return placed.map(q => q.id === id ? p : q)
}

export const toArrangement = (placed: readonly Placed[]): Arrangement => Object.fromEntries(placed.map(p => [p.id, { x: p.x, y: p.y }]))
