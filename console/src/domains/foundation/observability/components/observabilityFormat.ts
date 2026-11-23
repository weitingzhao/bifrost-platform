import { STATUS } from '@bifrost/ui/city/world'
import { STATE_WORD, TAG_VARIANT, type StatusWord } from '@/lib/shell/stateWords'
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
} from '@/domains/foundation/observability'
import { maxVerdict, VERDICT_LABELS } from '@/domains/foundation/observability'
import type { SystemDomainId } from '@/lib/architecture/systemDomainCatalog'

/**
 * A required signal's gap in the engine's seven words (箭10 2026-11-23): blind (the probe is missing) cannot be
 * judged, so unknown; by design (an optional contract) and the reference planes are not faults, so boundary.
 */
export const GAP_WORD: Record<SignalGap, StatusWord> = { ok: 'ok', fail: 'fail', blind: 'unknown', by_design: 'boundary' }

/** The legend: only the words this section uses, each in the WorldFrame legend's own words. */
export const GAP_LEGEND = (['ok', 'fail', 'unknown', 'boundary'] as const)
  .map(w => `${w} — ${STATUS[w].means}`)
  .join(' · ') + ' (here: by design · no runtime contract)'

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
  healthy: STATE_WORD.ok,
  degraded: STATE_WORD.warn,
  critical: STATE_WORD.fail,
  unknown: STATE_WORD.unknown,
  not_observed: STATE_WORD.boundary,
  expected_off: STATE_WORD.boundary,
}

export const GAP_TAG_VARIANT: Record<SignalGap, 'success' | 'danger' | 'warning' | 'neutral'> = {
  ok: TAG_VARIANT[GAP_WORD.ok],
  fail: TAG_VARIANT[GAP_WORD.fail],
  blind: TAG_VARIANT[GAP_WORD.blind],
  by_design: TAG_VARIANT[GAP_WORD.by_design],
}

export const GAP_LABEL: Record<SignalGap, string> = GAP_WORD

export function gapPartClass(gap: SignalGap): string {
  switch (gap) {
    case 'ok':
      return 'text-success'
    case 'fail':
      return 'text-danger'
    case 'blind':
      return 'text-muted-foreground'
    default:
      return 'text-muted-foreground'
  }
}

/** Every gap count in the seven words, for a tooltip: "3 ok · 1 fail · 0 unknown · 1 boundary · 5 required". */
export function gapSummaryTitle(g: GapSummary): string {
  return `${g.ok} ${GAP_WORD.ok} · ${g.fail} ${GAP_WORD.fail} · ${g.blind} ${GAP_WORD.blind} · ${g.byDesign} ${GAP_WORD.by_design} · ${g.total} required`
}

/** Domain card line: "4/4 ok" when all ok; else "1 ok · 1 fail · 2 unknown" (boundary in the tooltip). */
export function formatGapSummaryLine(g: GapSummary): { line: string; title: string; parts: { gap: SignalGap; text: string }[] } {
  const title = gapSummaryTitle(g)
  if (g.total === 0) return { line: '0 required', title, parts: [] }
  if (g.ok === g.total) return { line: `${g.ok}/${g.total} ok`, title, parts: [{ gap: 'ok', text: `${g.ok}/${g.total} ok` }] }
  const parts: { gap: SignalGap; text: string }[] = []
  if (g.ok > 0) parts.push({ gap: 'ok', text: `${g.ok} ${GAP_WORD.ok}` })
  if (g.fail > 0) parts.push({ gap: 'fail', text: `${g.fail} ${GAP_WORD.fail}` })
  if (g.blind > 0) parts.push({ gap: 'blind', text: `${g.blind} ${GAP_WORD.blind}` })
  // by_design omitted from primary line — surface via title tooltip
  if (parts.length === 0) parts.push({ gap: 'by_design', text: `${g.byDesign} ${GAP_WORD.by_design}` })
  return { line: parts.map(p => p.text).join(' · '), title, parts }
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
  return s.def.optionalContract === true ? `${STATE_WORD.boundary} · not observed by design` : STATE_WORD.ok
}
