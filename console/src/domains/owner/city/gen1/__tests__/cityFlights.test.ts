import { describe, expect, it } from 'vitest'
import { chicagoMidnight, flightLog, replayAt, type Run } from '../cityFlights'

// Origin: 2026-11-19, the Owner: the system launches over and over; the city shows how many times today and replays them.
const lanes = [
  { pipeline: 'bifrost-deliver-platform', repos: ['bifrost-platform', 'bifrost-trade-infra', 'bifrost-ui'] },
  { pipeline: 'bifrost-deliver-platform-prod', repos: ['bifrost-platform'] },
  { pipeline: 'bifrost-deliver-prod', repos: ['bifrost-trade-api', 'bifrost-trade-core'] },
]
// 2026-11-19 12:00 Chicago (CST, UTC-6) = 18:00 UTC.
const NOW = Date.parse('2026-11-19T18:00:00Z')
const run = (pipeline: string, start: string, end?: string, reason = 'Succeeded'): Run => ({ name: `${pipeline}-${start}`, pipeline, start_time: start, completion_time: end, reason })

describe('the flight log', () => {
  it('starts the day at midnight in Chicago', () => {
    expect(new Date(chicagoMidnight(NOW)).toISOString()).toBe('2026-11-19T06:00:00.000Z')
  })
  it('counts today\'s PROD launches and STG stackings, ok, failed and going, and leaves out yesterday', () => {
    const log = flightLog([
      run('bifrost-deliver-platform', '2026-11-19T05:59:00Z', '2026-11-19T06:03:00Z'),
      run('bifrost-deliver-platform', '2026-11-19T07:00:00Z', '2026-11-19T07:04:00Z'),
      run('bifrost-deliver-platform', '2026-11-19T08:00:00Z', '2026-11-19T08:05:00Z', 'Failed'),
      run('bifrost-deliver-platform-prod', '2026-11-19T09:00:00Z', '2026-11-19T09:06:00Z'),
      run('bifrost-deliver-prod', '2026-11-19T17:50:00Z'),
      run('bifrost-ci-platform', '2026-11-19T10:00:00Z', '2026-11-19T10:20:00Z'),
    ], lanes, NOW)
    expect(log.stg).toEqual({ total: 2, ok: 1, failed: 1, going: 0 })
    expect(log.prod).toEqual({ total: 2, ok: 1, failed: 0, going: 1 })
    expect(log.flights.find(f => f.pipeline === 'bifrost-deliver-prod')?.product?.name).toBe('Trade')
    expect(log.flights.find(f => f.pipeline === 'bifrost-deliver-platform-prod')?.product).toBeNull()
  })
  it('replays the day in order, each flight seen for a moment however short', () => {
    const log = flightLog([run('bifrost-deliver-platform-prod', '2026-11-19T09:00:00Z', '2026-11-19T09:00:30Z')], lanes, NOW)
    const length = 60_000, span = NOW - log.from, at = (iso: string) => ((Date.parse(iso) - log.from) / span) * length
    expect(replayAt(log, NOW, at('2026-11-19T08:59:00Z'), length).active).toHaveLength(0)
    expect(replayAt(log, NOW, at('2026-11-19T09:00:10Z'), length).active).toHaveLength(1)
    expect(replayAt(log, NOW, at('2026-11-19T09:00:10Z') + 1000, length).active).toHaveLength(1)
    expect(replayAt(log, NOW, length, length).clock).toBe(NOW)
  })
})
