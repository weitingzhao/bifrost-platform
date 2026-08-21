import { useCallback } from 'react'
import { Button, DenseTag, StatusLamp } from '@bifrost/ui'
import { OpsSection } from '@/components/layout/OpsSection'
import {
  OpsVerdictStrip,
  type OpsVerdictLamp,
  type OpsVerdictTagVariant,
} from '@/components/layout/OpsVerdictStrip'
import { useIbGatewayLiveProbe } from '@/hooks/useIbGatewayLiveProbe'
import { useMarketDataLiveProbe } from '@/hooks/useMarketDataLiveProbe'
import { useFlexQueryLiveProbe } from '@/hooks/useFlexQueryLiveProbe'

type PluginRegistryEntry = {
  id: string
  name: string
  vendor: string
  role: string
  lifecycle: 'live' | 'planned'
  openTabId: string | null
  openLabel: string | null
}

const PLUGIN_REGISTRY: PluginRegistryEntry[] = [
  {
    id: 'ib-gateway',
    name: 'IB Client',
    vendor: 'Interactive Brokers',
    role: 'TWS socket bridge · redis-ib @ data NS',
    lifecycle: 'live',
    openTabId: 'ib-gateway-manage',
    openLabel: 'Open IB Client',
  },
  {
    id: 'market-data',
    name: 'Massive',
    vendor: 'Polygon.io',
    role: 'REST ingest · stock/option bars + snapshots · redis-massive · PG-as-broker @ plugin-market-data NS',
    lifecycle: 'live',
    openTabId: 'market-data-manage',
    openLabel: 'Open Massive',
  },
  {
    id: 'flex-query',
    name: 'IB Flex',
    vendor: 'Interactive Brokers',
    role: 'Flex Web Service ingest · trades/cash → brokerage.* @ plugin-flex-query NS',
    lifecycle: 'live',
    openTabId: 'flex-query-manage',
    openLabel: 'Open IB Flex',
  },
  {
    id: 'analytics-pipeline',
    name: 'Analytics',
    vendor: 'dbt + Elementary',
    role: 'SEPA dbt marts · Elementary lineage/catalog report · CronJob @ plugin-market-data NS',
    lifecycle: 'live',
    openTabId: 'analytics-pipeline',
    openLabel: 'Open Analytics',
  },
]

function reachLabel(reach: 'ok' | 'degraded' | 'fail' | 'unknown', loading: boolean): string {
  if (loading) return 'PROBING'
  switch (reach) {
    case 'ok':
      return 'OK'
    case 'degraded':
      return 'DEGRADED'
    case 'fail':
      return 'FAIL'
    default:
      return 'UNKNOWN'
  }
}

function worseReach(
  a: 'ok' | 'degraded' | 'fail' | 'unknown',
  b: 'ok' | 'degraded' | 'fail' | 'unknown',
): 'ok' | 'degraded' | 'fail' | 'unknown' {
  const order = { ok: 0, unknown: 1, degraded: 2, fail: 3 } as const
  return order[b] > order[a] ? b : a
}

function reachToVerdict(reach: 'ok' | 'degraded' | 'fail' | 'unknown'): {
  lamp: OpsVerdictLamp
  tagLabel: string
  tagVariant: OpsVerdictTagVariant
} {
  switch (reach) {
    case 'ok':
      return { lamp: 'ok', tagLabel: 'OK', tagVariant: 'success' }
    case 'degraded':
      return { lamp: 'degraded', tagLabel: 'DEGRADED', tagVariant: 'warning' }
    case 'fail':
      return { lamp: 'fail', tagLabel: 'FAIL', tagVariant: 'danger' }
    default:
      return { lamp: 'unknown', tagLabel: 'UNKNOWN', tagVariant: 'neutral' }
  }
}

/**
 * Subcontractors → Plugin Gallery — registry + bus rollup only.
 * Runtime detail: IB Gateway / Market Data manage pages. Publish: Launch Desk → Plugin.
 */
export function PluginGalleryPage({ onNavigate }: { onNavigate?: (tabId: string) => void } = {}) {
  const liveProbe = useIbGatewayLiveProbe()
  const marketProbe = useMarketDataLiveProbe()
  const flexProbe = useFlexQueryLiveProbe()

  const liveCount = PLUGIN_REGISTRY.filter(p => p.lifecycle === 'live').length
  const plannedCount = PLUGIN_REGISTRY.filter(p => p.lifecycle === 'planned').length

  const busReach =
    liveProbe.isLoading || marketProbe.isLoading || flexProbe.isLoading
      ? 'unknown'
      : worseReach(worseReach(liveProbe.probeReach, marketProbe.probeReach), flexProbe.probeReach)
  const busVerdict = reachToVerdict(busReach)
  const busSummary = `IB ${reachLabel(liveProbe.probeReach, liveProbe.isLoading)} · Massive ${reachLabel(marketProbe.probeReach, marketProbe.isLoading)} · Flex ${reachLabel(flexProbe.probeReach, flexProbe.isLoading)}`

  const refreshBoth = useCallback(() => {
    liveProbe.refetch()
    marketProbe.refetch()
    flexProbe.refetch()
  }, [liveProbe, marketProbe, flexProbe])

  return (
    <div className="flex w-full min-w-0 flex-col gap-4">
      <OpsVerdictStrip
        ariaLabel="Plugin bus verdict"
        title="PLUGIN BUS"
        lamp={busVerdict.lamp}
        tagLabel={busVerdict.tagLabel}
        tagVariant={busVerdict.tagVariant}
        summary={busSummary}
        actions={
          <>
            <Button
              variant="outline"
              size="sm"
              disabled={liveProbe.isLoading || marketProbe.isLoading || flexProbe.isLoading}
              onClick={refreshBoth}
            >
              Refresh
            </Button>
            <Button variant="outline" size="sm" onClick={() => onNavigate?.('plugin-release')}>
              Need publish?
            </Button>
          </>
        }
        meta={
          <span>
            {liveCount} live · {plannedCount} planned · detail on manage pages
          </span>
        }
      />

      <OpsSection
        title="Plugin registry"
        description="Directory only — open IB Client / Massive / IB Flex for probes; Launch Desk → Plugin to publish."
        bodyPadding="default"
        overflow="visible"
      >
        <div className="grid gap-2 sm:grid-cols-2">
          {PLUGIN_REGISTRY.map(plugin => (
            <div
              key={plugin.id}
              className="rounded-md border border-[var(--border)] bg-[var(--secondary)] px-3 py-2"
            >
              <div className="flex flex-wrap items-center gap-2">
                {plugin.id === 'ib-gateway' ? (
                  <StatusLamp
                    value={liveProbe.isLoading ? 'unknown' : liveProbe.probeReach}
                    kind="reach"
                  />
                ) : null}
                {plugin.id === 'market-data' ? (
                  <StatusLamp
                    value={marketProbe.isLoading ? 'unknown' : marketProbe.probeReach}
                    kind="reach"
                  />
                ) : null}
                {plugin.id === 'flex-query' ? (
                  <StatusLamp
                    value={flexProbe.isLoading ? 'unknown' : flexProbe.probeReach}
                    kind="reach"
                  />
                ) : null}
                <span className="text-[var(--text-dense-label)] font-semibold">{plugin.name}</span>
                <DenseTag variant="neutral">{plugin.lifecycle}</DenseTag>
              </div>
              <p className="m-0 mt-1 text-[var(--text-dense-meta)] text-[var(--muted-foreground)]">
                {plugin.vendor} · {plugin.role}
              </p>
              {plugin.openTabId != null && plugin.openLabel != null && onNavigate != null ? (
                <div className="mt-2">
                  <Button
                    variant="outline"
                    size="sm"
                    onClick={() => onNavigate(plugin.openTabId as string)}
                  >
                    {plugin.openLabel}
                  </Button>
                </div>
              ) : null}
            </div>
          ))}
        </div>
      </OpsSection>
    </div>
  )
}
