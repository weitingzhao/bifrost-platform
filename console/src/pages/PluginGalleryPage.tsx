import { useCallback, useMemo, useState } from 'react'
import {
  Button,
  ConfirmDialog,
  DenseDataTable,
  DenseTableBody,
  DenseTableCell,
  DenseTableHead,
  DenseTableHeadRow,
  DenseTableHeader,
  DenseTableRow,
  DenseTag,
  StatusLamp,
} from '@bifrost/ui'
import { ExternalLink } from 'lucide-react'
import { postIbGatewayControl } from '@/api/network'
import type {
  MarketDataDeploymentInfo,
  MarketDataFreshnessInfo,
  MarketDataWorkerInfo,
} from '@/api/satelliteBusTypes'
import { IbGatewayCutoverStatusPanel } from '@/components/cluster/IbGatewayCutoverStatusPanel'
import { IbGatewayLiveStatusPanel } from '@/components/cluster/IbGatewayLiveStatusPanel'
import { OpsFeedback } from '@/components/feedback/OpsFeedback'
import { OpsSection, OpsSubsectionTitle } from '@/components/layout/OpsSection'
import {
  OpsVerdictStrip,
  type OpsVerdictLamp,
  type OpsVerdictTagVariant,
} from '@/components/layout/OpsVerdictStrip'
import { useIbGatewayLiveProbe } from '@/hooks/useIbGatewayLiveProbe'
import { useMarketDataLiveProbe } from '@/hooks/useMarketDataLiveProbe'
import { usePlatformAuth } from '@/hooks/usePlatformAuth'
import { TRADE_FRONTEND_URL_DEFAULT } from '@/lib/standards/designSystemCatalog'

const PLUGIN_REGISTRY = [
  {
    id: 'ib-gateway',
    name: 'IB Gateway',
    vendor: 'Interactive Brokers',
    role: 'TWS socket bridge · redis-ib @ data NS',
    lifecycle: 'live',
  },
  {
    id: 'market-data',
    name: 'Market Data (Polygon)',
    vendor: 'Polygon.io',
    role: 'REST ingest · stock/option bars + snapshots · PG-as-broker @ plugin-market-data NS',
    lifecycle: 'live',
  },
  {
    id: 'flex-query',
    name: 'IB Flex Query',
    vendor: 'Interactive Brokers',
    role: 'Planned subcontractor plugin',
    lifecycle: 'planned',
  },
] as const

const STOCK_DATA_READINESS_PATH = '/research/stock-data'

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

function freshnessVerdictVariant(
  verdict: string,
): 'success' | 'warning' | 'danger' | 'neutral' | 'info' {
  if (verdict === 'ok') return 'success'
  if (verdict === 'stale') return 'warning'
  if (verdict === 'fail') return 'danger'
  return 'neutral'
}

function sortFreshness(rows: MarketDataFreshnessInfo[]): MarketDataFreshnessInfo[] {
  const rank = (v: string) => {
    if (v === 'stale') return 0
    if (v === 'fail') return 1
    if (v === 'unknown') return 2
    return 3
  }
  return [...rows].sort((a, b) => {
    const d = rank(a.verdict) - rank(b.verdict)
    if (d !== 0) return d
    return a.dimension.localeCompare(b.dimension)
  })
}

function workerReady(w: MarketDataWorkerInfo): boolean {
  return w.status == null || w.status === '' || w.status.toLowerCase() === 'ok'
}

function formatUptime(sec: number | undefined): string {
  if (sec == null || !Number.isFinite(sec)) return '—'
  if (sec < 60) return `${Math.round(sec)}s`
  if (sec < 3600) return `${Math.round(sec / 60)}m`
  return `${(sec / 3600).toFixed(1)}h`
}

function FreshnessTable({
  rows,
  collapsibleWhenOk,
}: {
  rows: MarketDataFreshnessInfo[]
  collapsibleWhenOk: boolean
}) {
  const table = (
    <DenseDataTable>
      <DenseTableHeader>
        <DenseTableHeadRow>
          <DenseTableHead>Dimension</DenseTableHead>
          <DenseTableHead>Verdict</DenseTableHead>
          <DenseTableHead>Age</DenseTableHead>
          <DenseTableHead>Last run</DenseTableHead>
          <DenseTableHead>Rows</DenseTableHead>
          <DenseTableHead>Status</DenseTableHead>
        </DenseTableHeadRow>
      </DenseTableHeader>
      <DenseTableBody>
        {rows.map(f => (
          <DenseTableRow key={f.dimension}>
            <DenseTableCell className="font-mono text-xs">{f.dimension}</DenseTableCell>
            <DenseTableCell>
              <DenseTag variant={freshnessVerdictVariant(f.verdict)}>{f.verdict}</DenseTag>
            </DenseTableCell>
            <DenseTableCell className="font-mono tabular-nums">
              {Number.isFinite(f.age_hours) ? `${f.age_hours.toFixed(1)}h` : '—'}
            </DenseTableCell>
            <DenseTableCell className="font-mono text-xs">
              {f.last_run_at != null && f.last_run_at !== '' ? f.last_run_at : '—'}
            </DenseTableCell>
            <DenseTableCell className="font-mono tabular-nums">{f.rows_written}</DenseTableCell>
            <DenseTableCell>{f.status ?? '—'}</DenseTableCell>
          </DenseTableRow>
        ))}
      </DenseTableBody>
    </DenseDataTable>
  )
  if (!collapsibleWhenOk) return table
  return (
    <OpsSection
      variant="flat"
      title="All dimensions ok"
      collapsible
      defaultCollapsed
      bodyPadding="none"
      overflow="visible"
    >
      {table}
    </OpsSection>
  )
}

function WorkersTable({
  deployments,
  workers,
  collapsibleWhenOk,
}: {
  deployments: MarketDataDeploymentInfo[]
  workers: MarketDataWorkerInfo[]
  collapsibleWhenOk: boolean
}) {
  const table = (
    <DenseDataTable>
      <DenseTableHeader>
        <DenseTableHeadRow>
          <DenseTableHead>Name / pool</DenseTableHead>
          <DenseTableHead>Ready / status</DenseTableHead>
          <DenseTableHead>Done / fail</DenseTableHead>
          <DenseTableHead>Uptime</DenseTableHead>
          <DenseTableHead>Last claim</DenseTableHead>
        </DenseTableHeadRow>
      </DenseTableHeader>
      <DenseTableBody>
        {deployments.map(d => (
          <DenseTableRow key={`deploy-${d.name}`}>
            <DenseTableCell className="font-semibold">{d.name}</DenseTableCell>
            <DenseTableCell>
              <DenseTag
                variant={
                  d.reachability === 'ok'
                    ? 'success'
                    : d.reachability === 'degraded'
                      ? 'warning'
                      : d.reachability === 'fail'
                        ? 'danger'
                        : 'neutral'
                }
              >
                {d.ready}
              </DenseTag>
            </DenseTableCell>
            <DenseTableCell className="text-[var(--muted-foreground)]">—</DenseTableCell>
            <DenseTableCell className="text-[var(--muted-foreground)]">—</DenseTableCell>
            <DenseTableCell className="text-[var(--text-dense-meta)] text-[var(--muted-foreground)]">
              {d.detail ?? '—'}
            </DenseTableCell>
          </DenseTableRow>
        ))}
        {workers.map(w => (
          <DenseTableRow key={`pool-${w.pool}`}>
            <DenseTableCell className="font-mono text-xs">pool {w.pool}</DenseTableCell>
            <DenseTableCell>
              <DenseTag variant={workerReady(w) ? 'success' : 'warning'}>
                {w.status ?? 'ok'}
              </DenseTag>
            </DenseTableCell>
            <DenseTableCell className="font-mono tabular-nums">
              {w.jobs_done} / {w.jobs_failed}
            </DenseTableCell>
            <DenseTableCell className="font-mono tabular-nums">
              {formatUptime(w.uptime_sec)}
            </DenseTableCell>
            <DenseTableCell className="font-mono text-xs">
              {w.last_claim_at != null && w.last_claim_at !== '' ? w.last_claim_at : '—'}
            </DenseTableCell>
          </DenseTableRow>
        ))}
      </DenseTableBody>
    </DenseDataTable>
  )
  if (!collapsibleWhenOk) return table
  return (
    <OpsSection
      variant="flat"
      title="All workers ready"
      collapsible
      defaultCollapsed
      bodyPadding="none"
      overflow="visible"
    >
      {table}
    </OpsSection>
  )
}

export function PluginGalleryPage({ onNavigate }: { onNavigate?: (tabId: string) => void } = {}) {
  const liveProbe = useIbGatewayLiveProbe()
  const marketProbe = useMarketDataLiveProbe()
  const { canOperate } = usePlatformAuth()
  const [reconnectOpen, setReconnectOpen] = useState(false)
  const [acting, setActing] = useState(false)
  const [actionMsg, setActionMsg] = useState<string | null>(null)
  const [actionFailed, setActionFailed] = useState(false)

  const liveCount = PLUGIN_REGISTRY.filter(p => p.lifecycle === 'live').length
  const plannedCount = PLUGIN_REGISTRY.filter(p => p.lifecycle === 'planned').length

  const ibReach = liveProbe.isLoading ? 'unknown' : liveProbe.probeReach
  const mdReach = marketProbe.isLoading ? 'unknown' : marketProbe.probeReach
  const busReach =
    liveProbe.isLoading || marketProbe.isLoading
      ? 'unknown'
      : worseReach(liveProbe.probeReach, marketProbe.probeReach)
  const busVerdict = reachToVerdict(busReach)
  const ibVerdict = reachToVerdict(ibReach)
  const marketVerdict = reachToVerdict(mdReach)

  const busSummary = `IB ${reachLabel(liveProbe.probeReach, liveProbe.isLoading)} · Market Data ${reachLabel(marketProbe.probeReach, marketProbe.isLoading)}`

  const cutoverOk =
    liveProbe.status?.cutover?.reachability === 'ok' ||
    liveProbe.status?.cutover?.legacy_socket_retired === true
  const ibSectionHealthy = !liveProbe.isLoading && liveProbe.probeReach === 'ok' && cutoverOk

  const deployments = marketProbe.status?.deployments ?? []
  const workers = marketProbe.status?.workers ?? []
  const freshness = useMemo(
    () => sortFreshness(marketProbe.status?.freshness ?? []),
    [marketProbe.status?.freshness],
  )
  const freshnessOk = freshness.filter(f => f.verdict === 'ok').length
  const freshnessAttention = freshness.filter(f => f.verdict !== 'ok')
  const freshnessAllOk =
    freshness.length > 0 && freshnessAttention.length === 0 && !marketProbe.isLoading
  const workersAllReady =
    workers.length > 0 && workers.every(workerReady) && !marketProbe.isLoading
  const marketSectionHealthy =
    !marketProbe.isLoading && marketProbe.probeReach === 'ok' && freshnessAllOk && workersAllReady

  const readiness = marketProbe.status?.readiness_rollup ?? null
  const tradeBase = (
    (import.meta.env.VITE_TRADE_FRONTEND_URL as string | undefined)?.trim() ||
    TRADE_FRONTEND_URL_DEFAULT
  ).replace(/\/$/, '')
  const stockReadinessHref = `${tradeBase}${STOCK_DATA_READINESS_PATH}`

  const runReconnect = useCallback(async () => {
    setActing(true)
    setActionMsg(null)
    setActionFailed(false)
    try {
      const resp = await postIbGatewayControl('reconnect')
      setActionFailed(!resp.ok)
      setActionMsg(resp.ok ? resp.message : `Failed: ${resp.message}`)
      if (resp.ok) liveProbe.refetch()
    } catch (e) {
      setActionFailed(true)
      setActionMsg(e instanceof Error ? e.message : 'Reconnect failed')
    } finally {
      setActing(false)
      setReconnectOpen(false)
    }
  }, [liveProbe])

  const refreshBoth = useCallback(() => {
    liveProbe.refetch()
    marketProbe.refetch()
  }, [liveProbe, marketProbe])

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
              disabled={liveProbe.isLoading || marketProbe.isLoading}
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
            {liveCount} live · {plannedCount} planned
          </span>
        }
      />

      {actionMsg != null ? (
        <OpsFeedback variant={actionFailed ? 'error' : 'success'} title="Reconnect">
          {actionMsg}
        </OpsFeedback>
      ) : null}

      <OpsSection
        title="IB Gateway"
        description="Live probe + Trade cutover — observe / reconnect (Gallery ≠ Publish)."
        leading={<StatusLamp value={ibReach} kind="reach" />}
        headerExtra={
          <div className="flex flex-wrap items-center gap-2">
            <DenseTag variant={ibVerdict.tagVariant}>{ibVerdict.tagLabel}</DenseTag>
            {liveProbe.status?.mode != null && liveProbe.status.mode !== '' ? (
              <DenseTag variant={liveProbe.status.mode === 'live' ? 'warning' : 'neutral'}>
                mode {liveProbe.status.mode}
              </DenseTag>
            ) : null}
            {liveProbe.status?.deployment?.ready != null && liveProbe.status.deployment.ready !== '' ? (
              <span className="text-[var(--text-dense-meta)] text-[var(--muted-foreground)]">
                deployment {liveProbe.status.deployment.ready}
              </span>
            ) : null}
          </div>
        }
        actions={
          canOperate ? (
            <Button
              variant="outline"
              size="sm"
              disabled={acting}
              onClick={() => setReconnectOpen(true)}
            >
              Reconnect
            </Button>
          ) : undefined
        }
        bodyPadding="default"
        overflow="visible"
        collapsible
        defaultCollapsed={ibSectionHealthy}
      >
        <div className="flex flex-col gap-3">
          <IbGatewayLiveStatusPanel showPrimaryActions={false} embedded />
          <IbGatewayCutoverStatusPanel embedded />
        </div>
      </OpsSection>

      <OpsSection
        title="Market Data · Polygon"
        description="Worker health + ingest freshness @ plugin-market-data NS · L0 observe."
        leading={<StatusLamp value={mdReach} kind="reach" />}
        headerExtra={
          <div className="flex flex-wrap items-center gap-2">
            <DenseTag variant={marketVerdict.tagVariant}>{marketVerdict.tagLabel}</DenseTag>
            <span className="text-[var(--text-dense-meta)] text-[var(--muted-foreground)]">
              freshness {freshness.length > 0 ? `${freshnessOk}/${freshness.length} ok` : '—'}
            </span>
            {marketProbe.status?.autonomy != null ? (
              <DenseTag variant="info">{marketProbe.status.autonomy}</DenseTag>
            ) : null}
          </div>
        }
        actions={
          <Button
            variant="outline"
            size="sm"
            disabled={marketProbe.isLoading}
            onClick={() => marketProbe.refetch()}
          >
            Refresh
          </Button>
        }
        bodyPadding="default"
        overflow="visible"
        collapsible
        defaultCollapsed={marketSectionHealthy}
      >
        <div className="flex flex-col gap-4">
          {freshnessAttention.length > 0 ? (
            <div
              className="rounded-md border border-[color-mix(in_srgb,var(--warning)_45%,var(--border))] bg-[color-mix(in_srgb,var(--warning)_10%,var(--secondary))] px-3 py-2"
              role="status"
            >
              <p className="m-0 text-[var(--text-dense-label)] font-semibold">Attention</p>
              <p className="m-0 mt-1 text-[var(--text-dense-meta)] text-[var(--muted-foreground)]">
                {freshnessAttention.length} dimension
                {freshnessAttention.length === 1 ? '' : 's'} not ok:{' '}
                {freshnessAttention.map(f => `${f.dimension} (${f.verdict})`).join(' · ')}
              </p>
            </div>
          ) : null}

          <div className="flex flex-col gap-2">
            <OpsSubsectionTitle>Freshness</OpsSubsectionTitle>
            {freshness.length === 0 ? (
              <p className="m-0 text-[var(--text-dense-meta)] text-[var(--muted-foreground)]">
                No ingest_freshness rows yet — run workers / daily CronJobs, then refresh.
                {marketProbe.status?.freshness_reachability != null
                  ? ` · reach ${marketProbe.status.freshness_reachability}`
                  : ''}
              </p>
            ) : (
              <FreshnessTable rows={freshness} collapsibleWhenOk={freshnessAllOk} />
            )}
          </div>

          <div className="flex flex-col gap-2">
            <OpsSubsectionTitle>Workers</OpsSubsectionTitle>
            {deployments.length === 0 && workers.length === 0 ? (
              <p className="m-0 text-[var(--text-dense-meta)] text-[var(--muted-foreground)]">
                No deployment / worker snapshot yet — apply k8s/base or check platform-api probe.
              </p>
            ) : (
              <WorkersTable
                deployments={deployments}
                workers={workers}
                collapsibleWhenOk={workersAllReady}
              />
            )}
          </div>

          {readiness != null ? (
            <div className="rounded-md border border-[var(--border)] bg-[var(--secondary)] px-3 py-2">
              <div className="flex flex-wrap items-center justify-between gap-2">
                <div className="min-w-0">
                  <p className="m-0 text-[var(--text-dense-label)] font-semibold">
                    Data Readiness
                  </p>
                  <p className="m-0 mt-0.5 text-[var(--text-dense-meta)] text-[var(--muted-foreground)]">
                    Read-only snapshot from Trade · Universe {readiness.universe} · Price ready{' '}
                    {readiness.price_ready} · Fund cache {readiness.fund_cache_valid} · as-of{' '}
                    {readiness.as_of}
                  </p>
                </div>
                <Button variant="outline" size="sm" asChild>
                  <a href={stockReadinessHref} target="_blank" rel="noopener noreferrer">
                    Open Stock Data Readiness
                    <ExternalLink className="ml-1.5 h-3.5 w-3.5" aria-hidden />
                  </a>
                </Button>
              </div>
            </div>
          ) : null}
        </div>
      </OpsSection>

      <OpsSection
        title="Plugin registry"
        description="Lifecycle registry only — runtime health comes from probe sections above."
        bodyPadding="default"
        overflow="visible"
        collapsible
        defaultCollapsed
      >
        <div className="grid gap-2 sm:grid-cols-2">
          {PLUGIN_REGISTRY.map(plugin => (
            <div
              key={plugin.id}
              className="rounded-md border border-[var(--border)] bg-[var(--secondary)] px-3 py-2"
            >
              <div className="flex flex-wrap items-center gap-2">
                <span className="text-[var(--text-dense-label)] font-semibold">{plugin.name}</span>
                <DenseTag variant="neutral">{plugin.lifecycle}</DenseTag>
              </div>
              <p className="m-0 mt-1 text-[var(--text-dense-meta)] text-[var(--muted-foreground)]">
                {plugin.vendor} · {plugin.role}
              </p>
            </div>
          ))}
        </div>
      </OpsSection>

      <ConfirmDialog
        open={reconnectOpen}
        title="Reconnect IB Gateway"
        message="Rollout restart deployment/ib-gateway in data NS. Use when TWS sessions need a clean reconnect."
        confirmLabel="Confirm reconnect"
        confirming={acting}
        onConfirm={() => void runReconnect()}
        onCancel={() => setReconnectOpen(false)}
      />
    </div>
  )
}
