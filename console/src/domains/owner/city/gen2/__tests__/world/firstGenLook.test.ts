import { describe, expect, it } from 'vitest'
import { TINTS, tintOf } from '@bifrost/ui/city/theme'
import type { Place } from '@bifrost/ui/city/places'

// Origin (ISSUE-9): gen2 takes gen1's district
// tints and its landmarks. These pin that each district keeps its own tint by its place. (The pages that
// stood as landmarks left with the menu's tree, 2026-11-20: the districts became the domains.)
const town = (ids: string[]): Place => ({ id: 'town', kind: 'town', shape: '8x8', label: '', question: '', dashboard: null, children: ids.map(id => ({ id, kind: 'district', shape: '2x2', label: id, question: '', dashboard: null, children: [] })) })

describe('the first generation’s look in gen2', () => {
  it('a district takes gen1’s tint by its place in the town; the town itself and the night keep the terrain’s lot', () => {
    const t = town(['a', 'b', 'c'])
    expect(tintOf(t, ['b'], 'day', 0x123456)).toBe(TINTS[1])
    expect(tintOf(t, [], 'day', 0x123456)).toBe(0x123456)
    expect(tintOf(t, ['b'], 'night', 0x123456)).toBe(0x123456)
  })

})

