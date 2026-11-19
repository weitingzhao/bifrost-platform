import { describe, expect, it } from 'vitest'
import { assign, decodePlan, defaultPlan, encodePlan, fitsBudget, GRID, mergePlot, moveTo, plotAt, plotsOf, splitPlot, streetsOf, type Item, type Plan } from '../grid'

// 2026-11-19, the Owner: the ground is divided into plots he can cut and join (1×1, 2×2, 4×4, 4×8, 8×8…),
// the streets follow the plots, and a thing stands in a plot of its own, not every plot a 4×4.
const items = (...sizes: number[]): Item[] => sizes.map((s, i) => ({ id: `i${i}`, size: s as Item['size'] }))
const tiles = (plan: Plan) => plotsOf(plan).reduce((a, p) => a + p.w * p.h, 0)

describe('plans', () => {
  it('the default cuts the ground into sixteen 4×4 plots', () => {
    const ps = plotsOf(defaultPlan())
    expect(ps).toHaveLength(16)
    expect(ps.every(p => p.w === 4 && p.h === 4)).toBe(true)
    expect(tiles(defaultPlan())).toBe(GRID * GRID)
  })
  it('a square is cut across x, an oblong across its long side: plots stay squares or 2:1', () => {
    let plan: Plan = null
    plan = splitPlot(plan, '')!
    expect(plotsOf(plan).map(p => [p.w, p.h])).toEqual([[8, 16], [8, 16]])
    plan = splitPlot(plan, 'a')!
    plan = splitPlot(plan, 'aa')!
    expect(plotsOf(plan).find(p => p.id === 'aaa')).toMatchObject({ w: 4, h: 8 })
    for (const p of plotsOf(plan)) expect([1, 2]).toContain(Math.max(p.w, p.h) / Math.min(p.w, p.h))
    expect(tiles(plan)).toBe(GRID * GRID)
  })
  it('cuts down to 1×1 and no further', () => {
    const fine = defaultPlan(1)
    expect(plotsOf(fine)).toHaveLength(256)
    expect(splitPlot(fine, plotsOf(fine)[0].id)).toBeNull()
    expect(splitPlot(defaultPlan(), 'nowhere')).toBeNull()
  })
  it('joins a plot with its other half, but not across a half cut further', () => {
    const plan = splitPlot(defaultPlan(), 'aaaa')!
    expect(mergePlot(plan, 'aaaaa')).toEqual(defaultPlan())
    expect(mergePlot(plan, 'aaab')).toBeNull()
    expect(mergePlot(defaultPlan(), '')).toBeNull()
  })
  it('round-trips through its string; a damaged string reads as nothing', () => {
    const plan = splitPlot(defaultPlan(), 'abab')!
    expect(decodePlan(encodePlan(plan))).toEqual(plan)
    expect(decodePlan('SSL')).toBeNull()
    expect(decodePlan('X')).toBeNull()
  })
})

describe('streets follow the plots', () => {
  it('the first cut is the avenue; a ring runs round; finer plots get narrower streets', () => {
    const s = streetsOf(splitPlot(splitPlot(defaultPlan(), 'aaaa')!, 'aaaaa')!)
    expect(s.filter(x => x.kind === 'avenue')).toHaveLength(1)
    expect(s.filter(x => x.kind === 'ring')).toHaveLength(4)
    expect(s.some(x => x.kind === 'lane')).toBe(true)
    const half = (k: string) => s.find(x => x.kind === k)!.half
    expect(half('avenue')).toBeGreaterThan(half('street'))
    expect(half('street')).toBeGreaterThan(half('lane'))
  })
  it('a plot never has a street through it', () => {
    const plan = splitPlot(defaultPlan(), 'abab')!
    for (const st of streetsOf(plan)) for (const p of plotsOf(plan)) {
      if (st.x0 === st.x1) expect(p.x < st.x0 && st.x0 < p.x + p.w && Math.max(p.y, st.y0) < Math.min(p.y + p.h, st.y1)).toBe(false)
      else expect(p.y < st.y0 && st.y0 < p.y + p.h && Math.max(p.x, st.x0) < Math.min(p.x + p.w, st.x1)).toBe(false)
    }
  })
})

describe('things in plots', () => {
  const plots = plotsOf(defaultPlan())
  it('one thing a plot, centred, gathered near the middle', () => {
    const { placed, homeless } = assign(items(4, 4, 4, 4), plots)
    expect(homeless).toEqual([])
    expect(new Set(placed.map(p => p.plot)).size).toBe(4)
    expect(placed.map(p => `${p.x},${p.y}`).sort()).toEqual(['4,4', '4,8', '8,4', '8,8'])
  })
  it('a small thing in a big plot stands in its middle', () => {
    const { placed } = assign(items(4), plotsOf(splitPlot(null, '')!))
    expect(placed[0]).toMatchObject({ x: 2, y: 6 })
  })
  it('a thing too big for every free plot is listed, not drawn', () => {
    const { placed, homeless } = assign(items(8), plots)
    expect(placed).toEqual([])
    expect(homeless.map(h => h.id)).toEqual(['i0'])
  })
  it('the Owner’s plot is kept where it fits; moving onto a taken plot swaps the two', () => {
    const { placed } = assign(items(4, 4), plots, { i0: 'aaaa' })
    expect(placed.find(p => p.id === 'i0')!.plot).toBe('aaaa')
    const other = placed.find(p => p.id === 'i1')!.plot
    expect(moveTo(placed, plots, 'i0', other)).toEqual({ i0: other, i1: 'aaaa' })
  })
  it('a move into a plot too small is refused', () => {
    const ps = plotsOf(splitPlot(defaultPlan(), 'aaaa')!)
    const { placed } = assign(items(4), ps)
    expect(moveTo(placed, ps, 'i0', 'aaaaa')).toBeNull()
  })
  it('finds the plot under a cell', () => {
    expect(plotAt(plots, { x: 5.5, y: 0.2 })).toMatchObject({ x: 4, y: 0 })
  })
  it('the budget still caps what one ground holds', () => {
    expect(fitsBudget(items(8, 8, 8, 8))).toBe(true)
    expect(fitsBudget(items(8, 8, 8, 8, 1))).toBe(false)
  })
})
