import { DenseTag } from '@bifrost/ui'
import type { MarketDataReachability } from '@/api/satelliteBusTypes'
import { DashCard, Meter, ScoreRing } from '@/components/market-data/overviewDash'
import { toneByLevel } from '@/components/market-data/overviewDashModel'
import { OpsSection } from '@/components/layout/OpsSection'
import type { MarketDataLiveProbeState } from '@/hooks/useMarketDataLiveProbe'

function reachLevel(
  reach: MarketDataReachability | undefined,
): 'ready' | 'thin' | 'blocked' | 'unknown' {
  if (reach === 'ok') return 'ready'
  if (reach === 'degraded') return 'thin'
  if (reach === 'fail') return 'blocked'
  return 'unknown'
}

function reachFill(reach: MarketDataReachability | undefined): number {
  if (reach === 'ok') return 100
  if (reach === 'degraded') return 55
  if (reach === 'fail') return 12
  return 0
}

function reachTagVariant(
  reach: MarketDataReachability | undefined,
): 'success' | 'warning' | 'danger' | 'neutral' {
  if (reach === 'ok') return 'success'
  if (reach === 'degraded') return 'warning'
  if (reach === 'fail') return 'danger'
  return 'neutral'
}

export function ApiReachabilityPanel({
  marketProbe,
}: {
  marketProbe: MarketDataLiveProbeState
}) {
  const status = marketProbe.status
  const overall = marketProbe.isLoading ? 'unknown' : marketProbe.probeReach
  const probes: Array<{
    id: string
    title: string
    path: string
    reach: MarketDataReachability
  }> = [
    {
      id: 'status',
      title: 'Status',
      path: 'GET /api/v1/plugins/market-data/status',
      reach: overall,
    },
    {
      id: 'health',
      title: 'Health',
      path: 'health_reachability',
      reach: status?.health_reachability ?? 'unknown',
    },
    {
      id: 'freshness',
      title: 'Freshness',
      path: 'freshness_reachability',
      reach: status?.freshness_reachability ?? 'unknown',
    },
    {
      id: 'proxy',
      title: 'Proxy',
      path: '/api/v1/plugins/market-data/api/market/*',
      reach: status?.reachable === true ? 'ok' : status?.error ? 'fail' : overall,
    },
  ]

  const ok = probes.filter(p => p.reach === 'ok').length
  const thin = probes.filter(p => p.reach === 'degraded').length
  const blocked = probes.filter(p => p.reach === 'fail').length
  const unknown = probes.filter(p => p.reach === 'unknown').length

  return (
    <OpsSection
      title="API reachability"
      headerExtra={
        <div className="flex flex-wrap items-center gap-1.5">
          <DenseTag variant="success">ok {ok}</DenseTag>
          {thin > 0 ? <DenseTag variant="warning">degraded {thin}</DenseTag> : null}
          {blocked > 0 ? <DenseTag variant="danger">fail {blocked}</DenseTag> : null}
        </div>
      }
      bodyPadding="compact"
      overflow="visible"
      collapsible
      defaultCollapsed={false}
    >
      <div className="flex items-stretch gap-2">
        <ScoreRing
          ready={ok}
          thin={thin}
          blocked={blocked}
          unknown={unknown}
          total={probes.length}
          caption="ok"
        />
        <div className="grid min-w-0 flex-1 grid-cols-2 gap-1.5">
          {probes.map(p => (
            <DashCard
              key={p.id}
              title={p.title}
              tag={p.reach}
              tagVariant={reachTagVariant(p.reach)}
              value={p.reach.toUpperCase()}
              rawValue={p.reach}
              caption={p.path}
            >
              <Meter
                fillPct={reachFill(p.reach)}
                toneClass={toneByLevel(reachLevel(p.reach))}
                label={`${p.title} ${p.reach}`}
              />
            </DashCard>
          ))}
        </div>
      </div>
    </OpsSection>
  )
}
