import { describe, expect, it } from 'vitest'
import {
  buildLaunchCheckpoints,
  resolveLaunchVerdict,
  type ResolveLaunchVerdictInput,
} from '@/lib/task-mode/satelliteLaunchVerdict'

function rocketInput(partial: Partial<ResolveLaunchVerdictInput> = {}): ResolveLaunchVerdictInput {
  return {
    mode: 'rocket',
    canOperate: true,
    prodBlocked: false,
    deliverInFlight: false,
    ...partial,
  }
}

function satelliteInput(
  partial: Partial<ResolveLaunchVerdictInput> = {},
): ResolveLaunchVerdictInput {
  return {
    mode: 'satellite',
    canOperate: true,
    prodBlocked: false,
    deliverInFlight: false,
    ...partial,
  }
}

describe('buildLaunchCheckpoints', () => {
  it('Rocket has 4 checkpoints — verdict title is not a fifth item', () => {
    const cps = buildLaunchCheckpoints(
      rocketInput({
        prodBlocked: true,
        tradeProdLabel: 'CAUTION',
        tradeProdSignal: 'degraded',
      }),
    )
    expect(cps.map(c => c.id)).toEqual(['auth', 'platform-prod', 'promote', 'pipeline'])
    expect(cps.filter(c => c.ok)).toHaveLength(3)
    expect(cps.some(c => /Fix Prod/i.test(c.label))).toBe(false)
    expect(cps.find(c => c.id === 'platform-prod')?.detail).toBe('CAUTION')
  })

  it('Satellite has 5 checkpoints — Rocket IB bus + Trade Prod stay distinct', () => {
    const cps = buildLaunchCheckpoints(
      satelliteInput({
        prodBlocked: true,
        blockKind: 'prod',
        tradeProdLabel: 'CAUTION',
        tradeProdSignal: 'degraded',
        rocketSignal: 'ok',
      }),
    )
    expect(cps.map(c => c.id)).toEqual([
      'auth',
      'rocket',
      'trade-prod',
      'promote',
      'pipeline',
    ])
    expect(cps.filter(c => c.ok)).toHaveLength(4)
    expect(cps.some(c => /Fix Prod/i.test(c.label))).toBe(false)
  })
})

describe('resolveLaunchVerdict', () => {
  it('does not NO-GO Rocket when live prod is clear and pipeline is idle', () => {
    const v = resolveLaunchVerdict(rocketInput())
    expect(v.kind).toBe('GO')
  })

  it('NO-GO Rocket on live Platform Prod, not as a synthetic checklist row', () => {
    const v = resolveLaunchVerdict(
      rocketInput({ prodBlocked: true, tradeProdLabel: 'CAUTION' }),
    )
    expect(v.kind).toBe('NO_GO')
    expect(v.title).toBe('Fix Prod environment before release')
    expect(buildLaunchCheckpoints(rocketInput({ prodBlocked: true })).map(c => c.label)).not.toContain(
      v.title,
    )
  })

  it('treats an in-flight pipeline as IN_FLIGHT when prod is not blocked', () => {
    expect(resolveLaunchVerdict(rocketInput({ deliverInFlight: true })).kind).toBe('IN_FLIGHT')
    expect(resolveLaunchVerdict(satelliteInput({ deliverInFlight: true })).kind).toBe('IN_FLIGHT')
  })
})
