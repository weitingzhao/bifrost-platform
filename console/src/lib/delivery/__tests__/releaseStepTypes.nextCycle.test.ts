import { describe, expect, it } from 'vitest'
import type { DeliveryPipelineRunView } from '@/api/deliveryTypes'
import { pickNextCycleDeployRun } from '@/lib/delivery/releaseStepTypes'

function run(partial: Partial<DeliveryPipelineRunView> & Pick<DeliveryPipelineRunView, 'name' | 'status'>): DeliveryPipelineRunView {
  return {
    revision: 'main',
    start_time: '2026-08-10T00:00:00Z',
    completion_time: null,
    ...partial,
  }
}

describe('pickNextCycleDeployRun', () => {
  it('ignores a stale failed latest run that matches the baseline', () => {
    const runs = [
      run({ name: 'bifrost-deliver-platform-old-fail', status: 'Failed' }),
      run({ name: 'bifrost-deliver-platform-ok', status: 'Succeeded' }),
    ]
    expect(pickNextCycleDeployRun(runs, 'bifrost-deliver-platform-old-fail')).toBeUndefined()
  })

  it('returns a running run even when it matches the baseline name', () => {
    const runs = [run({ name: 'same', status: 'Running', reason: 'Running' })]
    // Baseline match means "still the old run" — wait for a new PipelineRun name.
    expect(pickNextCycleDeployRun(runs, 'same')).toBeUndefined()
  })

  it('returns a newer run that differs from the baseline', () => {
    const runs = [
      run({ name: 'bifrost-deliver-platform-new', status: 'Failed' }),
      run({ name: 'bifrost-deliver-platform-old-fail', status: 'Failed' }),
    ]
    expect(pickNextCycleDeployRun(runs, 'bifrost-deliver-platform-old-fail')?.name).toBe(
      'bifrost-deliver-platform-new',
    )
  })
})
