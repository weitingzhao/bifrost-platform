import type { DeliveryPipelineRunView, PipelinePhaseView } from '@/api/deliveryTypes'
import type { PluginFlowStep } from '@/components/delivery/pluginLaunchOutcome'
import {
  isPipelineRunFailed,
  isPipelineRunRunning,
  isPipelineRunSucceeded,
} from '@/lib/delivery/pipelineRunAskPack'

export const RESEARCH_CYCLE_STEPS = ['build', 'verify', 'pin', 'live'] as const
export type ResearchCycleStepKey = (typeof RESEARCH_CYCLE_STEPS)[number]

export const RESEARCH_STEP_LABELS: Record<ResearchCycleStepKey, string> = {
  build: 'Build',
  verify: 'Verify image',
  pin: 'Pin manifest',
  live: 'Live check',
}

function phaseStatus(phases: PipelinePhaseView[] | undefined, id: string): string | undefined {
  return phases?.find(p => p.id === id)?.status
}

/** Old platform-api mapped research runs onto Trade STG phases (prepare + 7-way clone). */
function researchPhases(phases?: PipelinePhaseView[]): PipelinePhaseView[] | undefined {
  if (phases == null || phases.length === 0) return undefined
  if (phases.some(p => p.id === 'prepare')) return undefined
  return phases
}

export function isResearchImageLandedLog(text: string | undefined): boolean {
  if (text == null || text === '') return false
  return /image was pushed|not running the tag this run built/i.test(text)
}

function step(
  key: ResearchCycleStepKey,
  status: PluginFlowStep['status'],
  statusLabel: string,
): PluginFlowStep {
  return { key, label: RESEARCH_STEP_LABELS[key], status, statusLabel }
}

/**
 * Owner-facing Research cycle: Build → Verify image → Pin → Live.
 * Tekton verify-research failing because the Deployment still pins the previous
 * tag is the expected first-pass outcome (image is in the registry).
 */
export function deriveResearchLaunchSteps(input: {
  run?: DeliveryPipelineRunView
  phases?: PipelinePhaseView[]
  desiredTag: string
  liveVersion?: string | null
  liveOk?: boolean
  agentInFlight?: boolean
  /** Log / API hint: Kaniko pushed the tag; verify failed because k8s is still unpinned. */
  imageLandedHint?: boolean
}): PluginFlowStep[] {
  const tag = input.desiredTag.trim()
  const pinned = input.liveVersion != null && input.liveVersion !== '' && input.liveVersion === tag
  const liveOk = input.liveOk === true && pinned
  const phases = researchPhases(input.phases)

  const buildPhase = phaseStatus(phases, 'build')
  const verifyPhase = phaseStatus(phases, 'verify')
  const running = input.run != null && isPipelineRunRunning(input.run)
  const succeeded = input.run != null && isPipelineRunSucceeded(input.run)
  const failed = input.run != null && isPipelineRunFailed(input.run)
  const paramMissing = input.run?.reason === 'ParameterMissing'
  const imageLanded = input.imageLandedHint === true

  const kanikoDone = buildPhase === 'succeeded' || succeeded || imageLanded
  const verifyExpectedFail =
    (verifyPhase === 'failed' && kanikoDone && !succeeded) || (imageLanded && failed && !succeeded)
  const verifyDone = verifyPhase === 'succeeded' || succeeded || verifyExpectedFail

  let build: PluginFlowStep
  if (paramMissing) {
    build = step('build', 'error', 'Parameter missing')
  } else if (running && !kanikoDone) {
    build = step('build', 'active', 'Building…')
  } else if (failed && !kanikoDone && !input.agentInFlight) {
    build = step('build', 'error', 'Build failed')
  } else if (kanikoDone) {
    build = step('build', 'done', `Built ${tag || 'image'}`)
  } else if (input.agentInFlight) {
    build = step('build', 'active', 'Awaiting agent')
  } else {
    build = step('build', 'pending', 'Not started')
  }

  let verify: PluginFlowStep
  if (build.status === 'pending' || build.status === 'error') {
    verify = step('verify', 'pending', 'Waits for build')
  } else if (running && kanikoDone && !verifyDone) {
    verify = step('verify', 'active', 'Asserting tag…')
  } else if (verifyExpectedFail) {
    verify = step('verify', 'done', 'Image landed — pin next')
  } else if (verifyDone) {
    verify = step('verify', 'done', 'Tag asserted')
  } else if (build.status === 'active') {
    verify = step('verify', 'pending', 'Waits for build')
  } else {
    verify = step('verify', 'pending', 'Not started')
  }

  const pin = liveOk
    ? step('pin', 'done', `Pinned ${tag}`)
    : verify.status === 'done'
      ? step('pin', 'active', 'Bump k8s after registry confirm')
      : step('pin', 'pending', 'Waits for image')

  const live = liveOk
    ? step('live', 'done', `research-api ${tag}`)
    : pin.status === 'done' || pin.status === 'active'
      ? step('live', pinned ? 'active' : 'pending', pinned ? 'Health check…' : 'Waits for pin')
      : step('live', 'pending', 'Waits for pin')

  return [build, verify, pin, live]
}
