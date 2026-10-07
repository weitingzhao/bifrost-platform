import { describe, expect, it } from 'vitest'
import type { ReleaseGateResponse } from '@/api/deliveryTypes'
import { releaseGateSignal } from '@/components/task-mode/readiness/utils'

function gate(over: Partial<ReleaseGateResponse>): ReleaseGateResponse {
  return {
    result: 'pass',
    log_path: '',
    checks: [],
    ready: true,
    generated_at: '2026-10-07T00:00:00Z',
    reachability: 'ok',
    detail: '',
    ...over,
  }
}

/** TD-230: a pass is green only while the API still calls it ready. */
describe('releaseGateSignal', () => {
  it('a ready pass is ok', () => {
    expect(releaseGateSignal(gate({})).signal).toBe('ok')
  })

  it('a stale pass (ready=false) is not ok and names the blocker', () => {
    const blocker = 'Release gate passed 36d ago (older than 24h) — re-run the gate'
    const s = releaseGateSignal(gate({ ready: false, blockers: [blocker] }))
    expect(s.signal).toBe('degraded')
    expect(s.detail).toBe(blocker)
  })

  it('an inconclusive gate (required check unknown) is not ok', () => {
    expect(releaseGateSignal(gate({ result: 'inconclusive', ready: false })).signal).toBe('degraded')
  })

  it('no data is unknown, a failed gate is fail', () => {
    expect(releaseGateSignal(undefined).signal).toBe('unknown')
    expect(releaseGateSignal(gate({ result: 'fail', ready: false })).signal).toBe('fail')
  })
})
