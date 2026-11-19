import type { Path, Place } from './places'
import { HUES, shade } from './sprites'

/**
 * Who looks like what (the Owner, 2026-11-19): the UI is laid out by domain — a town holds its
 * districts, a district its buildings, a building its rooms, each self-contained. So a thing's look
 * is its own and passes down: a district has a theme (a hue and a terrain); its buildings take
 * shades of its hue; a building's rooms take shades of the building's. The ways between them
 * (highway, street, corridor) are a standard and take no theme.
 */

export type Terrain = 'meadow' | 'shore' | 'upland' | 'forest'
const TERRAINS: readonly Terrain[] = ['meadow', 'shore', 'upland', 'forest']

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

/** A place's terrain: its district's (the town's own ground is a meadow). */
export function terrainOf(tree: Place, path: Path): Terrain {
  if (path.length === 0) return 'meadow'
  const i = Math.max(0, tree.children.findIndex(c => c.id === path[0]))
  return TERRAINS[i % TERRAINS.length]
}
