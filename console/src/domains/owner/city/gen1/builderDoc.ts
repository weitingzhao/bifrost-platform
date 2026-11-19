/**
 * The city builder's layout document (the Owner, 2026-11-17: he builds the
 * Ops home's city himself, a step at a time: bare land, roads he drags, then
 * buildings from a catalogue, each bound to what it stands for).
 *
 * The document is his arrangement and nothing else: the land's size, the
 * road cells, and the parts on their cells with what each one stands for.
 * Readings (who is working, what was published, what is on PROD) are never
 * written here; the page computes them when it draws. Footprints are whole
 * squares of 1, 2, 4 or 8 cells; a road is one cell wide and shares no cell
 * with a building. Saved as a Tiled map (isometric): a road tile layer and a
 * parts object group, so Tiled opens it.
 */

export type PartSize = 1 | 2 | 4 | 8
export type Placed = {
  id: string
  /** The catalogue's kind: which building this is. */
  kind: string
  x: number
  y: number
  size: PartSize
  /** What it stands for: `domain:<id>`, `lane:<pipeline>`, `plugin:<name>`, `law`; empty until bound. */
  binds: string
  /** The number the Owner paints on it (01, M1…): only how it is shown, never what it stands for. */
  label?: string
}
/** A district of the plan: a named rectangle of ground under its buildings, how the land is zoned (presentation only). */
export type Zone = { name: string; x: number; y: number; w: number; h: number; tint: string }
export type Doc = { width: number; height: number; roads: Set<string>; parts: Placed[]; zones?: Zone[] }

/** The longest number a building can carry. */
export const LABEL_MAX = 6

export const LAND_START = 16
export const LAND_STEP = 8
export const LAND_MAX = 96

export const key = (x: number, y: number) => `${x},${y}`
export const cellOf = (k: string): [number, number] => { const [x, y] = k.split(','); return [Number(x), Number(y)] }

export function emptyDoc(side = LAND_START): Doc {
  return { width: side, height: side, roads: new Set(), parts: [] }
}

export const inLand = (d: Doc, x: number, y: number) => x >= 0 && y >= 0 && x < d.width && y < d.height

/** The part standing on a cell, if any. */
export function partAt(d: Doc, x: number, y: number): Placed | undefined {
  return d.parts.find(p => x >= p.x && x < p.x + p.size && y >= p.y && y < p.y + p.size)
}

/** Whether a square of this size fits at (x, y): on the land, on no road, on no other part. */
export function canPlace(d: Doc, x: number, y: number, size: number, ignoreId?: string): boolean {
  if (x < 0 || y < 0 || x + size > d.width || y + size > d.height) return false
  for (let i = x; i < x + size; i++) for (let j = y; j < y + size; j++) if (d.roads.has(key(i, j))) return false
  return !d.parts.some(p => p.id !== ignoreId && x < p.x + p.size && p.x < x + size && y < p.y + p.size && p.y < y + size)
}

/** Lays a road cell; false when the cell is off the land, under a building or already road. */
export function addRoad(d: Doc, x: number, y: number): boolean {
  if (!inLand(d, x, y) || partAt(d, x, y) || d.roads.has(key(x, y))) return false
  d.roads.add(key(x, y))
  return true
}

/** The cells of a dragged stroke from one cell to the next: along x first, then y, so a stroke never jumps a corner. */
export function strokeCells(from: readonly [number, number], to: readonly [number, number]): [number, number][] {
  const out: [number, number][] = []
  let [x, y] = from
  out.push([x, y])
  while (x !== to[0]) { x += Math.sign(to[0] - x); out.push([x, y]) }
  while (y !== to[1]) { y += Math.sign(to[1] - y); out.push([x, y]) }
  return out
}

/** Which neighbours of a road cell are road: bits N (y-1) = 1, E (x+1) = 2, S (y+1) = 4, W (x-1) = 8. */
export function roadMask(d: Doc, x: number, y: number): number {
  return (d.roads.has(key(x, y - 1)) ? 1 : 0) | (d.roads.has(key(x + 1, y)) ? 2 : 0) | (d.roads.has(key(x, y + 1)) ? 4 : 0) | (d.roads.has(key(x - 1, y)) ? 8 : 0)
}

/** The shape a road cell takes from its neighbours. */
export function roadShape(mask: number): 'dot' | 'end' | 'straight' | 'corner' | 'tee' | 'cross' {
  const n = (mask & 1) + ((mask >> 1) & 1) + ((mask >> 2) & 1) + ((mask >> 3) & 1)
  if (n === 0) return 'dot'
  if (n === 1) return 'end'
  if (n === 2) return mask === 5 || mask === 10 ? 'straight' : 'corner'
  return n === 3 ? 'tee' : 'cross'
}

/** More land: a step of cells on the far sides; nothing already placed moves. */
export function expand(d: Doc, step = LAND_STEP): boolean {
  if (d.width + step > LAND_MAX) return false
  d.width += step
  d.height += step
  return true
}

/**
 * The land trimmed to what stands on it (2026-11-18, the Owner wants the city
 * tight): buildings, roads and districts keep their places relative to each
 * other, the empty border is cut to one cell. False when there is nothing to
 * cut or nothing on the land.
 */
export function trim(d: Doc, margin = 1): boolean {
  const xs: number[] = [], ys: number[] = []
  for (const p of d.parts) { xs.push(p.x, p.x + p.size - 1); ys.push(p.y, p.y + p.size - 1) }
  for (const k of d.roads) { const [x, y] = cellOf(k); xs.push(x); ys.push(y) }
  if (!xs.length) return false
  // Districts follow what stands in them; they do not hold land open by themselves.
  const x0 = Math.max(0, Math.min(...xs) - margin), y0 = Math.max(0, Math.min(...ys) - margin)
  const w = Math.min(d.width, Math.max(...xs) + 1 + margin) - x0, h = Math.min(d.height, Math.max(...ys) + 1 + margin) - y0
  if (x0 === 0 && y0 === 0 && w === d.width && h === d.height) return false
  for (const p of d.parts) { p.x -= x0; p.y -= y0 }
  d.roads = new Set([...d.roads].map(k => { const [x, y] = cellOf(k); return key(x - x0, y - y0) }).filter(k => { const [x, y] = cellOf(k); return x < w && y < h }))
  d.zones = d.zones?.map(z => {
    const zx = Math.max(0, z.x - x0), zy = Math.max(0, z.y - y0)
    return { ...z, x: zx, y: zy, w: Math.min(w, z.x - x0 + z.w) - zx, h: Math.min(h, z.y - y0 + z.h) - zy }
  }).filter(z => z.w > 0 && z.h > 0)
  d.width = w; d.height = h
  return true
}

let seq = 0
export function newPartId(d: Doc): string {
  let id: string
  do { id = `p${Date.now().toString(36)}${(seq++).toString(36)}` } while (d.parts.some(p => p.id === id))
  return id
}

type TiledProp = { name: string; type: string; value: string | number }
type TiledObject = { id: number; name: string; type: string; x: number; y: number; width: number; height: number; properties?: TiledProp[] }
type TiledLayer = { type: string; name: string; data?: number[]; objects?: TiledObject[] }
export type TiledMap = { type: 'map'; orientation: string; width: number; height: number; tilewidth: number; tileheight: number; layers: TiledLayer[]; [k: string]: unknown }

/** The document as a Tiled map (isometric). Object positions follow Tiled's isometric convention: cells × tile height. */
export function toTiled(d: Doc, tileW = 64, tileH = 32): TiledMap {
  const data: number[] = []
  for (let y = 0; y < d.height; y++) for (let x = 0; x < d.width; x++) data.push(d.roads.has(key(x, y)) ? 1 : 0)
  return {
    type: 'map', version: '1.10', tiledversion: '1.10.2', orientation: 'isometric', renderorder: 'right-down', infinite: false,
    width: d.width, height: d.height, tilewidth: tileW, tileheight: tileH, nextlayerid: 3, nextobjectid: d.parts.length + 1,
    properties: [{ name: 'bifrost.layout', type: 'int', value: 2 }],
    tilesets: [{ firstgid: 1, name: 'roads', tilewidth: tileW, tileheight: tileH, tilecount: 1, columns: 1 }],
    layers: [
      { id: 1, type: 'tilelayer', name: 'roads', width: d.width, height: d.height, data, opacity: 1, visible: true, x: 0, y: 0 } as TiledLayer,
      { id: 2, type: 'objectgroup', name: 'parts', opacity: 1, visible: true, x: 0, y: 0, objects: d.parts.map((p, i) => ({
        id: i + 1, name: p.id, type: p.kind, x: p.x * tileH, y: p.y * tileH, width: p.size * tileH, height: p.size * tileH, rotation: 0, visible: true,
        properties: [{ name: 'binds', type: 'string', value: p.binds }, { name: 'size', type: 'int', value: p.size }, ...(p.label ? [{ name: 'label', type: 'string', value: p.label }] : [])],
      })) } as TiledLayer,
      ...(d.zones?.length ? [{ id: 3, type: 'objectgroup', name: 'zones', opacity: 1, visible: true, x: 0, y: 0, objects: d.zones.map((z, i) => ({
        id: d.parts.length + i + 1, name: z.name, type: 'zone', x: z.x * tileH, y: z.y * tileH, width: z.w * tileH, height: z.h * tileH, rotation: 0, visible: true,
        properties: [{ name: 'tint', type: 'string', value: z.tint }],
      })) } as TiledLayer] : []),
    ],
  }
}

/** Reads a saved map back; null when it is not one of ours. Parts that no longer fit (off the land, overlapping) are dropped. */
export function fromTiled(m: unknown): Doc | null {
  const t = m as Partial<TiledMap> | null
  if (!t || t.type !== 'map' || t.orientation !== 'isometric' || !Array.isArray(t.layers)) return null
  const w = Number(t.width), h = Number(t.height), th = Number(t.tileheight) || 32
  if (!Number.isInteger(w) || !Number.isInteger(h) || w < 1 || h < 1 || w > LAND_MAX || h > LAND_MAX) return null
  const d: Doc = { width: w, height: h, roads: new Set(), parts: [] }
  const roads = t.layers.find(l => l.type === 'tilelayer' && l.name === 'roads')
  roads?.data?.forEach((v, i) => { if (v && i < w * h) d.roads.add(key(i % w, Math.floor(i / w))) })
  const parts = t.layers.find(l => l.type === 'objectgroup' && l.name === 'parts')
  for (const o of parts?.objects ?? []) {
    const size = Math.round(o.width / th)
    const x = Math.round(o.x / th), y = Math.round(o.y / th)
    if (![1, 2, 4, 8].includes(size) || !o.type || !canPlace(d, x, y, size)) continue
    const binds = o.properties?.find(p => p.name === 'binds')?.value
    const label = o.properties?.find(p => p.name === 'label')?.value
    d.parts.push({ id: o.name || newPartId(d), kind: o.type, x, y, size: size as PartSize, binds: typeof binds === 'string' ? binds : '', ...(typeof label === 'string' && label.trim() ? { label: label.trim().slice(0, LABEL_MAX) } : {}) })
  }
  const zones = t.layers.find(l => l.type === 'objectgroup' && l.name === 'zones')
  for (const o of zones?.objects ?? []) {
    const tint = o.properties?.find(p => p.name === 'tint')?.value
    const z = { name: o.name, x: Math.round(o.x / th), y: Math.round(o.y / th), w: Math.round(o.width / th), h: Math.round(o.height / th), tint: typeof tint === 'string' && /^#[0-9a-f]{6}$/i.test(tint) ? tint : '#e8e4d8' }
    if (z.name && z.w > 0 && z.h > 0 && inLand(d, z.x, z.y)) (d.zones ??= []).push(z)
  }
  return d
}

export type Footprint = { id: string; x: number; y: number; size: number }

/**
 * Back-to-front order for squares on the isometric grid. A sum of coordinates
 * is not enough: a small building beside a big one has a smaller sum and yet
 * stands in front of it. A goes before B when A lies wholly behind B on one
 * axis and not wholly in front of it on the other (then they sit side by side
 * on screen and do not overlap).
 */
export function depthOrder<T extends Footprint>(items: readonly T[]): T[] {
  const n = items.length
  const after: number[][] = items.map(() => [])
  const blockers = new Array<number>(n).fill(0)
  const behind = (a: T, b: T) =>
    (a.x + a.size <= b.x && !(b.y + b.size <= a.y)) || (a.y + a.size <= b.y && !(b.x + b.size <= a.x))
  for (let i = 0; i < n; i++) for (let j = 0; j < n; j++) {
    if (i !== j && behind(items[i], items[j])) { after[i].push(j); blockers[j]++ }
  }
  const rank = (i: number) => items[i].x + items[i].y + items[i].size
  const ready: number[] = []
  for (let i = 0; i < n; i++) if (blockers[i] === 0) ready.push(i)
  const out: T[] = []
  while (ready.length) {
    let best = 0
    for (let k = 1; k < ready.length; k++) if (rank(ready[k]) < rank(ready[best])) best = k
    const i = ready.splice(best, 1)[0]
    out.push(items[i])
    for (const j of after[i]) if (--blockers[j] === 0) ready.push(j)
  }
  return out
}

export type PlaceCheck = 'ok' | 'tight' | 'blocked'

/** Whether a square can stand here: blocked if it does not fit; tight if it fits but leaves no free cell to a neighbour (allowed: four small ones may make one big one). */
export function placeCheck(d: Doc, x: number, y: number, size: number, ignoreId?: string): PlaceCheck {
  if (!canPlace(d, x, y, size, ignoreId)) return 'blocked'
  const near = d.parts.some(p => p.id !== ignoreId && x < p.x + p.size + 1 && p.x < x + size + 1 && y < p.y + p.size + 1 && p.y < y + size + 1)
  return near ? 'tight' : 'ok'
}

export type Crawlerway = {
  cells: Set<string>; ends: { x: number; y: number }[]; served: Set<string>
  /** Each leg on its own, keyed '<from part id>><to part id>', so the leg a real run moves along can be lit. */
  legs: Map<string, Set<string>>
}

const STEPS = [[0, -1], [1, 0], [0, 1], [-1, 0]] as const

/**
 * The crawlerway (the Owner, 2026-11-18): the rocket's line runs from the
 * horizontal hangar (CI) to the vertical assembly building (STG) and on to
 * the pad (PROD); the satellites' line runs from their hangar to the same
 * pad. It follows from where the buildings stand, so it is worked out here
 * and never saved: cheapest way round the buildings, few turns, crossing roads
 * rather than running along them, legs sharing what is already laid.
 */
export function crawlerway(d: Doc): Crawlerway {
  const out: Crawlerway = { cells: new Set(), ends: [], served: new Set(), legs: new Map() }
  const front = (p: Placed) => ({ x: p.x + Math.floor(p.size / 2) - 1, y: p.y + p.size })
  const open = (x: number, y: number) => inLand(d, x, y) && !partAt(d, x, y)
  const W = d.width, id = (x: number, y: number, dir: number) => (y * W + x) * 4 + dir
  const leg = (from: Placed, to: Placed): boolean => {
    const start = front(from), goal = front(to)
    if (!open(start.x, start.y) || !open(goal.x, goal.y)) return false
    const best = new Map<number, number>(), prev = new Map<number, number>(), queue: { c: number; n: number }[] = []
    const push = (n: number, c: number, p?: number) => {
      if (c >= (best.get(n) ?? Infinity)) return
      best.set(n, c); if (p !== undefined) prev.set(n, p)
      let i = queue.length; queue.push({ c, n })
      while (i > 0) { const up = (i - 1) >> 1; if (queue[up].c <= c) break; [queue[i], queue[up]] = [queue[up], queue[i]]; i = up }
    }
    const pop = () => {
      const t = queue[0], last = queue.pop()!
      if (queue.length) {
        queue[0] = last
        for (let i = 0; ;) { let m = i; for (const k of [2 * i + 1, 2 * i + 2]) if (k < queue.length && queue[k].c < queue[m].c) m = k; if (m === i) break; [queue[i], queue[m]] = [queue[m], queue[i]]; i = m }
      }
      return t
    }
    push(id(start.x, start.y, 2), 0)
    let end = -1
    while (queue.length) {
      const { c, n } = pop()
      if (c > (best.get(n) ?? Infinity)) continue
      const dir = n % 4, cell = (n - dir) / 4, x = cell % W, y = (cell - x) / W
      if (x === goal.x && y === goal.y) { end = n; break }
      STEPS.forEach(([dx, dy], next) => {
        const nx = x + dx, ny = y + dy
        if (!open(nx, ny)) return
        const k = key(nx, ny), step = out.cells.has(k) ? 0.15 : d.roads.has(k) ? 2.5 : 1
        push(id(nx, ny, next), c + step + (next === dir ? 0 : 0.4), n)
      })
    }
    if (end < 0) return false
    const cells = new Set<string>()
    for (let n: number | undefined = end; n !== undefined; n = prev.get(n)) { const cell = (n - n % 4) / 4, x = cell % W; cells.add(key(x, (cell - x) / W)) }
    for (const c of cells) out.cells.add(c)
    out.legs.set(`${from.id}>${to.id}`, cells)
    for (const e of [start, goal]) if (!out.ends.some(o => o.x === e.x && o.y === e.y)) out.ends.push(e)
    out.served.add(from.id); out.served.add(to.id)
    return true
  }
  const near = (a: Placed) => (b: Placed, c: Placed) => Math.abs(b.x - a.x) + Math.abs(b.y - a.y) - (Math.abs(c.x - a.x) + Math.abs(c.y - a.y)) || b.id.localeCompare(c.id)
  const vab = d.parts.find(p => p.kind === 'assembly')
  const hif = d.parts.find(p => p.kind === 'hif')
  const sat = d.parts.find(p => p.kind === 'satellites')
  if (hif && vab) leg(hif, vab)
  // Two pads (2026-11-19): the assembly building's booster goes to the booster pad, the ship hall's ships to the ship pad;
  // a pad not yet told which it is takes both.
  const pads = d.parts.filter(p => p.kind === 'pad')
  const shipPad = (p: Placed) => p.binds === 'stage:ship-pad'
  if (vab) for (const pad of [...pads].filter(p => !shipPad(p)).sort(near(vab))) leg(vab, pad)
  if (sat) for (const pad of [...pads].filter(p => shipPad(p) || !p.binds).sort(near(sat))) leg(sat, pad)
  // Every tenant's campus sends its satellite to the hall (2026-11-18: many factories, one rocket).
  if (sat) for (const c of d.parts.filter(p => p.kind === 'campus' || p.kind === 'campus4').sort(near(sat))) leg(c, sat)
  return out
}

/** The building a worker goes to: the first of its kind, the one bound to `binds` first. */
export function targetPart(d: Doc, t: { kind: string; binds?: string }): Placed | undefined {
  const of = d.parts.filter(p => p.kind === t.kind)
  return of.find(p => t.binds && p.binds === t.binds) ?? of[0]
}

/** The cell a worker stands on at a building: just in front of its door. */
export function doorOf(p: Placed): { x: number; y: number } {
  return { x: p.x + Math.floor(p.size / 2), y: p.y + p.size }
}

/**
 * A worker's walk from one door to another, in order: along the roads where
 * there are roads (a step on grass costs four on a road), round the
 * buildings. Empty when there is no way.
 */
export function walkPath(d: Doc, from: { x: number; y: number }, to: { x: number; y: number }): { x: number; y: number }[] {
  const open = (x: number, y: number) => inLand(d, x, y) && !partAt(d, x, y)
  if (!open(from.x, from.y) || !open(to.x, to.y)) return []
  const W = d.width, idx = (x: number, y: number) => y * W + x
  const best = new Map<number, number>([[idx(from.x, from.y), 0]]), prev = new Map<number, number>()
  const queue: { c: number; n: number }[] = [{ c: 0, n: idx(from.x, from.y) }]
  while (queue.length) {
    queue.sort((a, b) => a.c - b.c)
    const { c, n } = queue.shift()!
    if (c > (best.get(n) ?? Infinity)) continue
    const x = n % W, y = (n - x) / W
    if (x === to.x && y === to.y) break
    for (const [dx, dy] of STEPS) {
      const nx = x + dx, ny = y + dy
      if (!open(nx, ny)) continue
      const m = idx(nx, ny), nc = c + (d.roads.has(key(nx, ny)) ? 1 : 4)
      if (nc < (best.get(m) ?? Infinity)) { best.set(m, nc); prev.set(m, n); queue.push({ c: nc, n: m }) }
    }
  }
  const end = idx(to.x, to.y)
  if (!best.has(end)) return []
  const out: { x: number; y: number }[] = []
  for (let n: number | undefined = end; n !== undefined; n = prev.get(n)) out.unshift({ x: n % W, y: (n - n % W) / W })
  return out
}
