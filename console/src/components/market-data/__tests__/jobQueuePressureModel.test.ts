import { describe, expect, it } from 'vitest'
import {
  buildQueuePressure,
  etaMinutesFromRate,
  pressureLevel,
} from '@/components/market-data/jobQueuePressureModel'

describe('jobQueuePressureModel', () => {
  it('computes ETA from pending / 15m rate', () => {
    expect(etaMinutesFromRate(1420, 14.2)).toBe(100)
    expect(etaMinutesFromRate(0, 14.2)).toBeNull()
    expect(etaMinutesFromRate(100, 0)).toBeNull()
  })

  it('classifies idle / stalled / high', () => {
    expect(pressureLevel({ pending: 0, running: 0, ratePerMin: 0, etaMinutes: null, waitedSec: null })).toBe(
      'idle',
    )
    expect(pressureLevel({ pending: 4000, running: 0, ratePerMin: 0, etaMinutes: null, waitedSec: 100 })).toBe(
      'stalled',
    )
    expect(
      pressureLevel({ pending: 4000, running: 10, ratePerMin: 14, etaMinutes: 280, waitedSec: 3000 }),
    ).toBe('high')
  })

  it('uses API ETA for the full queue and kind-local ETA when filtered', () => {
    const nowMs = Date.parse('2026-08-22T13:45:00Z')
    const all = buildQueuePressure({
      pending: 4566,
      running: 45,
      ratePerMin: 15,
      etaMinutes: 304.4,
      oldestPendingAgeSec: 3000,
      kinds: [
        { kind: 'financials', pending: 4500, running: 11, active: 4511 },
        { kind: 'option_snapshot', pending: 0, running: 16, active: 16 },
      ],
      nowMs,
    })
    expect(all.etaMinutes).toBe(304.4)
    expect(all.emptyAtMs).toBe(nowMs + 304.4 * 60_000)
    expect(all.level).toBe('high')
    expect(all.progress01).toBeGreaterThan(0)
    expect(all.progress01).toBeLessThan(1)

    const scoped = buildQueuePressure({
      pending: 4566,
      running: 45,
      ratePerMin: 15,
      etaMinutes: 304.4,
      selectedKind: 'financials',
      kinds: [{ kind: 'financials', pending: 4500, running: 11, active: 4511 }],
      nowMs,
    })
    expect(scoped.pending).toBe(4500)
    expect(scoped.etaMinutes).toBe(300)
  })
})
