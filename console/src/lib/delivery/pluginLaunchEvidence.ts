/**
 * Local evidence for Launch Plugin lane (Install / Verify / Live check).
 * Not Tekton PipelineRun — make install-ib-gateway + verify-ib-gateway-program.
 */

export const PLUGIN_LAUNCH_EVIDENCE_KEY = 'bifrost.pluginLaunch.evidence'

export type PluginLaunchStepId =
  | 'detect'
  | 'approve'
  | 'install'
  | 'verify'
  | 'live-check'

export type PluginLaunchEvidence = {
  /** Expected / dogfood revision hint (plugin git SHA short). */
  revisionHint?: string
  lastDetectAt?: string
  lastApproveAt?: string
  approvedBy?: string
  lastInstallAt?: string
  installOutcome?: 'ok' | 'failed' | 'pending'
  lastVerifyAt?: string
  verifyOutcome?: 'ok' | 'failed' | 'pending'
  lastLiveCheckAt?: string
  liveCheckOutcome?: 'ok' | 'failed' | 'pending'
  notes?: string
  updatedAt?: string
}

export const PLUGIN_DOGFOOD_REVISION = 'b2fb081'
export const PLUGIN_DOGFOOD_FEATURE = 'on-demand STK'

export function readPluginLaunchEvidence(): PluginLaunchEvidence {
  try {
    const raw = localStorage.getItem(PLUGIN_LAUNCH_EVIDENCE_KEY)
    if (raw == null || raw === '') return {}
    return JSON.parse(raw) as PluginLaunchEvidence
  } catch {
    return {}
  }
}

export function writePluginLaunchEvidence(patch: Partial<PluginLaunchEvidence>): PluginLaunchEvidence {
  const next: PluginLaunchEvidence = {
    ...readPluginLaunchEvidence(),
    ...patch,
    updatedAt: new Date().toISOString(),
  }
  try {
    localStorage.setItem(PLUGIN_LAUNCH_EVIDENCE_KEY, JSON.stringify(next))
  } catch {
    /* ignore quota */
  }
  return next
}

export function clearPluginLaunchEvidence(): void {
  try {
    localStorage.removeItem(PLUGIN_LAUNCH_EVIDENCE_KEY)
  } catch {
    /* ignore */
  }
}

export function evidenceSummaryLine(ev: PluginLaunchEvidence): string {
  const bits: string[] = []
  if (ev.revisionHint) bits.push(`rev ${ev.revisionHint}`)
  if (ev.lastInstallAt) {
    bits.push(
      `Install ${ev.installOutcome ?? '?'} @ ${new Date(ev.lastInstallAt).toLocaleString()}`,
    )
  }
  if (ev.lastVerifyAt) {
    bits.push(
      `Verify ${ev.verifyOutcome ?? '?'} @ ${new Date(ev.lastVerifyAt).toLocaleString()}`,
    )
  }
  if (ev.lastLiveCheckAt) {
    bits.push(
      `Live ${ev.liveCheckOutcome ?? '?'} @ ${new Date(ev.lastLiveCheckAt).toLocaleString()}`,
    )
  }
  return bits.length > 0 ? bits.join(' · ') : 'No install/verify evidence yet'
}
