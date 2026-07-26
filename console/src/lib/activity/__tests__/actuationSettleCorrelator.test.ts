import { describe, expect, it, beforeEach } from 'vitest'
import { correlateActuationSettle } from '@/lib/activity/actuationSettleCorrelator'
import {
  __resetActivityStoreForTests,
  getActivityEvents,
  upsertActivity,
} from '@/lib/activity/activityStore'
import { chipCorrelateKey } from '@/lib/activity/signalTransitionDetector'

describe('correlateActuationSettle', () => {
  beforeEach(() => {
    __resetActivityStoreForTests()
  })

  it('marks matching applying actuation as resolved (env-scoped)', () => {
    upsertActivity({
      id: 'actuation:restart',
      kind: 'actuation',
      phase: 'applying',
      title: 'Restart account-sync',
      correlateKey: chipCorrelateKey('prod', 'Account sync'),
      bumpTs: true,
    })
    const hit = correlateActuationSettle('Account sync', 'prod')
    expect(hit?.settledOutcome).toBe('resolved')
    expect(getActivityEvents()[0].phase).toBe('settled')
  })

  it('does not correlate across envs with the same chip label', () => {
    upsertActivity({
      id: 'actuation:stg',
      kind: 'actuation',
      phase: 'applying',
      title: 'Restart account-sync',
      correlateKey: chipCorrelateKey('stg', 'Account sync'),
      bumpTs: true,
    })
    const miss = correlateActuationSettle('Account sync', 'prod')
    expect(miss).toBeNull()
    const hit = correlateActuationSettle('Account sync', 'stg')
    expect(hit?.settledOutcome).toBe('resolved')
  })

  it('re-opens signal-unchanged settle when chip recovers', () => {
    upsertActivity({
      id: 'actuation:gw',
      kind: 'actuation',
      phase: 'settled',
      title: 'Gateway reconnect',
      correlateKey: chipCorrelateKey('shared', 'Rocket · IB socket'),
      settledOutcome: 'signal-unchanged',
      bumpTs: true,
    })
    const hit = correlateActuationSettle('Rocket · IB socket', 'shared')
    expect(hit?.settledOutcome).toBe('resolved')
  })
})
