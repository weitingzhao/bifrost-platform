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
    tradeProdSignal: 'ok',
    promoteSignal: 'ok',
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
    rocketSignal: 'ok',
    tradeProdSignal: 'ok',
    promoteSignal: 'ok',
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

/** TD-226: an unmeasured readiness dimension never reads as clear. */
describe('unknown readiness is not GO', () => {
  const satelliteDims = ['rocketSignal', 'tradeProdSignal', 'promoteSignal'] as const
  const rocketDims = ['tradeProdSignal', 'promoteSignal'] as const

  for (const dim of satelliteDims) {
    it(`Satellite ${dim} = unknown gives PROBING and no green checkpoint for it`, () => {
      const input = satelliteInput({ [dim]: 'unknown' })
      const v = resolveLaunchVerdict(input)
      expect(v.kind).not.toBe('GO')
      expect(v.kind).toBe('PROBING')
      expect(v.disabledReason).toBe('readiness not measured')
      const cps = buildLaunchCheckpoints(input)
      const unknownCps = cps.filter(c => c.signal === 'unknown')
      expect(unknownCps).toHaveLength(1)
      expect(unknownCps.every(c => !c.ok)).toBe(true)
      expect(cps.some(c => c.ok && c.signal === 'unknown')).toBe(false)
    })

    it(`Satellite ${dim} missing gives PROBING`, () => {
      expect(resolveLaunchVerdict(satelliteInput({ [dim]: undefined })).kind).toBe('PROBING')
    })
  }

  for (const dim of rocketDims) {
    it(`Rocket ${dim} = unknown gives PROBING and no green checkpoint for it`, () => {
      const input = rocketInput({ [dim]: 'unknown' })
      expect(resolveLaunchVerdict(input).kind).toBe('PROBING')
      const cps = buildLaunchCheckpoints(input)
      expect(cps.some(c => c.ok && c.signal === 'unknown')).toBe(false)
      expect(cps.filter(c => c.signal === 'unknown').every(c => !c.ok)).toBe(true)
    })
  }

  it('a failing dimension still wins over an unknown one (NO_GO, not PROBING)', () => {
    const v = resolveLaunchVerdict(
      satelliteInput({
        prodBlocked: true,
        blockKind: 'rocket',
        rocketSignal: 'fail',
        tradeProdSignal: 'unknown',
      }),
    )
    expect(v.kind).toBe('NO_GO')
  })

  it('an in-flight deliver stays IN_FLIGHT while readiness is unknown', () => {
    expect(
      resolveLaunchVerdict(satelliteInput({ deliverInFlight: true, tradeProdSignal: 'unknown' })).kind,
    ).toBe('IN_FLIGHT')
  })

  it('all dimensions measured ok gives GO with every checkpoint green', () => {
    expect(resolveLaunchVerdict(satelliteInput()).kind).toBe('GO')
    expect(buildLaunchCheckpoints(satelliteInput()).every(c => c.ok)).toBe(true)
    expect(buildLaunchCheckpoints(rocketInput()).every(c => c.ok)).toBe(true)
  })
})
