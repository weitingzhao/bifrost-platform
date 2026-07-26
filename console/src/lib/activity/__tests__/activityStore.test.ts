import { describe, expect, it, beforeEach } from 'vitest'
import {
  __resetActivityStoreForTests,
  getActivityEvents,
  updateActivityPhase,
  upsertActivity,
} from '@/lib/activity/activityStore'

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
})
