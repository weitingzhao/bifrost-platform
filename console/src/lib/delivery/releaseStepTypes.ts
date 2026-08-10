import type { DeliveryPipelineRunView, ReleaseGateResponse } from '@/api/deliveryTypes'
import {
  isPipelineRunFailed,
  isPipelineRunRunning,
  isPipelineRunSucceeded,
} from '@/lib/delivery/pipelineRunAskPack'

export type StepStatus = 'done' | 'active' | 'pending' | 'error'

export interface FlowStep {
  key: string
  label: string
  env: 'STG' | 'PROD'
  status: StepStatus
  statusLabel: string
}

export interface ReleaseOutcome {
  kind: 'released' | 'in_progress' | 'failed' | 'idle'
  label: string
  detail: string
}

export interface ReleaseIdentity {
  revision: string | null
  hint: string
  mismatch: boolean
}

export function runStepStatus(run: DeliveryPipelineRunView | undefined): { status: StepStatus; label: string } {
  if (run == null) return { status: 'pending', label: 'Not started' }
  if (isPipelineRunSucceeded(run)) return { status: 'done', label: 'Deployed' }
  if (isPipelineRunRunning(run)) return { status: 'active', label: 'Running…' }
  if (isPipelineRunFailed(run)) return { status: 'error', label: 'Failed' }
  return { status: 'pending', label: 'Pending' }
}

export type DeployRunPickHints = {
  /** STG/PROD release gate passed — STG is accepted for this revision. */
  gatePassed?: boolean
  /** Automated smoke / verify-stg reachability OK. */
  smokeOk?: boolean
}

/**
 * Pick the PipelineRun that best represents deploy status for summary badges.
 * API returns runs newest-first; a failed retry after a successful deliver should not
 * override Gate + smoke acceptance signals.
 */
export function pickDeployPipelineRun(
  runs: DeliveryPipelineRunView[] | undefined,
  hints?: DeployRunPickHints,
): DeliveryPipelineRunView | undefined {
  if (runs == null || runs.length === 0) return undefined
  const latest = runs[0]
  if (isPipelineRunSucceeded(latest) || isPipelineRunRunning(latest)) return latest

  const accepted = hints?.gatePassed === true || hints?.smokeOk === true
  if (accepted) {
    const lastSuccess = runs.find(r => isPipelineRunSucceeded(r))
    if (lastSuccess != null) return lastSuccess
  }
  return latest
}

/** True when newest run failed but an older succeeded run is shown (retry after acceptance). */
export function deployRunRetryFailed(
  runs: DeliveryPipelineRunView[] | undefined,
  displayRun: DeliveryPipelineRunView | undefined,
): boolean {
  if (runs == null || runs.length === 0 || displayRun == null) return false
  const latest = runs[0]
  return latest !== displayRun && isPipelineRunFailed(latest)
}

export function gateStepStatus(gate: ReleaseGateResponse | undefined): { status: StepStatus; label: string } {
  const result = gate?.result ?? ''
  if (result === 'pass') return { status: 'done', label: 'Passed' }
  if (result === 'fail') return { status: 'error', label: 'Failed' }
  return { status: 'pending', label: 'Not run' }
}

export function deriveReleaseIdentity(
  stgRun: DeliveryPipelineRunView | undefined,
  prodRun: DeliveryPipelineRunView | undefined,
  stgGate: ReleaseGateResponse | undefined,
  prodGate: ReleaseGateResponse | undefined,
): ReleaseIdentity {
  const stgRev = stgRun?.revision?.trim() || stgGate?.revision?.trim() || ''
  const prodRev = prodRun?.revision?.trim() || prodGate?.revision?.trim() || ''

  if (stgRev && prodRev && stgRev !== prodRev) {
    return {
      revision: stgRev,
      hint: `PROD is on ${prodRev} — promote ${stgRev} or re-deploy PROD`,
      mismatch: true,
    }
  }
  if (stgRev) {
    return {
      revision: stgRev,
      hint: prodRev ? 'Same revision across STG and PROD' : 'Release pipeline based on this revision',
      mismatch: false,
    }
  }
  if (prodRev) {
    return { revision: prodRev, hint: 'Production revision (no STG deploy recorded)', mismatch: false }
  }
  return {
    revision: null,
    hint: 'Pick a revision in Staging Deploy to start a release',
    mismatch: false,
  }
}

export function deriveReleaseOutcome(steps: FlowStep[]): ReleaseOutcome {
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
      detail: `${steps[activeIdx].label} running · ${doneCount}/${steps.length} done`,
    }
  }
  if (doneCount === steps.length) {
    return { kind: 'released', label: 'Released', detail: 'All stages passed — release complete' }
  }
  if (doneCount === 0) {
    return { kind: 'idle', label: 'Not started', detail: 'No stage completed yet' }
  }
  const nextPending = steps.find(s => s.status === 'pending')
  return {
    kind: 'in_progress',
    label: 'In progress',
    detail: `${doneCount}/${steps.length} done${nextPending ? ` · ${nextPending.label} next` : ''}`,
  }
}

/** True when the 4-step lane is in a finished Released cycle (not mid next-cycle). */
export function isReleaseCycleTerminal(
  outcome: ReleaseOutcome,
  nextCycleStarted: boolean,
): boolean {
  return outcome.kind === 'released' && !nextCycleStarted
}

export function stepRevisionForIndex(
  index: number,
  stgRun: DeliveryPipelineRunView | undefined,
  prodRun: DeliveryPipelineRunView | undefined,
  stgGate: ReleaseGateResponse | undefined,
  prodGate: ReleaseGateResponse | undefined,
): string | undefined {
  switch (index) {
    case 0:
      return stgRun?.revision?.trim() || undefined
    case 1:
      return stgGate?.revision?.trim() || stgRun?.revision?.trim() || undefined
    case 2:
      return prodRun?.revision?.trim() || undefined
    default:
      return prodGate?.revision?.trim() || prodRun?.revision?.trim() || undefined
  }
}
