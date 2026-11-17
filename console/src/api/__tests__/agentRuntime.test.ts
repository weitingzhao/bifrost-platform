import { describe, expect, it } from 'vitest'
import progress from '../__fixtures__/agentrt-progress.stg.json'
import task from '../__fixtures__/agentrt-task.stg.json'
import mission from '../__fixtures__/agentrt-mission.stg.json'
import { parseRuntimeProgress, parseRuntimeTask } from '../agentRuntime'

describe('agent runtime fixtures', () => {
  it('retains missions, tasks, hosts, counts and the publication time', () => {
    const parsed = parseRuntimeProgress(progress)
    expect(parsed.work.missions).toHaveLength(5)
    expect(parsed.hosts).toHaveLength(2)
    expect(parsed.hosts[1]).toMatchObject({ host_id: 'mbp-dev', online: true, poll_age_s: 0 })
    expect(parsed.needs_you).toEqual({ count: 0, items: [], truncated: false })
    expect(parsed.generated_at).toBe(progress.generated_at)
    expect(parsed.as_of_event_id).toBe(800)
    const mission = parsed.work.missions.find(m => m.mission_id === 'M-2')!
    expect(mission.tasks).toHaveLength(2)
    expect(mission.tasks[0]).toMatchObject({ role: 'implement', round_no: 2, max_rounds: 2, state: 'verifying' })
    expect(parsed.recent.tasks).toHaveLength(20)
    expect(parsed.controls).toHaveLength(5)
  })
  it('reads the companion task and mission fixtures defensively', () => {
    expect(parseRuntimeTask(task)).toMatchObject({ task_id: 't_01M50M1CZSDN8PNHRH8C2241RS', role: 'implement', state: 'accepted', round_no: 1, max_rounds: 2 })
    expect(parseRuntimeProgress({ work: { missions: [mission] } }).work.missions[0]).toMatchObject({ mission_id: 'M-2', state: 'active', tasks: [] })
  })
  it.each([null, undefined, false, 7, 'new', [], {}, { hosts: [null, 2], work: { missions: [{ tasks: [null, {}], eta: false }] }, live: [{}] }])('handles missing or unfamiliar JSON: %j', value => {
    expect(() => parseRuntimeProgress(value)).not.toThrow()
  })
})
