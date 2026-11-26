import { describe, expect, it } from 'vitest'
import { geoOf, GUTTER } from '@bifrost/ui/city/geo'
import { defaultPlan, GRID, plotsOf } from '@bifrost/ui/city/grid'

// 2026-11-19 (ISSUE-9): a thing keeps its true size; outside it, a buffer then the way, the same
// on every level — not a road dug into the plot that shrinks the thing (a 1×1 had no room left).
describe('the picture: true sizes, gutters between plots', () => {
  const plots = plotsOf(defaultPlan())
  const geo = geoOf(plots)
  it('a 4×4 plot keeps its four cells; a gutter at every cut, the ring included', () => {
    for (const p of plots) { expect(geo.plot(p).w).toBeCloseTo(4); expect(geo.plot(p).h).toBeCloseTo(4) }
    expect(geo.w).toBeCloseTo(GRID + 5 * GUTTER)
  })
  it('neighbours are one gutter apart, and the ring leaves one gutter at the edge', () => {
    const a = geo.plot(plots.find(p => p.x === 0 && p.y === 0)!), b = geo.plot(plots.find(p => p.x === 4 && p.y === 0)!)
    expect(b.x - (a.x + a.w)).toBeCloseTo(GUTTER)
    expect(a.x).toBeCloseTo(GUTTER)
  })
  it('finds the plot under a point, gutters split between the two sides', () => {
    const a = plots.find(p => p.x === 0 && p.y === 0)!
    const r = geo.plot(a)
    expect(geo.plotAt(plots, r.x + 1, r.y + 1)).toBe(a)
    expect(geo.plotAt(plots, r.x + r.w + GUTTER * 0.4, r.y + 1)).toBe(a)
    expect(geo.plotAt(plots, r.x + r.w + GUTTER * 0.6, r.y + 1)?.x).toBe(4)
  })
})
