import { describe, expect, it } from 'vitest'
import { groupCapabilities, upgradeLine } from '@/components/market-data/capabilitiesModel'

describe('groupCapabilities', () => {
  it('splits by status and keeps the API order inside each group', () => {
    const groups = groupCapabilities({
      capabilities: [
        { id: 'a', label: 'A', status: 'entitled' },
        { id: 'b', label: 'B', status: 'planned', requires: 'Options Developer' },
        { id: 'c', label: 'C', status: 'entitled' },
        { id: 'd', label: 'D', status: 'unavailable' },
      ],
    })
    expect(groups.entitled.map(c => c.id)).toEqual(['a', 'c'])
    expect(groups.planned.map(c => c.id)).toEqual(['b'])
    expect(groups.unavailable.map(c => c.id)).toEqual(['d'])
  })

  it('handles a missing matrix', () => {
    expect(groupCapabilities(null)).toEqual({ entitled: [], planned: [], unavailable: [] })
  })
})

describe('upgradeLine', () => {
  it('names the plan that enables a planned capability', () => {
    expect(upgradeLine({ id: 'x', label: 'X', status: 'planned', requires: 'Options Developer' })).toBe(
      'needs Options Developer',
    )
    expect(upgradeLine({ id: 'y', label: 'Y', status: 'planned' })).toBe('needs a subscription change')
  })
})
