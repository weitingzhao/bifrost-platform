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

  it('the pages that stand as a landmark, halls included, each one the library draws', () => {
    const marked = placeTree().children.flatMap(d => d.children).filter(b => b.landmark)
    expect(marked.map(b => [b.id, b.landmark]).sort()).toEqual([['command', 'court'], ['crew', 'dormitory'], ['ib', 'supplierDock'], ['records', 'archive'], ['releases', 'launchPad'], ['services', 'payloadBay'], ['status', 'missionControl'], ['work', 'assembly']])
    for (const b of marked) expect(LANDMARKS[b.landmark!].make().svg).toContain('<svg')
  })
})

// Origin: 2026-11-20, the Owner chose to move the rest of gen1's buildings: a district holds its entry page as its hall.
describe('every district holds its own page as its hall', () => {
  it('the hall comes first and opens the entry page', () => {
    for (const d of placeTree().children) {
      expect(d.children[0].id).toBe(d.id)
      expect(d.children[0].dashboard).toBe(`#${d.id}`)
    }
  })
})
