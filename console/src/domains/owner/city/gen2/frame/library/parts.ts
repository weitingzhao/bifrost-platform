import { Draw, faces, INK } from '../kit'
import type { Light } from '../light'
import type { State } from '../places'
import { shade } from '../sprites'
import { osMark, type MachineOs } from './landmarks'
import { encode, metaphor } from './dictionary'

/**
 * The part library (the Owner, 2026-11-19: one way per kind of thing, everywhere in the city; build
 * facilities from parts, so what is done well once serves everywhere, and nothing is copied). Every
 * facility and every room is put together from these; each part that shows a level takes it from a
 * reading, and one with nothing read stays empty.
 *
 *   tone        a facility's colours from its district's hue, the light and its state
 *   tank        a vertical tank filled to its level (memory: the cooling water; disk: the fuel)
 *   smoke       puffs from a stack, by how busy it is; none when it is not running
 *   beacon      the red light on whatever is down
 *   windowStrip a row of windows, as many lit as it is busy (CPU)
 *   hall        a hall: walls, ribbed siding, the band in the hue, a gable roof with skylights
 *   doorRow     roller doors along a face, one per unit, each with its lamp in the unit's state,
 *               crates stacked behind to how full it is
 *   dock        a loading dock along a face, its edge striped
 *   gatehouse   a small office, its windows lit by CPU
 *   signBand    a plate across a face with a name on it
 *   grille      a radiator grille low on a face
 *   plaque      a system's mark on a face: Linux, Mac or Windows (the first generation's marks)
 *   roofNumber  a number painted large on a roof, read from far off (who it is)
 *   tallyBoard  a board on a face counting what it holds now (a reading), dark when nothing is read
 */

export { shade }

export const gauge = (f: number) => f < 0.7 ? '#5cc06c' : f < 0.9 ? '#f2b84b' : '#e5534b'
export const EMPTY = '#d9d5cc'
export const lit = (f: number | null, n: number) => f == null ? 0 : Math.max(f > 0.005 ? 1 : 0, Math.round(f * n))

export type Tone = { wall: ReturnType<typeof faces>; roof: string; glass: string; glassLit: string; metal: ReturnType<typeof faces> }
export function toneOf(hue: string, light: Light, state: State): Tone {
  const night = light === 'night', grey = state === 'unread'
  const base = grey ? '#b9b6ae' : hue
  const wall = night ? faces(shade(base, -0.45), shade(base, -0.55), shade(base, -0.65)) : faces(shade(base, 0.7), shade(base, 0.45), shade(base, 0.25))
  return {
    wall,
    roof: night ? '#4a4f6a' : '#efece5',
    glass: night ? '#20223a' : '#c9d6e3',
    glassLit: state === 'online' ? '#ffd76a' : night ? '#20223a' : '#c9d6e3',
    metal: night ? faces('#5a5f7a', '#4a4f6a', '#3c4058') : faces('#e4e2dc', '#c9c6be', '#b3afa6'),
  }
}

/** A tank (a vertical cylinder) filled from the bottom to its level; nothing read, it stays empty. */
export function tank(g: Draw, x: number, y: number, r: number, h: number, f: number | null, liquid: string) {
  g.cylinder(x, y, r, 0, h, EMPTY, '#bdb8ad', '#ece9e2')
  if (f != null && f > 0.005) g.cylinder(x, y, r - 0.5, 0, Math.max(2, h * Math.min(1, f)), liquid, shade(liquid, -0.25), shade(liquid, 0.2))
}

/** Smoke from a stack: one puff for a quiet machine, up to three for a busy one; none when it is not running. */
export function smoke(g: Draw, x: number, y: number, z: number, f: number | null, on: boolean) {
  if (!on || f == null) return
  const n = Math.max(1, encode('cpu.smoke', f) ?? 1)
  for (let k = 0; k < n; k++) {
    const [cx, cy] = g.p(x + k * 0.12, y - k * 0.08, z + 8 + k * 9)
    g.add(`<circle cx="${cx.toFixed(1)}" cy="${cy.toFixed(1)}" r="${(5 + k * 1.6).toFixed(1)}" fill="#ffffff" opacity="${(0.85 - k * 0.2).toFixed(2)}" stroke="#b8b8c0" stroke-width="0.8"/>`)
  }
}

/** A red beacon on top of whatever is down. */
export const beacon = (g: Draw, x: number, y: number, z: number, state: State) => {
  metaphor('state')
  if (state !== 'down') return
  const [cx, cy] = g.p(x, y, z + 4)
  g.add(`<circle cx="${cx.toFixed(1)}" cy="${cy.toFixed(1)}" r="5" fill="#ff4d4d" stroke="${INK}" stroke-width="1"/><circle cx="${cx.toFixed(1)}" cy="${cy.toFixed(1)}" r="10" fill="#ff4d4d" opacity="0.25"/>`)
}

/** A row of windows along a face: of n, how many are lit tells how busy it is (CPU). */
export function windowStrip(g: Draw, face: 'left' | 'right', x: number, y: number, span: number, z0: number, z1: number, n: number, f: number | null, T: Tone) {
  metaphor('cpu.busy')
  const on = lit(f, n)
  g.windows(face, x, y, span, z0, z1, 1, n, { fill: T.glass, lit: (_r, c) => c < on, litFill: '#ffd76a', tall: 0.7 })
}


export const UNIT: Record<State, string> = { online: '#5ce07a', standby: '#f2b84b', down: '#ff4d4d', unread: '#a4a8b2' }


/** A hall along y, from y to y + len: walls, ribbed siding on the faces we see, the hue band, a gable roof with skylights. */
export function hall(g: Draw, x0: number, y: number, bw: number, len: number, H: number, T: Tone, hue: string, light: Light, o: { skylights?: boolean } = {}) {
  const night = light === 'night', fx = x0 + bw
  g.box(x0, y, bw, len, H, faces(T.wall.top, T.wall.left, T.wall.right), 0, 1.4)
  for (let q = 0.18; q < len - 0.05; q += 0.18) g.onRight(fx, y, q, q + 0.02, 2, H - 8, shade(T.wall.right, -0.08), 0.2)
  for (let q = 0.18; q < bw - 0.05; q += 0.18) g.onLeft(x0, y + len, q, q + 0.02, 2, H - 8, shade(T.wall.left, -0.08), 0.2)
  g.onRight(fx, y, 0, len, H - 7, H - 2, night ? shade(hue, -0.35) : hue, 0.8)
  g.onLeft(x0, y + len, 0, bw, H - 7, H - 2, night ? shade(hue, -0.35) : hue, 0.8)
  const mid = x0 + bw / 2, ridge = H + 9
  g.poly(shade(T.roof, -0.04), [[x0, y, H], [mid, y, ridge], [mid, y + len, ridge], [x0, y + len, H]], 1.1)
  g.poly(shade(T.roof, -0.14), [[mid, y, ridge], [fx, y, H], [fx, y + len, H], [mid, y + len, ridge]], 1.1)
  g.poly(T.wall.left, [[x0, y + len, H], [mid, y + len, ridge], [fx, y + len, H]], 1.1)
  if (o.skylights !== false) for (let q = 0.25; q < len - 0.35; q += 0.55) g.poly(night ? '#3a4a6a' : '#bfe0f5', [[mid + 0.15, y + q, ridge - 1.5], [fx - 0.2, y + q, H + 1.5], [fx - 0.2, y + q + 0.3, H + 1.5], [mid + 0.15, y + q + 0.3, ridge - 1.5]], 0.6)
  return { mid, ridge }
}

/** Roller doors on the +x face of a hall, one per unit: its lamp in the unit's state, crates stacked behind to the level f. */
export function doorRow(g: Draw, fx: number, y: number, len: number, H: number, units: State[], f: number | null, run: boolean, T: Tone) {
  metaphor('volume.disks'); metaphor('volume.used')
  const n = Math.max(1, units.length), span = len / n, dh = Math.min(30, H - 14)
  for (let u = 0; u < n; u++) {
    const a = u * span + span * 0.16, b = (u + 1) * span - span * 0.16
    g.onRight(fx, y, a - 0.05, b + 0.05, 5, dh + 3, shade(T.wall.right, -0.25), 0.8)
    g.onRight(fx, y, a, b, 5, dh, '#2c2e38', 0.8)
    if (run && f != null && f > 0.002) {
      const rows = Math.max(1, Math.round(Math.min(1, f) * 6))
      for (let r = 0; r < rows; r++) {
        const z = 6 + r * ((dh - 6) / 6)
        g.onRight(fx, y, a + 0.05, (a + b) / 2 - 0.02, z, z + (dh - 6) / 6 - 0.6, '#e0a24e', 0.9)
        g.onRight(fx, y, (a + b) / 2 + 0.02, b - 0.05, z, z + (dh - 6) / 6 - 0.6, '#d08f3c', 0.9)
      }
    }
    // The unit's lamp over its door: big enough to read from across the district.
    g.onRight(fx, y, (a + b) / 2 - 0.16, (a + b) / 2 + 0.16, dh + 5, dh + 12, UNIT[units[u] ?? 'unread'], 1.2)
  }
}

/** A loading dock along a +x face: a concrete apron, its edge striped. */
export function dock(g: Draw, x: number, y: number, w: number, d: number, light: Light) {
  const night = light === 'night'
  g.box(x, y, w, d, 5, faces(night ? '#5a5d6a' : '#e4e1da', night ? '#4a4d58' : '#cfcbc2', night ? '#3e414c' : '#bdb9b0'), 0, 1.1)
  for (let k = 0; k < d / 0.5; k++) g.onRight(x + w, y, k * 0.5, k * 0.5 + 0.25, 1, 4, '#f2c94c', 0.3)
}

/** A small office with a flat roof, its windows lit as busy as it is (CPU). */
export function gatehouse(g: Draw, x: number, y: number, w: number, d: number, T: Tone, cpu: number | null) {
  g.box(x, y, w, d, 20, faces(T.wall.top, T.wall.left, T.wall.right), 0, 1.2)
  g.poly(T.roof, [[x, y, 20], [x + w, y, 20], [x + w, y + d, 20], [x, y + d, 20]], 1)
  metaphor('cpu.busy')
  const on = lit(cpu, 4)
  g.windows('left', x, y + d, w, 6, 16, 1, 4, { fill: T.glass, lit: (_r, c) => c < on, litFill: '#ffd76a' })
  g.windows('right', x + w, y, d, 6, 16, 1, 2, { fill: T.glass, lit: (_r, c) => c < on, litFill: '#ffd76a' })
}

/** Face units to a cell (kit.ts, Draw.face). */
const U = 35.8
const esc = (t: string) => t.replace(/[&<>"]/g, c => ({ '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;' })[c]!)

/** A plate across a face, from its corner (x, y) for `span` cells, at height z, with a name on it. */
export function signBand(g: Draw, face: 'left' | 'right', x: number, y: number, span: number, z: number, text: string, h = 11) {
  const W = span * U
  const size = Math.min(8, (W - 10) / Math.max(4, text.length) * 1.6)
  g.face(face, x, y, z, `<rect x="3" y="${-h}" width="${(W - 6).toFixed(1)}" height="${h}" rx="1.5" fill="#fbfaf6" stroke="${INK}" stroke-width="0.8"/>` +
    `<text x="${(W / 2).toFixed(1)}" y="${(-h / 2 + size * 0.36).toFixed(1)}" text-anchor="middle" font-family="'JetBrains Mono',ui-monospace,Menlo,monospace" font-size="${size.toFixed(1)}" font-weight="700" fill="${INK}">${esc(text)}</text>`)
}

/** A radiator grille low on a face, from its corner for `span` cells, between heights z0 and z1. */
export function grille(g: Draw, face: 'left' | 'right', x: number, y: number, span: number, z0: number, z1: number) {
  const W = span * U, h = z1 - z0
  let s = `<rect x="4" y="${-h}" width="${(W - 8).toFixed(1)}" height="${h}" fill="#5b6070" stroke="${INK}" stroke-width="1"/>`
  for (let u = 7; u < W - 6; u += 3.2) s += `<line x1="${u.toFixed(1)}" y1="${(-h + 2).toFixed(1)}" x2="${u.toFixed(1)}" y2="-2" stroke="#8d93a1" stroke-width="0.9"/>`
  g.face(face, x, y, z0, s)
}

/** A system's mark on a plaque, `at` cells along the face from its corner, centred at height z. */
export function plaque(g: Draw, face: 'left' | 'right', x: number, y: number, at: number, z: number, os: MachineOs, r = 8) {
  g.face(face, x, y, z, osMark(os, at * U, 0, r))
}

/** A number painted large on a roof (x0..x1, y0..y1 at height z), read from far off. */
export function roofNumber(g: Draw, x0: number, y0: number, x1: number, y1: number, z: number, text: string, ink = INK) {
  const w = (x1 - x0) * U, d = (y1 - y0) * U
  const size = Math.min(d * 0.7, (w * 0.9) / Math.max(1, text.length) * 1.6)
  g.floor(x0, y0, z, `<text x="${(w / 2).toFixed(1)}" y="${(d / 2 + size * 0.36).toFixed(1)}" text-anchor="middle" font-family="'JetBrains Mono',ui-monospace,Menlo,monospace" font-size="${size.toFixed(1)}" font-weight="800" fill="${ink}" opacity="0.82" letter-spacing="1">${esc(text)}</text>`)
}

/** A board on a face, `at` cells along it, centred at height z: how many of something it holds now; dark and blank when nothing is read. */
export function tallyBoard(g: Draw, face: 'left' | 'right', x: number, y: number, at: number, z: number, n: number | null, what: string, w = 1.1) {
  const W = w * U, h = 13
  const text = n == null ? '' : `${Math.round(n)} ${what.toUpperCase()}`
  const size = Math.min(8.5, (W - 6) / Math.max(4, text.length) * 1.7)
  g.face(face, x, y, z, `<rect x="${(at * U - W / 2).toFixed(1)}" y="${(-h / 2).toFixed(1)}" width="${W.toFixed(1)}" height="${h}" rx="1.5" fill="#1d2230" stroke="${INK}" stroke-width="0.9"/>` +
    (text ? `<text x="${(at * U).toFixed(1)}" y="${(size * 0.36).toFixed(1)}" text-anchor="middle" font-family="'JetBrains Mono',ui-monospace,Menlo,monospace" font-size="${size.toFixed(1)}" font-weight="700" fill="#7be08f">${esc(text)}</text>` : ''))
}
