import { describe, expect, it } from 'vitest'
import { shapeByCount } from '../grid'
import { portsFor } from '../links'

// Origin: the Owner, 2026-11-20 ("三个十六相乘太复杂了，简化"): a district or a building is small, medium or
// large (4×4, 4×8, 8×8: one, two or four units), its ports 3, 4 or 5 a side.
describe('three sizes', () => {
  it('a size comes from a count, in three classes', () => {
    expect([1, 2, 3, 4, 5, 16].map(shapeByCount)).toEqual(['4x4', '4x4', '4x8', '4x8', '8x8', '8x8'])
  })
})

describe('ports a side by size class', () => {
  it('small 3, medium 4, large 5', () => {
    expect(['4x4', '4x8', '8x8'].map(portsFor)).toEqual([3, 4, 5])
  })
})
