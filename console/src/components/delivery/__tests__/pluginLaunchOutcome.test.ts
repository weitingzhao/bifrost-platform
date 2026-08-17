import { describe, expect, it } from 'vitest'
import {
  derivePluginLaunchOutcome,
  isAmbientReadyIdle,
  isPluginLaunchCycleTerminal,
  type PluginFlowStep,
} from '@/components/delivery/pluginLaunchOutcome'

function step(
  key: string,
  status: PluginFlowStep['status'],
  statusLabel: string,
  label = key,
): PluginFlowStep {
  return { key, label, status, statusLabel }
}

describe('derivePluginLaunchOutcome', () => {
  it('treats ambient Detect Ready + rest pending as Ready idle (not In progress)', () => {
    const steps = [
      step('detect', 'done', 'Ready', 'Detect'),
      step('approve', 'pending', 'Not started', 'Approve'),
      step('install', 'pending', 'Not started', 'Install'),
      step('verify', 'pending', 'Not started', 'Verify'),
      step('live-check', 'pending', 'Not started', 'Live check'),
    ]
    expect(isAmbientReadyIdle(steps)).toBe(true)
    const outcome = derivePluginLaunchOutcome(steps)
    expect(outcome.kind).toBe('idle')
    expect(outcome.label).toBe('Ready')
    expect(isPluginLaunchCycleTerminal(outcome)).toBe(false)
  })

  it('marks cycle Detect Probed + Approve active as In progress', () => {
    const steps = [
      step('detect', 'done', 'Probed', 'Detect'),
      step('approve', 'active', 'Awaiting approval', 'Approve'),
      step('install', 'pending', 'Not started', 'Install'),
      step('verify', 'pending', 'Not started', 'Verify'),
      step('live-check', 'pending', 'Not started', 'Live check'),
    ]
    expect(isAmbientReadyIdle(steps)).toBe(false)
    const outcome = derivePluginLaunchOutcome(steps)
    expect(outcome.kind).toBe('in_progress')
    expect(outcome.label).toBe('In progress')
    expect(outcome.detail).toContain('Approve')
  })

  it('marks all five done as Published terminal', () => {
    const steps = [
      step('detect', 'done', 'Probed', 'Detect'),
      step('approve', 'done', 'Approved', 'Approve'),
      step('install', 'done', 'Done', 'Install'),
      step('verify', 'done', 'Done', 'Verify'),
      step('live-check', 'done', 'Done', 'Live check'),
    ]
    const outcome = derivePluginLaunchOutcome(steps)
    expect(outcome.kind).toBe('released')
    expect(outcome.label).toBe('Published')
    expect(isPluginLaunchCycleTerminal(outcome)).toBe(true)
  })

  it('keeps failed ahead of Published', () => {
    const steps = [
      step('detect', 'done', 'Probed', 'Detect'),
      step('approve', 'done', 'Approved', 'Approve'),
      step('install', 'error', 'Failed', 'Install'),
      step('verify', 'pending', 'Not started', 'Verify'),
      step('live-check', 'pending', 'Not started', 'Live check'),
    ]
    expect(derivePluginLaunchOutcome(steps).kind).toBe('failed')
  })
})
