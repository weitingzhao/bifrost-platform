import { describe, expect, it } from 'vitest'
import type { ProgramPhaseDetail, ProgramSummary } from '@/api/programsTypes'
import { laneLifecycleFromQueue } from '@/lib/briefing/briefingStatus'
import {
  mapPhaseToQueueStatus,
  projectPhasesToQueue,
  projectQueueFromOpenPrograms,
} from '@/lib/briefing/projectProgramQueue'

function phase(partial: Partial<ProgramPhaseDetail> & { id: string }): ProgramPhaseDetail {
  return {
    title: partial.id,
    status: 'pending',
    ...partial,
  }
}

function program(partial: Partial<ProgramSummary> & { id: string }): ProgramSummary {
  return {
    title: partial.id,
    description: '',
    status: 'active',
    phase_count: partial.phases?.length ?? 0,
    phases_done: 0,
    all_phases_done: false,
    active: true,
    ...partial,
  }
}

describe('mapPhaseToQueueStatus', () => {
  it('marks signed gates as done', () => {
    expect(
      mapPhaseToQueueStatus(
        phase({ id: 'P1', signed_off: true, status: 'done', sign_off: { required: true } }),
        new Set(),
      ),
    ).toBe('done')
  })

  it('marks done gate awaiting sign-off as ready_for_signoff', () => {
    expect(
      mapPhaseToQueueStatus(
        phase({ id: 'P3', status: 'done', sign_off: { required: true } }),
        new Set(['P1', 'P2']),
      ),
    ).toBe('ready_for_signoff')
  })

  it('marks non-gate done as done', () => {
    expect(
      mapPhaseToQueueStatus(
        phase({ id: 'P4', status: 'done', sign_off: { required: false } }),
        new Set(),
      ),
    ).toBe('done')
  })
})

describe('projectQueueFromOpenPrograms (trade-iv-radar fixture)', () => {
  const phases: ProgramPhaseDetail[] = [
    phase({
      id: 'P1',
      title: 'Research IA M1',
      status: 'done',
      signed_off: true,
      sign_off: { required: true },
    }),
    phase({
      id: 'P2',
      title: 'IV Radar data layer',
      status: 'done',
      signed_off: true,
      sign_off: { required: true },
    }),
    phase({
      id: 'P3',
      title: 'IV Radar page',
      status: 'done',
      signed_off: false,
      sign_off: { required: true },
      progress: { phase_id: 'P3', status: 'done', verify_passed: true, updated_at: '2026-08-29T00:00:00Z' },
    }),
    phase({
      id: 'P4',
      title: 'QA polish',
      status: 'done',
      signed_off: false,
      sign_off: { required: false },
    }),
  ]

  it('projects a non-empty queue with ready_for_signoff for unsigned gate', () => {
    const p = program({
      id: 'trade-iv-radar',
      lane_id: 'trade-iv-radar',
      phase_count: 4,
      phases_done: 4,
      signed: 2,
      sign_off_required_count: 3,
      complete: false,
      all_phases_done: true,
      phases,
    })
    const queue = projectQueueFromOpenPrograms('trade-iv-radar', [p])
    expect(queue).toHaveLength(4)
    expect(queue.find(q => q.id === 'P3')?.status).toBe('ready_for_signoff')
    expect(queue.find(q => q.id === 'P1')?.status).toBe('done')
    expect(queue.find(q => q.id === 'P4')?.status).toBe('done')
    expect(laneLifecycleFromQueue(queue, { programsReleased: false })).toBe('active')
  })

  it('returns empty when program is sessionReleased', () => {
    const p = program({
      id: 'trade-iv-radar',
      lane_id: 'trade-iv-radar',
      complete: true,
      signed: 3,
      sign_off_required_count: 3,
      phases_done: 4,
      all_phases_done: true,
      phases,
    })
    expect(projectQueueFromOpenPrograms('trade-iv-radar', [p])).toEqual([])
  })

  it('returns empty for wrong lane or missing phases', () => {
    const p = program({
      id: 'trade-iv-radar',
      lane_id: 'trade-iv-radar',
      signed: 1,
      sign_off_required_count: 3,
      phases,
    })
    expect(projectQueueFromOpenPrograms('other-lane', [p])).toEqual([])
    expect(
      projectQueueFromOpenPrograms('trade-iv-radar', [
        program({ id: 'x', lane_id: 'trade-iv-radar', signed: 1 }),
      ]),
    ).toEqual([])
  })

  it('projectPhasesToQueue keeps phase ids for ActiveSessionPhaseBoard join', () => {
    const queue = projectPhasesToQueue(phases)
    expect(queue.map(q => q.id)).toEqual(['P1', 'P2', 'P3', 'P4'])
  })

  it('leaves In Flight when projected queue has ready_for_signoff and not sessionReleased', () => {
    const p = program({
      id: 'trade-iv-radar',
      lane_id: 'trade-iv-radar',
      signed: 2,
      sign_off_required_count: 3,
      phases,
    })
    const queue = projectQueueFromOpenPrograms('trade-iv-radar', [p])
    expect(laneLifecycleFromQueue(queue, { programsReleased: false })).toBe('active')
  })

  it('leaves Doing after sessionReleased even if a projected queue snapshot still has ready_for_signoff', () => {
    // Owner signed all gates + sessionReleased; board no longer projects, but stale
    // queue (e.g. before invalidate) must still exit In Flight via programsReleased.
    const staleQueue = projectPhasesToQueue(phases)
    expect(staleQueue.some(q => q.status === 'ready_for_signoff')).toBe(true)
    expect(laneLifecycleFromQueue(staleQueue, { programsReleased: true })).toBe('complete')
  })

  it('projected all-done queue + programsReleased → complete', () => {
    const signedPhases = phases.map(p =>
      p.sign_off?.required === false ? p : { ...p, signed_off: true },
    )
    const queue = projectPhasesToQueue(signedPhases)
    expect(queue.every(q => q.status === 'done' || q.status === 'closed')).toBe(true)
    expect(laneLifecycleFromQueue(queue, { programsReleased: true })).toBe('complete')
    expect(laneLifecycleFromQueue(queue, { programsReleased: false })).toBe('active')
  })
})
