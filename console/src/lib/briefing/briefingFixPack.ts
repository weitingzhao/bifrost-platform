/**
 * Briefing Sync Loop — fix pack generator.
 *
 * Turns the live fail/warning state of the detect → propose → approve → fix
 * pipeline into three Owner-facing actions:
 *   1. Ask AI Agent to fix   → copyable Cursor Agent prompt (manual paste)
 *   2. Fix with built-in agent → same prompt, prefilled into Agent Desk composer
 *   3. Copy doctrine to learn  → prevention/learning note for a .cursor rule
 *
 * Doctrine: briefingReconciliationCatalog.ts (gate rules + L1–L4 layers)
 */

import type { AgentNightlyReportResponse } from '@/api/types'
import type { BriefingSyncLoopStepView } from '@/lib/briefing/briefingSyncLoop'
import { parseNightlyLayerResults } from '@/lib/briefing/briefingSyncLoop'
import type { ReconcileFinding } from '@/lib/briefing/reconcileBriefing'

const WORKSPACE_ROOT = '/Users/vision-mac-trader/Desktop/stocks'

export type DriftClass =
  | 'runtime-sync'
  | 'missing_path'
  | 'unknown_port'
  | 'catalog_version'
  | 'api_probe'
  | 'l4_post'
  | 'generic'

export type SyncLoopFixPack = {
  /** Any fail/warning that warrants an action bar. */
  hasIssues: boolean
  /** Short human summary of what is wrong. */
  summary: string
  /** Drift classes present (drives fix hints + doctrine note). */
  classes: DriftClass[]
  /** Cursor-ready fix prompt (directions 1 & 2). */
  fixPrompt: string
  /** Prevention / learning doctrine note (direction 3). */
  learnNote: string
}

const DRIFT_FIX_HINTS: Record<DriftClass, { title: string; fix: string }> = {
  'runtime-sync': {
    title: 'Runtime SYNC reconcile finding',
    fix: 'Reconcile the briefing pack against spine — see reconcileBriefing.ts RECONCILE_GATE_RULES. Usually spine (ops-context.yaml) and a *Catalog.ts disagree on stream status / next_task.',
  },
  missing_path: {
    title: 'missing_path — catalog references a file that does not exist',
    fix: 'Open the cited `*Catalog.ts:line`. Either fix the referenced path to an existing file under the stocks workspace, or remove the stale reference. Do NOT create placeholder files just to satisfy the scanner.',
  },
  unknown_port: {
    title: 'unknown_port — port not in CANONICAL_PORTS',
    fix: 'Add the port to the canonical port map (agent/drift/scan_layer1.py CANONICAL_PORTS) if it is legitimate, or correct the catalog line that introduced the unexpected port.',
  },
  catalog_version: {
    title: 'catalog_version — spine meta version ≠ catalog / live API',
    fix: 'Align config/ops-context.yaml `meta_version` with environments-catalog.ts, then restart platform-api so GET /api/v1/context reflects the new version. These three must match.',
  },
  api_probe: {
    title: 'API probe failure (Layer 2)',
    fix: 'A platform-api probe is failing (e.g. cluster reachability degraded, remediation runner 503). Check the cited endpoint health; this may be environmental (runner down) rather than code drift.',
  },
  l4_post: {
    title: 'L4 proposal POST did not land in this Console',
    fix: 'The scan created (or failed to create) a drift proposal on a different platform-api than this Console reads. Approve on the matching K3s Console, or re-run the scan with PLATFORM_API_URL pointing at this API.',
  },
  generic: {
    title: 'Pipeline step needs attention',
    fix: 'Inspect the step detail and the nightly report Layer sections for the specific drift.',
  },
}

function reportSection(content: string, marker: string): string {
  const idx = content.indexOf(marker)
  if (idx < 0) return ''
  const rest = content.slice(idx)
  const next = rest.slice(marker.length).search(/\n## /)
  return next < 0 ? rest : rest.slice(0, next + marker.length)
}

/** Pull the bullet finding lines for a drift class out of the nightly report. */
function findingBullets(content: string, header: string, max = 12): string[] {
  const block = content
  const idx = block.indexOf(`### ${header}`)
  if (idx < 0) return []
  const rest = block.slice(idx)
  const lines = rest.split('\n').slice(1)
  const out: string[] = []
  for (const line of lines) {
    if (line.startsWith('### ') || line.startsWith('## ')) break
    if (line.trim().startsWith('- ')) out.push(line.trim())
    if (out.length >= max) break
  }
  return out
}

export function buildSyncLoopFixPack(input: {
  steps: BriefingSyncLoopStepView[]
  reconcileFindings: ReconcileFinding[]
  nightlyReport?: AgentNightlyReportResponse
}): SyncLoopFixPack {
  const { steps, reconcileFindings, nightlyReport } = input
  const content = nightlyReport?.content ?? ''
  const layers = parseNightlyLayerResults(content)

  const failing = steps.filter(s => s.status === 'fail')
  const warning = steps.filter(s => s.status === 'warning')
  const hasIssues = failing.length > 0 || warning.length > 0 || reconcileFindings.length > 0

  const classes: DriftClass[] = []
  const addClass = (c: DriftClass) => {
    if (!classes.includes(c)) classes.push(c)
  }

  if (reconcileFindings.length > 0) addClass('runtime-sync')
  if (findingBullets(content, 'missing_path').length > 0) addClass('missing_path')
  if (findingBullets(content, 'unknown_port').length > 0) addClass('unknown_port')
  if (findingBullets(content, 'catalog_version').length > 0) addClass('catalog_version')
  if (layers.l2 === 'fail') addClass('api_probe')
  if (layers.l4Hint === 'post_failed' || layers.l4Hint === 'posted') addClass('l4_post')
  if (classes.length === 0 && hasIssues) addClass('generic')

  // ---- summary ----
  const summaryParts: string[] = []
  if (failing.length > 0) summaryParts.push(`${failing.length} failing step(s)`)
  if (warning.length > 0) summaryParts.push(`${warning.length} warning(s)`)
  if (reconcileFindings.length > 0) summaryParts.push(`${reconcileFindings.length} reconcile finding(s)`)
  const summary = summaryParts.length > 0 ? summaryParts.join(' · ') : 'Pipeline clear'

  // ---- shared issue list ----
  const issueLines: string[] = []
  for (const s of [...failing, ...warning]) {
    issueLines.push(`- [${s.label} · ${s.status.toUpperCase()}] ${s.detail}`)
  }
  for (const f of reconcileFindings) {
    issueLines.push(`- [Runtime SYNC · ${f.severity.toUpperCase()}] ${f.ruleId}: ${f.message}`)
  }

  // ---- layer excerpts ----
  const excerptParts: string[] = []
  for (const header of ['missing_path', 'unknown_port', 'catalog_version']) {
    const bullets = findingBullets(content, header)
    if (bullets.length > 0) {
      excerptParts.push(`### ${header} (${bullets.length})\n${bullets.join('\n')}`)
    }
  }
  if (layers.l2 === 'fail') {
    const l2 = reportSection(content, '## Layer 2')
    const fails = l2.split('\n').filter(l => l.includes('**') && l.includes('—')).slice(0, 6)
    if (fails.length > 0) excerptParts.push(`### Layer 2 probe failures\n${fails.join('\n')}`)
  }
  const excerpts = excerptParts.length > 0 ? excerptParts.join('\n\n') : '(No structured layer findings parsed — read the nightly report Layer sections.)'

  // ---- fix hints by class ----
  const fixHints = classes
    .map(c => DRIFT_FIX_HINTS[c])
    .map(h => `- **${h.title}**\n  ${h.fix}`)
    .join('\n')

  // ---- direction 1 & 2: fix prompt ----
  const fixPrompt = [
    '# Bifrost Ops — Briefing Sync Loop drift fix',
    '',
    'You are an AI coding agent fixing drift detected by the Bifrost Ops Platform briefing reconciliation pipeline (detect → propose → approve → fix).',
    '',
    `- Workspace root: \`${WORKSPACE_ROOT}\``,
    '- Control plane to edit: `bifrost-platform/` (Go `api/`, React `console/`, `config/ops-context.yaml`, `agent/drift/`)',
    '- READ-ONLY (never edit): `bifrost-trader-engine/`',
    '',
    `## Detected (${summary})`,
    issueLines.length > 0 ? issueLines.join('\n') : '- (See pipeline step details in Ops Console → Agent Briefing)',
    '',
    '## Layer findings (from nightly report)',
    excerpts,
    '',
    '## How to fix (by drift class)',
    fixHints !== '' ? fixHints : '- Inspect the nightly report Layer sections and reconcile the cited catalog/spine entries.',
    '',
    '## Constraints',
    '- Edit only inside `bifrost-platform`. Never touch `bifrost-trader-engine` (read-only reference).',
    '- No unattended spine writes. Make changes on a branch `agent/drift-fix-<date>` and summarize a PR for the Owner.',
    '- Prefer fixing the catalog/spine source of truth over silencing the scanner.',
    '',
    '## Verify before done',
    '- `cd bifrost-platform && python3 agent/drift/scan_layer1.py && python3 agent/drift/scan_layer3.py` → each reports `Findings: 0`',
    '- `cd bifrost-platform/console && npm run build` passes',
    '- Re-open Ops Console → Agent Briefing → Briefing sync loop shows RUNTIME SYNC OK and no FAIL steps',
  ].join('\n')

  // ---- direction 3: doctrine / learning note ----
  const learnLines = classes.map(c => {
    const h = DRIFT_FIX_HINTS[c]
    return `### ${c}\n- **What it means:** ${h.title}\n- **Prevent next time:** ${h.fix}`
  })
  const learnNote = [
    '# Drift class learnings — Briefing Sync Loop',
    '',
    'Captured from a live Ops Console drift event. Save as a `.cursor/rules/*.mdc` doctrine entry or paste into an Agent so future sessions avoid re-introducing the same drift.',
    '',
    `Observed: ${summary}`,
    '',
    learnLines.length > 0 ? learnLines.join('\n\n') : '- No specific drift class parsed; review reconcileBriefing.ts RECONCILE_GATE_RULES.',
    '',
    '## General rule',
    '- Spine (`config/ops-context.yaml` → `GET /api/v1/context`) is the single progress source of truth.',
    '- `*Catalog.ts` files are read-only domain specs — their referenced paths/ports must stay valid against the live workspace.',
    '- When a catalog references a file or port, that reference is a contract; keep it in sync or remove it.',
    '- After changing spine meta_version, restart platform-api so the three version sources (yaml / catalog / live API) match.',
  ].join('\n')

  return { hasIssues, summary, classes, fixPrompt, learnNote }
}
