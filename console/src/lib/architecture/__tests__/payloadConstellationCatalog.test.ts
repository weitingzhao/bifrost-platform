import { describe, expect, it } from 'vitest'
import {
  CONSTELLATION_EDGES,
  companionsFor,
  flattenLaunchNav,
  orderedSatellitePayloads,
  pathMatchesAnyGlob,
  payloadsForRepos,
  SATELLITE_PAYLOADS,
} from '@/lib/architecture/payloadConstellationCatalog'
import type { ShellNavItem } from '@bifrost/ui'
import { COMPONENT_LINE_IDS } from '@/lib/briefing/workLanes'

describe('payloadConstellationCatalog', () => {
  it('keeps Briefing Line count at six (no research line)', () => {
    expect(COMPONENT_LINE_IDS).toEqual([
      'rocket',
      'satellite',
      'engineer',
      'ground',
      'operations',
      'subcontractor',
    ])
    expect(COMPONENT_LINE_IDS).not.toContain('research')
  })

  it('maps repos to payloads', () => {
    expect(payloadsForRepos(['bifrost-research'])).toEqual(['research'])
    expect(payloadsForRepos(['bifrost-trade-api'])).toEqual(['trade'])
    expect(payloadsForRepos(['bifrost-research', 'bifrost-trade-frontend']).sort()).toEqual([
      'research',
      'trade',
    ])
  })

  it('companions for research suggest trade via ui-surface and api-contract', () => {
    const c = companionsFor('research')
    expect(c).toHaveLength(1)
    expect(c[0]?.payload).toBe('trade')
    expect(c[0]?.strength).toBe('suggests')
    expect(c[0]?.kinds.sort()).toEqual(['api-contract', 'ui-surface'])
  })

  it('companions for trade are empty (no static Trade→Research edge)', () => {
    expect(companionsFor('trade')).toEqual([])
    expect(CONSTELLATION_EDGES.every(e => e.from !== 'trade')).toBe(true)
  })

  it('orders display-host before instruments', () => {
    expect(orderedSatellitePayloads().map(p => p.id)).toEqual(['trade', 'research'])
  })

  it('flattens Launch Desk children for allowed-tab filters', () => {
    const items: ShellNavItem[] = [
      { id: 'platform-release', label: 'Rocket' },
      {
        id: 'satellite-vehicle',
        label: 'Satellite',
        children: [
          { id: 'trade-release', label: 'Trade' },
          { id: 'research-release', label: 'Research' },
        ],
      },
      { id: 'plugin-release', label: 'Plugin' },
    ]
    expect(flattenLaunchNav(items).map(i => i.id)).toEqual([
      'platform-release',
      'trade-release',
      'research-release',
      'plugin-release',
    ])
  })

  it('matches Trade FE research path globs', () => {
    const globs = SATELLITE_PAYLOADS.find(p => p.id === 'trade')!.companionPathGlobs
    expect(pathMatchesAnyGlob('src/pages/research/Loop.tsx', globs)).toBe(true)
    expect(pathMatchesAnyGlob('src/components/research/harness/Foo.tsx', globs)).toBe(true)
    expect(pathMatchesAnyGlob('src/pages/positions/Index.tsx', globs)).toBe(false)
  })
})
