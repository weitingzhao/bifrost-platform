import { describe, expect, it } from 'vitest'
import { assign, decodePlan, defaultPlan, encodePlan, fitsBudget, GRID, mergePlot, moveTo, planFor, plotAt, plotsOf, splitPlot, streetsOf, turnPlot, joinPlots, type Item, type Plan, type Shape } from '../grid'

// 2026-11-19, the Owner: the ground is divided into plots he can cut and join (1×1, 2×2, 4×4, 4×8, 8×8…),
// the streets follow the plots, and a thing stands in a plot of its own, not every plot a 4×4.
const items = (...shapes: (Shape | 1 | 2 | 4 | 8)[]): Item[] => shapes.map((s, i) => ({ id: `i${i}`, shape: typeof s === 'number' ? `${s}x${s}` as Shape : s }))
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
  // 2026-11-20, the Owner: the unit is 4×4, sixteen to a sheet; no plot is cut smaller.
  it('cuts down to 4×4 and no further', () => {
    expect(splitPlot(defaultPlan(), plotsOf(defaultPlan())[0].id)).toBeNull()
    expect(splitPlot(defaultPlan(), 'nowhere')).toBeNull()
  })
  it('joins a plot with its other half, but not across a half cut further', () => {
    const coarse = defaultPlan(8)
    const plan = splitPlot(coarse, 'aa')!
    expect(mergePlot(plan, 'aaa')).toEqual(coarse)
    expect(mergePlot(plan, 'ab')).toBeNull()
    expect(mergePlot(coarse, '')).toBeNull()
  })
  it('round-trips through its string; a damaged string reads as nothing', () => {
    const plan = splitPlot(defaultPlan(8), 'ab')!
    expect(decodePlan(encodePlan(plan))).toEqual(plan)
    expect(decodePlan('SSL')).toBeNull()
    expect(decodePlan('X')).toBeNull()
  })
})

describe('streets follow the plots', () => {
  // 2026-11-19, the Owner: corridor, street and highway all one width, so the picture stays regular.
  it('the first cut is the avenue; a ring runs round; every street is one width', () => {
    const s = streetsOf(defaultPlan())
    expect(s.filter(x => x.kind === 'avenue')).toHaveLength(1)
    expect(s.filter(x => x.kind === 'ring')).toHaveLength(4)
    expect(new Set(s.map(x => x.half)).size).toBe(1)
  })
  it('a plot never has a street through it', () => {
    const plan = splitPlot(defaultPlan(8), 'ab')!
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
    expect(placed[0]).toMatchObject({ x: 2, y: 6, w: 4, h: 4 })
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

// 2026-11-20, the Owner: three sizes, small 4×4, medium 4×8, large 8×8.
describe('oblong things', () => {
  it('a 4×8 lies along its plot’s long side, and does not fit a 4×4', () => {
    const plan = splitPlot(defaultPlan(8), 'aa')!
    const ps = plotsOf(plan)
    const long = ps.find(p => p.w * p.h === 32)!
    expect(assign(items('4x8'), [long]).placed[0]).toMatchObject({ x: long.x, y: long.y, w: long.w, h: long.h })
    const square = plotsOf(defaultPlan())[0]
    expect(assign(items('4x8'), [square]).homeless).toHaveLength(1)
  })
  it('the default plan is cut to fit what the place holds: every shape housed, each in a plot its own shape', () => {
    const its = items('8x8', '4x8', '4x4', '4x4')
    const plan = planFor(its)
    const { placed, homeless } = assign(its, plotsOf(plan))
    expect(homeless).toEqual([])
    const byId = new Map(plotsOf(plan).map(p => [p.id, p]))
    for (const p of placed) expect(byId.get(p.plot)!.w * byId.get(p.plot)!.h).toBe(p.w * p.h)
  })
  it('the ground left over is cut to 4×4, so the streets read the same on every page', () => {
    expect(planFor([])).toEqual(defaultPlan())
  })
})

// 2026-11-20, the Owner: a 4×8 has a direction, lengthwise or across; and the land comes first: a building goes
// only onto a plot that fits it (the auto-join on drop tried the same day was taken out at his word).
describe('a long plot lies either way', () => {
  // The Owner, later the same day: join any two plots side by side, left, right, back or front, from the land itself.
  it('joins two plots side by side either way, the others keeping their places on the ground', () => {
    const plan = defaultPlan(4), plots = plotsOf(plan)
    const p = plots.find(q => q.x === 0 && q.y === 0)!
    const right = plots.find(q => q.x === 4 && q.y === 0)!, below = plots.find(q => q.x === 0 && q.y === 4)!
    for (const other of [right, below]) {
      const j = joinPlots(plan, p.id, other.id)!
      const merged = plotsOf(j.plan).find(q => q.id === j.moved[p.id])!
      expect(merged.w * merged.h).toBe(32)
      expect(j.moved[other.id]).toBe(j.moved[p.id])
      for (const q of plots.filter(q => q.id !== p.id && q.id !== other.id)) {
        const now = plotsOf(j.plan).find(r => r.id === j.moved[q.id])
        if (now) expect([now.x, now.y, now.w, now.h]).toEqual([q.x, q.y, q.w, q.h])
      }
    }
    const far = plots.find(q => q.x === 8 && q.y === 8)!
    expect(joinPlots(plan, p.id, far.id)).toBeNull()
  })
  it('refuses a building on a plot too small for it: the land is made first, the map never reshapes it', () => {
    const plan = defaultPlan(4)
    const plots = plotsOf(plan)
    const placed = assign([{ id: 'big', shape: '4x8' }], plotsOf(splitPlot(defaultPlan(8), plotsOf(defaultPlan(8))[0].id)!)).placed
    expect(moveTo(placed, plots, 'big', plots[0].id)).toBeNull()
  })
  it('turns a 4×8 plot across, its block cut the other way, and keeps the turn through the saved string', () => {
    const plan = defaultPlan(8)
    const p8 = plotsOf(plan).find(p => p.w === 8 && p.h === 8)!
    const cut = splitPlot(plan, p8.id)!
    const long = plotsOf(cut).find(p => p.id === p8.id + 'a')!
    expect([long.w, long.h]).toEqual([4, 8])
    const turnedPlan = turnPlot(cut, long.id)!
    const across = plotsOf(turnedPlan).find(p => p.id === long.id)!
    expect([across.w, across.h]).toEqual([8, 4])
    expect(decodePlan(encodePlan(turnedPlan))).toEqual(turnedPlan)
    expect(turnPlot(turnedPlan, long.id)).toEqual(cut)
  })
})
