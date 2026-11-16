import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { QueryClient } from '@tanstack/react-query'
import { startRestartActuationSettle } from '../restartActuationSettle'
import { __resetActivityStoreForTests, getActivityEvents, upsertActivity } from '../activityStore'
import * as compatibility from '@/lib/activity/activityStore'

describe('read-only rollout observation lifecycle', () => {
  beforeEach(() => {
    vi.useFakeTimers()
    __resetActivityStoreForTests()
    upsertActivity({ id: 'test', kind: 'actuation', phase: 'requested', title: 'Observe' })
  })
  afterEach(() => { vi.clearAllTimers(); vi.useRealTimers() })

  it('uses the same feed through the compatibility entry', () => {
    expect(compatibility.getActivityEvents).toBe(getActivityEvents)
    expect(compatibility.getActivityEvents()[0].id).toBe('test')
  })

  it('observes progress and settles after a completed rollout', () => {
    const queryClient = new QueryClient()
    const key = ['cluster', 'workloads', 'example']
    const workload = { name: 'example', kind: 'Deployment', ready: '0/1', status: 'Progressing', generation: 2 }
    queryClient.setQueryData(key, { workloads: [workload] })
    startRestartActuationSettle({ activityId: 'test', queryClient, namespace: 'example', name: 'example', baselineReady: '1/1', baselineGeneration: 1 })
    expect(getActivityEvents()[0].phase).toBe('applying')
    vi.advanceTimersByTime(400)
    queryClient.setQueryData(key, { workloads: [{ ...workload, ready: '1/1', status: 'Ready', observed_generation: 2 }] })
    vi.advanceTimersByTime(1600)
    expect(getActivityEvents()[0].settledOutcome).toBe('resolved')
    expect(getActivityEvents()[0].phase).toBe('settled')
    queryClient.clear()
  })

  it('times out missing progress at the existing 90 second boundary', () => {
    const queryClient = new QueryClient()
    startRestartActuationSettle({ activityId: 'test', queryClient, namespace: 'example', name: 'example', baselineReady: '1/1' })
    vi.advanceTimersByTime(89_999)
    expect(getActivityEvents()[0].phase).toBe('applying')
    vi.advanceTimersByTime(1)
    expect(getActivityEvents()[0].settledOutcome).toBe('timeout')
    queryClient.clear()
  })

  it('stops interval and timeout observation when disposed after first tick', () => {
    const queryClient = new QueryClient()
    const invalidate = vi.spyOn(queryClient, 'invalidateQueries')
    const stop = startRestartActuationSettle({ activityId: 'test', queryClient, namespace: 'example', name: 'example' })
    vi.advanceTimersByTime(400)
    stop()
    const count = invalidate.mock.calls.length
    vi.advanceTimersByTime(90_000)
    expect(invalidate).toHaveBeenCalledTimes(count)
    expect(getActivityEvents()[0].phase).toBe('applying')
    queryClient.clear()
  })
})
