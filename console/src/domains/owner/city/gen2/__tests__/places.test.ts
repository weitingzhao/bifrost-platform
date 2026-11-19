import { describe, expect, it } from 'vitest'
import { ENTRY_ROUTE_IDS } from '@/lib/shell/consoleRoutes'
import { formatPath, here, parsePath, placeTree, resolve, slots, trail } from '../places'

// 2026-11-19, the Owner: the game look and the dashboard look are two styles of one Console,
// so the city's places must be the Console's menu, read from its routes, not a second map.
describe('the city is the Console menu', () => {
  const tree = placeTree()
  it('has one district per menu entry, in menu order', () => {
    expect(tree.children.map(d => d.id)).toEqual([...ENTRY_ROUTE_IDS])
  })
  it('puts each detail page in the district the Console puts it under', () => {
    const of = (id: string) => tree.children.find(d => d.children.some(b => b.id === id))?.id
    expect(of('infrastructure')).toBe('system')
    expect(of('releases')).toBe('work')
    expect(of('data')).toBe('services')
    expect(of('ib')).toBe('services')
  })
  it('does not put the City inside itself', () => {
    expect(tree.children.flatMap(d => d.children).some(b => b.id === 'city')).toBe(false)
  })
  it('gives every district and building its dashboard address', () => {
    for (const d of tree.children) {
      expect(d.dashboard).toBe(`#${d.id}`)
      for (const b of d.children) expect(b.dashboard).toBe(`#${b.id}`)
    }
  })
})

describe('addresses', () => {
  const tree = placeTree()
  it('round-trip', () => {
    expect(parsePath(formatPath(['system', 'infrastructure']))).toEqual(['system', 'infrastructure'])
    expect(parsePath('')).toEqual([])
    expect(parsePath('#/')).toEqual([])
  })
  it('keep only the part of a path that exists', () => {
    expect(resolve(tree, ['system', 'nowhere', 'x'])).toEqual(['system'])
    expect(here(tree, ['nowhere']).id).toBe('town')
    expect(trail(tree, ['system', 'infrastructure']).map(p => p.label)).toEqual(['City', 'System', 'Infrastructure'])
  })
})

describe('slots', () => {
  it('stand apart and centre on the origin', () => {
    const s = slots(5)
    expect(new Set(s.map(p => `${p.x},${p.y}`)).size).toBe(5)
    expect(slots(4).reduce((a, p) => a + p.x + p.y, 0)).toBe(0)
  })
  // 2026-11-19: on an upright phone the town's five blocks sat in a band across the middle, half the screen empty.
  it('on a tall screen run down the screen rather than across', () => {
    const across = (s: { x: number; y: number }[]) => Math.max(...s.map(p => p.x - p.y)) - Math.min(...s.map(p => p.x - p.y))
    const down = (s: { x: number; y: number }[]) => Math.max(...s.map(p => p.x + p.y)) - Math.min(...s.map(p => p.x + p.y))
    const tall = slots(5, true)
    expect(down(tall) * 16).toBeGreaterThan(across(tall) * 32 * 0.8)
    expect(down(slots(5)) * 16).toBeLessThan(across(slots(5)) * 32)
  })
})
