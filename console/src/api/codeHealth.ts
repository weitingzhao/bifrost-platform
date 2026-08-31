import { parseError } from './client'

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
 * `reported: false` means nothing has ever been submitted. It must never be
 * rendered as healthy — that is precisely the blind spot this feature exists
 * to close.
 */
export type CodeHealthResponse = {
  reported: boolean
  note?: string
  latest?: CodeHealthReportDto
  history?: CodeHealthReportDto[]
}

export async function fetchCodeHealth(history = 10): Promise<CodeHealthResponse> {
  const r = await fetch(`/api/v1/code-health?history=${encodeURIComponent(String(history))}`)
  if (!r.ok) throw await parseError('code health', r)
  return r.json() as Promise<CodeHealthResponse>
}
