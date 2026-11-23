import { describe, expect, it } from 'vitest'
import { peerAddress, visit, worldLocation } from '../location'
// 2026-11-22 inventory: cross-place links discard selection; pinned time has no public route contract.
describe('world journey address continuity', () => {
  it('preserves opaque identity and all existing query fields through peer views', () => {
    const source = '#collaboration?exchange=a%2Fb%26c&scope=crew&at=2026-11-22&future=one%2Btwo'
    expect(peerAddress(peerAddress(source, 'console'), '')).toBe(source)
    expect(worldLocation(source).object).toBe('a/b&c')
  })
  it('retains domain path and selection without inventing a Console domain mapping', () => {
    expect(peerAddress('#city?at=compute%2Fhost&sel=disk', 'ledger')).toBe('#city?at=compute%2Fhost&sel=disk&look=ledger')
    expect(worldLocation('#system').object).toBe('')
  })
  it('returns to exact selected exchange and unwinds browser back', () => {
    const source = '#collaboration?exchange=message%3A42'
    expect(visit([], source, '#observation')).toEqual([source])
    expect(visit(['#city', source], '#observation', source)).toEqual(['#city'])
    expect(visit([], '#city?sel=a', '#city?sel=b')).toEqual([])
    expect(visit(Array(12).fill('#system'), '#city', '#observation')).toHaveLength(12)
  })
})
