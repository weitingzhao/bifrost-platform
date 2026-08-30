/**
 * URL helpers for Market Data manage page deep links.
 * Uses ?tab=coverage&panel=readiness (native search params; console has no react-router).
 */

export type MarketDataManageTab = 'overview' | 'coverage' | 'ingest' | 'analytics'

export type CoverageDetailPanel =
  | 'quality'
  | 'readiness'
  | 'financials'
  | 'db-summary'
  | 'capability'

const VALID_TABS = new Set<MarketDataManageTab>([
  'overview',
  'coverage',
  'ingest',
  'analytics',
])

const VALID_PANELS = new Set<CoverageDetailPanel>([
  'quality',
  'readiness',
  'financials',
  'db-summary',
  'capability',
])

export function readMdSearchParams(search = window.location.search): {
  tab: MarketDataManageTab | null
  panel: CoverageDetailPanel | null
} {
  const q = new URLSearchParams(search)
  const tabRaw = q.get('tab')
  const panelRaw = q.get('panel')
  const tab =
    tabRaw != null && VALID_TABS.has(tabRaw as MarketDataManageTab)
      ? (tabRaw as MarketDataManageTab)
      : null
  const panel =
    panelRaw != null && VALID_PANELS.has(panelRaw as CoverageDetailPanel)
      ? (panelRaw as CoverageDetailPanel)
      : null
  return { tab, panel }
}

export function writeMdSearchParams(next: {
  tab?: MarketDataManageTab
  panel?: CoverageDetailPanel | null
}): void {
  const url = new URL(window.location.href)
  if (next.tab != null) url.searchParams.set('tab', next.tab)
  if (next.panel === null) {
    url.searchParams.delete('panel')
  } else if (next.panel != null) {
    url.searchParams.set('panel', next.panel)
  }
  window.history.replaceState({}, '', url.toString())
}

/**
 * JSON Probe is an engineer contract escape hatch — not Mission Control narrative.
 * Enable with `?debug=1` (or `debug=true`) on Market Data manage URLs.
 */
export function isMdDebugProbeEnabled(search = window.location.search): boolean {
  const raw = new URLSearchParams(search).get('debug')
  if (raw == null) return false
  const v = raw.trim().toLowerCase()
  return v === '1' || v === 'true' || v === 'yes'
}
