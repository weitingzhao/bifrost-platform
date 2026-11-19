import type { Light } from './light'
import { Draw, faces, hash, INK, type Sprite } from './kit'

/**
 * The second generation's look (the Owner, 2026-11-19: keep the first generation's style — flat
 * pastel isometric, dark outlines, rows of windows, a hue band per place, rooftop units; refine it,
 * don't restart it). One sprite per kind of place, size, hue and light; drawn with the kit as SVG,
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

/** A district: a tall hall on a plinth, a hue band under the parapet, rows of windows, a door, rooftop units. */
export function districtHall(n: number, hue: string, light: Light, seed = 0): Sprite {
  const W = WALLS[light]
  const h = 30 + n * 9
  const g = new Draw(n, n, h + 30)
  const i = 0.32, x0 = i, y0 = i, x1 = n - i, y1 = n - i, w = x1 - x0, d = y1 - y0
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
export function workshop(n: number, hue: string, light: Light, seed = 0): Sprite {
  const W = WALLS[light]
  const h = 22 + n * 5
  const g = new Draw(n, n, h + 30)
  const i = 0.3, x0 = i, y0 = i, x1 = n - i, y1 = n - i, w = x1 - x0, d = y1 - y0
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

/** A room: a floor with low walls on its two back sides and a hue rug, so it reads as inside. */
export function roomFloor(n: number, hue: string, light: Light): Sprite {
  const W = WALLS[light]
  const g = new Draw(n, n, 26)
  const i = 0.18, x0 = i, y0 = i, x1 = n - i, y1 = n - i, w = x1 - x0, d = y1 - y0
  g.box(x0, y0, w, d, 4, faces(W.floor, W.left, W.right))
  const t = 0.1, hw = 14
  g.box(x0, y0, w, t, hw, faces(W.top, W.left, W.right), 4)
  g.box(x0, y0 + t, t, d - t, hw, faces(W.top, W.left, W.right), 4)
  const rug = light === 'night' ? shade(hue, -0.25) : shade(hue, 0.35)
  g.poly(rug, [[x0 + w * 0.22, y0 + d * 0.25, 4], [x1 - w * 0.15, y0 + d * 0.25, 4], [x1 - w * 0.15, y1 - d * 0.15, 4], [x0 + w * 0.22, y1 - d * 0.15, 4]], 1)
  return g.done()
}

export type SpriteKey = string
export const spriteKey = (kind: string, n: number, hue: string, light: Light, seed: number): SpriteKey => `g2:${kind}:${n}:${hue}:${light}:${seed}`

export function spriteFor(kind: 'district' | 'building' | 'room' | 'town', n: number, hue: string, light: Light, seed: number): Sprite {
  return kind === 'room' ? roomFloor(n, hue, light) : kind === 'building' ? workshop(n, hue, light, seed) : districtHall(n, hue, light, seed)
}

export const spriteUrl = (s: Sprite) => `data:image/svg+xml;utf8,${encodeURIComponent(s.svg)}`
