import { describe, expect, it } from 'vitest'
import { buildingsOf, OTHER } from '../districtBuildings'
import type { DomainHoldings } from '../domainOwns'

// 2026-11-22 (ISSUE-9): the DAG must show at every level; the map line serves each domain's buildings (its Go
// packages grouped by the first directory under a Go root, f2c674e8). These pin that City draws them as read,
// sized by packages, and that a domain with more than its ground holds (foundation: 30 buildings that day) keeps
// the biggest and puts the rest in one Other that names them: the frame's 16-slot rule, nothing dropped.
const holdings = (n: number, big = 0): DomainHoldings => ({
  id: 'foundation', packages: [], groups: [], paths: [], workloads: [],
  buildings: [...(big ? [{ id: 'bus', packages: Array.from({ length: big }, (_, i) => `api/internal/bus/p${i}`) }] : []), ...Array.from({ length: n }, (_, i) => ({ id: `b${i}`, packages: [`api/internal/b${i}`] }))],
})

describe('a domain’s buildings in its district', () => {
  it('each building as read, sized by its packages', () => {
    const out = buildingsOf(holdings(2, 25), 'online')
    expect(out.map(b => [b.id, b.shape])).toEqual([['bus', '8x8'], ['b0', '4x4'], ['b1', '4x4']])
  })
  it('more than the ground holds: the biggest kept, the rest in one Other that names them', () => {
    const out = buildingsOf(holdings(29, 25), 'online')
    const units = out.reduce((u, b) => u + ({ '4x4': 1, '4x8': 2, '8x8': 4 } as Record<string, number>)[b.shape], 0)
    expect(out.length).toBeLessThanOrEqual(16)
    expect(units).toBeLessThanOrEqual(16)
    const other = out.find(b => b.id === OTHER)!
    expect(other.label).toMatch(/^Other · \d+$/)
    expect(other.question).toContain('b28')
    // 2026-11-22 (ISSUE-35): past 16 a level is cut too fine, to decouple or merge: Other stands as a construction site.
    expect(other.landmark).toBe('site')
  })
  it('no buildings served: none drawn', () => {
    expect(buildingsOf(undefined, 'unread')).toEqual([])
  })
})
