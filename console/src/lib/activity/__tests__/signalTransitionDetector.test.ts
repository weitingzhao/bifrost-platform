import { describe, expect, it, beforeEach } from 'vitest'
import {
  SignalTransitionDetector,
  SIGNAL_TRANSITION_DEBOUNCE_MS,
  chipCorrelateKey,
  signalTransitionActivityId,
} from '@/lib/activity/signalTransitionDetector'

describe('SignalTransitionDetector', () => {
  let det: SignalTransitionDetector

  beforeEach(() => {
    det = new SignalTransitionDetector()
  })

  it('ignores first-load baseline (including unknown→ok)', () => {
    const t1 = det.observe([
      { label: 'Account sync', signal: 'unknown', envScope: 'stg' },
      { label: 'Trade · APIs STG', signal: 'ok', envScope: 'stg' },
    ])
    expect(t1).toEqual([])
    const t2 = det.observe([
      { label: 'Account sync', signal: 'ok', envScope: 'stg' },
      { label: 'Trade · APIs STG', signal: 'ok', envScope: 'stg' },
    ])
    expect(t2).toEqual([])
  })

  it('emits ok→fail and fail→ok; recovery is not debounced', () => {
    det.observe([{ label: 'IB Socket', signal: 'ok', envScope: 'shared' }])
    const t0 = 1_000_000
    const down = det.observe(
      [{ label: 'IB Socket', signal: 'fail', detail: 'down', envScope: 'shared' }],
      t0,
    )
    expect(down).toHaveLength(1)
    expect(down[0]).toMatchObject({
      from: 'ok',
      to: 'fail',
      chipLabel: 'IB Socket',
      envScope: 'shared',
    })

    // Recovery within debounce window must still emit (P1).
    const up = det.observe(
      [{ label: 'IB Socket', signal: 'ok', envScope: 'shared' }],
      t0 + 2000,
    )
    expect(up).toHaveLength(1)
    expect(up[0]).toMatchObject({ from: 'fail', to: 'ok', envScope: 'shared' })
  })

  it('debounces degrade-only on the same scoped chip for 5 minutes', () => {
    det.observe([{ label: 'K8s', signal: 'ok', envScope: 'prod' }])
    const t0 = 1_000_000
    const first = det.observe(
      [{ label: 'K8s', signal: 'degraded', envScope: 'prod' }],
      t0,
    )
    expect(first).toHaveLength(1)
    // second degrade (via ok→fail path: go ok first without emitting if we jump degraded→fail)
    const severity = det.observe(
      [{ label: 'K8s', signal: 'fail', envScope: 'prod' }],
      t0 + 2000,
    )
    expect(severity).toHaveLength(0)

    const after = det.observe(
      [{ label: 'K8s', signal: 'ok', envScope: 'prod' }],
      t0 + SIGNAL_TRANSITION_DEBOUNCE_MS + 1,
    )
    // still degraded→... wait we left it at fail, so fail→ok is recovery
    expect(after).toHaveLength(1)
    expect(after[0]).toMatchObject({ from: 'fail', to: 'ok' })
  })

  it('keeps STG and PROD same chip labels independent', () => {
    det.observe([
      { label: 'Account sync', signal: 'ok', envScope: 'stg' },
      { label: 'Account sync', signal: 'ok', envScope: 'prod' },
    ])
    const t0 = 1_000_000
    const transitions = det.observe(
      [
        { label: 'Account sync', signal: 'fail', envScope: 'stg' },
        { label: 'Account sync', signal: 'ok', envScope: 'prod' },
      ],
      t0,
    )
    expect(transitions).toHaveLength(1)
    expect(transitions[0]).toMatchObject({ envScope: 'stg', to: 'fail' })

    const idStg = signalTransitionActivityId('stg', 'Account sync', 'ok', 'fail')
    const idProd = signalTransitionActivityId('prod', 'Account sync', 'ok', 'fail')
    expect(idStg).not.toBe(idProd)
    expect(chipCorrelateKey('stg', 'Account sync')).toBe('stg:account sync')
    expect(chipCorrelateKey('prod', 'Account sync')).toBe('prod:account sync')
  })
})
