import { describe, expect, it } from 'vitest'
import { shortIngestKind } from '@/components/market-data/ingestKindLabel'

describe('shortIngestKind', () => {
  it('aliases long option kinds', () => {
    expect(shortIngestKind('option_open_interest')).toBe('opt_oi')
    expect(shortIngestKind('option_snapshot')).toBe('opt_snap')
  })

  it('passes through unknown kinds', () => {
    expect(shortIngestKind('calendar')).toBe('calendar')
  })
})
