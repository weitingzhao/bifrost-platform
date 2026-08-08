import { describe, expect, it } from 'vitest'
import type { ProgramSummary } from '@/api/programsTypes'
import {
  pickBoardProgramForLane,
  programLaneCompatible,
  resolveDevProgramId,
} from '@/lib/task-mode/devProgramResolve'

function prog(partial: Partial<ProgramSummary> & { id: string }): ProgramSummary {
  return {
    title: partial.id,
    description: '',
    status: 'active',
    phase_count: 5,
    phases_done: 0,
    all_phases_done: false,
    active: false,
    complete: false,
    ...partial,
  }
}

describe('programLaneCompatible', () => {
  it('rejects empty program lane when session lane is set', () => {
    expect(programLaneCompatible(undefined, 'trade-stack')).toBe(false)
    expect(programLaneCompatible('', 'trade-stack')).toBe(false)
  })

  it('requires exact lane match', () => {
    expect(programLaneCompatible('trade-stack', 'trade-stack')).toBe(true)
    expect(programLaneCompatible('console-api', 'trade-stack')).toBe(false)
  })
})

describe('pickBoardProgramForLane', () => {
  const programs = [
    prog({ id: 'a--old', lane_id: 'console-api', active: false, complete: true }),
    prog({ id: 'a--active', lane_id: 'console-api', active: true, complete: false }),
    prog({ id: 'b--trade', lane_id: 'trade-stack', active: true, complete: false }),
    prog({ id: 'c--nolan' }),
  ]

  it('ignores preferred sessionReleased id when a live sibling exists', () => {
    const hit = pickBoardProgramForLane(programs, 'console-api', 'a--old')
    expect(hit?.id).toBe('a--active')
  })

  it('honors preferred id among live programs', () => {
    const extra = [
      ...programs,
      prog({ id: 'a--other', lane_id: 'console-api', active: false, complete: false }),
    ]
    expect(pickBoardProgramForLane(extra, 'console-api', 'a--other')?.id).toBe('a--other')
  })

  it('ignores preferred id on wrong lane and picks active', () => {
    const hit = pickBoardProgramForLane(programs, 'console-api', 'b--trade')
    expect(hit?.id).toBe('a--active')
  })

  it('returns undefined when no lane-compatible programs', () => {
    expect(pickBoardProgramForLane(programs, 'agent-infra')).toBeUndefined()
  })

  it('excludes programs without lane_id', () => {
    expect(pickBoardProgramForLane(programs, 'console-api', 'c--nolan')?.id).toBe('a--active')
  })

  it('falls back to historical when every sibling is sessionReleased', () => {
    const hist = [
      prog({ id: 'a--old', lane_id: 'console-api', active: false, complete: true }),
      prog({ id: 'a--older', lane_id: 'console-api', active: false, complete: true }),
    ]
    expect(pickBoardProgramForLane(hist, 'console-api')?.id).toBe('a--old')
  })
})

describe('resolveDevProgramId', () => {
  const board = [
    prog({ id: 'ctrl--build', lane_id: 'console-api', active: true }),
    prog({ id: 'trade--build', lane_id: 'trade-stack', active: true }),
  ]

  it('returns undefined without Active Session', () => {
    expect(
      resolveDevProgramId({
        hasActiveSession: false,
        activeLane: undefined,
        sessionProgramId: 'ctrl--build',
        boardPrograms: board,
        boardFetched: true,
        storedProgramId: 'ctrl--build',
      }),
    ).toBeUndefined()
  })

  it('uses session program when board confirms lane', () => {
    expect(
      resolveDevProgramId({
        hasActiveSession: true,
        activeLane: 'trade-stack',
        sessionProgramId: 'trade--build',
        boardPrograms: board,
        boardFetched: true,
        storedProgramId: 'ctrl--build',
      }),
    ).toBe('trade--build')
  })

  it('ignores session program when board proves wrong lane', () => {
    expect(
      resolveDevProgramId({
        hasActiveSession: true,
        activeLane: 'trade-stack',
        sessionProgramId: 'ctrl--build',
        boardPrograms: board,
        boardFetched: true,
        storedProgramId: null,
      }),
    ).toBe('trade--build')
  })

  it('waits for board before localStorage fallback', () => {
    expect(
      resolveDevProgramId({
        hasActiveSession: true,
        activeLane: 'console-api',
        sessionProgramId: undefined,
        boardPrograms: [],
        boardFetched: false,
        storedProgramId: 'ctrl--build',
      }),
    ).toBeUndefined()
  })

  it('falls back to board lane pick then stored', () => {
    expect(
      resolveDevProgramId({
        hasActiveSession: true,
        activeLane: 'console-api',
        sessionProgramId: undefined,
        boardPrograms: board,
        boardFetched: true,
        storedProgramId: null,
      }),
    ).toBe('ctrl--build')
  })

  it('does not bind a sessionReleased program when an open sibling exists', () => {
    const mixed = [
      prog({ id: 'hist', lane_id: 'console-api', complete: true }),
      prog({ id: 'live', lane_id: 'console-api', complete: false }),
    ]
    expect(
      resolveDevProgramId({
        hasActiveSession: true,
        activeLane: 'console-api',
        sessionProgramId: 'hist',
        boardPrograms: mixed,
        boardFetched: true,
        storedProgramId: null,
      }),
    ).toBe('live')
  })
})
