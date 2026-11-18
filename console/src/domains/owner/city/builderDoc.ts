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
}
export type Doc = { width: number; height: number; roads: Set<string>; parts: Placed[] }

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
        properties: [{ name: 'binds', type: 'string', value: p.binds }, { name: 'size', type: 'int', value: p.size }],
      })) } as TiledLayer,
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
    d.parts.push({ id: o.name || newPartId(d), kind: o.type, x, y, size: size as PartSize, binds: typeof binds === 'string' ? binds : '' })
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

export type Crawlerway = { cells: Set<string>; ends: { x: number; y: number }[]; served: Set<string> }

const STEPS = [[0, -1], [1, 0], [0, 1], [-1, 0]] as const

/**
 * The crawlerway: from the assembly building's great door to the front of
 * every launch pad (the Owner, 2026-11-17: the main axis, merge to CI to
 * release). It follows from where he put them, so it is worked out here and
 * never saved: cheapest way round the buildings, few turns, crossing his
 * roads rather than running along them, and pads share the trunk.
 */
export function crawlerway(d: Doc): Crawlerway {
  const out: Crawlerway = { cells: new Set(), ends: [], served: new Set() }
  const hall = d.parts.find(p => p.kind === 'assembly')
  if (!hall) return out
  const front = (p: Placed) => ({ x: p.x + Math.floor(p.size / 2) - 1, y: p.y + p.size })
  const open = (x: number, y: number) => inLand(d, x, y) && !partAt(d, x, y)
  const start = front(hall)
  if (!open(start.x, start.y)) return out
  const pads = d.parts.filter(p => p.kind === 'pad').sort((a, b) => Math.abs(a.x - hall.x) + Math.abs(a.y - hall.y) - (Math.abs(b.x - hall.x) + Math.abs(b.y - hall.y)) || a.id.localeCompare(b.id))
  const W = d.width, id = (x: number, y: number, dir: number) => (y * W + x) * 4 + dir
  for (const pad of pads) {
    const goal = front(pad)
    if (!open(goal.x, goal.y)) continue
    const best = new Map<number, number>(), from = new Map<number, number>(), queue: { c: number; n: number }[] = []
    const push = (n: number, c: number, prev?: number) => {
      if (c >= (best.get(n) ?? Infinity)) return
      best.set(n, c); if (prev !== undefined) from.set(n, prev)
      let i = queue.length; queue.push({ c, n })
      while (i > 0) { const up = (i - 1) >> 1; if (queue[up].c <= c) break; [queue[i], queue[up]] = [queue[up], queue[i]]; i = up }
    }
    const pop = () => {
      const top = queue[0], last = queue.pop()!
      if (queue.length) {
        queue[0] = last
        for (let i = 0; ;) { let m = i; for (const k of [2 * i + 1, 2 * i + 2]) if (k < queue.length && queue[k].c < queue[m].c) m = k; if (m === i) break; [queue[i], queue[m]] = [queue[m], queue[i]]; i = m }
      }
      return top
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
    if (end < 0) continue
    for (let n: number | undefined = end; n !== undefined; n = from.get(n)) { const cell = (n - n % 4) / 4, x = cell % W; out.cells.add(key(x, (cell - x) / W)) }
    out.ends.push(goal); out.served.add(pad.id)
  }
  if (out.served.size) { out.ends.push(start); out.served.add(hall.id) }
  return out
}
