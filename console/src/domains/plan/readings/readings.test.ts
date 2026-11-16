import { describe, expect, it } from 'vitest'
import { parseRuntimeProgress } from '@/api/agentRuntime'
import type { PlanSnapshot } from '@/api/planSnapshot'
import { roundsRead } from './rounds'
import { changeText, changeRef, clock } from './recentChanges'

describe('plan read projections outside the Work shell', () => {
  it('keeps an unread or failed plan distinct from an empty list', () => {
    const read = { data: undefined, error: null, isError: false, isPaused: false, dataUpdatedAt: 0 }
    expect(roundsRead(read)).toEqual({ state: 'reading' })
    expect(roundsRead({ ...read, isError: true, error: new Error('offline') })).toEqual({ state: 'unknown', reason: 'offline' })
  })
  it('orders numbered rounds and preserves the last read failure', () => {
    const data = parseRuntimeProgress({ plan: [{ kind: 'round', plan_node_id: 'R10', title: 'ten' }, { kind: 'round', plan_node_id: 'R2', title: 'two' }] })
    expect(roundsRead({ data, error: new Error('refresh failed'), isError: true, isPaused: false, dataUpdatedAt: 123 })).toMatchObject({ state: 'ok', newest: 'R10', at: 123, failed: 'refresh failed', rounds: [{ id: 'R2', title: 'two' }, { id: 'R10', title: 'ten' }] })
  })
  it('preserves event text, fallback labels and invalid time without requiring a mounted shell', () => {
    const event = { event_id: 1, event_type: 'external_work', actor_id: 'a', at: 'invalid', body_json: { text: 'read observation', external_work: { source_run_key: 'run-1', plan_node_id: 'R2:1' } } } as PlanSnapshot['recent_changes'][number]
    const view = { nodes: [{ plan_node_id: 'R2:1', label: 'one' }] } as PlanSnapshot
    expect(changeText(event)).toBe('read observation')
    expect(changeRef(event, view)).toBe('run-1 · one')
    expect(changeText({ ...event, body_json: {} })).toBe('External work')
    expect(clock('invalid', 0)).toBe('invalid')
  })
})
