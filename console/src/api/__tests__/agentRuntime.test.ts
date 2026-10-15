import { describe, expect, it } from 'vitest'
import progress from '../__fixtures__/agentrt-progress.stg.json'
import events from '../__fixtures__/agentrt-events-M-2.stg.json'
import roadmap from '../__fixtures__/board-roadmap-round-arch.json'
import task from '../__fixtures__/agentrt-task.stg.json'
import mission from '../__fixtures__/agentrt-mission.stg.json'
import { parseRuntimeEvents, parseRuntimeProgress, parseGoalResult, parseRuntimeTask } from '../agentRuntime'
import { parseRoadmap } from '../agentProgressBoard'
import { parseAgentProgress } from '../agentProgress'

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
  it('retains the event order and artifact ids', () => {
    const parsed = parseRuntimeEvents(events)
    expect(parsed).toHaveLength(events.length)
    expect(parsed.map(e => e.at)).toEqual(events.map(e => e.at))
    const persisted = parsed.find(e => e.event_type === 'attempt_persisted')!
    expect(persisted.body_json.artifacts).toEqual(events.find(e => e.event_type === 'attempt_persisted')!.body_json.artifacts)
  })
  it('keeps the typed round and architecture on the board document', () => {
    const parsed = parseRoadmap(roadmap)!
    expect(parsed.round?.n).toBe(2)
    expect(parsed.round?.items).toHaveLength(7)
    expect(parsed.round?.items[0]).toMatchObject({ key: '2.0', done: 3, total: 3 })
    expect(parsed.round?.items[0].steps[0]).toMatchObject({ state: 'done', when: '10-13 23:00' })
    expect(parsed.arch?.rows).toHaveLength(17)
    expect(parsed.arch?.rows[0].state).toBe('part')
    expect(parsed.arch?.checked).toBe(1792106220)
    const response = parseAgentProgress({ document: { roadmap }, generated_at: 'now' })
    expect(response.document?.roadmap?.round).toEqual(parsed.round)
  })
  it.each([null, undefined, false, 7, 'new', [], {}, { hosts: [null, 2], work: { missions: [{ tasks: [null, {}], eta: false }] }, live: [{}] }])('handles missing or unfamiliar JSON: %j', value => {
    expect(() => parseRuntimeProgress(value)).not.toThrow()
    expect(() => parseRuntimeEvents(value)).not.toThrow()
    expect(() => parseGoalResult(value)).not.toThrow()
    expect(() => parseRoadmap(value)).not.toThrow()
  })
  it('does not accept invalid nested plan arrays or numeric strings', () => {
    expect(parseRoadmap({ round: { n: '2', items: [null, { done: '3', steps: [null, {}] }] }, arch: { rows: [null, {}] } }))
      .toMatchObject({ round: { n: undefined, items: [{ done: undefined, steps: [{ t: '' }] }] }, arch: { rows: [{ state: '' }] } })
  })
})
