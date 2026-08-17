/**
 * Map AI Launch Agent remediation job phase + events → step progress.
 * Aligns Launch Desk Step strip with Operator Dock (parity with Plugin).
 */

import type { RemediationEvent, RemediationJob, RemediationPhase } from '@/api/remediationTypes'
import type { AgentLaunchEvidence } from '@/lib/delivery/agentLaunchEvidence'

export type AgentLaunchStepKey =
  | 'detect'
  | 'approve'
  | 'deploy'
  | 'verify'
  | 'live-check'

export type AgentLaunchAgentProgress = {
  detectDone: boolean
  approveDone: boolean
  approveAwaiting: boolean
  deployOutcome?: 'ok' | 'failed' | 'pending'
  verifyAwaiting: boolean
  verifyOutcome?: 'ok' | 'failed' | 'pending'
  liveOutcome?: 'ok' | 'failed' | 'pending'
  focusStep: AgentLaunchStepKey
  failed: boolean
}

const EMPTY: AgentLaunchAgentProgress = {
  detectDone: false,
  approveDone: false,
  approveAwaiting: false,
  verifyAwaiting: false,
  focusStep: 'detect',
  failed: false,
}

function eventBlob(events: RemediationEvent[] | undefined): string {
  if (events == null || events.length === 0) return ''
  return events
    .map(e => {
      const meta = e.meta != null ? JSON.stringify(e.meta) : ''
      return `${e.type} ${e.text} ${meta}`
    })
    .join('\n')
    .toLowerCase()
}

function mentionsDeployDone(blob: string): boolean {
  return (
    /deploy(?:ment)?\s+(?:finished|complete|ok|done|succeeded)/.test(blob) ||
    /deploy_mac_mini[\s\S]{0,80}(?:ok|success|complete|finished)/.test(blob) ||
    /start_agent_host_deploy[\s\S]{0,80}(?:ok|success|done)/.test(blob) ||
    /host deploy\s+(?:done|ok|finished)/.test(blob) ||
    /rsync[\s\S]{0,40}launchctl[\s\S]{0,40}(?:ok|success)/.test(blob)
  )
}

function mentionsVerifyStage(blob: string): boolean {
  return (
    /verify[-_ ]?(?:阶段|stage)/.test(blob) ||
    /get_agent_bridge/.test(blob) ||
    /get_agent_deploy_status/.test(blob) ||
    /runner heartbeat/.test(blob) ||
    /recheck.*bridge/.test(blob)
  )
}

function mentionsLiveStage(blob: string): boolean {
  return (
    /live check/.test(blob) ||
    /heartbeat ok/.test(blob) ||
    /runner.*\bok\b/.test(blob) ||
    /detect→approve→deploy/.test(blob)
  )
}

/**
 * Infer Detect→Live progress from an in-flight (or just-finished) agent-launch job.
 */
export function inferAgentLaunchAgentProgress(
  job: RemediationJob | null | undefined,
  agentInFlight: boolean,
): AgentLaunchAgentProgress {
  if (!agentInFlight && job == null) return EMPTY

  const phase: RemediationPhase | undefined = job?.phase
  const failed = phase === 'failed' || job?.status === 'failed'
  const blob = eventBlob(job?.events)
  const deployDoneHint = mentionsDeployDone(blob)
  const verifyHint = mentionsVerifyStage(blob)
  const liveHint = mentionsLiveStage(blob)
  const hasApprovalEvent = (job?.events ?? []).some(e => e.type === 'approval_request')

  const detectDone = agentInFlight || phase != null

  let approveDone = false
  let approveAwaiting = false
  let deployOutcome: AgentLaunchAgentProgress['deployOutcome']
  let verifyAwaiting = false
  let verifyOutcome: AgentLaunchAgentProgress['verifyOutcome']
  let liveOutcome: AgentLaunchAgentProgress['liveOutcome']
  let focusStep: AgentLaunchStepKey = 'detect'

  if (phase === 'starting' || phase === 'diagnosing' || phase == null) {
    approveAwaiting = true
    focusStep = 'approve'
  } else if (phase === 'awaiting_approval') {
    if (deployDoneHint || (verifyHint && hasApprovalEvent && /verify|bridge|heartbeat/.test(blob))) {
      // Second-wave Dock prompt after deploy (verify / live), not the initial publish approve.
      approveDone = true
      deployOutcome = 'ok'
      verifyAwaiting = true
      focusStep = 'verify'
    } else if (deployDoneHint) {
      approveDone = true
      deployOutcome = 'ok'
      verifyAwaiting = true
      focusStep = 'verify'
    } else {
      approveAwaiting = true
      focusStep = 'approve'
    }
  } else if (phase === 'remediating') {
    approveDone = true
    if (deployDoneHint || verifyHint) {
      deployOutcome = 'ok'
      if (verifyHint) {
        verifyAwaiting = true
        focusStep = 'verify'
      } else {
        focusStep = 'deploy'
      }
    } else {
      deployOutcome = 'pending'
      focusStep = 'deploy'
    }
  } else if (phase === 'verifying') {
    approveDone = true
    deployOutcome = 'ok'
    if (liveHint && !failed) {
      verifyOutcome = 'ok'
      liveOutcome = 'pending'
      focusStep = 'live-check'
    } else {
      verifyOutcome = failed ? 'failed' : 'pending'
      verifyAwaiting = !failed
      focusStep = 'verify'
    }
  } else if (phase === 'done') {
    approveDone = true
    deployOutcome = 'ok'
    verifyOutcome = 'ok'
    liveOutcome = 'ok'
    focusStep = 'live-check'
  } else if (phase === 'failed') {
    approveDone = true
    if (deployDoneHint) {
      deployOutcome = 'ok'
      verifyOutcome = 'failed'
      focusStep = 'verify'
    } else if (hasApprovalEvent) {
      deployOutcome = 'failed'
      focusStep = 'deploy'
    } else {
      focusStep = 'approve'
    }
  } else if (phase === 'cancelled') {
    focusStep = 'approve'
  }

  if (deployDoneHint && deployOutcome !== 'failed') {
    approveDone = true
    deployOutcome = deployOutcome === 'pending' || deployOutcome == null ? 'ok' : deployOutcome
  }
  if (verifyHint && deployOutcome === 'ok' && verifyOutcome == null && phase !== 'done') {
    verifyAwaiting = true
    if (focusStep === 'deploy') focusStep = 'verify'
  }

  return {
    detectDone,
    approveDone,
    approveAwaiting,
    deployOutcome,
    verifyAwaiting,
    verifyOutcome,
    liveOutcome,
    focusStep,
    failed,
  }
}

/** Evidence patch that only advances (never clears) cycle markers from agent progress. */
export function evidencePatchFromAgentLaunchProgress(
  evidence: AgentLaunchEvidence,
  progress: AgentLaunchAgentProgress,
): Partial<AgentLaunchEvidence> | null {
  if (!progress.detectDone && !progress.approveDone && progress.deployOutcome == null) {
    return null
  }
  const now = new Date().toISOString()
  const patch: Partial<AgentLaunchEvidence> = {}

  if (progress.detectDone && evidence.lastDetectAt == null) {
    patch.lastDetectAt = now
  }
  if (progress.approveDone && evidence.lastApproveAt == null) {
    patch.lastApproveAt = now
    patch.approvedBy = evidence.approvedBy ?? 'operator-dock'
  }
  if (progress.deployOutcome === 'ok' && evidence.deployOutcome !== 'ok') {
    patch.lastDeployAt = now
    patch.deployOutcome = 'ok'
  }
  if (progress.deployOutcome === 'failed' && evidence.deployOutcome !== 'failed') {
    patch.lastDeployAt = now
    patch.deployOutcome = 'failed'
  }
  if (progress.verifyOutcome === 'ok' && evidence.verifyOutcome !== 'ok') {
    patch.lastVerifyAt = now
    patch.verifyOutcome = 'ok'
  }
  if (progress.verifyOutcome === 'failed' && evidence.verifyOutcome !== 'failed') {
    patch.lastVerifyAt = now
    patch.verifyOutcome = 'failed'
  }
  if (progress.liveOutcome === 'ok' && evidence.liveCheckOutcome !== 'ok') {
    patch.lastLiveCheckAt = now
    patch.liveCheckOutcome = 'ok'
  }
  if (progress.liveOutcome === 'failed' && evidence.liveCheckOutcome !== 'failed') {
    patch.lastLiveCheckAt = now
    patch.liveCheckOutcome = 'failed'
  }

  return Object.keys(patch).length > 0 ? patch : null
}
