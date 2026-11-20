import { describe, expect, it } from 'vitest'
import { onSheet, sheetCount, sheetsOf, type Place } from '../places'

// 2026-11-20, the Owner: a district of 4×8 holds two 16×16 grounds, 8×8 four; each cell of its town on the city
// stands for one, and inside you see one at a time and step between them without going out.
const P = (id: string, kind: Place['kind'], shape: string, children: Place[] = [], sheet?: number): Place =>
  ({ id, kind, shape, label: id, question: '', dashboard: null, children, ...(sheet != null ? { sheet } : {}) }) as Place

describe('a district has as many sheets as its town has 4×4 cells', () => {
  it('counts 1, 2 and 4 for 4×4, 4×8 and 8×8; anything that is not a district has one', () => {
    expect(sheetCount(P('d', 'district', '4x4'))).toBe(1)
    expect(sheetCount(P('d', 'district', '4x8'))).toBe(2)
    expect(sheetCount(P('d', 'district', '8x8'))).toBe(4)
    expect(sheetCount(P('b', 'building', '8x8'))).toBe(1)
  })
  it('shares the children out evenly in their order, or by their hints, clamped to the sheets there are', () => {
    const kids = ['a', 'b', 'c', 'd', 'e'].map(id => P(id, 'building', '4x4'))
    expect(sheetsOf(P('d', 'district', '4x8', kids)).map(s => s.map(c => c.id))).toEqual([['a', 'b', 'c'], ['d', 'e']])
    const hinted = [P('a', 'building', '4x4', [], 0), P('b', 'building', '4x4', [], 3), P('c', 'building', '4x4', [], 1)]
    expect(sheetsOf(P('d', 'district', '4x8', hinted)).map(s => s.map(c => c.id))).toEqual([['a'], ['b', 'c']])
  })
  it('shows one sheet as its own 16×16 ground: its children only, laid out anew', () => {
    const d = { ...P('d', 'district', '4x8', ['a', 'b', 'c'].map(id => P(id, 'building', '4x4'))), arrangement: { plan: 'x', at: {} } } as Place
    const two = onSheet(d, 1)
    expect(two.shape).toBe('4x4')
    expect(two.children.map(c => c.id)).toEqual(['c'])
    expect(two.arrangement).toBeUndefined()
    expect(onSheet(P('d', 'district', '4x4', [P('a', 'building', '4x4')]), 3).children.map(c => c.id)).toEqual(['a'])
  })
})
