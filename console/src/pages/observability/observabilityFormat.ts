/**
 * Pure helpers / constants for ObservabilityPage chrome.
 * No signal evaluation — display mapping only.
 */

import type {
  AttentionItem,
  DomainHealth,
  EvaluatedSignal,
  GapSummary,
  GrafanaDashboardEntry,
  ObservabilityVerdict,
  SignalGap,
  SignalState,
} from '@/lib/observability'
import { maxVerdict, VERDICT_LABELS } from '@/lib/observability'
import type { SystemDomainId } from '@/lib/architecture/systemDomainCatalog'

export const GAP_LEGEND =
  'ok = matched · fail = unhealthy · blind = probe missing · by-design = optional contract · reference = plane not probed'

export type AttentionScopeFilter = 'all' | 'trade_env' | 'shared'

export const ATTENTION_SCOPE_OPTIONS: { value: AttentionScopeFilter; label: string }[] = [
  { value: 'all', label: 'All' },
  { value: 'trade_env', label: 'Trade env' },
  { value: 'shared', label: 'Shared' },
]

/** Worst verdict among participating runtime domains (ignores pure not_observed). */
export function rollupDomainVerdict(domains: DomainHealth[]): {
  verdict: ObservabilityVerdict
  cause: string
} {
  const participating = domains.filter(d => d.verdict !== 'not_observed')
  if (domains.length === 0) {
    return { verdict: 'not_observed', cause: 'No domains in this plane' }
  }
  if (participating.length === 0) {
    return { verdict: 'not_observed', cause: 'No observed domains' }
  }
  let verdict: ObservabilityVerdict = 'healthy'
  for (const d of participating) {
    verdict = maxVerdict(verdict, d.verdict)
  }
  if (verdict === 'healthy') {
    return { verdict, cause: 'Healthy' }
  }
  const worst = participating.find(d => d.verdict === verdict)
  return {
    verdict,
    cause: worst != null ? `${worst.label}: ${worst.reason}` : VERDICT_LABELS[verdict],
  }
}

export function attentionMatchesScope(item: AttentionItem, filter: AttentionScopeFilter): boolean {
  if (filter === 'all') return true
  if (filter === 'shared') return item.env === 'shared'
  return item.env !== 'shared'
}

export function verdictLamp(v: ObservabilityVerdict) {
  switch (v) {
    case 'healthy':
      return 'ok' as const
    case 'degraded':
      return 'degraded' as const
    case 'critical':
      return 'fail' as const
    default:
      return 'unknown' as const
  }
}

export function verdictTag(v: ObservabilityVerdict): 'success' | 'warning' | 'danger' | 'neutral' {
  switch (v) {
    case 'healthy':
      return 'success'
    case 'degraded':
      return 'warning'
    case 'critical':
      return 'danger'
    default:
      return 'neutral'
  }
}

export function severityLamp(s: AttentionItem['severity']) {
  if (s === 'critical') return 'fail' as const
  if (s === 'warning') return 'degraded' as const
  return 'unknown' as const
}

export function formatFreshness(ms: number | null): string {
  if (ms == null) return 'unknown'
  if (ms < 60_000) return `${Math.round(ms / 1000)}s ago`
  return `${Math.round(ms / 60_000)}m ago`
}

/** Compact scrape age for dense target rows; full ISO stays in title. */
export function formatScrapeAge(iso?: string): string {
  if (iso == null || iso === '') return '—'
  const t = Date.parse(iso)
  if (Number.isNaN(t)) return iso
  return formatFreshness(Date.now() - t)
}

export const scrapeCell =
  '!py-0.5 px-1.5 text-[var(--text-dense-caption)] leading-tight align-middle'

export const SIGNAL_STATE_LABELS: Record<SignalState, string> = {
  healthy: 'HEALTHY',
  degraded: 'DEGRADED',
  critical: 'CRITICAL',
  unknown: 'UNKNOWN',
  not_observed: 'NOT OBSERVED',
  expected_off: 'EXPECTED OFF',
}

export const GAP_TAG_VARIANT: Record<SignalGap, 'success' | 'danger' | 'warning' | 'neutral'> = {
  ok: 'success',
  fail: 'danger',
  blind: 'warning',
  by_design: 'neutral',
}

export const GAP_LABEL: Record<SignalGap, string> = {
  ok: 'ok',
  fail: 'fail',
  blind: 'blind',
  by_design: 'by-design',
}

export function gapPartClass(gap: SignalGap): string {
  switch (gap) {
    case 'ok':
      return 'text-success'
    case 'fail':
      return 'text-danger'
    case 'blind':
      return 'text-warning'
    default:
      return 'text-muted-foreground'
  }
}

/** Domain card line: "4/4 ok" when all ok; else "1 ok · 1 fail · 2 blind" (by_design in tooltip). */
export function formatGapSummaryLine(g: GapSummary): { line: string; title: string } {
  const title = `${g.ok} ok · ${g.fail} fail · ${g.blind} blind · ${g.byDesign} by-design · ${g.total} required`
  if (g.total === 0) return { line: '0 required', title }
  if (g.ok === g.total) return { line: `${g.ok}/${g.total} ok`, title }
  const parts: string[] = []
  if (g.ok > 0) parts.push(`${g.ok} ok`)
  if (g.fail > 0) parts.push(`${g.fail} fail`)
  if (g.blind > 0) parts.push(`${g.blind} blind`)
  // by_design omitted from primary line — surface via title tooltip
  return { line: parts.length > 0 ? parts.join(' · ') : `${g.byDesign} by-design`, title }
}

export type DomainGrafanaLink = { label: string; url: string }

/** Primary catalog dashboard for a domain (card shortcut). */
export function primaryGrafanaForDomain(
  domain: SystemDomainId,
  dashboards: Array<GrafanaDashboardEntry & { available: boolean; url: string | null }>,
): DomainGrafanaLink | null {
  const hit = dashboards.find(d => d.domain === domain && d.available && d.url != null)
  if (hit?.url == null) return null
  return { label: hit.title, url: hit.url }
}

export function checkpointExpect(s: EvaluatedSignal): string {
  return s.def.optionalContract === true ? 'NOT OBSERVED (by design)' : 'HEALTHY'
}
