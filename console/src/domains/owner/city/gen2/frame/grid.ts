/**
 * The ground of every place (the Owner, 2026-11-19): a square of 16 × 16 cells, the same sheet on every
 * level (2026-11-20), its unit 4 × 4 (sixteen to a sheet). It is divided into
 * plots, and the streets run along the plots' edges; each thing stands in a plot of its own.
 *
 *   things  1×1, 1×2, 2×2, 2×4, 4×4, 4×8 or 8×8: a square or a 2 : 1 oblong, by how big and how
 *           important the thing is (the Owner, 2026-11-19: not every thing the same). The shape is
 *           the design's, set with the thing.
 *   plots   made by halving: the ground is cut in two, each half may be cut again, down to 1 × 1.
 *           A square is cut across x, an oblong across its long side, so every plot is a square
 *           or a 2 : 1 oblong with sides of 1, 2, 4, 8 or 16, and its edges always meet. The
 *           Owner may cut and join plots (display only); the default cuts the ground into 4 × 4.
 *   streets every cut is a street, all one width on every level (named by where they run: the
 *           first cut the avenue, then streets, lanes between 2-wide plots, paths between 1-wide
 *           ones), and a ring road runs round the ground.
 *
 * A thing goes in a plot it fits (its long side along the plot's long side), one to a plot, centred.
 * Until the Owner cuts the ground himself, the plan is cut to fit what the place holds (planFor).
 */

export const GRID = 16
export const SIZES = [1, 2, 4, 8] as const
export type Size = (typeof SIZES)[number]

/** The smallest thing a finger must hit: a 1×1 is a marker, not a door. */
export const TAPPABLE: Size = 2

export type Cell = { x: number; y: number }
export const SHAPES = ['1x1', '1x2', '2x2', '2x4', '4x4', '4x8', '8x8'] as const
export type Shape = (typeof SHAPES)[number]
/** A shape's short and long side. */
export const sidesOf = (s: Shape) => { const [a, b] = s.split('x').map(Number) as [Size, Size]; return { short: a, long: b } }
export const shapeArea = (s: Shape) => { const { short, long } = sidesOf(s); return short * long }

export type Item = { id: string; shape: Shape }
/** Where a thing stands: its back corner and its footprint along x (w) and y (h), turned to its plot. */
export type Placed = Item & Cell & { plot: string; w: number; h: number }

/**
 * A district's footprint on the town (the Owner, 2026-11-20): one unit. Whatever it holds lives on its own
 * sheet inside (up to 16 things), so holding more does not make it bigger on the town; a medium or large
 * footprint is for a later reason (a district whose things themselves grow), not for a count.
 */
export const DISTRICT: Shape = '4x4'

export const area = (items: readonly Item[]) => items.reduce((a, i) => a + shapeArea(i.shape), 0)
export const fitsBudget = (items: readonly Item[], g: Ground = FULL) => area(items) <= g.w * g.h

// ---- the plan: a tree of halvings ----

/**
 * A plan: a whole plot (null) or a plot cut in two halves, each a plan. A square cut is across x (left and
 * right halves) unless it is turned (a third element, 1): then across y (back and front). So an oblong plot
 * can lie either way (the Owner, 2026-11-20: a 4×8 has a direction, lengthwise or across).
 */
export type Plan = null | [Plan, Plan] | [Plan, Plan, 1]
const turned = (p: Plan) => p !== null && p.length === 3
/** A plot: its id is its path in the plan ('' the whole ground, then 'a'/'b' for each half). */
export type Plot = { id: string; x: number; y: number; w: number; h: number }
type Rect = Omit<Plot, 'id'>

/** The two halves of a rectangle: a square across x, or across y when turned; an oblong across its long side. */
function halves(r: Rect, turn = false): [Rect, Rect] {
  return (r.w === r.h ? !turn : r.w > r.h)
    ? [{ x: r.x, y: r.y, w: r.w / 2, h: r.h }, { x: r.x + r.w / 2, y: r.y, w: r.w / 2, h: r.h }]
    : [{ x: r.x, y: r.y, w: r.w, h: r.h / 2 }, { x: r.x, y: r.y + r.h / 2, w: r.w, h: r.h / 2 }]
}

/**
 * A place's ground (the Owner, 2026-11-20, simplified the same day): every level is the same sheet of
 * 16 × 16; going in opens a new sheet, it is not a geometric zoom. A person learns one page and it holds on
 * every level. What a place holds is limited by count (MAX_THINGS) and its size by three classes (CLASSES).
 */
export type Ground = { w: number; h: number }
export const FULL: Ground = { w: GRID, h: GRID }
const whole = (g: Ground): Rect => ({ x: 0, y: 0, w: g.w, h: g.h })

/**
 * The unit is 4 × 4 cells (the Owner, 2026-11-20: four to a row, sixteen to a sheet): a sheet is drawn
 * 16 × 16 so the pictures can be fine, but it holds 4 × 4 units; nothing smaller than one unit, and at most
 * 16 things on any level.
 */
export const UNIT = 4
export const MAX_THINGS = 16

/** A district's or a building's size: small (one unit), medium (two) or large (four): its weight, not how much it holds (the Owner, 2026-11-20). */
export const CLASSES = ['4x4', '4x8', '8x8'] as const


/** Every plot of a plan (its leaves). */
export function plotsOf(plan: Plan, g: Ground = FULL): Plot[] {
  const out: Plot[] = []
  const walk = (p: Plan, r: Rect, id: string) => {
    if (p === null) { out.push({ id, ...r }); return }
    const [a, b] = halves(r, turned(p))
    walk(p[0], a, id + 'a'); walk(p[1], b, id + 'b')
  }
  walk(plan, whole(g), '')
  return out
}

/** The plan cut until every plot is at most `max` a side: the default is 4 × 4 plots. */
export function defaultPlan(max = 4, g: Ground = FULL): Plan {
  const grow = (r: Rect): Plan => {
    if (r.w <= max && r.h <= max) return null
    const [a, b] = halves(r)
    return [grow(a), grow(b)]
  }
  return grow(whole(g))
}

const nodeAt = (plan: Plan, id: string): Plan | undefined => {
  let p: Plan | undefined = plan
  for (const c of id) { if (!p) return undefined; p = p[c === 'a' ? 0 : 1] }
  return p
}
const replace = (plan: Plan, id: string, next: Plan): Plan => {
  if (id === '') return next
  if (plan === null) return plan
  const [a, b] = plan
  const kids: [Plan, Plan] = id[0] === 'a' ? [replace(a, id.slice(1), next), b] : [a, replace(b, id.slice(1), next)]
  return turned(plan) ? [kids[0], kids[1], 1] : kids
}

/** Cut a plot in two; null if it is a single cell already or not a plot. */
export function splitPlot(plan: Plan, id: string, g: Ground = FULL): Plan | null {
  const p = plotsOf(plan, g).find(q => q.id === id)
  // No plot smaller than the unit.
  if (!p || (p.w <= UNIT && p.h <= UNIT)) return null
  return replace(plan, id, [null, null])
}

/** Join a plot with its other half; null when the other half is cut further or it is the whole ground. */
export function mergePlot(plan: Plan, id: string): Plan | null {
  if (id === '') return null
  const node = nodeAt(plan, id.slice(0, -1))
  if (!node || node[0] !== null || node[1] !== null) return null
  return replace(plan, id.slice(0, -1), null)
}

/** The rectangle of a node of the plan (a plot or a block of plots), by its id. */
function rectOf(plan: Plan, id: string, g: Ground = FULL): Rect | null {
  let p: Plan | undefined = plan, r = whole(g)
  for (const c of id) {
    if (!p) return null
    const [a, b] = halves(r, turned(p))
    r = c === 'a' ? a : b
    p = p[c === 'a' ? 0 : 1]
  }
  return r
}

/**
 * Turn a plot a quarter (the Owner, 2026-11-20: which way a 4×8 lies): the nearest square block it is part of
 * is cut the other way, so its halves, and what stands on them, lie across instead of lengthwise. Null when no
 * square block holds it (the whole ground cut into halves that are not square has none) or it is the whole ground.
 */
export function turnPlot(plan: Plan, id: string, g: Ground = FULL): Plan | null {
  for (let k = id.length - 1; k >= 0; k--) {
    const at = id.slice(0, k), node = nodeAt(plan, at), r = rectOf(plan, at, g)
    if (!node || !r || r.w !== r.h) continue
    const flipped: Plan = turned(node) ? [node[0], node[1]] : [node[0], node[1], 1]
    return replace(plan, at, flipped)
  }
  return null
}

/**
 * Join two plots side by side, whichever way (the Owner, 2026-11-20: not only the fixed other half; left, right,
 * back or front): two plots of one size, touching along a whole side, become one, the road between them gone. If
 * they are not each other's other half but share a square block cut the other way, that block is cut anew (the
 * plots in it keep their places on the ground). The plan after, and where each old plot now is (the two joined
 * map to the new one), or null when they cannot be joined.
 */
export function joinPlots(plan: Plan, aId: string, bId: string, g: Ground = FULL): { plan: Plan; moved: Record<string, string> } | null {
  const before = plotsOf(plan, g)
  const a = before.find(p => p.id === aId), b = before.find(p => p.id === bId)
  if (!a || !b || a.id === b.id || a.w !== b.w || a.h !== b.h) return null
  const touch = (a.x === b.x && a.w === b.w && (a.y + a.h === b.y || b.y + b.h === a.y)) || (a.y === b.y && a.h === b.h && (a.x + a.w === b.x || b.x + b.w === a.x))
  if (!touch) return null
  const same = (p: Plot, q: Plot) => p.x === q.x && p.y === q.y && p.w === q.w && p.h === q.h
  const remap = (from: Plan) => {
    const after = plotsOf(from, g), moved: Record<string, string> = {}
    for (const p of before) { const q = after.find(r => same(r, p)); if (q) moved[p.id] = q.id }
    return { after, moved }
  }
  // Each other's other half: join them.
  if (aId.slice(0, -1) === bId.slice(0, -1) && aId.length === bId.length) {
    const next = mergePlot(plan, aId)
    if (!next) return null
    const { moved } = remap(next)
    moved[aId] = moved[bId] = aId.slice(0, -1)
    return { plan: next, moved }
  }
  // In one square block cut the other way: cut it anew, then they are each other's other half.
  let k = 0
  while (k < aId.length && aId[k] === bId[k]) k++
  const block = aId.slice(0, k), node = nodeAt(plan, block), r = rectOf(plan, block, g)
  if (!node || !r || r.w !== r.h || aId.length !== block.length + 2 || bId.length !== block.length + 2) return null
  const turnedPlan = replace(plan, block, turned(node) ? [node[0], node[1]] : [node[0], node[1], 1])
  const { after, moved } = remap(turnedPlan)
  const a2 = moved[aId], b2 = moved[bId]
  if (!a2 || !b2 || !after.some(p => p.id === a2)) return null
  if (a2.slice(0, -1) !== b2.slice(0, -1)) return null
  const next = mergePlot(turnedPlan, a2)
  if (!next) return null
  const out = remap(next).moved
  for (const id of Object.keys(moved)) if (out[id] === undefined && moved[id] !== a2 && moved[id] !== b2) out[id] = moved[id]
  out[aId] = out[bId] = a2.slice(0, -1)
  return { plan: next, moved: out }
}

/**
 * The land is shaped empty (the Owner, 2026-11-20: the land first, then the building; never the land bent to a
 * building): a change of the plan may go ahead only if every plot a thing stands on is still there, the same
 * plot on the same ground, after it. A cut, a turn or a join that would touch built land is refused.
 */
export function leavesBuiltLand(plan: Plan, placed: readonly Placed[], plots: readonly Plot[], g: Ground = FULL): boolean {
  const after = plotsOf(plan, g)
  return placed.every(it => {
    const was = plots.find(p => p.id === it.plot)
    return !!was && after.some(q => q.x === was.x && q.y === was.y && q.w === was.w && q.h === was.h)
  })
}

/** A plan as a short string, and back (for the browser to keep): 'L' a plot, 'S' a cut followed by its halves. */
export const encodePlan = (plan: Plan): string => plan === null ? 'L' : (turned(plan) ? 'T' : 'S') + encodePlan(plan[0]) + encodePlan(plan[1])
export function decodePlan(s: string): Plan | null {
  let i = 0
  const read = (depth: number): Plan => {
    if (depth > 8 || i >= s.length) throw new Error('bad plan')
    const c = s[i++]
    if (c === 'L') return null
    if (c !== 'S' && c !== 'T') throw new Error('bad plan')
    const a = read(depth + 1), b = read(depth + 1)
    return c === 'T' ? [a, b, 1] : [a, b]
  }
  try { const p = read(0); return i === s.length ? p : null } catch { return null }
}

// ---- streets ----

export type StreetKind = 'ring' | 'avenue' | 'street' | 'lane' | 'path'
/** A stretch of street along a line from (x0, y0) to (x1, y1), in cells, and half its width. */
export type Street = { kind: StreetKind; x0: number; y0: number; x1: number; y1: number; half: number }

/** One width for every way between things, on every level (the Owner, 2026-11-19: regular, like pixel art). */
/** A whole cell wide: the ways carry the traffic between things, so they get room (the Owner, 2026-11-19). */
const WAY_HALF = 0.5
export const STREET_HALF: Record<StreetKind, number> = { ring: WAY_HALF, avenue: WAY_HALF, street: WAY_HALF, lane: WAY_HALF, path: WAY_HALF }

/** Every cut is a street, as wide as the narrower side of the plots it divides allows; the ring runs round the ground. */
export function streetsOf(plan: Plan, g: Ground = FULL): Street[] {
  const out: Street[] = []
  const walk = (p: Plan, r: Rect, depth: number) => {
    if (p === null) return
    const [a, b] = halves(r, turned(p))
    const side = Math.min(a.w, a.h)
    const kind: StreetKind = depth === 0 ? 'avenue' : side >= 4 ? 'street' : side === 2 ? 'lane' : 'path'
    const line = a.w < r.w ? { x0: b.x, y0: r.y, x1: b.x, y1: r.y + r.h } : { x0: r.x, y0: b.y, x1: r.x + r.w, y1: b.y }
    out.push({ kind, ...line, half: STREET_HALF[kind] })
    walk(p[0], a, depth + 1); walk(p[1], b, depth + 1)
  }
  walk(plan, whole(g), 0)
  for (const e of [{ x0: 0, y0: 0, x1: g.w, y1: 0 }, { x0: 0, y0: g.h, x1: g.w, y1: g.h }, { x0: 0, y0: 0, x1: 0, y1: g.h }, { x0: g.w, y0: 0, x1: g.w, y1: g.h }])
    out.push({ kind: 'ring', ...e, half: STREET_HALF.ring })
  return out
}

// ---- things in plots ----

export const fitsIn = (it: Item, p: Rect) => { const { short, long } = sidesOf(it.shape); return Math.min(p.w, p.h) >= short && Math.max(p.w, p.h) >= long }
const centred = (it: Item, p: Plot): Placed => {
  const { short, long } = sidesOf(it.shape)
  const w = p.w >= p.h ? long : short, h = p.w >= p.h ? short : long
  return { ...it, plot: p.id, w, h, x: p.x + (p.w - w) / 2, y: p.y + (p.h - h) / 2 }
}

const nearMiddle = (p: Rect, g: Ground = FULL) => (p.x + p.w / 2 - g.w / 2) ** 2 + (p.y + p.h / 2 - g.h / 2) ** 2
/** The ground a set of plots tiles. */
const groundOfPlots = (plots: readonly Plot[]): Ground => plots.length ? { w: Math.max(...plots.map(p => p.x + p.w)), h: Math.max(...plots.map(p => p.y + p.h)) } : FULL

/**
 * The plan cut to fit a place's things: largest first, each takes the smallest free plot that fits,
 * nearest the middle, cut in half while a half still fits it; the ground left over is cut to 4 × 4,
 * so the streets read the same on every page. Things that do not fit are left for assign to list.
 */
export function planFor(items: readonly Item[], g: Ground = FULL): Plan {
  let plan: Plan = null
  const taken = new Set<string>()
  const order = [...items].sort((a, b) => shapeArea(b.shape) - shapeArea(a.shape))
  for (const it of order) {
    for (;;) {
      const p = plotsOf(plan, g).filter(q => !taken.has(q.id) && fitsIn(it, q)).sort((a, b) => a.w * a.h - b.w * b.h || nearMiddle(a, g) - nearMiddle(b, g) || a.y - b.y || a.x - b.x)[0]
      if (!p) break
      if (halves(p).some(h => fitsIn(it, h))) { plan = replace(plan, p.id, [null, null]); continue }
      taken.add(p.id); break
    }
  }
  for (;;) {
    const big = plotsOf(plan, g).find(q => !taken.has(q.id) && Math.max(q.w, q.h) > 4)
    if (!big) return plan
    plan = replace(plan, big.id, [null, null])
  }
}

/** The Owner's choices for one place: how the ground is cut, and which plot each thing stands in. */
export type Arrangement = { plan?: string; at?: Record<string, string> }

/**
 * Where everything stands: the Owner's plot for a thing where it still fits and is free; for the
 * rest, largest first, the smallest free plot that fits, nearest the centre. Things that find no
 * plot are listed (the plan is cut too fine for them) and not drawn.
 */
export function assign(items: readonly Item[], plots: readonly Plot[], chosen: Record<string, string> = {}): { placed: Placed[]; homeless: Item[] } {
  const taken = new Map<string, Item>()
  const byId = new Map(plots.map(p => [p.id, p]))
  for (const it of items) {
    const p = byId.get(chosen[it.id] ?? '\u0000')
    if (p && fitsIn(it, p) && !taken.has(p.id)) taken.set(p.id, it)
  }
  const housed = new Set([...taken.values()].map(it => it.id))
  const g = groundOfPlots(plots), d = (p: Rect) => nearMiddle(p, g)
  const order = items.map((it, i) => ({ it, i })).filter(({ it }) => !housed.has(it.id)).sort((a, b) => shapeArea(b.it.shape) - shapeArea(a.it.shape) || a.i - b.i)
  const homeless: Item[] = []
  for (const { it } of order) {
    const p = plots.filter(q => !taken.has(q.id) && fitsIn(it, q)).sort((a, b) => a.w * a.h - b.w * b.h || d(a) - d(b) || a.y - b.y || a.x - b.x)[0]
    if (p) taken.set(p.id, it); else homeless.push(it)
  }
  const placedById = new Map([...taken].map(([pid, it]) => [it.id, centred(it, byId.get(pid)!)]))
  return { placed: items.map(it => placedById.get(it.id)).filter((p): p is Placed => !!p), homeless }
}

/**
 * Put a thing in a plot. A free plot that fits takes it; a plot with another thing swaps them if each
 * fits the other's plot. The choices after, or null if it cannot go there.
 */
export function moveTo(placed: readonly Placed[], plots: readonly Plot[], id: string, plotId: string): Record<string, string> | null {
  const it = placed.find(p => p.id === id), to = plots.find(p => p.id === plotId)
  if (!it || !to || !fitsIn(it, to)) return null
  const now = Object.fromEntries(placed.map(p => [p.id, p.plot]))
  if (it.plot === plotId) return now
  const other = placed.find(p => p.plot === plotId)
  if (other) {
    const from = plots.find(p => p.id === it.plot)
    if (!from || !fitsIn(other, from)) return null
    now[other.id] = it.plot
  }
  now[id] = plotId
  return now
}

/** The plot under a cell, if any. */
export const plotAt = (plots: readonly Plot[], c: Cell) => plots.find(p => c.x >= p.x && c.x < p.x + p.w && c.y >= p.y && c.y < p.y + p.h)
