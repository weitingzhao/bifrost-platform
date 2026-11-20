import { GRID, type Plot } from './grid'

/**
 * Where things stand in the picture (the Owner, 2026-11-19): a thing keeps its true size — a 4×4 is
 * four cells across, a 1×1 one. Outside every thing, between plots, run the same bands on every level:
 * a buffer on each side (a green belt outdoors, a threshold indoors) and the way between them (a
 * highway on the town, a street in a district, a corridor in a building), one width everywhere.
 *
 * So the picture is the 16 × 16 cells with a gutter inserted at every cut, the ring included: a cut
 * (a plot's edge) maps to the middle of its gutter. A plot that spans a cut made elsewhere (a cut
 * runs the whole ground) is wider by that gutter; its thing stands in its middle at its true size.
 */
export const WAY = 1
export const BUFFER = 0.3
export const GUTTER = WAY + 2 * BUFFER

export type Rect = { x: number; y: number; w: number; h: number }

export type Geo = {
  /** A grid position to the picture; a cut maps to the middle of its gutter. */
  x: (v: number) => number
  y: (v: number) => number
  /** The whole picture: cells, gutters and the ring. */
  w: number
  h: number
  /** A plot's own ground in the picture, inside the gutters round it. */
  plot: (p: Rect) => Rect
  /** A thing w × h standing in a plot's ground: its true size, in the middle. */
  thing: (ground: Rect, w: number, h: number) => Rect
  /** The plot whose ground, with half of each gutter round it, holds a point of the picture. */
  plotAt: <P extends Plot>(plots: readonly P[], px: number, py: number) => P | undefined
}

export function geoOf(plots: readonly Plot[]): Geo {
  // The ground the plots tile (groundOf in grid.ts): at most 16 × 16.
  const GW = plots.length ? Math.max(...plots.map(p => p.x + p.w)) : GRID, GH = plots.length ? Math.max(...plots.map(p => p.y + p.h)) : GRID
  const cuts = (edges: (p: Plot) => number[], end: number) => [...new Set([0, end, ...plots.flatMap(edges)])].sort((a, b) => a - b)
  const xs = cuts(p => [p.x, p.x + p.w], GW), ys = cuts(p => [p.y, p.y + p.h], GH)
  const map = (cs: number[]) => (v: number) => {
    let before = 0
    for (const c of cs) if (c < v) before++
    return v + GUTTER * before + (cs.includes(v) ? GUTTER / 2 : 0)
  }
  const x = map(xs), y = map(ys)
  const plot = (p: Rect): Rect => ({ x: x(p.x) + GUTTER / 2, y: y(p.y) + GUTTER / 2, w: x(p.x + p.w) - x(p.x) - GUTTER, h: y(p.y + p.h) - y(p.y) - GUTTER })
  const thing = (r: Rect, w: number, h: number): Rect => ({ x: r.x + (r.w - w) / 2, y: r.y + (r.h - h) / 2, w, h })
  const plotAt = <P extends Plot>(ps: readonly P[], px: number, py: number) => ps.find(p => {
    const r = plot(p), g = GUTTER / 2
    return px >= r.x - g && px < r.x + r.w + g && py >= r.y - g && py < r.y + r.h + g
  })
  return { x, y, w: GW + GUTTER * xs.length, h: GH + GUTTER * ys.length, plot, thing, plotAt }
}
