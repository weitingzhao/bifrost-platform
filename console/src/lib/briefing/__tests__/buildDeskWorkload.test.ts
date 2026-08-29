import { describe, expect, it } from 'vitest'
import type { ProgramSummary } from '@/api/programsTypes'
import {
  computeBuildDeskWorkloadCounts,
  countOpenBoardPrograms,
} from '@/lib/briefing/buildDeskWorkload'

function program(partial: Partial<ProgramSummary> & { id: string }): ProgramSummary {
  return {
    title: partial.id,
    description: '',
    status: 'active',
    phase_count: 1,
    phases_done: 0,
    all_phases_done: false,
    active: true,
    ...partial,
  }
}

describe('countOpenBoardPrograms', () => {
  it('counts only not-sessionReleased programs', () => {
    const open = program({
      id: 'trade-iv-radar',
      signed: 2,
      sign_off_required_count: 3,
      complete: false,
    })
    const released = program({
      id: 'done-prog',
      signed: 2,
      sign_off_required_count: 2,
      complete: true,
      phases_done: 2,
      phase_count: 2,
    })
    expect(countOpenBoardPrograms([open, released])).toBe(1)
  })
})

describe('computeBuildDeskWorkloadCounts', () => {
  it('returns briefing count from programs even before matrix loads', () => {
    const counts = computeBuildDeskWorkloadCounts({
      programs: [
        program({
          id: 'trade-iv-radar',
          signed: 1,
          sign_off_required_count: 3,
        }),
      ],
      context: undefined,
      matrices: [],
      clusterSummary: undefined,
      releasedByLane: undefined,
      programsReady: false,
    })
    expect(counts.briefing).toBe(1)
    expect(counts.activeSession).toBe(0)
  })
})
