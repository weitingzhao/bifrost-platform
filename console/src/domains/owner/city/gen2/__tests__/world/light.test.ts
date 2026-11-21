import { describe, expect, it } from 'vitest'
import { CORNERS, lightAt, lightOf, NEXT, PALETTE } from '@bifrost/ui/city/light'

// 2026-11-19, the Owner: the town has day and night (dusk too), following the time; he may pin one.
const at = (h: number, m = 0) => new Date(2026, 10, 19, h, m)

describe('light', () => {
  it('follows the clock', () => {
    expect(lightAt(at(12))).toBe('day')
    expect(lightAt(at(7, 30))).toBe('day')
    expect(lightAt(at(18))).toBe('dusk')
    expect(lightAt(at(6, 15))).toBe('dusk')
    expect(lightAt(at(19))).toBe('night')
    expect(lightAt(at(2))).toBe('night')
  })
  it('a pinned light wins over the clock', () => {
    expect(lightOf('night', at(12))).toBe('night')
    expect(lightOf('auto', at(12))).toBe('day')
  })
  it('the switch goes round all four choices', () => {
    const seen = new Set<string>()
    let c: keyof typeof NEXT = 'auto'
    for (let i = 0; i < 4; i++) { seen.add(c); c = NEXT[c] }
    expect(c).toBe('auto')
    expect(seen.size).toBe(4)
  })
  it('every light has a palette for the canvas and the corners', () => {
    for (const l of ['day', 'dusk', 'night'] as const) {
      expect(PALETTE[l]).toBeDefined()
      expect(Object.keys(CORNERS[l])).toEqual(Object.keys(CORNERS.day))
    }
  })
})
