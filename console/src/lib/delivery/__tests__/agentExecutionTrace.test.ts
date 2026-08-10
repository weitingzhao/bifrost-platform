import { describe, expect, it } from 'vitest'
import type { DevAgentJob, DevAgentProgramJobsResponse, DevAgentStatusResponse } from '@/api/devAgentTypes'
import {
  collectTraceJobs,
  computeAgentExecutionKpis,
  formatDurationMs,
  formatRate,
  groupJobsByPhase,
  jobDurationMs,
} from '@/lib/delivery/agentExecutionTrace'

function job(partial: Partial<DevAgentJob> & Pick<DevAgentJob, 'id' | 'phase_id' | 'status'>): DevAgentJob {
  return {
    output: '',
    ...partial,
  }
}

function status(over: Partial<DevAgentStatusResponse> = {}): DevAgentStatusResponse {
  return {
    project: 'p',
    program: { id: 'prog-a', title: 'A', description: '', status: 'active' },
    phases: [],
    active_job: null,
    history: [],
    ...over,
  }
}

describe('collectTraceJobs', () => {
  it('puts active_job first and dedupes history', () => {
    const active = job({ id: 'j2', phase_id: 'p1', status: 'running', summary: 'live' })
    const older = job({ id: 'j1', phase_id: 'p1', status: 'done', summary: 'old' })
    const dup = job({ id: 'j2', phase_id: 'p1', status: 'done', summary: 'dup' })
    const ids = collectTraceJobs(
      status({ active_job: active, history: [older, dup] }),
    ).map(j => j.id)
    expect(ids).toEqual(['j2', 'j1'])
  })

  it('accepts per-program jobs response shape', () => {
    const payload: DevAgentProgramJobsResponse = {
      program_id: 'prog-a',
      active_job: job({ id: 'live', phase_id: 'p1', status: 'running' }),
      history: [job({ id: 'old', phase_id: 'p1', status: 'done' })],
    }
    expect(collectTraceJobs(payload).map(j => j.id)).toEqual(['live', 'old'])
  })
})

describe('groupJobsByPhase', () => {
  it('preserves first-seen phase order', () => {
    const groups = groupJobsByPhase([
      job({ id: 'a', phase_id: 'beta', status: 'done' }),
      job({ id: 'b', phase_id: 'alpha', status: 'done' }),
      job({ id: 'c', phase_id: 'beta', status: 'failed' }),
    ])
    expect(groups.map(g => g.phaseId)).toEqual(['beta', 'alpha'])
    expect(groups[0]?.jobs.map(j => j.id)).toEqual(['a', 'c'])
  })
})

describe('computeAgentExecutionKpis', () => {
  it('uses per-phase first terminal job for first_pass', () => {
    const kpis = computeAgentExecutionKpis([
      job({ id: 'a1', phase_id: 'p1', status: 'done' }),
      job({ id: 'a2', phase_id: 'p1', status: 'failed' }),
      job({ id: 'b1', phase_id: 'p2', status: 'failed' }),
      job({ id: 'c1', phase_id: 'p3', status: 'running' }),
    ])
    expect(kpis.totalRuns).toBe(4)
    expect(kpis.firstPassHeuristic).toBe('per-phase-first')
    expect(kpis.firstPassRate).toBe(0.5)
    expect(kpis.avgDurationMs).toBeNull()
  })

  it('falls back to done/(done+failed) when no terminal first jobs', () => {
    const kpis = computeAgentExecutionKpis([
      job({ id: 'a', phase_id: 'p1', status: 'cancelled' }),
      job({ id: 'a2', phase_id: 'p1', status: 'done' }),
      job({ id: 'b', phase_id: 'p2', status: 'running' }),
    ])
    expect(kpis.firstPassHeuristic).toBe('done-over-terminal')
    expect(kpis.firstPassRate).toBe(1)
  })

  it('averages duration only when started_at and completed_at parse', () => {
    const kpis = computeAgentExecutionKpis([
      job({
        id: 'a',
        phase_id: 'p1',
        status: 'done',
        started_at: '2026-08-09T12:00:00Z',
        completed_at: '2026-08-09T12:02:00Z',
      }),
      job({
        id: 'b',
        phase_id: 'p1',
        status: 'done',
        completed_at: '2026-08-09T12:10:00Z',
      }),
    ])
    expect(kpis.avgDurationMs).toBe(120_000)
  })
})

describe('format helpers', () => {
  it('formats duration and rate', () => {
    expect(formatDurationMs(4_000)).toBe('4s')
    expect(formatDurationMs(125_000)).toBe('2m 5s')
    expect(formatRate(0.75)).toBe('75%')
  })

  it('parses job duration only when both stamps are valid', () => {
    expect(
      jobDurationMs({
        started_at: '2026-08-09T12:00:00Z',
        completed_at: '2026-08-09T12:02:00Z',
      }),
    ).toBe(120_000)
    expect(jobDurationMs({ completed_at: '2026-08-09T12:02:00Z' })).toBeNull()
  })
})
