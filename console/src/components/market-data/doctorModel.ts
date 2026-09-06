import type {
  DoctorFinding,
  DoctorFix,
  DoctorReport,
  DoctorSeverity,
  DoctorVerdict,
} from '@/api/marketDataDoctor'

const SEVERITY_ORDER: Record<DoctorSeverity, number> = { crit: 0, warn: 1, ok: 2 }

export function sortFindings(findings: readonly DoctorFinding[]): DoctorFinding[] {
  return [...findings].sort((a, b) => SEVERITY_ORDER[a.severity] - SEVERITY_ORDER[b.severity])
}

export function severityVariant(sev: DoctorSeverity): 'danger' | 'warning' | 'success' {
  if (sev === 'crit') return 'danger'
  if (sev === 'warn') return 'warning'
  return 'success'
}

export function verdictVariant(verdict: DoctorVerdict | null | undefined): 'danger' | 'warning' | 'success' | 'neutral' {
  if (verdict === 'critical') return 'danger'
  if (verdict === 'degraded') return 'warning'
  if (verdict === 'healthy') return 'success'
  return 'neutral'
}

/** Human line for a prescription: what the button will do. */
export function describeFix(fix: DoctorFix | null | undefined): string {
  if (fix == null) return '—'
  if (fix.action === 'enqueue-slot') {
    return `Enqueue ${fix.slot ?? '?'}${fix.date != null ? ` for ${fix.date}` : ''}${fix.force ? ' (force)' : ''}`
  }
  if (fix.action === 'retry-jobs') return `Retry ${fix.job_ids?.length ?? 0} ${fix.kind ?? ''} job(s)`
  if (fix.action === 'rollout-restart') return `Restart ${fix.deployment ?? 'deployment'} (kubectl / agent)`
  if (fix.action === 'check-vendor-key') return 'Check the Polygon API key / plan (manual)'
  return fix.action
}

export function formatValue(v: unknown): string {
  if (v == null) return '—'
  if (typeof v === 'object') {
    return Object.entries(v as Record<string, unknown>)
      .map(([k, x]) => `${k}=${String(x)}`)
      .join(' ')
  }
  return String(v)
}

export function autoFixableIds(report: DoctorReport | null | undefined): string[] {
  if (report == null) return []
  return report.findings.filter(f => f.auto_fixable && f.fix != null).map(f => f.id)
}

/**
 * What an agent needs to act, not just to read: the findings, the exact tool
 * calls that execute the prescriptions, and the recheck loop. Pasted into any
 * Claude / Cursor session with the bifrost-platform MCP server attached.
 */
export function buildDoctorAgentReport(report: DoctorReport): string {
  const bad = sortFindings(report.findings.filter(f => f.severity !== 'ok'))
  const lines: string[] = [
    '# Massive (Market Data) Plugin — doctor report',
    '',
    `Session: ${report.session}${report.session_is_today ? ' (today, EOD window passed)' : ' (last completed)'}`,
    `Verdict: ${report.verdict} — ${report.summary}`,
    `Generated: ${report.generated_at}`,
    `Universe: watchlist ${report.universe.watchlist} · underlyings ${report.universe.underlyings} · optionable ${report.universe.optionable}`,
    '',
    '## Findings (non-ok)',
  ]
  if (bad.length === 0) {
    lines.push('- none')
  }
  for (const f of bad) {
    lines.push(
      `- [${f.severity}] ${f.title} (${f.id}) — expected ${formatValue(f.expected)}, actual ${formatValue(f.actual)}. ${f.detail}`,
    )
    if (f.missing_sample != null && f.missing_sample.length > 0) {
      lines.push(`  missing: ${f.missing_sample.join(', ')}`)
    }
    lines.push(`  fix: ${describeFix(f.fix)}${f.auto_fixable ? ' [auto]' : ' [manual]'}`)
  }
  lines.push('', '## Prescriptions the plugin can execute')
  if (report.prescriptions.length === 0) {
    lines.push('- none — nothing to enqueue; remaining findings need a human or the cluster')
  }
  for (const p of report.prescriptions) {
    lines.push(`- ${describeFix(p)} ← ${p.finding_ids.join(', ')}`)
  }
  lines.push(
    '',
    '## How to act (bifrost-platform MCP)',
    '1. `market_data_heal` with `{"dry_run": true}` — preview.',
    '2. `market_data_heal` with `{"dry_run": false}` (or `finding_ids: [...]` for a subset) — enqueues the exact slot for the exact session.',
    '3. Wait for the queue to drain (`get_market_data_plugin_status`), then `market_data_doctor` again; healthy = done.',
    '4. Not auto-fixable: worker unreachable → `rollout_restart_deployment` for that Deployment only; vendor 401/403 → rotate the Polygon key (Owner).',
    '',
    'Without MCP: `curl -s http://127.0.0.1:8780/api/v1/plugins/market-data/api/market/doctor` and',
    '`curl -s -X POST -H "Authorization: Bearer $PLATFORM_OPERATOR_TOKEN" -H "Content-Type: application/json" -d \'{"dry_run":false}\' http://127.0.0.1:8780/api/v1/plugins/market-data/api/market/doctor/heal`',
    '',
    'D10 BLOCKED: observe/data surfaces only — never scale daemon, never touch live trading.',
  )
  return lines.join('\n')
}
