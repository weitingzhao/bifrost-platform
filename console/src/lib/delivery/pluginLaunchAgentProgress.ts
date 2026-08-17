/**
 * Map AI Launch Plugin remediation job phase + events → step progress.
 * Keeps Launch Desk Step strip in sync with Operator Dock (observe-only UX).
 */

import type { RemediationEvent, RemediationJob, RemediationPhase } from '@/api/remediationTypes'
import type { PluginLaunchEvidence } from '@/lib/delivery/pluginLaunchEvidence'

export type PluginLaunchStepKey =
  | 'detect'
  | 'approve'
  | 'install'
  | 'verify'
  | 'live-check'

export type PluginLaunchAgentProgress = {
  /** Session opened — Detect counted as done for this cycle. */
  detectDone: boolean
  approveDone: boolean
  /** Dock is waiting on Owner for publish/install approval. */
  approveAwaiting: boolean
  installOutcome?: 'ok' | 'failed' | 'pending'
  /** Dock waiting on verify / manual verify steps. */
  verifyAwaiting: boolean
  verifyOutcome?: 'ok' | 'failed' | 'pending'
  liveOutcome?: 'ok' | 'failed' | 'pending'
  /** Preferred Step focus while session is live. */
  focusStep: PluginLaunchStepKey
  failed: boolean
}

const EMPTY: PluginLaunchAgentProgress = {
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

function mentionsInstallDone(blob: string): boolean {
  return (
    /installation finished/.test(blob) ||
    /install(?:ation)?\s+(?:finished|complete|ok|done|succeeded)/.test(blob) ||
    /make install-ib-gateway[\s\S]{0,80}(?:ok|success|complete|finished)/.test(blob) ||
    /kubectl apply[\s\S]{0,80}(?:ok|success|configured|unchanged)/.test(blob)
  )
}

function mentionsVerifyStage(blob: string): boolean {
  return (
    /verify[-_ ]?(?:ib-gateway|market-data|阶段|stage)/.test(blob) ||
    /make verify-ib-gateway-program/.test(blob) ||
    /verify-market-data/.test(blob) ||
    /program 验收/.test(blob)
  )
}

function mentionsLiveStage(blob: string): boolean {
  return (
    /live check/.test(blob) ||
    /mode\s*=\s*live/.test(blob) ||
    /restore.*live/.test(blob) ||
    /live mode/.test(blob)
  )
}

function mentionsPublishApprove(blob: string): boolean {
  return (
    /publish\s+(?:ib gateway|market data|plugin)/.test(blob) ||
    /make install-ib-gateway/.test(blob) ||
    /request_operator_approval/.test(blob)
  )
}

/**
 * Infer Detect→Live progress from an in-flight (or just-finished) plugin-launch job.
 */
export function inferPluginLaunchAgentProgress(
  job: RemediationJob | null | undefined,
  agentInFlight: boolean,
): PluginLaunchAgentProgress {
  if (!agentInFlight && job == null) return EMPTY

  const phase: RemediationPhase | undefined = job?.phase
  const failed = phase === 'failed' || job?.status === 'failed'
  const blob = eventBlob(job?.events)
  const installDoneHint = mentionsInstallDone(blob)
  const verifyHint = mentionsVerifyStage(blob)
  const liveHint = mentionsLiveStage(blob)
  const hasApprovalEvent = (job?.events ?? []).some(e => e.type === 'approval_request')

  // Session started ⇒ Detect done for this publish cycle.
  const detectDone = agentInFlight || phase != null

  let approveDone = false
  let approveAwaiting = false
  let installOutcome: PluginLaunchAgentProgress['installOutcome']
  let verifyAwaiting = false
  let verifyOutcome: PluginLaunchAgentProgress['verifyOutcome']
  let liveOutcome: PluginLaunchAgentProgress['liveOutcome']
  let focusStep: PluginLaunchStepKey = 'detect'

  if (phase === 'starting' || phase === 'diagnosing' || phase == null) {
    approveAwaiting = true
    focusStep = 'approve'
  } else if (phase === 'awaiting_approval') {
    if (installDoneHint || verifyHint) {
      approveDone = true
      installOutcome = 'ok'
      verifyAwaiting = true
      focusStep = 'verify'
    } else {
      approveAwaiting = true
      focusStep = 'approve'
    }
  } else if (phase === 'remediating') {
    approveDone = true
    if (installDoneHint || verifyHint) {
      installOutcome = 'ok'
      if (verifyHint) {
        verifyAwaiting = true
        focusStep = 'verify'
      } else {
        focusStep = 'install'
      }
    } else {
      installOutcome = 'pending'
      focusStep = 'install'
    }
  } else if (phase === 'verifying') {
    approveDone = true
    installOutcome = 'ok'
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
    installOutcome = 'ok'
    verifyOutcome = 'ok'
    liveOutcome = 'ok'
    focusStep = 'live-check'
  } else if (phase === 'failed') {
    approveDone = true
    if (installDoneHint) {
      installOutcome = 'ok'
      verifyOutcome = 'failed'
      focusStep = 'verify'
    } else if (hasApprovalEvent || mentionsPublishApprove(blob)) {
      installOutcome = 'failed'
      focusStep = 'install'
    } else {
      focusStep = 'approve'
    }
  } else if (phase === 'cancelled') {
    focusStep = 'approve'
  }

  // Event hints can advance beyond coarse phase (e.g. remediating + "Installation finished").
  if (installDoneHint && installOutcome !== 'failed') {
    approveDone = true
    installOutcome = installOutcome === 'pending' || installOutcome == null ? 'ok' : installOutcome
  }
  if (verifyHint && installOutcome === 'ok' && verifyOutcome == null && phase !== 'done') {
    verifyAwaiting = true
    if (focusStep === 'install') focusStep = 'verify'
  }

  return {
    detectDone,
    approveDone,
    approveAwaiting,
    installOutcome,
    verifyAwaiting,
    verifyOutcome,
    liveOutcome,
    focusStep,
    failed,
  }
}

/** Evidence patch that only advances (never clears) cycle markers from agent progress. */
export function evidencePatchFromAgentProgress(
  evidence: PluginLaunchEvidence,
  progress: PluginLaunchAgentProgress,
): Partial<PluginLaunchEvidence> | null {
  if (!progress.detectDone && !progress.approveDone && progress.installOutcome == null) {
    return null
  }
  const now = new Date().toISOString()
  const patch: Partial<PluginLaunchEvidence> = {}

  if (progress.detectDone && evidence.lastDetectAt == null) {
    patch.lastDetectAt = now
  }
  if (progress.approveDone && evidence.lastApproveAt == null) {
    patch.lastApproveAt = now
    patch.approvedBy = evidence.approvedBy ?? 'operator-dock'
  }
  if (progress.installOutcome === 'ok' && evidence.installOutcome !== 'ok') {
    patch.lastInstallAt = now
    patch.installOutcome = 'ok'
  }
  if (progress.installOutcome === 'failed' && evidence.installOutcome !== 'failed') {
    patch.lastInstallAt = now
    patch.installOutcome = 'failed'
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
