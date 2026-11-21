import { describe, expect, it } from 'vitest'
import { cellsOf, townSprite } from '@bifrost/ui/city/library/towns'
import { nuclearCore } from '@bifrost/ui/city/library/landmarks'
import { portsFor } from '@bifrost/ui/city/links'

// 2026-11-20, the Owner: a district on the city may be 4×4, 4×8 or 8×8; its town must grow with it, not
// stretch one picture (Utilities at 4×8 showed its plant in one half and bare ground in the other), and its
// gates must stand where the streets meet it.
describe('a district’s town grows with its size', () => {
  it('has one 4×4 cell per 4×4 of its shape, the landmark’s in the back corner, the others at the far edges', () => {
    const at = (shape: string, w: number, d: number) => cellsOf(shape, w, d).map(c => c.map(v => Math.round(v * 10) / 10))
    expect(at('4x4', 4, 4)).toEqual([[0, 0]])
    expect(at('4x8', 4, 9.2)).toEqual([[0, 0], [0, 5.2]])
    // Lying across (2026-11-20: its second cell was drawn over its first): the cells go along x.
    expect(at('4x8', 9.2, 4)).toEqual([[0, 0], [5.2, 0]])
    expect(at('8x8', 9.2, 9.2)).toEqual([[0, 0], [0, 5.2], [5.2, 0], [5.2, 5.2]])
  })
  it('puts a gate pair of posts at every port its size has on each front side', () => {
    for (const [shape, w, d] of [['4x4', 4, 4], ['4x8', 4, 9.2], ['8x8', 9.2, 9.2]] as const) {
      const svg = townSprite(shape, w, d, nuclearCore, []).svg
      // Each gate is two posts; posts are the only boxes topped in the gate amber.
      const posts = (svg.match(/fill="#f2b234"/g) ?? []).length
      expect(posts).toBeGreaterThanOrEqual(2 * 2 * portsFor(shape))
    }
  })
})
