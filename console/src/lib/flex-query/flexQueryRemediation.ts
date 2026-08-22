import type { MarketDataFreshnessInfo, MarketDataStatusResponse } from '@/api/satelliteBusTypes'

export type FlexRemediationFinding = {
  id: string
  severity: 'info' | 'warning' | 'danger'
  title: string
  detail: string
  /** Suggested ingest kind for enqueue remediation */
  enqueueKind?: string
}

export type FlexRemediationAnalysis = {
  findings: FlexRemediationFinding[]
  staleKinds: string[]
  needsAttention: boolean
  primaryCause: string | null
}

const FRESHNESS_MAX_AGE_H = 48

function formatAgeHours(h: number | undefined): string {
  if (h == null || !Number.isFinite(h)) return '—'
  if (h < 1) return `${Math.round(h * 60)}m ago`
  if (h < 48) return `${Math.round(h)}h ago`
  const d = Math.floor(h / 24)
  const rem = Math.round(h % 24)
  return rem > 0 ? `${d}d ${rem}h ago` : `${d}d ago`
}

export function analyzeFlexProbe(status: MarketDataStatusResponse | undefined): FlexRemediationAnalysis {
  const findings: FlexRemediationFinding[] = []
  const staleKinds: string[] = []

  if (status == null) {
    return {
      findings: [
        {
          id: 'probe-missing',
          severity: 'warning',
          title: 'Probe unavailable',
          detail: 'platform-api flex-query status not loaded yet.',
        },
      ],
      staleKinds: [],
      needsAttention: true,
      primaryCause: 'Probe unavailable',
    }
  }

  if (status.error) {
    findings.push({
      id: 'probe-error',
      severity: 'danger',
      title: 'Plugin probe error',
      detail: status.hint ? `${status.error} — ${status.hint}` : status.error,
    })
  }

  const deploys = status.deployments ?? []
  const notReady = deploys.filter(d => d.reachability !== 'ok')
  if (notReady.length > 0) {
    findings.push({
      id: 'deploy-not-ready',
      severity: 'danger',
      title: 'Deployment not ready',
      detail: notReady.map(d => `${d.name} ${d.ready}`).join(', '),
    })
  }

  const freshness = status.freshness ?? []
  for (const row of freshness) {
    if (row.verdict === 'ok') continue
    const kind = row.dimension
    staleKinds.push(kind)
    findings.push({
      id: `fresh-stale-${kind}`,
      severity: row.verdict === 'unknown' ? 'warning' : 'warning',
      title: `${kind} ingest stale`,
      detail: `Last run ${formatAgeHours(row.age_hours)} · ${row.rows_written ?? 0} rows (threshold ${FRESHNESS_MAX_AGE_H}h).`,
      enqueueKind: kind,
    })
  }

  if (freshness.length === 0 && status.freshness_reachability === 'unknown') {
    findings.push({
      id: 'fresh-empty',
      severity: 'warning',
      title: 'No ingest_freshness rows',
      detail: 'Run a flex-trades / flex-transactions job once to seed freshness markers.',
      enqueueKind: 'flex-trades',
    })
    staleKinds.push('flex-trades', 'flex-transactions')
  }

  const worker = status.workers?.[0]
  const freshnessAllOk =
    freshness.length > 0 && freshness.every(row => row.verdict === 'ok')
  if (worker != null && (worker.jobs_failed ?? 0) > 0) {
    // jobs_failed is attempts since pod restart, not open queue failures.
    if (!freshnessAllOk || status.reachability !== 'ok') {
      findings.push({
        id: 'worker-failed',
        severity: 'danger',
        title: 'Worker reported failures',
        detail: `${worker.jobs_failed} failed job(s) on pool ${worker.pool}.`,
      })
    }
  }

  const needsAttention =
    status.reachability === 'degraded' ||
    status.reachability === 'fail' ||
    findings.some(f => f.severity !== 'info')

  const primaryCause =
    staleKinds.length > 0
      ? `Ingest freshness stale (${staleKinds.join(', ')})`
      : notReady.length > 0
        ? 'Deployment not ready'
        : status.error
          ? 'Plugin probe error'
          : needsAttention
            ? 'Plugin health degraded'
            : null

  return { findings, staleKinds: [...new Set(staleKinds)], needsAttention, primaryCause }
}

export function buildFlexDiagnosePrefill(
  status: MarketDataStatusResponse | undefined,
  analysis: FlexRemediationAnalysis,
): string {
  const lines = [
    'IB Flex Query plugin — assisted diagnose (L0 read-only + safe enqueue only).',
    'D10: do NOT enable live trading or IB place_order.',
    '',
    `Verdict: ${status?.reachability ?? 'unknown'} · ${status?.summary ?? '—'}`,
  ]

  if (analysis.primaryCause) {
    lines.push(`Primary cause: ${analysis.primaryCause}`)
  }

  if ((status?.freshness?.length ?? 0) > 0) {
    lines.push('', 'ingest_freshness:')
    for (const f of status!.freshness as MarketDataFreshnessInfo[]) {
      lines.push(
        `- ${f.dimension}: verdict=${f.verdict} age=${formatAgeHours(f.age_hours)} rows=${f.rows_written}`,
      )
    }
  }

  if (analysis.findings.length > 0) {
    lines.push('', 'Findings:')
    for (const f of analysis.findings) {
      lines.push(`- [${f.severity}] ${f.title}: ${f.detail}`)
    }
  }

  lines.push(
    '',
    'Remediation plan (operator approval for writes):',
    '1. GET flex config summary — tokens/query IDs set?',
    '2. If stale: POST enqueue flex-trades + flex-transactions (or Flex Refresh on Manual tab).',
    '3. Poll GET flex/ingest/jobs until done; confirm flex_ops.ingest_freshness latest_ts < 48h.',
    '4. If worker stuck: check plugin-flex-query worker logs; rollout restart flex-query-worker.',
    '5. Report evidence before any k8s secret or credential changes.',
  )

  return lines.join('\n')
}
