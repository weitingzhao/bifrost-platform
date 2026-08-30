/**
 * Deep links from Research Engine Feedstock → Massive Readiness / Flex manage.
 * Console uses hash tabs + native ?tab=&panel= search (no react-router).
 */

import { writeMdSearchParams } from '@/components/market-data/quality/mdNavParams'
import type { MarketDataReadinessRollup } from '@/api/satelliteBusTypes'

export const MASSIVE_MANAGE_TAB = 'market-data-manage'
export const FLEX_MANAGE_TAB = 'flex-query-manage'

/** Full URL for Open Massive (HusbandryStrip / new-tab fallback). */
export function massiveReadinessHref(): string {
  return '/?tab=coverage&panel=readiness#market-data-manage'
}

/** Full URL for Massive Ingest (queue dashboard). */
export function massiveIngestHref(): string {
  return '/?tab=ingest#market-data-manage'
}

export function flexManageHref(): string {
  return '/#flex-query-manage'
}

/** In-app navigate: set readiness search params then switch hash tab. */
export function openMassiveReadiness(onNavigate?: (tabId: string) => void): void {
  writeMdSearchParams({ tab: 'coverage', panel: 'readiness' })
  if (onNavigate != null) {
    onNavigate(MASSIVE_MANAGE_TAB)
    return
  }
  window.location.assign(massiveReadinessHref())
}

/** In-app navigate: Massive → Ingest (shell Queue pulse deep link). */
export function openMassiveIngest(onNavigate?: (tabId: string) => void): void {
  writeMdSearchParams({ tab: 'ingest', panel: null })
  if (onNavigate != null) {
    onNavigate(MASSIVE_MANAGE_TAB)
    return
  }
  window.location.assign(massiveIngestHref())
}

export function openFlexManage(onNavigate?: (tabId: string) => void): void {
  if (onNavigate != null) {
    onNavigate(FLEX_MANAGE_TAB)
    return
  }
  window.location.assign(flexManageHref())
}

/** One-line Massive coverage KPI for Feedstock (fail-soft). */
export function formatReadinessRollupLine(
  rollup: MarketDataReadinessRollup | null | undefined,
): string | null {
  if (rollup == null) return null
  const covered = rollup.snapshot_covered
  const universe = rollup.universe
  const gap = rollup.vendor_gap_count
  const asOf = rollup.as_of ? formatShortAsOf(rollup.as_of) : '—'
  return `snap ${covered}/${universe} · vendor_gap ${gap} · as_of ${asOf}`
}

function formatShortAsOf(iso: string): string {
  try {
    return new Date(iso).toLocaleString()
  } catch {
    return iso
  }
}
