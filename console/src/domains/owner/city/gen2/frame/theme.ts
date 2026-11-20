import type { Path, Place } from './places'
import { HUES, shade } from './sprites'

/**
 * Who looks like what (the Owner, 2026-11-19): the UI is laid out by domain — a town holds its
 * districts, a district its buildings, a building its rooms, each self-contained. So a thing's look
 * is its own and passes down: a district has a theme (a hue); its buildings take
 * shades of its hue; a building's rooms take shades of the building's. The ways between them
 * (highway, street, corridor) are a standard and take no theme.
 */


/** How far a child's hue moves from its parent's, by its place in the parent's list. */
const VARY = [0, 0.3, -0.2, 0.45, -0.32, 0.15, -0.1, 0.36] as const

/** A place's hue: a district's from the palette by its place in the town; below that, a shade of its parent's. */
export function hueOf(tree: Place, path: Path): string {
  let hue: string = HUES[0]
  let node = tree
  path.forEach((id, depth) => {
    const i = Math.max(0, node.children.findIndex(c => c.id === id))
    hue = depth === 0 ? HUES[i % HUES.length] : shade(hue, VARY[i % VARY.length] * (depth === 1 ? 1 : 0.6))
    const next = node.children[i]
    if (next) node = next
  })
  return hue
}


/**
 * A district's ground tint, the first generation's (the Owner, 2026-11-20: its districts' colours were
 * beautiful): a pale plate under its plots, one per district by its place in the town. By day as it was;
 * at dusk half-way to dusk's own lot; at night the terrain's lot alone.
 */
export const TINTS = [0xe3e8ee, 0xe7e1f1, 0xe0edf0, 0xf1e8d6, 0xe9e6da, 0xe2e0e8, 0xe2eedf, 0xf3e2e2] as const

const mix = (a: number, b: number, t: number) => {
  const ch = (s: number) => Math.round(((a >> s) & 255) * (1 - t) + ((b >> s) & 255) * t) << s
  return ch(16) | ch(8) | ch(0)
}

export function tintOf(tree: Place, path: Path, light: 'day' | 'dusk' | 'night', lot: number): number {
  if (path.length === 0 || light === 'night') return lot
  const i = Math.max(0, tree.children.findIndex(c => c.id === path[0]))
  const t = TINTS[i % TINTS.length]
  return light === 'day' ? t : mix(t, lot, 0.5)
}
