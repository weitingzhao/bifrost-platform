import { Button, DenseTag, StatusLamp, cn } from '@bifrost/ui'
import type {
  MarketDataDeploymentInfo,
  MarketDataFreshnessInfo,
  MarketDataReadinessRollup,
  MarketDataWorkerInfo,
} from '@/api/satelliteBusTypes'
import {
  MarketDataFreshnessTable,
  MarketDataWorkersTable,
} from '@/components/market-data/MarketDataProbeTables'
import { workerReady } from '@/components/market-data/marketDataProbeUtils'
import {
  DashCard,
  FlashValue,
  Meter,
  ScoreRing,
} from '@/components/market-data/overviewDash'
import { fmtCount, parseReadyRatio, toneByLevel } from '@/components/market-data/overviewDashModel'
import { OpsSection } from '@/components/layout/OpsSection'
import type { MarketDataLiveProbeState } from '@/hooks/useMarketDataLiveProbe'

function freshnessTone(verdict: string): 'ready' | 'thin' | 'blocked' | 'unknown' {
  if (verdict === 'ok') return 'ready'
  if (verdict === 'stale') return 'thin'
  if (verdict === 'fail') return 'blocked'
  return 'unknown'
}

function formatUptime(sec: number | undefined): string {
  if (sec == null || !Number.isFinite(sec)) return '—'
  if (sec < 60) return `${Math.round(sec)}s`
  if (sec < 3600) return `${Math.round(sec / 60)}m`
  return `${(sec / 3600).toFixed(1)}h`
}

export function WorkersFreshnessPanel({
  marketProbe,
  freshness,
  deployments,
  workers,
  readiness,
  onOpenCoverage,
  reachLamp,
  reachTag,
  reachVariant,
}: {
  marketProbe: MarketDataLiveProbeState
  freshness: MarketDataFreshnessInfo[]
  deployments: MarketDataDeploymentInfo[]
  workers: MarketDataWorkerInfo[]
  readiness: MarketDataReadinessRollup | null
  onOpenCoverage?: (panel: 'readiness' | 'financials' | 'quality') => void
  reachLamp: 'ok' | 'degraded' | 'fail' | 'unknown'
  reachTag: string
  reachVariant: 'success' | 'warning' | 'danger' | 'neutral'
}) {
  const freshnessOk = freshness.filter(f => f.verdict === 'ok').length
  const freshnessStale = freshness.filter(f => f.verdict === 'stale').length
  const freshnessFail = freshness.filter(f => f.verdict !== 'ok' && f.verdict !== 'stale').length
  const freshnessAttention = freshness.filter(f => f.verdict !== 'ok')
  const freshnessAllOk =
    freshness.length > 0 && freshnessAttention.length === 0 && !marketProbe.isLoading
  const workersAllReady =
    workers.length > 0 && workers.every(workerReady) && !marketProbe.isLoading

  const poolByName = new Map(workers.map(w => [w.pool, w]))
  const workerCards = deployments.map(d => {
    const poolKey = d.name.replace(/^polygon-worker-/, '')
    return { deploy: d, pool: poolByName.get(poolKey) }
  })
  const jobsDone = workers.reduce((s, w) => s + (w.jobs_done ?? 0), 0)
  const jobsFailed = workers.reduce((s, w) => s + (w.jobs_failed ?? 0), 0)

  const snapPct =
    readiness != null && readiness.snapshot_rows > 0
      ? (readiness.snapshot_covered / readiness.snapshot_rows) * 100
      : 0

  return (
    <OpsSection
      title="Workers & freshness"
      leading={<StatusLamp value={reachLamp} kind="reach" />}
      headerExtra={
        <div className="flex flex-wrap items-center gap-1.5">
          <DenseTag variant={reachVariant}>{reachTag}</DenseTag>
          <DenseTag variant="success">fresh {freshnessOk}</DenseTag>
          {freshnessStale > 0 ? <DenseTag variant="warning">stale {freshnessStale}</DenseTag> : null}
          {freshnessFail > 0 ? <DenseTag variant="danger">other {freshnessFail}</DenseTag> : null}
          <span className="font-mono text-[var(--text-dense-micro)] text-[var(--muted-foreground)]">
            <FlashValue value={jobsDone}>{fmtCount(jobsDone)}</FlashValue> done
            {jobsFailed > 0 ? (
              <>
                {' · '}
                <FlashValue value={jobsFailed} invert>
                  {fmtCount(jobsFailed)}
                </FlashValue>{' '}
                fail
              </>
            ) : null}
          </span>
        </div>
      }
      bodyPadding="compact"
      overflow="visible"
      collapsible
      defaultCollapsed={false}
    >
      <div className="flex flex-col gap-1.5">
        <div className="flex items-stretch gap-2">
          <ScoreRing
            ready={freshnessOk}
            thin={freshnessStale}
            blocked={freshnessFail}
            total={Math.max(freshness.length, 1)}
            caption="fresh"
          />
          <div className="grid min-w-0 flex-1 grid-cols-2 gap-1.5">
            {workerCards.map(({ deploy, pool }) => {
              const ratio = parseReadyRatio(deploy.ready)
              const fill = ratio != null && ratio.d > 0 ? (ratio.n / ratio.d) * 100 : 0
              const tone =
                deploy.reachability === 'ok'
                  ? 'ready'
                  : deploy.reachability === 'degraded'
                    ? 'thin'
                    : deploy.reachability === 'fail'
                      ? 'blocked'
                      : 'unknown'
              return (
                <DashCard
                  key={deploy.name}
                  title={deploy.name.replace(/^polygon-worker-/, '')}
                  tag={deploy.ready ?? deploy.reachability}
                  tagVariant={
                    tone === 'ready'
                      ? 'success'
                      : tone === 'thin'
                        ? 'warning'
                        : tone === 'blocked'
                          ? 'danger'
                          : 'neutral'
                  }
                  value={fmtCount(pool?.jobs_done)}
                  rawValue={pool?.jobs_done}
                  unit="done"
                  caption={`${fmtCount(pool?.jobs_failed)} fail · ${formatUptime(pool?.uptime_sec)}`}
                >
                  <Meter fillPct={fill} toneClass={toneByLevel(tone)} label={`replicas ${deploy.ready}`} />
                </DashCard>
              )
            })}
          </div>
        </div>

        {freshness.length > 0 ? (
          <div className="flex flex-wrap items-center gap-1">
            {freshness.map(row => (
              <span
                key={row.dimension}
                title={`${row.dimension} · ${row.verdict} · ${row.age_hours.toFixed(1)}h`}
                className={cn(
                  'h-3 w-3 rounded-[2px]',
                  toneByLevel(freshnessTone(row.verdict)),
                )}
              />
            ))}
            <span className="text-[var(--text-dense-micro)] text-[var(--muted-foreground)]">
              {freshnessOk}/{freshness.length} dimensions
            </span>
          </div>
        ) : (
          <p className="m-0 text-[var(--text-dense-caption)] text-[var(--muted-foreground)]">
            No ingest_freshness yet
          </p>
        )}

        {freshnessAttention.length > 0 ? (
          <p className="m-0 truncate text-[var(--text-dense-caption)] text-warning" title={freshnessAttention.map(f => `${f.dimension} (${f.verdict})`).join(' · ')}>
            {freshnessAttention.length} not ok:{' '}
            {freshnessAttention.map(f => f.dimension).join(' · ')}
          </p>
        ) : null}

        {readiness != null ? (
          <button
            type="button"
            className="flex items-center gap-2 border-0 bg-transparent p-0 text-left"
            onClick={onOpenCoverage ? () => onOpenCoverage('readiness') : undefined}
            title="Open Coverage → Readiness"
          >
            <span className="w-16 shrink-0 text-[var(--text-dense-micro)] uppercase tracking-wide text-[var(--muted-foreground)]">
              Snapshot
            </span>
            <Meter
              fillPct={snapPct}
              toneClass={readiness.vendor_gap_count > 0 ? toneByLevel('thin') : toneByLevel('ready')}
              label={`snapshot ${readiness.snapshot_covered}/${readiness.snapshot_rows}`}
            />
            <FlashValue
              value={readiness.snapshot_covered}
              className="shrink-0 font-mono text-[var(--text-dense-micro)] tabular-nums text-[var(--muted-foreground)]"
            >
              {fmtCount(readiness.snapshot_covered)}/{fmtCount(readiness.snapshot_rows)}
            </FlashValue>
          </button>
        ) : onOpenCoverage != null ? (
          <Button variant="outline" size="sm" onClick={() => onOpenCoverage('readiness')}>
            Open Readiness
          </Button>
        ) : null}

        <OpsSection
          variant="flat"
          title="Tables"
          collapsible
          defaultCollapsed
          bodyPadding="none"
          overflow="visible"
        >
          {freshness.length > 0 ? (
            <MarketDataFreshnessTable rows={freshness} collapsibleWhenOk={freshnessAllOk} />
          ) : null}
          {deployments.length === 0 && workers.length === 0 ? (
            <p className="m-0 text-[var(--text-dense-caption)] text-[var(--muted-foreground)]">
              No worker snapshot yet
            </p>
          ) : (
            <MarketDataWorkersTable
              deployments={deployments}
              workers={workers}
              collapsibleWhenOk={workersAllReady}
            />
          )}
        </OpsSection>
      </div>
    </OpsSection>
  )
}
