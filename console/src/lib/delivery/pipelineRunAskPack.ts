import type { DeliveryPipelineRunView } from '@/api/deliveryTypes'

/**
 * Context that customizes the Ask-AI pack for a given deliver pipeline.
 * Defaults target the Trade STG rollout; platform deliver supplies its own.
 */
export interface PipelineRunAskContext {
  /** One-line task framing in the "## Task" section. */
  task: string
  /** "## <title> milestones (reference)" — optional reference checklist. */
  milestonesTitle?: string
  milestones?: string
  /** Pipeline tasks in typical order (one bullet each). */
  pipelineTitle: string
  pipelineOrder: string[]
  /** Cluster signals to inspect (one bullet each). */
  clusterSignals: string[]
  /** Closing acceptance hint for "what I need from you" #4. */
  acceptanceHint: string
}

export function isPipelineRunSucceeded(run: DeliveryPipelineRunView): boolean {
  const status = run.status.toLowerCase()
  const reason = (run.reason ?? '').toLowerCase()
  if (reason === 'running' || reason === 'pending') return false
  if (status === 'true') return true
  if (reason === 'succeeded' || reason === 'completed') return true
  return false
}

export function isPipelineRunFailed(run: DeliveryPipelineRunView): boolean {
  const status = run.status.toLowerCase()
  const reason = (run.reason ?? '').toLowerCase()
  if (status === 'false') return true
  if (reason === 'failed' || reason === 'pipelinerunfailed') return true
  return false
}

export function isPipelineRunRunning(run: DeliveryPipelineRunView): boolean {
  if (isPipelineRunSucceeded(run) || isPipelineRunFailed(run)) return false
  const reason = (run.reason ?? '').toLowerCase()
  if (reason === 'running' || reason === 'pending') return true
  const status = run.status.toLowerCase()
  if (status === 'unknown') return true
  return run.completion_time == null || run.completion_time === ''
}

export function formatPipelineRunStatus(run: DeliveryPipelineRunView): string {
  if (isPipelineRunSucceeded(run)) return run.reason != null && run.reason !== '' ? run.reason : 'Succeeded'
  if (isPipelineRunFailed(run)) return run.reason != null && run.reason !== '' ? run.reason : 'Failed'
  const reason = (run.reason ?? '').trim()
  if (reason !== '') return reason
  const status = run.status.toLowerCase()
  if (status === 'unknown') return 'Running'
  if (status === 'true') return 'Succeeded'
  if (status === 'false') return 'Failed'
  return run.status
}

/** Explain kubectl rollout status lines that look like a hang but are normal progress output. */
/** Log hint for a Failed bifrost-deliver-research run (build-only runs Succeed, so every Failed run is real). */
export function researchRunLogHint(
  logs: string | undefined,
): { tone: 'info' | 'warning'; message: string } {
  const text = logs ?? ''
  if (/has not applied the manifest pinning/i.test(text)) {
    return {
      tone: 'warning',
      message:
        'rollout-research timed out waiting for Argo CD to apply the pinned tag. Check that the pin commit reached main and that Application bifrost-research synced.',
    }
  }
  if (/image was pushed/i.test(text)) {
    return {
      tone: 'info',
      message:
        'Pre pin-check run: Kaniko pushed the image and verify-research failed only because the manifest was not pinned yet.',
    }
  }
  if (/not running the tag this run built/i.test(text)) {
    return {
      tone: 'warning',
      message:
        'verify-research: the manifest pins this tag but the Deployment is not running it. Check Argo CD sync of bifrost-research.',
    }
  }
  return {
    tone: 'warning',
    message:
      'Research run failed. Build-only runs (manifest not yet pinned) succeed, so this is a real failure — open the failed phase logs.',
  }
}

export type PipelineRunSortKey = 'status' | 'started'
export type PipelineRunSortDir = 'asc' | 'desc'
