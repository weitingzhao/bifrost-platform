import { describe, expect, it } from 'vitest'
import {
  resolveConstellationImpact,
  staticConstellationImpact,
} from '@/lib/delivery/constellationImpact'

describe('resolveConstellationImpact', () => {
  it('Trade origin with no changes: Research SKIP', () => {
    const impact = staticConstellationImpact('trade')
    expect(impact.rows.find(r => r.payload === 'trade')?.verdict).toBe('origin')
    expect(impact.rows.find(r => r.payload === 'research')?.verdict).toBe('skip')
    expect(impact.flyWith).toEqual([])
  })

  it('Research origin: Trade SUGGEST via static edges', () => {
    const impact = staticConstellationImpact('research')
    expect(impact.rows.find(r => r.payload === 'research')?.verdict).toBe('origin')
    expect(impact.rows.find(r => r.payload === 'trade')?.verdict).toBe('suggest')
    expect(impact.flyWith).toEqual(['trade'])
  })

  it('only bifrost-research changed + Research origin → suggest Trade', () => {
    const impact = resolveConstellationImpact({
      origin: 'research',
      changedRepos: ['bifrost-research'],
    })
    expect(impact.flyWith).toEqual(['trade'])
  })

  it('only bifrost-trade-api changed + Trade origin → Research skip', () => {
    const impact = resolveConstellationImpact({
      origin: 'trade',
      changedRepos: ['bifrost-trade-api'],
    })
    expect(impact.rows.find(r => r.payload === 'research')?.verdict).toBe('skip')
    expect(impact.flyWith).toEqual([])
  })

  it('both research + trade repos: Research origin still suggests Trade', () => {
    const impact = resolveConstellationImpact({
      origin: 'research',
      changedRepos: ['bifrost-research', 'bifrost-trade-frontend'],
    })
    expect(impact.flyWith).toEqual(['trade'])
  })

  it('path lift: Trade FE research path → suggest Research', () => {
    const impact = resolveConstellationImpact({
      origin: 'trade',
      changedRepos: ['bifrost-trade-frontend'],
      changedPathsByRepo: {
        'bifrost-trade-frontend': ['src/pages/research/Loop.tsx'],
      },
    })
    expect(impact.rows.find(r => r.payload === 'research')?.verdict).toBe('suggest')
    expect(impact.flyWith).toEqual(['research'])
  })

  it('path miss: Trade FE positions path → Research skip', () => {
    const impact = resolveConstellationImpact({
      origin: 'trade',
      changedRepos: ['bifrost-trade-frontend'],
      changedPathsByRepo: {
        'bifrost-trade-frontend': ['src/pages/positions/Index.tsx'],
      },
    })
    expect(impact.rows.find(r => r.payload === 'research')?.verdict).toBe('skip')
  })
})
