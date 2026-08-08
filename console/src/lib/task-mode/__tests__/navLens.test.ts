import { describe, expect, it } from 'vitest'
import type { ProgramDetailResponse, ProgramSummary } from '@/api/programsTypes'
import { resolveTaskPhaseStatus } from '@/lib/task-mode/navLens'

function detail(partial: Partial<ProgramSummary>): ProgramDetailResponse {
  return {
    program: {
      id: partial.id ?? 'p',
      title: 'P',
      description: '',
      status: 'active',
      phase_count: partial.phase_count ?? 9,
      phases_done: partial.phases_done ?? 9,
      all_phases_done: true,
      active: false,
      ...partial,
    },
    phases: [],
    active: false,
  }
}

describe('resolveTaskPhaseStatus build playbook', () => {
  it('does not mark deliver-stg done when catalog is not closed (req_pc)', () => {
    const status = resolveTaskPhaseStatus('build', 'deliver-stg', {
      programDetail: detail({
        complete: true,
        signed: 9,
        sign_off_required_count: 9,
        requires_post_completion: true,
      }),
    })
    expect(status).not.toBe('done')
  })

  it('does not mark sign-off done when gates are unsigned', () => {
    const status = resolveTaskPhaseStatus('build', 'sign-off', {
      programDetail: detail({
        complete: false,
        signed: 0,
        sign_off_required_count: 9,
        phases_done: 9,
        requires_post_completion: true,
      }),
      briefingOpened: true,
      devAgentPhaseDone: () => true,
    })
    expect(status).not.toBe('done')
  })
})
