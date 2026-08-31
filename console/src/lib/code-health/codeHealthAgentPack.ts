/**
 * Code Health → Code Refactor Task pack for Agent IDE.
 *
 * Console owns the ratchet reading (Live Re-scan + metrics). The IDE Agent
 * owns semantic cut planning — this pack never invents playbook steps.
 */

import {
  fetchCodeHealth,
  rescanCodeHealth,
  type CodeHealthResponse,
} from '@/api/codeHealth'
import {
  buildCodeHealthLens,
  type CodeHealthLens,
} from '@/lib/code-health/codeHealthLens'
import { listLowerBaselineProposals } from '@/lib/code-health/codeHealthLowerBaseline'

export type CodeHealthAgentPackSnapshot = {
  generatedAt: string
  response: CodeHealthResponse
  lens: CodeHealthLens
  /** How the reading was obtained for this pack. */
  gatherMode: 'live-rescan' | 'stored-snapshot' | 'rescan-unavailable' | 'rescan-failed'
  gatherNote?: string
}

export type GatherRefactorTaskOptions = {
  /**
   * When true (default), attempt POST /code-health/rescan first so the pack
   * describes live workspace code. Falls back to GET if rescan is unavailable.
   */
  liveRescanFirst?: boolean
}

export async function gatherCodeHealthSnapshot(
  options: GatherRefactorTaskOptions = {},
): Promise<CodeHealthAgentPackSnapshot> {
  const liveRescanFirst = options.liveRescanFirst !== false
  const generatedAt = new Date().toISOString()

  if (liveRescanFirst) {
    // Probe freshness without auth first — avoid a doomed rescan when cluster-only.
    let probe: CodeHealthResponse | null = null
    try {
      probe = await fetchCodeHealth(1)
    } catch {
      probe = null
    }
    const available = probe?.freshness?.rescan_available === true
    if (available) {
      try {
        await rescanCodeHealth()
        const response = await fetchCodeHealth(30)
        return {
          generatedAt,
          response,
          lens: buildCodeHealthLens(response),
          gatherMode: 'live-rescan',
          gatherNote: 'Live Re-scan completed before building this pack.',
        }
      } catch (err) {
        const response = probe ?? (await fetchCodeHealth(30))
        return {
          generatedAt,
          response,
          lens: buildCodeHealthLens(response),
          gatherMode: 'rescan-failed',
          gatherNote: err instanceof Error ? err.message : String(err),
        }
      }
    }
    const response = probe ?? (await fetchCodeHealth(30))
    return {
      generatedAt,
      response,
      lens: buildCodeHealthLens(response),
      gatherMode: 'rescan-unavailable',
      gatherNote:
        response.freshness?.note ??
        'Live Re-scan unavailable on this API host — pack uses the last stored reading.',
    }
  }

  const response = await fetchCodeHealth(30)
  return {
    generatedAt,
    response,
    lens: buildCodeHealthLens(response),
    gatherMode: 'stored-snapshot',
  }
}

/** @deprecated alias — prefer gatherCodeHealthSnapshot({ liveRescanFirst: true }) */
export const gatherCodeHealthRefactorTask = gatherCodeHealthSnapshot

export function buildCodeHealthAgentPack(snap: CodeHealthAgentPackSnapshot): string {
  const lines: string[] = []
  const push = (s = '') => lines.push(s)

  push('# Code Health — Code Refactor Agent Task Content')
  push(`Generated: ${snap.generatedAt}`)
  push('Source: Ops Console → Mission Control → Code Health (Generate Agent Pack)')
  push('')
  push('## Your job (IDE Agent)')
  push(
    'You receive a **mechanical ratchet reading** below (scan.sh / Live Re-scan). Console does **not** invent refactor steps.',
  )
  push('1. Treat the metrics + offender `detail` as ground truth for this pack.')
  push(
    '2. Open the listed offender files in the workspace and propose a **Suggested task list** grounded in current code — no feature drift, no speculative refactors.',
  )
  push(
    '3. Prefer the paydown queue order (OVER first, then ascending slack). One primary cut at a time unless the Owner asks for a backlog.',
  )
  push(
    '4. After a real reduction, lock baselines only to values `scan.sh` prints — never invent numbers.',
  )
  push('5. Do **not** invent a weighted health score or re-weight dimensions.')
  push('')
  push('## Required deliverable from you')
  push('Reply with a **Suggested task list** (markdown), each item:')
  push('- Title (one cut)')
  push('- Repo + metric id')
  push('- Why (cite value / baseline / slack / detail from this pack)')
  push('- Concrete steps (files / symbols you actually inspected)')
  push('- Verify command (`scan.sh --repo …` or repo check script)')
  push('- Risk / no-drift notes (what must stay behavior-compatible)')
  push('')
  push('## D10')
  push(
    'Trade execution remains BLOCKED. Do not enable live trading, scale daemon for trade, or write ib:operator:cmd.',
  )
  push('')

  push('## Pack gather')
  push(`- Mode: ${snap.gatherMode}`)
  if (snap.gatherNote) push(`- Note: ${snap.gatherNote}`)
  push('')

  const { lens } = snap
  if (!lens.reported || lens.report == null) {
    push('## Status: NOT OBSERVED')
    push(lens.note ?? 'No code-health report has ever been submitted.')
    push('')
    push('## Required first step')
    push('Produce a reading before proposing tasks:')
    push('```bash')
    push('cd bifrost-trade-infra')
    push('bash agent-config/scripts/code-health/scan.sh --report')
    push('```')
    push('Or Ops Console → Code Health → Live Re-scan (local DEV platform-api).')
    push('Treat absence of data as unmeasured — never as healthy. Do not invent a task list without metrics.')
    return lines.join('\n')
  }

  const report = lens.report
  push('## Posture Summary')
  push(`- ${lens.posture.summaryLine}`)
  push(`- Headroom: ${lens.posture.headroomLine}`)
  push(`- Trend: ${lens.posture.trendLine}`)
  if (lens.posture.nextLine !== '') push(`- ${lens.posture.nextLine}`)
  push('')

  push('## Freshness')
  const fresh = snap.response.freshness
  if (fresh == null) {
    push('- Freshness: unknown (API did not return freshness)')
  } else {
    push(`- Rescan available: ${fresh.rescan_available ? 'yes' : 'no'}`)
    if (fresh.workspace_root) push(`- Workspace: ${fresh.workspace_root}`)
    if (fresh.infra_head) push(`- Live infra HEAD: ${fresh.infra_head}`)
    if (fresh.reading_commit) push(`- Reading commit: ${fresh.reading_commit}`)
    push(
      `- Stale vs HEAD: ${
        fresh.stale_vs_head
          ? 'YES — prefer Live Re-scan before trusting this reading for cut planning'
          : 'no'
      }`,
    )
    if (fresh.note) push(`- Note: ${fresh.note}`)
  }
  push('')

  push('## Snapshot')
  push(`- Commit: ${report.commit}`)
  push(`- Received: ${report.received_at}`)
  push(`- Source: ${report.source ?? 'unknown'}`)
  push(`- Planning lamp: ${lens.planningLamp} (${lens.planningTag})`)
  push(`- Metrics: ${lens.metrics.length}`)
  push(`- Over baseline: ${lens.overCount}`)
  push(`- At ceiling (slack 0): ${lens.atCeilingCount}`)
  push(`- Baseline lowering owed: ${lens.owedCount}`)
  push(`- Min slack: ${lens.minSlack ?? '—'}`)
  if (lens.hasTrend) {
    push(
      `- Δ slack vs previous (sum): ${
        lens.totalDeltaSlack == null
          ? '—'
          : lens.totalDeltaSlack > 0
            ? `+${lens.totalDeltaSlack}`
            : String(lens.totalDeltaSlack)
      }`,
    )
  } else {
    push('- Trend: NO TREND (need ≥2 reported readings)')
  }
  if (report.not_measured != null && report.not_measured.trim() !== '') {
    push(`- Not measured: ${report.not_measured.trim()}`)
  }
  push('')

  push('## Dimensions (labels only — not weighted)')
  if (lens.dimensionSummaries.length === 0) {
    push('- (none)')
  } else {
    for (const d of lens.dimensionSummaries) {
      push(
        `- ${d.label}: ${d.metricCount} metric(s), min slack ${d.minSlack ?? '—'}, ${d.atCeilingCount} at ceiling, ${d.overCount} over`,
      )
    }
  }
  push('')

  push('## Paydown queue (priority order for your Suggested tasks)')
  if (lens.paydownQueue.length === 0) {
    push('- (empty — all metrics have positive slack; suggest only lock-baseline owed items if any)')
  } else {
    lens.paydownQueue.forEach((row, i) => {
      push(
        `${i + 1}. [${row.over ? 'OVER' : 'AT CEILING'}] ${row.metric.id} · ${row.metric.label} (${row.metric.repo})`,
      )
      push(
        `   value=${row.metric.value} baseline=${row.metric.baseline} slack=${row.slack} status=${row.metric.status}`,
      )
      if (row.metric.detail) push(`   detail: ${row.metric.detail}`)
      if (row.metric.baseline_var) push(`   baseline_var: ${row.metric.baseline_var}`)
    })
  }
  push('')

  push('## All metrics')
  for (const row of lens.metrics) {
    push(
      `- ${row.metric.id}: value=${row.metric.value} baseline=${row.metric.baseline} slack=${row.slack} status=${row.metric.status} dim=${row.dimension} repo=${row.metric.repo}${
        row.metric.detail ? ` · ${row.metric.detail}` : ''
      }`,
    )
  }
  push('')

  push('## How to lower a baseline after improvement')
  const owed = listLowerBaselineProposals(snap.response.latest?.metrics ?? [])
  if (owed.length === 0) {
    push('1. Run `bash agent-config/scripts/code-health/scan.sh` and note the printed value.')
    push('2. Set that exact number in `agent-config/scripts/code-health/baselines.env`.')
    push('3. Never hand-edit a baseline to a number scan.sh has not printed.')
  } else {
    push('Owed lock-ins from this reading (value is locked — do not invent another number):')
    for (const p of owed) {
      push('')
      push(p.agentBrief)
      push('```')
      push(p.patch)
      push('```')
    }
  }
  push('')

  push('## Forbidden')
  push('- Do not invent a composite health score or re-weight dimensions.')
  push('- Do not treat NOT OBSERVED / missing scan as healthy.')
  push('- Do not raise baselines to silence OVER.')
  push('- Do not paste generic playbook steps without reading the offender files.')
  push('- Do not propose cuts that change product behavior unless the Owner explicitly asks.')

  return lines.join('\n')
}
