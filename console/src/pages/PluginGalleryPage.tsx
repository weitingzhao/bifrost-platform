import { useCallback, useState } from 'react'
import { Button, ConfirmDialog, DenseTag } from '@bifrost/ui'
import { postIbGatewayControl } from '@/api/network'
import { IbGatewayCutoverStatusPanel } from '@/components/cluster/IbGatewayCutoverStatusPanel'
import { IbGatewayLiveStatusPanel } from '@/components/cluster/IbGatewayLiveStatusPanel'
import { OpsFeedback } from '@/components/feedback/OpsFeedback'
import { OpsSection } from '@/components/layout/OpsSection'
import {
  OpsVerdictStrip,
  type OpsVerdictLamp,
  type OpsVerdictTagVariant,
} from '@/components/layout/OpsVerdictStrip'
import { useIbGatewayLiveProbe } from '@/hooks/useIbGatewayLiveProbe'
import { useMarketDataLiveProbe } from '@/hooks/useMarketDataLiveProbe'
import { usePlatformAuth } from '@/hooks/usePlatformAuth'

const PLUGIN_REGISTRY = [
  {
    id: 'ib-gateway',
    name: 'IB Gateway',
    vendor: 'Interactive Brokers',
    role: 'TWS socket bridge · redis-ib @ data NS',
    status: 'live',
  },
  {
    id: 'market-data',
    name: 'Market Data (Polygon)',
    vendor: 'Polygon.io',
    role: 'REST ingest · stock/option bars + snapshots · PG-as-broker @ plugin-market-data NS',
    status: 'live',
  },
  {
    id: 'flex-query',
    name: 'IB Flex Query',
    vendor: 'Interactive Brokers',
    role: 'Planned subcontractor plugin',
    status: 'planned',
  },
] as const

function statusVariant(status: string): 'success' | 'neutral' | 'info' {
  if (status === 'live') return 'success'
  if (status === 'planned') return 'neutral'
  return 'info'
}

function reachVerdict(
  isLoading: boolean,
  probeReach: 'ok' | 'degraded' | 'fail' | 'unknown',
  summary: string,
  loadingSummary: string,
): {
  lamp: OpsVerdictLamp
  tagLabel: string
  tagVariant: OpsVerdictTagVariant
  summary: string
} {
  if (isLoading) {
    return {
      lamp: 'unknown',
      tagLabel: 'PROBING',
      tagVariant: 'neutral',
      summary: loadingSummary,
    }
  }
  switch (probeReach) {
    case 'ok':
      return { lamp: 'ok', tagLabel: 'OK', tagVariant: 'success', summary }
    case 'degraded':
      return { lamp: 'degraded', tagLabel: 'DEGRADED', tagVariant: 'warning', summary }
    case 'fail':
      return { lamp: 'fail', tagLabel: 'FAIL', tagVariant: 'danger', summary }
    default:
      return { lamp: 'unknown', tagLabel: 'UNKNOWN', tagVariant: 'neutral', summary }
  }
}

export function PluginGalleryPage({ onNavigate }: { onNavigate?: (tabId: string) => void } = {}) {
  const liveProbe = useIbGatewayLiveProbe()
  const marketProbe = useMarketDataLiveProbe()
  const { canOperate } = usePlatformAuth()
  const [reconnectOpen, setReconnectOpen] = useState(false)
  const [acting, setActing] = useState(false)
  const [actionMsg, setActionMsg] = useState<string | null>(null)
  const [actionFailed, setActionFailed] = useState(false)

  const liveCount = PLUGIN_REGISTRY.filter(p => p.status === 'live').length
  const plannedCount = PLUGIN_REGISTRY.filter(p => p.status === 'planned').length

  const mode = liveProbe.status?.mode
  const modeNote = mode != null && mode !== '' ? ` · mode ${mode}` : ''
  const deploy = liveProbe.status?.deployment?.ready
  const deployNote = deploy != null && deploy !== '' ? ` · deployment ${deploy}` : ''
  const verdict = reachVerdict(
    liveProbe.isLoading,
    liveProbe.probeReach,
    `${liveProbe.summary}${modeNote}${deployNote}`,
    'Probing ib-gateway via platform-api…',
  )
  const marketVerdict = reachVerdict(
    marketProbe.isLoading,
    marketProbe.probeReach,
    marketProbe.summary,
    'Probing market-data via platform-api…',
  )

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

  const deployments = marketProbe.status?.deployments ?? []
  const workers = marketProbe.status?.workers ?? []
  const freshness = marketProbe.status?.freshness ?? []

  return (
    <div className="flex w-full min-w-0 flex-col gap-4">
      <OpsVerdictStrip
        ariaLabel="Plugin bus verdict"
        title="PLUGIN BUS · IB GATEWAY"
        lamp={verdict.lamp}
        tagLabel={verdict.tagLabel}
        tagVariant={verdict.tagVariant}
        summary={verdict.summary}
        actions={
          <>
            <Button
              variant="outline"
              size="sm"
              disabled={liveProbe.isLoading}
              onClick={() => liveProbe.refetch()}
            >
              Refresh
            </Button>
            <Button variant="outline" size="sm" onClick={() => onNavigate?.('plugin-release')}>
              Need publish?
            </Button>
            {canOperate ? (
              <Button
                variant="outline"
                size="sm"
                disabled={acting}
                onClick={() => setReconnectOpen(true)}
              >
                Reconnect
              </Button>
            ) : null}
          </>
        }
        meta={
          <span>
            {liveCount} live · {plannedCount} planned
            {liveProbe.status?.mode != null && liveProbe.status.mode !== ''
              ? ` · mode ${liveProbe.status.mode}`
              : ''}
          </span>
        }
      />

      <OpsVerdictStrip
        ariaLabel="Market data plugin verdict"
        title="MARKET DATA · POLYGON"
        lamp={marketVerdict.lamp}
        tagLabel={marketVerdict.tagLabel}
        tagVariant={marketVerdict.tagVariant}
        summary={marketVerdict.summary}
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
        meta={
          <span>
            NS plugin-market-data · L0 observe
            {marketProbe.status?.autonomy != null ? ` · ${marketProbe.status.autonomy}` : ''}
          </span>
        }
      />

      {actionMsg != null ? (
        <OpsFeedback variant={actionFailed ? 'error' : 'success'} title="Reconnect">
          {actionMsg}
        </OpsFeedback>
      ) : null}

      <OpsSection title="Plugin registry" bodyPadding="default" overflow="visible">
        <div className="grid gap-2 sm:grid-cols-2">
          {PLUGIN_REGISTRY.map(plugin => (
            <div
              key={plugin.id}
              className="rounded-md border border-[var(--border)] bg-[var(--secondary)] px-3 py-2"
            >
              <div className="flex flex-wrap items-center gap-2">
                <span className="text-[var(--text-dense-label)] font-semibold">{plugin.name}</span>
                <DenseTag variant={statusVariant(plugin.status)}>{plugin.status}</DenseTag>
              </div>
              <p className="m-0 mt-1 text-[var(--text-dense-meta)] text-[var(--muted-foreground)]">
                {plugin.vendor} · {plugin.role}
              </p>
            </div>
          ))}
        </div>
      </OpsSection>

      <OpsSection title="Market Data workers" bodyPadding="default" overflow="visible">
        {deployments.length === 0 && workers.length === 0 ? (
          <p className="m-0 text-[var(--text-dense-meta)] text-[var(--muted-foreground)]">
            No deployment / worker snapshot yet — apply k8s/base or check platform-api probe.
          </p>
        ) : (
          <div className="flex flex-col gap-2">
            {deployments.map(d => (
              <div
                key={d.name}
                className="rounded-md border border-[var(--border)] bg-[var(--secondary)] px-3 py-2"
              >
                <div className="flex flex-wrap items-center gap-2">
                  <span className="text-[var(--text-dense-label)] font-semibold">{d.name}</span>
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
                </div>
                {d.detail != null && d.detail !== '' ? (
                  <p className="m-0 mt-1 text-[var(--text-dense-meta)] text-[var(--muted-foreground)]">
                    {d.detail}
                  </p>
                ) : null}
              </div>
            ))}
            {workers.map(w => (
              <div
                key={w.pool}
                className="rounded-md border border-[var(--border)] bg-[var(--secondary)] px-3 py-2"
              >
                <div className="flex flex-wrap items-center gap-2">
                  <span className="text-[var(--text-dense-label)] font-semibold">
                    pool {w.pool}
                  </span>
                  <DenseTag variant="info">
                    {w.jobs_done} done · {w.jobs_failed} failed
                  </DenseTag>
                </div>
                <p className="m-0 mt-1 text-[var(--text-dense-meta)] text-[var(--muted-foreground)]">
                  status {w.status ?? '—'}
                  {w.uptime_sec != null ? ` · uptime ${Math.round(w.uptime_sec)}s` : ''}
                  {w.last_claim_at != null && w.last_claim_at !== ''
                    ? ` · last claim ${w.last_claim_at}`
                    : ''}
                </p>
              </div>
            ))}
          </div>
        )}
      </OpsSection>

      <OpsSection title="Market Data freshness" bodyPadding="default" overflow="visible">
        {freshness.length === 0 ? (
          <p className="m-0 text-[var(--text-dense-meta)] text-[var(--muted-foreground)]">
            No ingest_freshness rows yet — run workers / daily CronJobs, then refresh.
            {marketProbe.status?.freshness_reachability != null
              ? ` · reach ${marketProbe.status.freshness_reachability}`
              : ''}
          </p>
        ) : (
          <div className="flex flex-col gap-2">
            {freshness.map(f => (
              <div
                key={f.dimension}
                className="rounded-md border border-[var(--border)] bg-[var(--secondary)] px-3 py-2"
              >
                <div className="flex flex-wrap items-center gap-2">
                  <span className="text-[var(--text-dense-label)] font-semibold font-mono">
                    {f.dimension}
                  </span>
                  <DenseTag
                    variant={
                      f.verdict === 'ok'
                        ? 'success'
                        : f.verdict === 'stale'
                          ? 'warning'
                          : 'neutral'
                    }
                  >
                    {f.verdict}
                  </DenseTag>
                  <DenseTag variant="info">{f.rows_written} rows</DenseTag>
                </div>
                <p className="m-0 mt-1 text-[var(--text-dense-meta)] text-[var(--muted-foreground)]">
                  status {f.status ?? '—'}
                  {Number.isFinite(f.age_hours) ? ` · age ${f.age_hours.toFixed(1)}h` : ''}
                  {f.last_run_at != null && f.last_run_at !== ''
                    ? ` · last run ${f.last_run_at}`
                    : ''}
                </p>
              </div>
            ))}
          </div>
        )}
      </OpsSection>

      <IbGatewayLiveStatusPanel showPrimaryActions={false} />
      <IbGatewayCutoverStatusPanel />

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
