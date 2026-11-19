import { describe, expect, it } from 'vitest'
import { ENTRY_ROUTE_IDS } from '@/lib/shell/consoleRoutes'
import { fitsBudget, TAPPABLE } from '../../frame/grid'
import { formatPath, here, parsePath, resolve, trail, type Place } from '../../frame/places'
import { placeTree } from '../consolePlaces'

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

describe('density (the Owner, 2026-11-19: one 16×16 ground per page; too much goes a level down)', () => {
  const all = (p: Place): Place[] => [p, ...p.children.flatMap(all)]
  it('every place holds no more than its ground can', () => {
    for (const p of all(placeTree())) expect(fitsBudget(p.children), `${p.id} is over its ground`).toBe(true)
  })
  it('everything you can tap is at least 2×2', () => {
    for (const p of all(placeTree()).slice(1)) expect(p.size, p.id).toBeGreaterThanOrEqual(TAPPABLE)
  })
})

// 2026-11-19, the Owner: names and contents must change without touching the frame.
describe('content is swappable', () => {
  it('a different tree renders through the same frame contract', async () => {
    const { consoleContent } = await import('../console')
    const c = consoleContent()
    const other = { ...c, tree: { ...c.tree, label: 'Family', children: c.tree.children.slice(0, 2).map(d => ({ ...d, label: `${d.label} (renamed)` })) } }
    expect(other.tree.children.map(d => d.label)).toEqual(['Command (renamed)', 'Crew (renamed)'])
    expect(other.panels(other.tree, () => {}).map(p => p.id)).toEqual(['inside', 'chart'])
  })
})
