import { describe, expect, it } from 'vitest'
import { tickRank } from '@/components/market-data/overviewDashModel'

describe('tickRank', () => {
  it('ranks numbers and formatted counts', () => {
    expect(tickRank(1172)).toBe(1172)
    expect(tickRank('1,172')).toBe(1172)
  })

  it('ranks status words for color direction', () => {
    expect(tickRank('ok')).toBeGreaterThan(tickRank('degraded') ?? -1)
    expect(tickRank('fail')).toBe(0)
    expect(tickRank('Today OK')).toBe(2)
  })
})
