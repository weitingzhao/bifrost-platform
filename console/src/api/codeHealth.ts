import { authedFetch, parseError } from './client'

/**
 * Code-health ratchet readings from platform-api.
 *
 * Produced by bifrost-trade-infra/agent-config/scripts/code-health/scan.sh.
 * Every other Observability signal measures runtime; this one measures code
 * assets — the axis on which a cluster can be entirely green while the code
 * inside it rots.
 */
export type CodeHealthMetricDto = {
  id: string
  label: string
  domain: string
  repo: string
  value: number
  baseline: number
  /** baselines.env variable name — required for the lower-baseline workflow. */
  baseline_var?: string
  /** over = regression, at_baseline = held, improved = baseline owes a lowering. */
  status: 'over' | 'at_baseline' | 'improved'
  detail?: string
}

export type CodeHealthReportDto = {
  generated_at: string
  commit: string
  /** Repos the scan could not reach — their metrics are absent, not zero. */
  not_measured?: string
  source?: string
  metrics: CodeHealthMetricDto[]
  received_at: string
}

/**
 * Live workspace freshness for whether Suggested-task planning should trust
 * the stored reading (prefer Live Re-scan / Generate Agent Pack when stale).
 * RescanAvailable is typically true only on local DEV platform-api.
 */
export type CodeHealthFreshnessDto = {
  rescan_available: boolean
  workspace_root?: string
  infra_head?: string
  reading_commit?: string
  stale_vs_head: boolean
  note?: string
}

/**
 * `reported: false` means nothing has ever been submitted. It must never be
 * rendered as healthy — that is precisely the blind spot this feature exists
 * to close.
 */
export type CodeHealthResponse = {
  reported: boolean
  note?: string
  latest?: CodeHealthReportDto
  history?: CodeHealthReportDto[]
  freshness?: CodeHealthFreshnessDto
}

export type CodeHealthRescanResult = {
  stored: boolean
  commit: string
  metrics: number
  over_baseline: number
  source: string
  received_at: string
  freshness: CodeHealthFreshnessDto
  latest: CodeHealthReportDto
}

export async function fetchCodeHealth(history = 10): Promise<CodeHealthResponse> {
  const r = await fetch(`/api/v1/code-health?history=${encodeURIComponent(String(history))}`)
  if (!r.ok) throw await parseError('code health', r)
  return r.json() as Promise<CodeHealthResponse>
}

/**
 * Operator-gated Live Re-scan: platform-api runs scan.sh against the workspace
 * and stores the reading. Prefer this (or Generate Agent Pack) over Refresh when
 * Agent cut planning must describe current workspace code.
 */
export async function rescanCodeHealth(): Promise<CodeHealthRescanResult> {
  const r = await authedFetch('code-health rescan', '/api/v1/code-health/rescan', {
    method: 'POST',
  })
  return r.json() as Promise<CodeHealthRescanResult>
}
