import { describe, expect, it } from 'vitest'
import { TINTS, tintOf } from '../theme'
import { LANDMARKS } from '../library/landmarkLooks'
import { placeTree } from '../../content/consolePlaces'
import type { Place } from '../places'

// Origin: the Owner, 2026-11-20 ("一代的建筑和街区配色很漂亮，搬过来再删一代"): gen2 takes gen1's district
// tints and its landmarks. These pin that each district keeps its own tint by its place, and that the
// pages given a landmark get one the library can draw.
const town = (ids: string[]): Place => ({ id: 'town', kind: 'town', shape: '8x8', label: '', question: '', dashboard: null, children: ids.map(id => ({ id, kind: 'district', shape: '2x2', label: id, question: '', dashboard: null, children: [] })) })

describe('the first generation’s look in gen2', () => {
  it('a district takes gen1’s tint by its place in the town; the town itself and the night keep the terrain’s lot', () => {
    const t = town(['a', 'b', 'c'])
    expect(tintOf(t, ['b'], 'day', 0x123456)).toBe(TINTS[1])
    expect(tintOf(t, [], 'day', 0x123456)).toBe(0x123456)
    expect(tintOf(t, ['b'], 'night', 0x123456)).toBe(0x123456)
  })

  it('the pages that stand as a landmark are releases, records, status and ib, each one the library draws', () => {
    const marked = placeTree().children.flatMap(d => d.children).filter(b => b.landmark)
    expect(marked.map(b => [b.id, b.landmark]).sort()).toEqual([['ib', 'supplierDock'], ['records', 'archive'], ['releases', 'launchPad'], ['status', 'missionControl']])
    for (const b of marked) expect(LANDMARKS[b.landmark!].make().svg).toContain('<svg')
  })
})
