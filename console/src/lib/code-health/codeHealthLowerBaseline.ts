/**
 * Lower-baseline workflow — lock in an IMPROVED scan reading.
 *
 * Contract: proposed value MUST equal the metric.value that scan.sh printed.
 * Console never writes baselines.env; it only copies a patch / Agent brief for
 * bifrost-trade-infra. Raising baselines is forbidden.
 */

import type { CodeHealthMetricDto } from '@/api/codeHealth'

export const BASELINES_ENV_PATH =
  'bifrost-trade-infra/agent-config/scripts/code-health/baselines.env'

/** Fallback when an older report lacked baseline_var (pre–Wave 5.1 scan). */
export const BASELINE_VAR_BY_METRIC_ID: Record<string, string> = {
  'code.duplication.satellite': 'DUP_FUNCS_FRONTEND_BASELINE',
  'code.duplication.research': 'DUP_FUNCS_RESEARCH_BASELINE',
  'code.duplication.satellite.trade-api': 'DUP_FUNCS_TRADE_API_BASELINE',
  'code.duplication.satellite.trade-core': 'DUP_FUNCS_TRADE_CORE_BASELINE',
  'code.duplication.satellite.trade-worker': 'DUP_FUNCS_TRADE_WORKER_BASELINE',
  'code.duplication.subcontractors.plugin': 'DUP_FUNCS_PLUGIN_BASELINE',
  'code.duplication.subcontractors.market-data': 'DUP_FUNCS_MARKET_DATA_BASELINE',
  'code.duplication.subcontractors.flex-query': 'DUP_FUNCS_FLEX_QUERY_BASELINE',
  'code.oversized.rocket': 'OVERSIZED_PLATFORM_BASELINE',
  'code.oversized.rocket.ui': 'OVERSIZED_UI_BASELINE',
  'code.oversized.satellite': 'OVERSIZED_FRONTEND_BASELINE',
  'code.oversized.satellite.trade-api': 'OVERSIZED_TRADE_API_BASELINE',
  'code.oversized.satellite.trade-core': 'OVERSIZED_TRADE_CORE_BASELINE',
  'code.oversized.satellite.trade-worker': 'OVERSIZED_TRADE_WORKER_BASELINE',
  'code.oversized.research': 'OVERSIZED_RESEARCH_BASELINE',
  'code.oversized.subcontractors.plugin': 'OVERSIZED_PLUGIN_BASELINE',
  'code.oversized.subcontractors.market-data': 'OVERSIZED_MARKET_DATA_BASELINE',
  'code.oversized.subcontractors.flex-query': 'OVERSIZED_FLEX_QUERY_BASELINE',
  'code.contract-coverage.satellite': 'UNVALIDATED_API_BASELINE',
  'code.image-version-spread.research': 'RESEARCH_IMAGE_TIERS_BASELINE',
}

export type LowerBaselineProposal = {
  metricId: string
  label: string
  repo: string
  baselineVar: string
  from: number
  /** Locked to scan reading — never a free-typed number. */
  to: number
  path: string
  patch: string
  agentBrief: string
}

export function resolveBaselineVar(m: CodeHealthMetricDto): string | null {
  const fromReport = m.baseline_var?.trim()
  if (fromReport) return fromReport
  return BASELINE_VAR_BY_METRIC_ID[m.id] ?? null
}

/**
 * Returns a proposal only for IMPROVED metrics (value < baseline).
 * Null when lowering is not owed or baseline_var is unknown.
 */
export function proposeLowerBaseline(m: CodeHealthMetricDto): LowerBaselineProposal | null {
  if (m.status !== 'improved') return null
  if (!(m.value < m.baseline)) return null
  const baselineVar = resolveBaselineVar(m)
  if (baselineVar == null || baselineVar === '') return null

  const from = m.baseline
  const to = m.value
  const path = BASELINES_ENV_PATH
  const patch = [
    `# Lock in scan.sh reading for ${m.id} (${m.repo})`,
    `# File: ${path}`,
    `# Only this exact number is allowed — it is what the scanner printed.`,
    `-${baselineVar}=${from}`,
    `+${baselineVar}=${to}`,
  ].join('\n')

  const agentBrief = [
    `## Lower baseline (ratchet lock-in)`,
    ``,
    `- Metric: ${m.label} (\`${m.id}\`)`,
    `- Repo: ${m.repo}`,
    `- Change: \`${baselineVar}\` ${from} → **${to}** in \`${path}\``,
    `- Rule: set the constant to **exactly ${to}** (the scan value). Do not invent another number.`,
    `- Do not raise any baseline.`,
    `- After edit: \`make check-code-health\` in bifrost-trade-infra (or \`scan.sh --repo ${m.repo}\`).`,
    `- Then commit in bifrost-trade-infra and re-report: \`bash scripts/code-health/scan.sh --report\`.`,
  ].join('\n')

  return {
    metricId: m.id,
    label: m.label,
    repo: m.repo,
    baselineVar,
    from,
    to,
    path,
    patch,
    agentBrief,
  }
}

export function listLowerBaselineProposals(
  metrics: CodeHealthMetricDto[],
): LowerBaselineProposal[] {
  const out: LowerBaselineProposal[] = []
  for (const m of metrics) {
    const p = proposeLowerBaseline(m)
    if (p != null) out.push(p)
  }
  return out
}
