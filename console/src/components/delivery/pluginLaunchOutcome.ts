import type { StepStatus } from '@/lib/delivery/releaseStepTypes'

export type PluginFlowStep = {
  key: string
  label: string
  status: StepStatus
  statusLabel: string
}

export type PluginLaunchOutcome = {
  kind: 'released' | 'in_progress' | 'failed' | 'idle'
  label: string
  detail: string
}

/** Ambient probe Ready on Detect only — not a publish cycle in flight. */
export function isAmbientReadyIdle(steps: PluginFlowStep[]): boolean {
  if (steps.length === 0) return false
  const [detect, ...rest] = steps
  if (detect?.key !== 'detect') return false
  if (detect.status !== 'done' || detect.statusLabel !== 'Ready') return false
  return rest.every(s => s.status === 'pending')
}

export function derivePluginLaunchOutcome(steps: PluginFlowStep[]): PluginLaunchOutcome {
  const doneCount = steps.filter(s => s.status === 'done').length
  const failedIdx = steps.findIndex(s => s.status === 'error')
  const activeIdx = steps.findIndex(s => s.status === 'active')

  if (failedIdx >= 0) {
    return {
      kind: 'failed',
      label: 'Failed',
      detail: `${steps[failedIdx].label} failed`,
    }
  }
  if (doneCount === steps.length && steps.length > 0) {
    return {
      kind: 'released',
      label: 'Published',
      detail: 'Detect → Live check complete — lane published',
    }
  }
  if (activeIdx >= 0) {
    return {
      kind: 'in_progress',
      label: 'In progress',
      detail: `${steps[activeIdx].label} · ${doneCount}/${steps.length} done`,
    }
  }
  if (doneCount === 0) {
    return { kind: 'idle', label: 'Not started', detail: 'No stage completed yet' }
  }
  if (isAmbientReadyIdle(steps)) {
    return {
      kind: 'idle',
      label: 'Ready',
      detail: 'Probe OK · ready for next publish',
    }
  }
  const next = steps.find(s => s.status === 'pending')
  return {
    kind: 'in_progress',
    label: 'In progress',
    detail: `${doneCount}/${steps.length} done${next ? ` · ${next.label} next` : ''}`,
  }
}

/** True when the 5-step lane finished this publish cycle (not mid next-cycle). */
export function isPluginLaunchCycleTerminal(outcome: PluginLaunchOutcome): boolean {
  return outcome.kind === 'released'
}
