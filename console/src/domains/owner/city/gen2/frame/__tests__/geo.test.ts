import { describe, expect, it } from 'vitest'
import { geoOf, GUTTER } from '../geo'
import { defaultPlan, GRID, plotsOf, splitPlot } from '../grid'

// 2026-11-19, the Owner: a thing keeps its true size; outside it, a buffer then the way, the same
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
  it('a 1×1 is a whole cell', () => {
    const fine = plotsOf(splitPlot(splitPlot(defaultPlan(), 'aaaa')!, 'aaaaa')!)
    const g = geoOf(fine)
    const one = fine.find(p => p.w === 2 && p.h === 2)!
    expect(g.thing(g.plot(one), 1, 1)).toMatchObject({ w: 1, h: 1 })
  })
  it('a plot spanning a cut made elsewhere is wider by that gutter, and its thing stands in its middle', () => {
    const fine = plotsOf(splitPlot(defaultPlan(), 'aaaa')!)
    const g = geoOf(fine)
    const whole = fine.find(p => p.x === 0 && p.y === 4 && p.w === 4)!
    const r = g.plot(whole)
    expect(r.w).toBeCloseTo(4 + GUTTER)
    expect(g.thing(r, 4, 4).x).toBeCloseTo(r.x + GUTTER / 2)
  })
  it('finds the plot under a point, gutters split between the two sides', () => {
    const a = plots.find(p => p.x === 0 && p.y === 0)!
    const r = geo.plot(a)
    expect(geo.plotAt(plots, r.x + 1, r.y + 1)).toBe(a)
    expect(geo.plotAt(plots, r.x + r.w + GUTTER * 0.4, r.y + 1)).toBe(a)
    expect(geo.plotAt(plots, r.x + r.w + GUTTER * 0.6, r.y + 1)?.x).toBe(4)
  })
})
