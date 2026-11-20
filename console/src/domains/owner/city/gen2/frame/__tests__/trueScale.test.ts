import { describe, expect, it } from 'vitest'
import { groundOf } from '../grid'
import { portsFor } from '../links'

// Origin: the Owner, 2026-11-20 ("里面也要等比例缩放；芯片面积越大接口越多，但不是同样的三个"). These pin the
// true zoom (one cell outside is 2 × 2 inside, at most 16 × 16) and the ports a side by area (2 + log4 area).
describe('a place’s ground is its footprint, zoomed', () => {
  it('one cell outside is two by two inside; the town and an 8×8 have the full ground', () => {
    expect(groundOf('district', '2x4', [])).toEqual({ w: 8, h: 4, over: false })
    expect(groundOf('district', '4x4', [])).toEqual({ w: 8, h: 8, over: false })
    expect(groundOf('district', '8x8', [])).toEqual({ w: 16, h: 16, over: false })
    expect(groundOf('town', '8x8', [])).toEqual({ w: 16, h: 16, over: false })
  })
  it('a place holding more than its footprint grows, and says so', () => {
    const g = groundOf('building', '2x2', [{ id: 'a', shape: '4x4' }, { id: 'b', shape: '4x4' }])
    expect(g.over).toBe(true)
    expect(g.w * g.h).toBeGreaterThanOrEqual(32)
  })
})

describe('ports a side grow slower than area', () => {
  it('1×1 and 1×2 have 2, 2×2 and 2×4 3, 4×4 and 4×8 4, 8×8 5', () => {
    expect(['1x1', '1x2', '2x2', '2x4', '4x4', '4x8', '8x8'].map(portsFor)).toEqual([2, 2, 3, 3, 4, 4, 5])
  })
})
