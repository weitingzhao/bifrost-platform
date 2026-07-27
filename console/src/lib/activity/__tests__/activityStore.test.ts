import { describe, expect, it, beforeEach } from 'vitest'
import {
  __resetActivityStoreForTests,
  dismissActivity,
  dismissAllInFlight,
  getActivityEvents,
  pruneActivityFeed,
  updateActivityPhase,
  upsertActivity,
} from '@/lib/activity/activityStore'
import { ACTIVITY_INFLIGHT_STALE_MS } from '@/lib/activity/activityTypes'

describe('activityStore', () => {
  beforeEach(() => {
    __resetActivityStoreForTests()
  })

  it('upserts by id without spamming duplicates', () => {
    upsertActivity({
      id: 'actuation:test',
      kind: 'actuation',
      phase: 'requested',
      title: 'Restart api-monitor',
      bumpTs: true,
    })
    upsertActivity({
      id: 'actuation:test',
      kind: 'actuation',
      phase: 'applying',
      title: 'Restart api-monitor',
      detail: 'monitoring',
      bumpTs: true,
    })
    const events = getActivityEvents()
    expect(events).toHaveLength(1)
    expect(events[0].phase).toBe('applying')
    expect(events[0].detail).toBe('monitoring')
  })

  it('updateActivityPhase advances settle outcome', () => {
    upsertActivity({
      id: 'a1',
      kind: 'actuation',
      phase: 'applying',
      title: 'Gateway reconnect',
      bumpTs: true,
    })
    updateActivityPhase('a1', 'settled', {
      settledOutcome: 'resolved',
      detail: 'IB Socket is ok',
    })
    const ev = getActivityEvents()[0]
    expect(ev.phase).toBe('settled')
    expect(ev.settledOutcome).toBe('resolved')
  })

  it('persists events to sessionStorage and clears on reset', () => {
    upsertActivity({
      id: 'persist:1',
      kind: 'actuation',
      phase: 'applying',
      title: 'Restart daemon',
      bumpTs: true,
    })
    const raw = sessionStorage.getItem('bifrost.activity.events')
    expect(raw).toBeTruthy()
    const parsed = JSON.parse(raw!) as Array<{ id: string }>
    expect(parsed.some(e => e.id === 'persist:1')).toBe(true)

    __resetActivityStoreForTests()
    expect(sessionStorage.getItem('bifrost.activity.events')).toBeNull()
    expect(getActivityEvents()).toHaveLength(0)
  })

  it('dismissActivity removes a row from the feed', () => {
    upsertActivity({
      id: 'actuation:stuck',
      kind: 'actuation',
      phase: 'applying',
      title: 'Restart account-sync',
      bumpTs: true,
    })
    expect(dismissActivity('actuation:stuck')).toBe(true)
    expect(getActivityEvents()).toHaveLength(0)
  })

  it('dismissAllInFlight keeps terminal rows', () => {
    upsertActivity({
      id: 'a',
      kind: 'actuation',
      phase: 'applying',
      title: 'Restart a',
      bumpTs: true,
    })
    upsertActivity({
      id: 'b',
      kind: 'agent',
      phase: 'completed',
      title: 'Done',
      bumpTs: true,
    })
    expect(dismissAllInFlight()).toBe(1)
    expect(getActivityEvents().map(e => e.id)).toEqual(['b'])
  })

  it('upsert drops already-stale in-flight; pruneActivityFeed ages live ones', () => {
    upsertActivity({
      id: 'already-stale',
      kind: 'actuation',
      phase: 'applying',
      title: 'Restart account-sync',
      ts: Date.now() - ACTIVITY_INFLIGHT_STALE_MS - 1_000,
    })
    expect(getActivityEvents()).toHaveLength(0)

    upsertActivity({
      id: 'fresh',
      kind: 'actuation',
      phase: 'applying',
      title: 'Restart account-sync',
      bumpTs: true,
    })
    expect(getActivityEvents()).toHaveLength(1)
    expect(pruneActivityFeed(Date.now() + ACTIVITY_INFLIGHT_STALE_MS + 1_000)).toBe(true)
    expect(getActivityEvents()).toHaveLength(0)
  })
})
