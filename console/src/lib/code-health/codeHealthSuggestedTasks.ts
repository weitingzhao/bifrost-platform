/**
 * Suggested paydown tasks from the Code Health lens.
 *
 * Not a score — each paydown-queue metric becomes one concrete cut with
 * why / steps / verify / Agent brief. OVER first (same order as paydownQueue).
 */

import type { CodeHealthMetricLens, CodeHealthDimension } from '@/lib/code-health/codeHealthLens'
import { dimensionLabel } from '@/lib/code-health/codeHealthLens'
import { resolveBaselineVar } from '@/lib/code-health/codeHealthLowerBaseline'

export type CodeHealthTaskKind = 'unblock_gate' | 'create_headroom' | 'lock_baseline'

export type CodeHealthSuggestedTask = {
  id: string
  priority: number
  kind: CodeHealthTaskKind
  title: string
  why: string
  outcome: string
  steps: string[]
  verify: string
  repo: string
  dimension: CodeHealthDimension
  dimensionLabel: string
  slack: number
  value: number
  baseline: number
  detail: string
  agentBrief: string
}

type Playbook = {
  title: (row: CodeHealthMetricLens) => string
  why: (row: CodeHealthMetricLens) => string
  steps: (row: CodeHealthMetricLens) => string[]
  outcome: (row: CodeHealthMetricLens) => string
}

function targetValue(row: CodeHealthMetricLens): number {
  if (row.over) return row.metric.baseline
  // At ceiling: need at least −1 to create slack, then lock baseline.
  return Math.max(0, row.metric.value - 1)
}

function baselineHint(row: CodeHealthMetricLens): string {
  const v = resolveBaselineVar(row.metric)
  return v ?? '(baselines.env constant for this metric)'
}

const PLAYBOOKS: Record<string, Playbook> = {
  'code.oversized.rocket': {
    title: () => 'Split oversized Console / API modules (rocket)',
    why: row =>
      `${row.metric.value} tracked source file(s) exceed 800 lines in bifrost-platform. At ceiling, the next large file fails CI.`,
    outcome: row =>
      `Reduce files-over-800 from ${row.metric.value} to ≤${targetValue(row)}; then lower ${baselineHint(row)} to that count.`,
    steps: row => [
      `Open offenders from detail: ${row.metric.detail ?? 'run scan.sh for the list'}.`,
      'Extract cohesive slices (page sections, hooks, catalogs) into sibling modules — keep public exports stable.',
      'Prefer moving UI chrome into existing layout / data-display primitives rather than new one-off CSS.',
      `Re-run: bash ../scripts/code-health/scan.sh --repo bifrost-platform`,
      `If value dropped: set ${baselineHint(row)} to the printed value in baselines.env (never invent a number).`,
    ],
  },
  'code.oversized.satellite': {
    title: () => 'Split oversized Trade frontend modules (satellite)',
    why: row =>
      `${row.metric.value} Trade frontend file(s) exceed 800 lines. Large files are where the next duplicate helper gets written.`,
    outcome: row =>
      `Reduce files-over-800 from ${row.metric.value} to ≤${targetValue(row)}; lock ${baselineHint(row)}.`,
    steps: row => [
      `Start from offenders: ${row.metric.detail ?? 'scan.sh --repo bifrost-trade-frontend'}.`,
      'Split by domain: table columns, hooks, formatters — reuse Dense UI primitives; no new table module CSS.',
      'Run npm run check:legacy-css && npm run check:code-health in bifrost-trade-frontend.',
      `Lower ${baselineHint(row)} only to the scan-printed value.`,
    ],
  },
  'code.oversized.research': {
    title: () => 'Split oversized Research Python modules',
    why: row =>
      `${row.metric.value} research source file(s) exceed 800 lines — hard to review and easy to re-duplicate.`,
    outcome: row =>
      `Reduce files-over-800 from ${row.metric.value} to ≤${targetValue(row)}; lock ${baselineHint(row)}.`,
    steps: row => [
      `Offenders: ${row.metric.detail ?? 'scan.sh --repo bifrost-research'}.`,
      'Extract engines / API routers / repositories into focused modules; keep D13 (no Trade DB writes).',
      'make check-code-health (or scan.sh --repo bifrost-research).',
      `Lower ${baselineHint(row)} to the printed value.`,
    ],
  },
  'code.duplication.satellite': {
    title: () => 'Collapse duplicated function names (Trade frontend)',
    why: row =>
      `${row.metric.value} distinct function name(s) appear >3 times. New names at the ceiling trip CI; copies of existing names do not.`,
    outcome: row =>
      `Reduce duplicated-name count from ${row.metric.value} to ≤${targetValue(row)}; lock ${baselineHint(row)}.`,
    steps: row => [
      `Top names from detail: ${row.metric.detail ?? 'see scan stderr offenders'}.`,
      'Merge copies into one shared helper, or rename one-off variants so they are not the same concept.',
      'npm run check:code-health in bifrost-trade-frontend.',
      `Lower ${baselineHint(row)} to the printed value.`,
    ],
  },
  'code.duplication.research': {
    title: () => 'Collapse duplicated function names (Research)',
    why: row =>
      `${row.metric.value} distinct Python def name(s) appear >3 times outside tests/dunders.`,
    outcome: row =>
      `Reduce duplicated-name count from ${row.metric.value} to ≤${targetValue(row)}; lock ${baselineHint(row)}.`,
    steps: row => [
      `Top names: ${row.metric.detail ?? 'scan offenders'}.`,
      'Centralize shared helpers under bifrost_research packages; avoid parallel _run-style clones for new concepts.',
      'scan.sh --repo bifrost-research; lower baseline to the printed value only.',
    ],
  },
  'code.contract-coverage.satellite': {
    title: () => 'Add runtime schemas to unvalidated API modules',
    why: row =>
      `${row.metric.value} Trade frontend src/api/*.ts module(s) lack withValidation / lib/schemas — backend drift will be silent.`,
    outcome: row =>
      `Reduce unvalidated modules from ${row.metric.value} to ≤${targetValue(row)}; lock ${baselineHint(row)}.`,
    steps: row => [
      `Unvalidated list hint: ${row.metric.detail ?? 'scan.sh lists files on OVER'}.`,
      'Wire Zod (or existing lib/schemas) + withValidation on the hottest read paths first.',
      'npm run check:code-health; lower UNVALIDATED_API_BASELINE to the printed value.',
    ],
  },
  'code.image-version-spread.research': {
    title: () => 'Collapse Research image tag tiers',
    why: row =>
      `${row.metric.value} distinct bifrost-research image tags are pinned in k8s — each tier is untracked running code.`,
    outcome: row =>
      `Reduce distinct tags from ${row.metric.value} to ≤${targetValue(row)}; lock ${baselineHint(row)}.`,
    steps: row => [
      `Tags: ${row.metric.detail ?? 'grep image: in bifrost-research/k8s'}.`,
      'Pin CronJobs / Deployments / Dagster to one released tag (or a deliberate two-tier max).',
      'scan.sh --repo bifrost-research; lower RESEARCH_IMAGE_TIERS_BASELINE to the printed value.',
    ],
  },
}

function fallbackPlaybook(): Playbook {
  return {
    title: r => `Pay down ${r.metric.label} (${r.metric.repo})`,
    why: r =>
      r.over
        ? `${r.metric.label} is OVER baseline (${r.metric.value} > ${r.metric.baseline}) — CI blocks merge.`
        : `${r.metric.label} is at ceiling (slack 0). Next regression fails CI.`,
    outcome: r =>
      `Move value from ${r.metric.value} toward ≤${targetValue(r)}, then lower ${baselineHint(r)}.`,
    steps: r => [
      `Inspect detail: ${r.metric.detail ?? '—'}.`,
      `Improve the metric in ${r.metric.repo} (lower-is-better).`,
      `bash ../scripts/code-health/scan.sh --repo ${r.metric.repo}`,
      `If improved: lower ${baselineHint(r)} to the exact printed value.`,
    ],
  }
}

function kindFor(row: CodeHealthMetricLens): CodeHealthTaskKind {
  if (row.over) return 'unblock_gate'
  if (row.improved) return 'lock_baseline'
  return 'create_headroom'
}

function kindLabel(kind: CodeHealthTaskKind): string {
  switch (kind) {
    case 'unblock_gate':
      return 'UNBLOCK GATE'
    case 'lock_baseline':
      return 'LOCK BASELINE'
    default:
      return 'CREATE HEADROOM'
  }
}

function buildAgentBrief(task: Omit<CodeHealthSuggestedTask, 'agentBrief'>): string {
  return [
    `## Code Health cut #${task.priority}: ${task.title}`,
    '',
    `- Kind: ${kindLabel(task.kind)}`,
    `- Metric: ${task.id} · ${task.repo}`,
    `- Now / baseline / slack: ${task.value} / ${task.baseline} / ${task.slack}`,
    `- Dimension: ${task.dimensionLabel}`,
    `- Why: ${task.why}`,
    `- Outcome: ${task.outcome}`,
    '',
    '### Steps',
    ...task.steps.map((s, i) => `${i + 1}. ${s}`),
    '',
    `### Verify`,
    task.verify,
    '',
    '### Forbidden',
    '- Do not raise baselines to silence OVER.',
    '- Do not invent a composite health score.',
    '- D10 remains BLOCKED — no live trading paths.',
  ].join('\n')
}

export function buildSuggestedTasks(
  paydownQueue: CodeHealthMetricLens[],
  opts?: { limit?: number },
): CodeHealthSuggestedTask[] {
  const limit = opts?.limit ?? 12
  const out: CodeHealthSuggestedTask[] = []
  paydownQueue.slice(0, limit).forEach((row, i) => {
    const pb = PLAYBOOKS[row.metric.id] ?? fallbackPlaybook()
    const base: Omit<CodeHealthSuggestedTask, 'agentBrief'> = {
      id: row.metric.id,
      priority: i + 1,
      kind: kindFor(row),
      title: pb.title(row),
      why: pb.why(row),
      outcome: pb.outcome(row),
      steps: pb.steps(row),
      verify: `bash ../scripts/code-health/scan.sh --repo ${row.metric.repo}`,
      repo: row.metric.repo,
      dimension: row.dimension,
      dimensionLabel: dimensionLabel(row.dimension),
      slack: row.slack,
      value: row.metric.value,
      baseline: row.metric.baseline,
      detail: row.metric.detail ?? '',
    }
    out.push({ ...base, agentBrief: buildAgentBrief(base) })
  })
  return out
}

export function suggestedTaskKindLabel(kind: CodeHealthTaskKind): string {
  return kindLabel(kind)
}
