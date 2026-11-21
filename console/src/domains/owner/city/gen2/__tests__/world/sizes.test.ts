import { describe, expect, it } from 'vitest'
import { DISTRICT } from '@bifrost/ui/city/grid'
import { portsFor } from '@bifrost/ui/city/links'

// Origin: the Owner, 2026-11-20 ("三个十六相乘太复杂了，简化"): a district or a building is small, medium or
// large (4×4, 4×8, 8×8: one, two or four units), its ports 3, 4 or 5 a side.
describe('three sizes', () => {
  // 2026-11-20, the Owner: Utilities took four units for its count and the roads no longer met; a district is one unit.
  it('a district is one unit on the town, whatever it holds', () => {
    expect(DISTRICT).toBe('4x4')
  })
})

describe('ports a side by size class', () => {
  it('small 3, medium 4, large 5', () => {
    expect(['4x4', '4x8', '8x8'].map(portsFor)).toEqual([3, 4, 5])
  })
})
