import { useMemo, useState } from 'react'
import { useQuery } from '@tanstack/react-query'
import { Button, DenseTag } from '@bifrost/ui'
import { fetchQualityScore, isProxyError } from '@/api/marketDataPlugin'
import type { MarketDataWorkerInfo } from '@/api/satelliteBusTypes'
import { AnalyticsDemandPanel } from '@/components/market-data/AnalyticsDemandPanel'
import { ApiReachabilityPanel } from '@/components/market-data/ApiReachabilityPanel'
import { DataVitalsStrip } from '@/components/market-data/DataVitalsStrip'
import {
  buildMassiveAgentPack,
  gatherMassiveAgentSnapshot,
} from '@/components/market-data/massiveAgentPack'
import { WorkersFreshnessPanel } from '@/components/market-data/WorkersFreshnessPanel'
import { sortFreshness } from '@/components/market-data/marketDataProbeUtils'
import { FlashValue } from '@/components/market-data/overviewDash'
import { fmtCount, parseReadyRatio } from '@/components/market-data/overviewDashModel'
import { OpsVerdictStrip } from '@/components/layout/OpsVerdictStrip'
import { HusbandryStrip } from '@/components/delivery/HusbandryStrip'
import type { MarketDataLiveProbeState } from '@/hooks/useMarketDataLiveProbe'

function poolOf(workers: MarketDataWorkerInfo[], pool: string): MarketDataWorkerInfo | undefined {
  return workers.find(w => (w.pool ?? '').toLowerCase() === pool)
}

function reachToVerdict(reach: MarketDataLiveProbeState['probeReach']): {
  lamp: 'ok' | 'degraded' | 'fail' | 'unknown'
  tagLabel: string
  tagVariant: 'success' | 'warning' | 'danger' | 'neutral'
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

export function MarketDataOverviewTab({
  marketProbe,
  onOpenCoverage,
}: {
  marketProbe: MarketDataLiveProbeState
  onOpenCoverage?: (panel: 'readiness' | 'financials' | 'quality') => void
}) {
  const mdReach = marketProbe.isLoading ? 'unknown' : marketProbe.probeReach
  const marketVerdict = reachToVerdict(mdReach)

  const deployments = marketProbe.status?.deployments ?? []
  const workers = marketProbe.status?.workers ?? []
  const freshness = useMemo(
    () => sortFreshness(marketProbe.status?.freshness ?? []),
    [marketProbe.status?.freshness],
  )
  const freshnessOk = freshness.filter(f => f.verdict === 'ok').length
  const readiness = marketProbe.status?.readiness_rollup ?? null
  const stocks = poolOf(workers, 'stocks')
  const options = poolOf(workers, 'options')
  const deployReady = deployments.filter(d => {
    const ratio = parseReadyRatio(d.ready)
    return ratio != null && ratio.d > 0 && ratio.n === ratio.d
  }).length

  const qualityQ = useQuery({
    queryKey: ['market-data', 'coverage', 'quality-score'],
    queryFn: fetchQualityScore,
    refetchInterval: 60_000,
    retry: 1,
  })
  const quality =
    qualityQ.data != null && !isProxyError(qualityQ.data) ? qualityQ.data : null
  const qualitySummary =
    quality?.summary ?? (quality?.ok === true ? 'PASS' : quality != null ? 'FAIL' : null)

  const [copyState, setCopyState] = useState<'idle' | 'busy' | 'copied' | 'error'>('idle')

  async function handleCopyForAgent() {
    if (copyState === 'busy') return
    setCopyState('busy')
    try {
      const snap = await gatherMassiveAgentSnapshot()
      const text = buildMassiveAgentPack(snap)
      await navigator.clipboard.writeText(text)
      setCopyState('copied')
      window.setTimeout(() => setCopyState('idle'), 2000)
    } catch {
      setCopyState('error')
      window.setTimeout(() => setCopyState('idle'), 3000)
    }
  }

  return (
    <div className="flex flex-col gap-2">
      <HusbandryStrip />
      <OpsVerdictStrip
        compact
        ariaLabel="Market Data plugin verdict"
        title="MARKET DATA PLUGIN"
        lamp={marketVerdict.lamp}
        tagLabel={marketVerdict.tagLabel}
        tagVariant={marketVerdict.tagVariant}
        tagTitle="Plugin reach — Coverage quality is a separate axis"
        extraTags={
          qualitySummary != null ? (
            <button
              type="button"
              className="inline-flex border-0 bg-transparent p-0"
              title="Coverage → Quality Score"
              onClick={() => onOpenCoverage?.('quality')}
            >
              <DenseTag variant={qualitySummary === 'PASS' ? 'success' : 'danger'}>
                Quality {qualitySummary}
              </DenseTag>
            </button>
          ) : null
        }
        summary={
          <span className="inline-flex flex-wrap items-center gap-x-1.5">
            <span>
              {deployments.length > 0 ? `${deployReady}/${deployments.length} ready` : marketProbe.summary}
            </span>
            {stocks != null ? (
              <span>
                · stocks{' '}
                <FlashValue value={stocks.jobs_done}>{fmtCount(stocks.jobs_done)}</FlashValue> done
                {stocks.jobs_failed > 0 ? (
                  <>
                    {' '}
                    <FlashValue value={stocks.jobs_failed} invert className="text-destructive">
                      {fmtCount(stocks.jobs_failed)} fail
                    </FlashValue>
                  </>
                ) : null}
              </span>
            ) : null}
            {options != null ? (
              <span>
                · options{' '}
                <FlashValue value={options.jobs_done}>{fmtCount(options.jobs_done)}</FlashValue> done
                {options.jobs_failed > 0 ? (
                  <>
                    {' '}
                    <FlashValue value={options.jobs_failed} invert className="text-destructive">
                      {fmtCount(options.jobs_failed)} fail
                    </FlashValue>
                  </>
                ) : null}
              </span>
            ) : null}
            {freshness.length > 0 ? (
              <span>
                · fresh{' '}
                <FlashValue value={freshnessOk}>
                  {freshnessOk}/{freshness.length}
                </FlashValue>
              </span>
            ) : null}
            {marketProbe.status?.autonomy != null ? (
              <span className="text-[var(--muted-foreground)]">
                · {marketProbe.status.autonomy}
              </span>
            ) : null}
            {marketProbe.status?.health_reachability != null ? (
              <span className="text-[var(--muted-foreground)]">
                · health {marketProbe.status.health_reachability}
              </span>
            ) : null}
          </span>
        }
        actions={
          <div className="flex flex-wrap items-center gap-1.5">
            <Button
              variant="outline"
              size="sm"
              disabled={copyState === 'busy'}
              title="Copy a repair pack (husbandry + queue + vitals + analytics demand) for an AI agent"
              onClick={() => void handleCopyForAgent()}
            >
              {copyState === 'busy'
                ? 'Exporting…'
                : copyState === 'copied'
                  ? 'Copied!'
                  : copyState === 'error'
                    ? 'Copy failed'
                    : 'Copy for Agent'}
            </Button>
            <Button
              variant="outline"
              size="sm"
              disabled={marketProbe.isLoading}
              onClick={() => marketProbe.refetch()}
            >
              Refresh
            </Button>
          </div>
        }
      />

      <DataVitalsStrip onOpenCoverage={onOpenCoverage} />

      <AnalyticsDemandPanel freshness={freshness} onOpenCoverage={onOpenCoverage} />

      <div className="grid grid-cols-1 gap-2 xl:grid-cols-[minmax(0,1.15fr)_minmax(0,0.85fr)]">
        <WorkersFreshnessPanel
          marketProbe={marketProbe}
          freshness={freshness}
          deployments={deployments}
          workers={workers}
          readiness={readiness}
          onOpenCoverage={onOpenCoverage}
          reachLamp={marketVerdict.lamp}
          reachTag={marketVerdict.tagLabel}
          reachVariant={marketVerdict.tagVariant}
        />
        <ApiReachabilityPanel marketProbe={marketProbe} />
      </div>
    </div>
  )
}
