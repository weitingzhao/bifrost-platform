/**
 * Code Health Copy / Ask for Agent pack — page-independent gather.
 * Planning language only (slack / paydown). No weighted score. D10 freeze noted.
 */

import { fetchCodeHealth, type CodeHealthResponse } from '@/api/codeHealth'
import {
  buildCodeHealthLens,
  type CodeHealthLens,
} from '@/lib/code-health/codeHealthLens'
import { listLowerBaselineProposals } from '@/lib/code-health/codeHealthLowerBaseline'

export type CodeHealthAgentPackSnapshot = {
  generatedAt: string
  response: CodeHealthResponse
  lens: CodeHealthLens
}

export async function gatherCodeHealthSnapshot(): Promise<CodeHealthAgentPackSnapshot> {
  const response = await fetchCodeHealth(30)
  return {
    generatedAt: new Date().toISOString(),
    response,
    lens: buildCodeHealthLens(response),
  }
}

export function buildCodeHealthAgentPack(snap: CodeHealthAgentPackSnapshot): string {
  const lines: string[] = []
  const push = (s = '') => lines.push(s)

  push('# Code Health — Agent paydown pack')
  push(`Generated: ${snap.generatedAt}`)
  push('Source: Ops Console → Mission Control → Code Health (Copy for Agent)')
  push('')
  push('## Goal')
  push(
    'Pay down structural debt in priority order (OVER first, then ascending slack). Do NOT invent a weighted health score. Gate remains: value may never exceed baseline.',
  )
  push('')
  push('## D10')
  push(
    'Trade execution remains BLOCKED. Do not enable live trading, scale daemon for trade, or write ib:operator:cmd.',
  )
  push('')

  const { lens } = snap
  if (!lens.reported || lens.report == null) {
    push('## Status: NOT OBSERVED')
    push(lens.note ?? 'No code-health report has ever been submitted.')
    push('')
    push('## Required first step')
    push('```bash')
    push('cd bifrost-trade-infra')
    push('bash agent-config/scripts/code-health/scan.sh --report')
    push('```')
    push('Treat absence of data as unmeasured — never as healthy.')
    return lines.join('\n')
  }

  const report = lens.report
  push('## Posture Summary')
  push(`- ${lens.posture.summaryLine}`)
  push(`- Headroom: ${lens.posture.headroomLine}`)
  push(`- Trend: ${lens.posture.trendLine}`)
  if (lens.posture.nextLine !== '') push(`- ${lens.posture.nextLine}`)
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

  push('## Paydown queue (do these first)')
  if (lens.paydownQueue.length === 0) {
    push('- (empty — all metrics have positive slack)')
  } else {
    lens.paydownQueue.forEach((row, i) => {
      push(
        `${i + 1}. [${row.over ? 'OVER' : 'AT CEILING'}] ${row.metric.label} (${row.metric.repo}) slack=${row.slack} · ${row.metric.detail ?? ''}`,
      )
    })
  }
  push('')

  push('## All metrics')
  for (const row of lens.metrics) {
    push(
      `- ${row.metric.id}: value=${row.metric.value} baseline=${row.metric.baseline} slack=${row.slack} status=${row.metric.status} dim=${row.dimension}`,
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

  return lines.join('\n')
}
