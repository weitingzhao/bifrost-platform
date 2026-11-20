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
 *   smoke       puffs from a stack, by how busy it is; none when it is not running
 *   beacon      the red light on whatever is down
 *   plaque      a system's mark on a face: Linux, Mac or Windows (the first generation's marks)
 */

export { shade }

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



export const UNIT: Record<State, string> = { online: '#5ce07a', standby: '#f2b84b', down: '#ff4d4d', unread: '#a4a8b2' }






/** Face units to a cell (kit.ts, Draw.face). */
const U = 35.8
const esc = (t: string) => t.replace(/[&<>"]/g, c => ({ '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;' })[c]!)



/** A system's mark on a plaque, `at` cells along the face from its corner, centred at height z. */
export function plaque(g: Draw, face: 'left' | 'right', x: number, y: number, at: number, z: number, os: MachineOs, r = 8) {
  g.face(face, x, y, z, osMark(os, at * U, 0, r))
}



/**
 * How a reading is written, everywhere in the city: one format per unit (the Owner, 2026-11-19: what
 * can be shown is shown to one standard). A percent whole; a clock in GHz to a tenth; a temperature
 * whole in °C; bytes in MB, GB or TB (a tenth above a GB); a rate in kB/s or MB/s; nothing read is a dash.
 */
export type ReadingUnit = 'share' | 'Hz' | '°C' | 'bytes' | 'bytes/s' | 'count' | 'seconds' | 'per second'
export function readingText(v: number | null, unit: ReadingUnit): string {
  if (v == null || !Number.isFinite(v)) return '—'
  if (unit === 'share') return `${Math.round(v * 100)}%`
  if (unit === 'Hz') return `${(v / 1e9).toFixed(1)} GHz`
  if (unit === '°C') return `${Math.round(v)}°C`
  if (unit === 'bytes') return v >= 1e12 ? `${(v / 1e12).toFixed(1)} TB` : v >= 1e9 ? `${(v / 1e9).toFixed(1)} GB` : `${Math.round(v / 1e6)} MB`
  if (unit === 'bytes/s') return v >= 1e6 ? `${(v / 1e6).toFixed(1)} MB/s` : `${Math.round(v / 1e3)} kB/s`
  if (unit === 'seconds') return v >= 1 ? `${v.toFixed(1)} s` : v >= 0.01 ? `${Math.round(v * 1e3)} ms` : `${(v * 1e3).toFixed(1)} ms`
  if (unit === 'per second') return `${v >= 10 ? Math.round(v) : v.toFixed(1)}/s`
  return String(Math.round(v))
}

/** A half dial in a face's plane, centred at u, its needle at share f of the sweep (none when not read); green to `redFrom`, red after. */
export function halfDial(r: number, u: number, f: number | null, redFrom: number): string {
  const arc = (a: number, b: number, c: string) => {
    const p = (t: number) => [u + r * Math.cos(Math.PI * (1 - t)), -r * Math.sin(Math.PI * (1 - t))]
    const [x0, y0] = p(a), [x1, y1] = p(b)
    return `<path d="M${x0.toFixed(1)},${y0.toFixed(1)} A${r},${r} 0 0 1 ${x1.toFixed(1)},${y1.toFixed(1)}" fill="none" stroke="${c}" stroke-width="${(r * 0.28).toFixed(1)}"/>`
  }
  let s = `<path d="M${u - r - 1},0 A${r + 1},${r + 1} 0 0 1 ${u + r + 1},0 Z" fill="#fbfaf6" stroke="${INK}" stroke-width="0.8"/>`
  s += arc(0.02, redFrom, '#9fcf9f') + arc(redFrom, 0.98, '#e27d6f')
  if (f != null) {
    const t = Math.max(0, Math.min(1, f)), a = Math.PI * (1 - t)
    s += `<line x1="${u}" y1="0" x2="${(u + r * 0.92 * Math.cos(a)).toFixed(1)}" y2="${(-r * 0.92 * Math.sin(a)).toFixed(1)}" stroke="${INK}" stroke-width="1.2" stroke-linecap="round"/>`
  }
  return s + `<circle cx="${u}" cy="0" r="1.2" fill="${INK}"/>`
}

/**
 * The standard instrument (one kind for every console in the city): a slot INSTRUMENT cells wide on a
 * +y face, from height z up INSTRUMENT_H px. In its bezel, a half dial (needle at its share, red band
 * from `redFrom`), the reading written beneath it to the standard (readingText), and its name plate.
 * A console is as wide as its instruments; nothing on it stands outside its slot.
 */
export const INSTRUMENT = 0.8, INSTRUMENT_H = 32
export function instrument(g: Draw, x: number, y: number, slot: number, z: number, o: { name: string; share: number | null; redFrom: number; reading: string }) {
  const W = INSTRUMENT * U, u = (slot + 0.5) * W, bw = W - 4
  const mono = `font-family="'JetBrains Mono',ui-monospace,Menlo,monospace" text-anchor="middle" font-weight="700"`
  g.face('left', x, y, z, `<rect x="${(u - bw / 2).toFixed(1)}" y="${-INSTRUMENT_H + 1}" width="${bw.toFixed(1)}" height="${INSTRUMENT_H - 9}" rx="2" fill="#1f242e" stroke="#9aa0ab" stroke-width="1.2"/>` +
    `<g transform="translate(0 ${-INSTRUMENT_H + 12.5})">${halfDial(8, u, o.share, o.redFrom)}</g>` +
    `<text x="${u.toFixed(1)}" y="${-INSTRUMENT_H + 18.6}" ${mono} font-size="5" fill="#f2f0e8">${esc(o.reading)}</text>` +
    `<rect x="${(u - bw / 2).toFixed(1)}" y="-7" width="${bw.toFixed(1)}" height="6" rx="1" fill="#e8e2cf" stroke="${INK}" stroke-width="0.5"/>` +
    `<text x="${u.toFixed(1)}" y="-2.4" ${mono} font-size="4.2" fill="${INK}">${esc(o.name)}</text>`)
}

/**
 * The standard split bar (the second kind of standard instrument, 2026-11-19: a whole made of kinds, read
 * at a glance — the Owner's "energy bar" on the console): `slots` instrument slots wide on a +y face, from
 * height z up INSTRUMENT_H px. In its bezel, a bar split left to right in the kinds' shares, each kind's
 * amount written beneath in its colour, and its name plate. The kinds add up to the whole.
 */
export function splitBar(g: Draw, x: number, y: number, slot: number, slots: number, z: number, o: { name: string; parts: { label: string; bytes: number; colour: string }[] }) {
  const W = INSTRUMENT * U, u0 = slot * W + 2, bw = slots * W - 4
  const total = o.parts.reduce((a, p) => a + p.bytes, 0)
  const mono = `font-family="'JetBrains Mono',ui-monospace,Menlo,monospace" font-weight="700"`
  let s = `<rect x="${u0.toFixed(1)}" y="${-INSTRUMENT_H + 1}" width="${bw.toFixed(1)}" height="${INSTRUMENT_H - 9}" rx="2" fill="#1f242e" stroke="#9aa0ab" stroke-width="1.2"/>`
  const bx = u0 + 4, by = -INSTRUMENT_H + 4, blen = bw - 8, bh = 4.5
  s += `<rect x="${bx.toFixed(1)}" y="${by}" width="${blen.toFixed(1)}" height="${bh}" fill="#2b313d" stroke="#5d6573" stroke-width="0.6"/>`
  let at = 0
  o.parts.forEach(p => {
    const w = total > 0 ? p.bytes / total * blen : 0
    if (w > 0.2) s += `<rect x="${(bx + at).toFixed(1)}" y="${by}" width="${w.toFixed(1)}" height="${bh}" fill="${p.colour}"/>`
    at += w
  })
  // Each kind on a line of its own beneath the bar: its colour, its name and amount.
  o.parts.forEach((p, k) => {
    const ty = by + bh + 2 + k * 4.3
    s += `<rect x="${bx.toFixed(1)}" y="${ty.toFixed(1)}" width="3" height="3" fill="${p.colour}"/><text x="${(bx + 4.5).toFixed(1)}" y="${(ty + 2.9).toFixed(1)}" ${mono} font-size="3.6" fill="#f2f0e8">${esc(p.label)}</text>`
  })
  s += `<rect x="${u0.toFixed(1)}" y="-7" width="${bw.toFixed(1)}" height="6" rx="1" fill="#e8e2cf" stroke="${INK}" stroke-width="0.5"/>` +
    `<text x="${(u0 + bw / 2).toFixed(1)}" y="-2.4" text-anchor="middle" ${mono} font-size="4.2" fill="${INK}">${esc(o.name)}</text>`
  g.face('left', x, y, z, s)
}
