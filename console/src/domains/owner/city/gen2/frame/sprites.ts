import type { Light } from './light'
import { Draw, faces, hash, INK, type Sprite } from './kit'

/**
 * The second generation's look (the Owner, 2026-11-19: keep the first generation's style — flat
 * pastel isometric, dark outlines, rows of windows, a hue band per place, rooftop units; refine it,
 * don't restart it). One sprite per kind of place, footprint, hue and light; drawn with the kit as SVG,
 * turned into a texture by the scene.
 *
 * What the art says is fixed by what the place is: a district is a tall hall, a building a
 * workshop with a saw-tooth roof, a room a floor with low walls. The hue only tells places apart;
 * it carries no state. State (readings) is drawn on top later, from contracts.
 */

/** Hues to tell places apart, in the order their parent lists them. */
export const HUES = ['#f2d27a', '#f4a9b8', '#f6a65a', '#7fd1c1', '#8fb6f2', '#c3a6f2', '#a8d672', '#f29b8f'] as const

const WALLS: Record<Light, { top: string; left: string; right: string; roof: string; window: string; lit: string; plinth: ReturnType<typeof faces>; door: string; floor: string }> = {
  day: { top: '#f6f4ef', left: '#e9e4da', right: '#d4cec2', roof: '#efece5', window: '#d8ecfb', lit: '#ffe39a', plinth: faces('#e2ddd2', '#d3cdc1', '#c2bcb0'), door: '#9aa3b0', floor: '#f5f1e8' },
  dusk: { top: '#f7e6d6', left: '#e8cfba', right: '#d0b29c', roof: '#efdccb', window: '#5b5068', lit: '#ffd48a', plinth: faces('#e4cdb9', '#d2b9a3', '#bea38c'), door: '#7d7286', floor: '#f3e3d3' },
  night: { top: '#4a4f72', left: '#3a3e5c', right: '#2c2f48', roof: '#43476a', window: '#20223a', lit: '#ffd27a', plinth: faces('#3a3e5c', '#30344e', '#262a40'), door: '#1c1e30', floor: '#3d4263' },
}

/** A colour moved toward white (t > 0) or toward the night's ink (t < 0). */
export function shade(hex: string, t: number): string {
  const n = parseInt(hex.slice(1), 16)
  const c = [n >> 16, (n >> 8) & 255, n & 255]
  const to = t >= 0 ? [255, 255, 255] : [29, 32, 51]
  const k = Math.abs(t)
  return `#${c.map((v, i) => Math.round(v + (to[i] - v) * k).toString(16).padStart(2, '0')).join('')}`
}

/** A wall washed with the place's hue, so places read apart at a glance: lighter by day, deeper at night. */
const wash = (hue: string, light: Light, side: 'left' | 'right') =>
  light === 'night' ? shade(hue, side === 'left' ? -0.62 : -0.72) : light === 'dusk' ? shade(hue, side === 'left' ? 0.38 : 0.22) : shade(hue, side === 'left' ? 0.6 : 0.42)

const hueFor = (hue: string, light: Light) => light === 'night' ? shade(hue, -0.35) : light === 'dusk' ? shade(hue, -0.08) : hue

/** Which windows are lit: by day none; at dusk some; at night most, the same ones every time for a place. */
const litBy = (light: Light, seed: number) => (r: number, c: number) =>
  light === 'day' ? false : ((hash(`${seed}:${r}:${c}`) % 100) < (light === 'night' ? 72 : 35))

/**
 * How far a thing stands back from its plot's edge (the Owner, 2026-11-19: streets and green belts
 * between neighbours). The way and the buffer are outside the thing now (geo.ts), so it stands
 * only a hair in from its own footprint, to show the lot's edge.
 */
export const setback = (n: number) => n >= 2 ? 0.14 : 0.08

/** A district: a tall hall on a plinth, a hue band under the parapet, rows of windows, a door, rooftop units. */
export function districtHall(fw: number, fd: number, hue: string, light: Light, seed = 0): Sprite {
  const W = WALLS[light]
  const n = Math.min(fw, fd)
  const h = 30 + n * 9
  const g = new Draw(fw, fd, h + 30)
  const i = setback(n), x0 = i, y0 = i, x1 = fw - i, y1 = fd - i, w = x1 - x0, d = y1 - y0
  g.shadow(x0, y0, w, d, 0.5)
  g.box(x0 - 0.12, y0 - 0.12, w + 0.24, d + 0.24, 4, W.plinth)
  g.box(x0, y0, w, d, h, faces(W.top, wash(hue, light, 'left'), wash(hue, light, 'right')), 4)
  const band = hueFor(hue, light)
  g.onLeft(x0, y1, 0, w, h - 6, h + 1, band, 1)
  g.onRight(x1, y0, 0, d, h - 6, h + 1, shade(band, -0.12), 1)
  const rows = Math.max(1, Math.floor((h - 22) / 13)), lit = litBy(light, seed)
  g.windows('left', x0, y1, w, 18, h - 10, rows, Math.round(w * 2.4), { fill: W.window, lit, litFill: W.lit })
  g.windows('right', x1, y0, d, 18, h - 10, rows, Math.round(d * 2.4), { fill: W.window, lit: (r, c) => lit(r + 7, c), litFill: W.lit })
  g.onLeft(x0, y1, w * 0.42, w * 0.58, 4, 16, W.door, 1.1)
  // Parapet: a lip around the roof, then the rooftop units.
  g.poly(W.roof, [[x0 + 0.12, y0 + 0.12, h + 4], [x1 - 0.12, y0 + 0.12, h + 4], [x1 - 0.12, y1 - 0.12, h + 4], [x0 + 0.12, y1 - 0.12, h + 4]], 0.8)
  g.unit(x0 + w * 0.25, y0 + d * 0.25, h + 4, 0.5)
  g.unit(x0 + w * 0.6, y0 + d * 0.3, h + 4, 0.4)
  return g.done()
}

/** A building: a workshop with a saw-tooth roof of hue-glazed skylights, windows and a roller door. */
export function workshop(fw: number, fd: number, hue: string, light: Light, seed = 0): Sprite {
  const W = WALLS[light]
  const n = Math.min(fw, fd)
  const h = 22 + n * 5
  const g = new Draw(fw, fd, h + 30)
  const i = setback(n), x0 = i, y0 = i, x1 = fw - i, y1 = fd - i, w = x1 - x0, d = y1 - y0
  g.shadow(x0, y0, w, d, 0.4)
  g.box(x0 - 0.1, y0 - 0.1, w + 0.2, d + 0.2, 3, W.plinth)
  g.box(x0, y0, w, d, h, faces(W.top, wash(hue, light, 'left'), wash(hue, light, 'right')), 3)
  const glass = light === 'day' ? shade(hue, 0.45) : light === 'dusk' ? shade(hue, 0.15) : W.lit
  const teeth = Math.max(2, Math.round(w * 1.2))
  for (let k = 0; k < teeth; k++) {
    const a = x0 + k * w / teeth, b = a + w / teeth
    g.poly(glass, [[b, y0, h + 3 + 10], [b, y1, h + 3 + 10], [b, y1, h + 3], [b, y0, h + 3]], 0.9)
    g.poly(W.roof, [[a, y0, h + 3], [b, y0, h + 3 + 10], [b, y1, h + 3 + 10], [a, y1, h + 3]], 0.9)
  }
  const band = hueFor(hue, light)
  g.onLeft(x0, y1, 0, w, h - 5, h, band, 0.9)
  const lit = litBy(light, seed)
  g.windows('left', x0, y1, w, 14, h - 8, 1, Math.round(w * 2.6), { fill: W.window, lit, litFill: W.lit, tall: 0.7 })
  g.windows('right', x1, y0, d, 8, h - 8, 2, Math.round(d * 2), { fill: W.window, lit: (r, c) => lit(r + 3, c), litFill: W.lit })
  // A roller door, ribbed.
  const da = w * 0.12, db = w * 0.36
  g.onLeft(x0, y1, da, db, 3, 15, W.door, 1.1)
  for (let z = 6; z < 15; z += 3) g.line([x0 + da, y1, z], [x0 + db, y1, z], INK, 0.5)
  return g.done()
}

/**
 * A room, cut away like the storey it is on (the Owner, 2026-11-19: a room has walls and doors): a
 * floor in its hue, the two back walls up, the two front ones down to their foot with a door in each
 * long enough, and a little furniture, so it reads as a room and not a rug.
 */
export function roomFloor(fw: number, fd: number, hue: string, light: Light): Sprite {
  const W = WALLS[light]
  const g = new Draw(fw, fd, 34)
  // The room's walls stand on its own footprint; the corridor and the threshold are outside it (geo.ts).
  const i = 0.06, x0 = i, y0 = i, x1 = fw - i, y1 = fd - i, w = x1 - x0, d = y1 - y0
  const t = 0.12, HIGH = 18, LOW = 5, wall = faces(W.top, W.left, W.right)
  const carpet = light === 'night' ? shade(hue, -0.5) : light === 'dusk' ? shade(hue, 0.3) : shade(hue, 0.6)
  g.box(x0, y0, w, d, 3, faces(carpet, W.left, W.right))
  g.box(x0, y0, w, t, HIGH, wall, 3)
  g.box(x0, y0 + t, t, d - t, HIGH, wall, 3)
  furniture(g, x0 + t, y0 + t, w - 2 * t, d - 2 * t, light)
  const door = Math.min(0.7, Math.max(w, d) * 0.3)
  // Front-right wall (along y at x1): a door in it when the room is deep enough.
  if (d >= 1.5) {
    const b0 = y0 + d * 0.55 - door / 2
    g.box(x1 - t, y0 + t, t, b0 - y0 - t, LOW, wall, 3)
    g.box(x1 - t, b0 + door, t, y1 - t - b0 - door, LOW, wall, 3)
  } else g.box(x1 - t, y0 + t, t, d - 2 * t, LOW, wall, 3)
  // Front-left wall (along x at y1): always a door.
  const a0 = x0 + w * 0.45 - door / 2
  g.box(x0, y1 - t, a0 - x0, t, LOW, wall, 3)
  g.box(a0 + door, y1 - t, x1 - a0 - door, t, LOW, wall, 3)
  return g.done()
}

/** A desk and chair, and against the back wall a cabinet; a bigger room gets a second desk. */
function furniture(g: Draw, x: number, y: number, w: number, d: number, light: Light) {
  const wood = light === 'night' ? faces('#6b5a4a', '#55473a', '#463a30') : faces('#d9b48a', '#c09a70', '#a8845c')
  const metal = light === 'night' ? faces('#4a4f6a', '#3c4058', '#30344a') : faces('#e4e2dc', '#c9c6be', '#b3afa6')
  const desk = (dx: number, dy: number) => {
    const dw = Math.min(0.9, w * 0.4), dd = Math.min(0.45, d * 0.35)
    g.box(dx, dy, dw, dd, 7, wood, 3, 1)
    g.box(dx + dw * 0.35, dy + dd + 0.08, 0.24, 0.22, 5, metal, 3, 1)
  }
  if (w >= 0.9 && d >= 0.9) desk(x + w * 0.3, y + d * 0.2)
  if (w >= 1.6 && d >= 1.2) g.box(x + 0.04, y + d * 0.55, 0.28, Math.min(0.8, d * 0.3), 14, metal, 3, 1)
  if (w >= 3 && d >= 2) desk(x + w * 0.62, y + d * 0.5)
}

/**
 * A district seen from the town (the Owner, 2026-11-19: a block of a city is many buildings, never
 * one): as many buildings as the district holds, and never fewer than its footprint calls for (2 on
 * a 2-wide block, 4 on a 4-wide one, 6 on an 8-wide one), on its paved block, a tree where a lot is
 * left over.
 */
export function districtBlock(fw: number, fd: number, hue: string, light: Light, seed: number, holds: number): Sprite {
  const W = WALLS[light]
  const n = Math.min(fw, fd)
  if (n < 2) return districtHall(fw, fd, hue, light, seed)
  const count = Math.max(holds, n >= 8 ? 6 : n >= 4 ? 4 : 2) + (fw !== fd ? 1 : 0)
  const i = setback(n), x0 = i, y0 = i, x1 = fw - i, y1 = fd - i, w = x1 - x0, d = y1 - y0
  const cols = Math.max(1, Math.round(Math.sqrt(count * w / d))), rows = Math.ceil(count / cols)
  // Between the buildings of a block, the same width as every way (the Owner, 2026-11-19).
  const gap = Math.min(0.8, Math.min(w / cols, d / rows) * 0.35)
  const cw = (w - gap * (cols - 1)) / cols, cd = (d - gap * (rows - 1)) / rows
  const top = 20 + n * 9
  const g = new Draw(fw, fd, top + 40)
  const paving = light === 'night' ? '#3a3e5c' : light === 'dusk' ? '#e6d2c0' : '#ebe6dc'
  g.poly(paving, [[x0 - 0.12, y0 - 0.12, 0], [x1 + 0.12, y0 - 0.12, 0], [x1 + 0.12, y1 + 0.12, 0], [x0 - 0.12, y1 + 0.12, 0]], 1.2)
  const lots: { r: number; c: number; k: number }[] = []
  for (let r = 0; r < rows; r++) for (let c = 0; c < cols; c++) lots.push({ r, c, k: r * cols + c })
  lots.sort((a, b) => (a.r + a.c) - (b.r + b.c))
  const band = hueFor(hue, light), lit = litBy(light, seed)
  for (const { r, c, k } of lots) {
    const bx = x0 + c * (cw + gap), by = y0 + r * (cd + gap)
    if (k >= count) continue
    const h = Math.round(16 + n * 3 + (hash(`${seed}:${k}`) % 5) * n * 1.7)
    g.shadow(bx, by, cw, cd, 0.35)
    g.box(bx, by, cw, cd, h, faces(W.top, wash(hue, light, 'left'), wash(hue, light, 'right')), 0, 1.4)
    g.onLeft(bx, by + cd, 0, cw, h - 5, h, band, 0.8)
    const rws = Math.max(1, Math.floor((h - 14) / 11))
    g.windows('left', bx, by + cd, cw, 8, h - 8, rws, Math.max(1, Math.round(cw * 2.4)), { fill: W.window, lit: (a, b) => lit(a + k * 5, b), litFill: W.lit })
    g.windows('right', bx + cw, by, cd, 8, h - 8, rws, Math.max(1, Math.round(cd * 2.4)), { fill: W.window, lit: (a, b) => lit(a + k * 5 + 3, b), litFill: W.lit })
    if (cw > 0.9 && cd > 0.9) g.unit(bx + cw * 0.3, by + cd * 0.3, h, Math.min(0.45, cw * 0.3))
  }
  return g.done()
}

/** One wing of a building: where one of its rooms is, in the building's own footprint, and the room's hue. */
export type Wing = { x: number; y: number; w: number; h: number; hue: string }

/**
 * A building seen from its street (the Owner, 2026-11-19: what you see outside is what is inside):
 * a low podium over its footprint, and on it one wing per room, where the room is and as big, in
 * the room's hue; the gaps between wings are where the corridors run inside. A building with no
 * rooms is a plain workshop.
 */
export function buildingWings(fw: number, fd: number, hue: string, light: Light, seed: number, wings: readonly Wing[]): Sprite {
  if (wings.length === 0) return workshop(fw, fd, hue, light, seed)
  const W = WALLS[light]
  const n = Math.min(fw, fd)
  const POD = 5
  const tallest = 14 + n * 7
  const g = new Draw(fw, fd, tallest + 30)
  const i = setback(n)
  g.shadow(i, i, fw - 2 * i, fd - 2 * i, 0.4)
  // The podium: the building's ground floor in its own hue, a shade lighter than its wings.
  const deck = light === 'night' ? shade(hue, -0.55) : light === 'dusk' ? shade(hue, 0.45) : shade(hue, 0.7)
  g.box(i, i, fw - 2 * i, fd - 2 * i, POD, faces(deck, wash(hue, light, 'left'), wash(hue, light, 'right')), 0, 1.4)
  const lit = litBy(light, seed)
  const order = wings.map((w, k) => ({ ...w, k })).sort((a, b) => (a.x + a.y + (a.w + a.h) / 2) - (b.x + b.y + (b.w + b.h) / 2))
  for (const w of order) {
    const side = Math.min(w.w, w.h)
    const h = Math.round(8 + n * 3 + Math.sqrt(w.w * w.h) * (n >= 4 ? 4 : 6))
    g.box(w.x, w.y, w.w, w.h, h, faces(W.top, wash(w.hue, light, 'left'), wash(w.hue, light, 'right')), POD, 1.2)
    g.onLeft(w.x, w.y + w.h, 0, w.w, POD + h - 4, POD + h, hueFor(w.hue, light), 0.8)
    if (side > 0.35) {
      const rows = Math.max(1, Math.floor((h - 8) / 9))
      g.windows('left', w.x, w.y + w.h, w.w, POD + 4, POD + h - 6, rows, Math.max(1, Math.round(w.w * 2.6)), { fill: W.window, lit: (a, b) => lit(a + w.k * 7, b), litFill: W.lit })
      g.windows('right', w.x + w.w, w.y, w.h, POD + 4, POD + h - 6, rows, Math.max(1, Math.round(w.h * 2.6)), { fill: W.window, lit: (a, b) => lit(a + w.k * 7 + 3, b), litFill: W.lit })
    }
  }
  return g.done()
}

export type SpriteKey = string
export const spriteKey = (kind: string, w: number, d: number, hue: string, light: Light, seed: number, count = 0): SpriteKey => `g2:${kind}:${w}x${d}:${hue}:${light}:${seed}:${count}`

export function spriteFor(kind: 'district' | 'building' | 'room' | 'town', w: number, d: number, hue: string, light: Light, seed: number, count = 0): Sprite {
  return kind === 'room' ? roomFloor(w, d, hue, light) : kind === 'building' ? workshop(w, d, hue, light, seed) : districtBlock(w, d, hue, light, seed, count)
}

export const spriteUrl = (s: Sprite) => `data:image/svg+xml;utf8,${encodeURIComponent(s.svg)}`
