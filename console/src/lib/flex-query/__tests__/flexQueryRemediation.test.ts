import { describe, expect, it } from 'vitest'
import { analyzeFlexProbe } from '@/lib/flex-query/flexQueryRemediation'
import type { MarketDataStatusResponse } from '@/api/satelliteBusTypes'

describe('flexQueryRemediation', () => {
  it('flags stale freshness dimensions', () => {
    const status: MarketDataStatusResponse = {
      reachability: 'degraded',
      summary: 'freshness 0/2 ok',
      freshness: [
        {
          dimension: 'flex-trades',
          verdict: 'stale',
          age_hours: 80,
          rows_written: 58,
        },
        {
          dimension: 'flex-transactions',
          verdict: 'ok',
          age_hours: 1,
          rows_written: 8,
        },
      ],
    }
    const analysis = analyzeFlexProbe(status)
    expect(analysis.staleKinds).toEqual(['flex-trades'])
    expect(analysis.needsAttention).toBe(true)
    expect(analysis.findings.some(f => f.id === 'fresh-stale-flex-trades')).toBe(true)
  })

  it('ignores worker attempt counter when freshness is healthy', () => {
    const status: MarketDataStatusResponse = {
      reachability: 'ok',
      summary: 'flex 3 done 1 failed · freshness 2/2 ok',
      workers: [{ pool: 'flex', status: 'ok', jobs_done: 3, jobs_failed: 1 }],
      freshness: [
        { dimension: 'flex-trades', verdict: 'ok', age_hours: 0.1, rows_written: 60 },
        { dimension: 'flex-transactions', verdict: 'ok', age_hours: 0.1, rows_written: 8 },
      ],
    }
    const analysis = analyzeFlexProbe(status)
    expect(analysis.findings.some(f => f.id === 'worker-failed')).toBe(false)
    expect(analysis.needsAttention).toBe(false)
  })
})

// 2026-11-23 箭10: the IB Flex remediation tags printed the Console's own danger / warning beside the engine words,
// and a stale ingest was a warning. Stale is older than its declared freshness (fail); a probe not answered is pending.
describe('flex remediation in the engine words', () => {
  it('tags a stale ingest fail and a probe not yet loaded pending', () => {
    const stale = analyzeFlexProbe({
      reachability: 'degraded',
      freshness: [{ dimension: 'flex-trades', verdict: 'stale', age_hours: 80, rows_written: 58 }],
    } as MarketDataStatusResponse)
    expect(stale.findings.find(f => f.id === 'fresh-stale-flex-trades')?.severity).toBe('fail')
    expect(analyzeFlexProbe(undefined).findings[0].severity).toBe('pending')
  })
})
