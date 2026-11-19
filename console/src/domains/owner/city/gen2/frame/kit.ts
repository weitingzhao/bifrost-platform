/**
 * Copied from gen1/kit.ts (2026-11-19: the second generation keeps the first one's look; gen1 stays frozen).
 *
 * The drawing kit of the city's sprites (2026-11-17, the Owner: each kind of
 * facility its own recognisable building, detail like the reference, flat
 * pastel isometric with dark outlines). A sprite is an SVG string drawn on a
 * 2:1 grid of 64×32 cells; its anchor is the back corner of its footprint on
 * the ground, so the city places it by its cell. Phaser loads it as a
 * texture; the gallery shows it as an image.
 */

export const CW = 64
export const CH = 32
export const INK = '#2f2d3a'
export const STROKE = 1.7

export type Sprite = { svg: string; width: number; height: number; ax: number; ay: number }

/** A sprite under construction: footprint w×d cells, room above for height (px), and the parts drawn in order. */
export class Draw {
  readonly parts: string[] = []
  readonly ox: number
  readonly oy: number
  constructor(readonly w: number, readonly d: number, readonly above: number, readonly margin = 6) {
    this.ox = d * (CW / 2) + margin
    this.oy = above + margin
  }
  /** Grid (x, y) and height z (px) to the sprite's pixels. */
  p(x: number, y: number, z = 0): [number, number] {
    return [this.ox + (x - y) * (CW / 2), this.oy + (x + y) * (CH / 2) - z]
  }
  pts(...q: [number, number, number][]): string {
    return q.map(([x, y, z]) => this.p(x, y, z).map(v => v.toFixed(1)).join(',')).join(' ')
  }
  add(s: string) { this.parts.push(s); return this }
  poly(fill: string, q: [number, number, number][], sw = STROKE, stroke = INK) {
    return this.add(`<polygon points="${this.pts(...q)}" fill="${fill}" stroke="${stroke}" stroke-width="${sw}" stroke-linejoin="round"/>`)
  }
  /** A soft shadow on the ground, cast to the front right. */
  shadow(x: number, y: number, w: number, d: number, reach = 0.5) {
    reach = Math.min(reach, 0.5)
    return this.add(`<polygon points="${this.pts([x + w, y, 0], [x + w + reach, y + reach * 0.6, 0], [x + w + reach, y + d + reach, 0], [x + reach * 0.4, y + d + reach, 0], [x, y + d, 0], [x + w, y + d, 0])}" fill="#5a5880" opacity="0.1"/>`)
  }
  /** A box: its front-left (+y) face, front-right (+x) face and top. */
  box(x: number, y: number, w: number, d: number, h: number, c: { top: string; left: string; right: string }, z = 0, sw = STROKE) {
    this.poly(c.left, [[x, y + d, z], [x + w, y + d, z], [x + w, y + d, z + h], [x, y + d, z + h]], sw)
    this.poly(c.right, [[x + w, y, z], [x + w, y + d, z], [x + w, y + d, z + h], [x + w, y, z + h]], sw)
    this.poly(c.top, [[x, y, z + h], [x + w, y, z + h], [x + w, y + d, z + h], [x, y + d, z + h]], sw)
    return this
  }
  /** A quad on the +y face (along x from a to b, z from z0 to z1) or on the +x face (along y). */
  onLeft(x: number, y: number, a: number, b: number, z0: number, z1: number, fill: string, sw = 0.9) {
    return this.poly(fill, [[x + a, y, z0], [x + b, y, z0], [x + b, y, z1], [x + a, y, z1]], sw)
  }
  onRight(x: number, y: number, a: number, b: number, z0: number, z1: number, fill: string, sw = 0.9) {
    return this.poly(fill, [[x, y + a, z0], [x, y + b, z0], [x, y + b, z1], [x, y + a, z1]], sw)
  }
  /** A grid of windows on a face, each with a sill line; lit ones warm. */
  windows(face: 'left' | 'right', x: number, y: number, span: number, z0: number, z1: number, rows: number, cols: number, o: { fill?: string; lit?: (r: number, c: number) => boolean; litFill?: string; wide?: number; tall?: number } = {}) {
    const cw = span / cols, rh = (z1 - z0) / rows
    const ww = cw * (o.wide ?? 0.55), wh = rh * (o.tall ?? 0.55)
    for (let r = 0; r < rows; r++) for (let c = 0; c < cols; c++) {
      const a = c * cw + (cw - ww) / 2, b = a + ww
      const za = z0 + r * rh + (rh - wh) / 2, zb = za + wh
      const f = o.lit?.(r, c) ? (o.litFill ?? '#ffd76a') : (o.fill ?? '#f4f7fb')
      if (face === 'left') this.onLeft(x, y, a, b, za, zb, f, 0.8)
      else this.onRight(x, y, a, b, za, zb, f, 0.8)
    }
    return this
  }
  /** Text on the +y face (reads along x) or the +x face (reads along -y, left to right on screen). */
  text(face: 'left' | 'right', x: number, y: number, z: number, s: string, size = 9, fill = INK, weight = 600) {
    const [px, py] = this.p(x, y, z)
    const m = face === 'left' ? `matrix(0.894,0.447,0,1,${px.toFixed(1)},${py.toFixed(1)})` : `matrix(0.894,-0.447,0,1,${px.toFixed(1)},${py.toFixed(1)})`
    return this.add(`<text transform="${m}" font-family="'JetBrains Mono',ui-monospace,Menlo,monospace" font-size="${size}" font-weight="${weight}" fill="${fill}" letter-spacing="0.5">${s}</text>`)
  }
  /**
   * Free drawing on a face: `inner` is SVG in the face's own plane, origin at
   * grid (x, y, z), u to the right along the face (35.8 units to a cell), v up
   * the screen negative as usual. Circles and text drawn there lie on the wall.
   */
  face(face: 'left' | 'right', x: number, y: number, z: number, inner: string) {
    const [px, py] = this.p(x, y, z)
    const m = face === 'left' ? `matrix(0.894,0.447,0,1,${px.toFixed(1)},${py.toFixed(1)})` : `matrix(0.894,-0.447,0,1,${px.toFixed(1)},${py.toFixed(1)})`
    return this.add(`<g transform="${m}">${inner}</g>`)
  }
  /** Free drawing on the ground plane (or a roof at height z): u along +x, v along +y, 35.8 units to a cell. */
  floor(x: number, y: number, z: number, inner: string) {
    const [px, py] = this.p(x, y, z)
    return this.add(`<g transform="matrix(0.894,0.447,-0.894,0.447,${px.toFixed(1)},${py.toFixed(1)})">${inner}</g>`)
  }
  line(a: [number, number, number], b: [number, number, number], stroke = INK, sw = 1, extra = '') {
    const [x1, y1] = this.p(...a), [x2, y2] = this.p(...b)
    return this.add(`<line x1="${x1.toFixed(1)}" y1="${y1.toFixed(1)}" x2="${x2.toFixed(1)}" y2="${y2.toFixed(1)}" stroke="${stroke}" stroke-width="${sw}" stroke-linecap="round" ${extra}/>`)
  }
  /** A sign: a plate on a face with its text. */
  sign(face: 'left' | 'right', x: number, y: number, a: number, b: number, z: number, s: string, plate = '#fbfaf6', size = 8) {
    const h = size + 5
    if (face === 'left') this.onLeft(x, y, a, b, z, z + h, plate, 1)
    else this.onRight(x, y, b, a, z, z + h, plate, 1)
    const len = Math.abs(b - a)
    const tx = face === 'left' ? x + a + len * 0.08 : x
    const ty = face === 'left' ? y : y + Math.max(a, b) - len * 0.08
    return this.text(face, tx, ty, z + 3.5, s, size)
  }
  /** A small rooftop unit (air handler) on top of a box of height h. */
  unit(x: number, y: number, h: number, s = 0.5, c = { top: '#eceae4', left: '#d6d2c8', right: '#c3beb2' }) {
    this.box(x, y, s, s, 6, c, h, 1)
    return this.poly('#9b978d', [[x + s * 0.2, y + s * 0.2, h + 6], [x + s * 0.8, y + s * 0.2, h + 6], [x + s * 0.8, y + s * 0.8, h + 6], [x + s * 0.2, y + s * 0.8, h + 6]], 0.6)
  }
  /** A vertical cylinder at grid (x, y): radius in px, from z0 to z1. */
  cylinder(x: number, y: number, r: number, z0: number, z1: number, fill: string, shade: string, cap = fill) {
    const [cx, cy0] = this.p(x, y, z0), [, cy1] = this.p(x, y, z1)
    const id = `g${this.parts.length}`
    this.add(`<defs><linearGradient id="${id}" x1="0" x2="1"><stop offset="0" stop-color="${fill}"/><stop offset="0.6" stop-color="${fill}"/><stop offset="1" stop-color="${shade}"/></linearGradient></defs>`)
    this.add(`<path d="M${cx - r},${cy1} L${cx - r},${cy0} A${r},${r / 2} 0 0 0 ${cx + r},${cy0} L${cx + r},${cy1}" fill="url(#${id})" stroke="${INK}" stroke-width="${STROKE}"/>`)
    return this.add(`<ellipse cx="${cx}" cy="${cy1}" rx="${r}" ry="${r / 2}" fill="${cap}" stroke="${INK}" stroke-width="${STROKE}"/>`)
  }
  done(): Sprite {
    const width = (this.w + this.d) * (CW / 2) + this.margin * 2
    const height = this.above + (this.w + this.d) * (CH / 2) + this.margin * 2
    const svg = `<svg xmlns="http://www.w3.org/2000/svg" width="${width}" height="${height}" viewBox="0 0 ${width} ${height}">${this.parts.join('')}</svg>`
    return { svg, width, height, ax: this.ox, ay: this.oy }
  }
}

/** A colour's three faces: the top lightest, the right darkest. */
export function faces(top: string, left: string, right: string) {
  return { top, left, right }
}

export function hash(s: string): number {
  let h = 2166136261
  for (let i = 0; i < s.length; i++) h = Math.imul(h ^ s.charCodeAt(i), 16777619)
  return h >>> 0
}
