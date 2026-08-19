/**
 * Local evidence for Launch Agent lane (Detect → Live check).
 * Not Tekton — executor is POST /api/v1/agent/deploy → deploy_mac_mini.sh.
 *
 * Store is keyed by target (primary / standby).
 */

export const AGENT_LAUNCH_STORE_KEY = 'bifrost.agentLaunch.store.v1'
/** Same-tab listeners (Launch Desk sidebar) re-read after the lane page writes evidence. */
export const AGENT_LAUNCH_STORE_EVENT = 'bifrost:agent-launch-store'

export type AgentLaunchTargetId = 'primary' | 'standby' | 'both'

/** `both` stores evidence under primary — the sequential both-cycle is one record. */
export function agentLaunchEvidenceKey(
  target: AgentLaunchTargetId,
): Exclude<AgentLaunchTargetId, 'both'> {
  return target === 'both' ? 'primary' : target
}

export type AgentLaunchStepId =
  | 'detect'
  | 'approve'
  | 'deploy'
  | 'verify'
  | 'live-check'

export type AgentLaunchEvidence = {
  lastDetectAt?: string
  lastApproveAt?: string
  approvedBy?: string
  lastDeployAt?: string
  deployOutcome?: 'ok' | 'failed' | 'pending'
  lastVerifyAt?: string
  verifyOutcome?: 'ok' | 'failed' | 'pending'
  lastLiveCheckAt?: string
  liveCheckOutcome?: 'ok' | 'failed' | 'pending'
  /**
   * ISO — host deploy jobs finished before this are ignored by the stepper
   * (set by Start next publish after a Published cycle).
   */
  cycleStartedAt?: string
  lastPublishedAt?: string
  notes?: string
  updatedAt?: string
}

export type AgentLaunchStore = {
  selectedTarget: AgentLaunchTargetId
  byKey: Record<string, AgentLaunchEvidence>
  updatedAt?: string
}

function emptyStore(): AgentLaunchStore {
  return {
    selectedTarget: 'both',
    byKey: {},
  }
}

export function readAgentLaunchStore(): AgentLaunchStore {
  try {
    const raw = localStorage.getItem(AGENT_LAUNCH_STORE_KEY)
    if (raw != null && raw !== '') {
      const parsed = JSON.parse(raw) as AgentLaunchStore
      if (parsed.byKey != null && typeof parsed.byKey === 'object') {
        return {
          selectedTarget: parsed.selectedTarget ?? 'primary',
          byKey: parsed.byKey,
          updatedAt: parsed.updatedAt,
        }
      }
    }
  } catch {
    /* ignore */
  }
  return emptyStore()
}

export function writeAgentLaunchStore(patch: Partial<AgentLaunchStore>): AgentLaunchStore {
  const cur = readAgentLaunchStore()
  const next: AgentLaunchStore = {
    ...cur,
    ...patch,
    byKey: patch.byKey ?? cur.byKey,
    updatedAt: new Date().toISOString(),
  }
  try {
    localStorage.setItem(AGENT_LAUNCH_STORE_KEY, JSON.stringify(next))
  } catch {
    /* ignore quota */
  }
  if (typeof window !== 'undefined') {
    window.dispatchEvent(new Event(AGENT_LAUNCH_STORE_EVENT))
  }
  return next
}

export function readAgentLaunchEvidence(target?: AgentLaunchTargetId): AgentLaunchEvidence {
  const store = readAgentLaunchStore()
  const t = agentLaunchEvidenceKey(target ?? store.selectedTarget)
  return store.byKey[t] ?? {}
}

export function writeAgentLaunchEvidence(
  patch: Partial<AgentLaunchEvidence>,
  target?: AgentLaunchTargetId,
): AgentLaunchEvidence {
  const store = readAgentLaunchStore()
  const t = agentLaunchEvidenceKey(target ?? store.selectedTarget)
  const nextEv: AgentLaunchEvidence = {
    ...(store.byKey[t] ?? {}),
    ...patch,
    updatedAt: new Date().toISOString(),
  }
  writeAgentLaunchStore({
    byKey: { ...store.byKey, [t]: nextEv },
  })
  return nextEv
}

/** Last-deploy lamp: API last job or local cycle evidence (Launch Agent checklist / sidebar). */
export function isAgentLaunchLastDeployOk(
  lastStatus: string | undefined,
  evidence: AgentLaunchEvidence,
): boolean {
  return lastStatus === 'done' || evidence.deployOutcome === 'ok'
}

export function evidenceSummaryLine(ev: AgentLaunchEvidence): string {
  const bits: string[] = []
  if (ev.lastDeployAt) {
    bits.push(
      `Deploy ${ev.deployOutcome ?? '?'} @ ${new Date(ev.lastDeployAt).toLocaleString()}`,
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
  if (bits.length === 0 && ev.lastPublishedAt) {
    bits.push(`Last published @ ${new Date(ev.lastPublishedAt).toLocaleString()}`)
  }
  return bits.length > 0 ? bits.join(' · ') : 'No deploy/verify evidence yet'
}

/**
 * Clear this-cycle Detect → Live evidence after Published.
 * Host deploy jobs finished before cycleStartedAt no longer drive the stepper.
 */
export function beginNextAgentLaunchCycle(target?: AgentLaunchTargetId): AgentLaunchEvidence {
  const store = readAgentLaunchStore()
  const t = agentLaunchEvidenceKey(target ?? store.selectedTarget)
  const cur = store.byKey[t] ?? {}
  const now = new Date().toISOString()
  const nextEv: AgentLaunchEvidence = {
    cycleStartedAt: now,
    lastPublishedAt:
      cur.lastLiveCheckAt ?? cur.lastVerifyAt ?? cur.lastDeployAt ?? cur.updatedAt ?? now,
    updatedAt: now,
  }
  writeAgentLaunchStore({
    byKey: { ...store.byKey, [t]: nextEv },
  })
  return nextEv
}
