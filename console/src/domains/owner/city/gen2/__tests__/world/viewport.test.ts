import { describe, expect, it } from 'vitest'
import { deviceOf, frameOf, inside, overlaps, swapRects, type Frame } from '@bifrost/ui/city/viewport'

// 2026-11-19, the Owner: the frame must feel right on phone, tablet and computer before anything is
// built on it. On the first greybox the summary ran under the minimap at 375 px wide; these hold the
// corners apart at every common size.
const SIZES: [string, number, number][] = [
  ['iPhone SE', 375, 667], ['iPhone 15', 393, 852], ['iPhone 15 landscape', 852, 393], ['small Android landscape', 640, 360],
  ['iPad mini', 744, 1133], ['iPad', 820, 1180], ['iPad landscape', 1180, 820], ['iPad Pro', 1024, 1366],
  ['laptop', 1280, 800], ['desktop', 1440, 900], ['wide', 1920, 1080], ['narrow window', 700, 900],
]

// 2026-11-19, the Owner: the three levels get a control of their own (under the minimap); it must clear every corner too.
const corners = (f: Frame) => ({ summary: f.summary, mini: f.mini, levels: f.levels, alerts: f.alerts, menu: f.menu, search: f.search,
  ...(f.docks ? { dockLeft: f.docks.left, dockRight: f.docks.right } : {}), ...(f.peek ? { peek: f.peek } : {}) })

describe('frameOf', () => {
  for (const [name, w, h] of SIZES) {
    it(`${name} (${w}×${h}): corners stay on screen and apart, the map keeps room`, () => {
      const f = frameOf(w, h)
      const all = Object.entries(corners(f))
      for (const [k, r] of all) expect(inside(r, w, h), `${k} off screen`).toBe(true)
      for (let i = 0; i < all.length; i++)
        for (let j = i + 1; j < all.length; j++)
          expect(overlaps(all[i][1], all[j][1]), `${all[i][0]} overlaps ${all[j][0]}`).toBe(false)
      expect(inside(f.rail, w, h)).toBe(true)
      expect(overlaps(f.rail, f.menu)).toBe(false)
      // The map never sits under the side rail or the drawer's handle.
      if (f.peek) expect(f.world.y + f.world.h).toBeLessThanOrEqual(f.peek.y)
      expect(inside(f.focus, w, h)).toBe(true)
      expect(f.world.h).toBeGreaterThanOrEqual(Math.min(200, h * 0.35))
      expect(f.menu.h).toBeGreaterThanOrEqual(f.device === 'desktop' ? 28 : 40)
    })
  }

  it('tells the devices apart by size', () => {
    expect(deviceOf(375, 667)).toBe('phone')
    expect(deviceOf(852, 393)).toBe('phone')
    expect(deviceOf(820, 1180)).toBe('tablet')
    expect(deviceOf(1440, 900)).toBe('desktop')
  })

  // 2026-11-19, the Owner: tables and charts beside the 2.5D middle; a rail where the screen is wide, a drawer elsewhere.
  it('docks the panels in the bottom corners on wide screens and a drawer on upright ones', () => {
    expect(frameOf(1440, 900).docks).not.toBeNull()
    expect(frameOf(1180, 820).docks).not.toBeNull()
    expect(frameOf(1440, 900).search.mode).toBe('bar')
    expect(frameOf(393, 852).search.mode).toBe('icon')
    expect(frameOf(820, 1180).rail.mode).toBe('drawer')
    expect(frameOf(393, 852).rail.mode).toBe('drawer')
    expect(frameOf(852, 393).rail.mode).toBe('drawer')
  })

  it('on a phone: compact summary, panels in a drawer, 44 px targets', () => {
    const f = frameOf(375, 667)
    expect(f.summary.compact).toBe(true)
    expect(f.rail.mode).toBe('drawer')
    expect(f.tap).toBe(44)
    expect(f.world.y).toBeGreaterThanOrEqual(f.summary.y + f.summary.h)
  })

  it('the overlap check catches an overlap (positive control for the size sweep above)', () => {
    expect(overlaps({ x: 0, y: 0, w: 10, h: 10 }, { x: 9, y: 9, w: 10, h: 10 })).toBe(true)
    expect(overlaps({ x: 0, y: 0, w: 10, h: 10 }, { x: 10, y: 0, w: 10, h: 10 })).toBe(false)
  })

  // 2026-11-19, the Owner: the trail repeated the levels and the minimap; notices go up by the minimap, as in a game.
  it('keeps the right column in order: minimap, levels, alerts', () => {
    for (const [, w, h] of SIZES) {
      const f = frameOf(w, h)
      expect(f.levels.y).toBeGreaterThan(f.mini.y + f.mini.h)
      expect(f.alerts.y).toBeGreaterThan(f.levels.y + f.levels.h)
      expect(f.alerts.x).toBe(f.levels.x)
    }
  })

  it('keeps clear of the notch and the home bar', () => {
    const f = frameOf(393, 852, { top: 47, right: 0, bottom: 34, left: 0 })
    expect(f.summary.y).toBeGreaterThanOrEqual(47)
    expect(f.mini.y).toBeGreaterThanOrEqual(47)
    expect(f.menu.y + f.menu.h).toBeLessThanOrEqual(852 - 34)
  })
})

// 2026-11-19, the Owner: a panel may take the middle; the map then sits where the panel was.
describe('swapRects', () => {
  for (const [name, w, h] of SIZES) {
    it(`${name}: the map and the panel stay on screen, apart, clear of the corners`, () => {
      const f = frameOf(w, h)
      for (const side of ['left', 'right'] as const) {
        const { map, panel } = swapRects(f, side)
        expect(inside(map, w, h) && inside(panel, w, h)).toBe(true)
        expect(overlaps(map, panel)).toBe(false)
        for (const r of [f.summary, f.menu, f.levels, f.alerts]) expect(overlaps(panel, r)).toBe(false)
        expect(panel.w).toBeGreaterThan(Math.min(260, w * 0.5))
      }
    })
  }
})
