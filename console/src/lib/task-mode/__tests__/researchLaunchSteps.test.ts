import { describe, expect, it } from 'vitest'
import type { DeliveryPipelineRunView, PipelinePhaseView } from '@/api/deliveryTypes'
import { deriveResearchLaunchSteps } from '@/lib/task-mode/researchLaunchSteps'

function run(partial: Partial<DeliveryPipelineRunView>): DeliveryPipelineRunView {
  return {
    name: 'bifrost-deliver-research-1',
    namespace: 'cicd',
    pipeline: 'bifrost-deliver-research',
    status: 'Unknown',
    ...partial,
  }
}

function phases(map: Record<string, string>): PipelinePhaseView[] {
  return Object.entries(map).map(([id, status]) => ({ id, label: id, status }))
}

describe('deriveResearchLaunchSteps', () => {
  it('is idle when no run has started', () => {
    const steps = deriveResearchLaunchSteps({ desiredTag: '0.48.4' })
    expect(steps.map(s => s.key)).toEqual(['build', 'verify', 'pin', 'live'])
    expect(steps.every(s => s.status === 'pending')).toBe(true)
  })

  it('marks Build active while Kaniko is running', () => {
    const steps = deriveResearchLaunchSteps({
      desiredTag: '0.48.4',
      run: run({ status: 'Unknown', reason: 'Running' }),
      phases: phases({ mirror: 'succeeded', clone: 'succeeded', build: 'running' }),
    })
    expect(steps[0].status).toBe('active')
    expect(steps[1].status).toBe('pending')
  })

  it('treats verify-fail after a successful build as image-landed', () => {
    const steps = deriveResearchLaunchSteps({
      desiredTag: '0.48.4',
      run: run({ status: 'False', reason: 'Failed' }),
      phases: phases({
        mirror: 'succeeded',
        clone: 'succeeded',
        build: 'succeeded',
        rollout: 'succeeded',
        verify: 'failed',
      }),
    })
    expect(steps[0].status).toBe('done')
    expect(steps[1].status).toBe('done')
    expect(steps[1].statusLabel).toMatch(/pin next/i)
    expect(steps[2].status).toBe('active')
  })

  it('treats a failed run with image-landed log hint as pin-next (no Trade phases)', () => {
    const steps = deriveResearchLaunchSteps({
      desiredTag: '0.48.4',
      run: run({ status: 'False', reason: 'Failed' }),
      phases: phases({ clone: 'pending', prepare: 'pending', build: 'pending' }),
      imageLandedHint: true,
    })
    expect(steps[0].status).toBe('done')
    expect(steps[1].status).toBe('done')
    expect(steps[1].statusLabel).toMatch(/pin next/i)
    expect(steps[2].status).toBe('active')
  })

  it('completes Pin + Live when research-api already serves the tag', () => {
    const steps = deriveResearchLaunchSteps({
      desiredTag: '0.48.4',
      run: run({ status: 'True', reason: 'Succeeded' }),
      liveVersion: '0.48.4',
      liveOk: true,
    })
    expect(steps.every(s => s.status === 'done')).toBe(true)
  })
})
