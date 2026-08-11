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
  if (activeIdx >= 0) {
    return {
      kind: 'in_progress',
      label: 'In progress',
      detail: `${steps[activeIdx].label} · ${doneCount}/${steps.length} done`,
    }
  }
  if (doneCount === steps.length && steps.length > 0) {
    return {
      kind: 'released',
      label: 'Published',
      detail: 'Detect → Live check complete — lane published',
    }
  }
  if (doneCount === 0) {
    return { kind: 'idle', label: 'Not started', detail: 'No stage completed yet' }
  }
  const next = steps.find(s => s.status === 'pending')
  return {
    kind: 'in_progress',
    label: 'In progress',
    detail: `${doneCount}/${steps.length} done${next ? ` · ${next.label} next` : ''}`,
  }
}
