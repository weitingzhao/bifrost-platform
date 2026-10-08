import { useQuery } from '@tanstack/react-query'
import { DenseTag, StatusLamp } from '@bifrost/ui'
import { fetchClusterWorkloads } from '@/api/cluster'
import { fetchSatelliteBusDeep, fetchSelfHealth, isAllSatelliteBusDeep } from '@/api/core'
import type { SatelliteBusDeepResponse } from '@/api/satelliteBusTypes'
import { OpsSection } from '@/components/layout/OpsSection'
import { RequestRestartDeployment } from '@/components/shell/clusterActionRequests'
import { usePlatformAuth } from '@/hooks/usePlatformAuth'
import { SOCKET_TRADE_NS } from '@/lib/satellite/socketHealthSemantics'
import type { TradeEnv } from '@/pages/satellite-bus/useSatelliteBusQueries'
import { IB_SELF_HEALTH_PATH, ibBusDeepPath } from '@/pages/shell/ib/ibHealthRequests'
import { TradeDaemonOperatePanel } from '@/pages/satellite-bus/TradeDaemonOperatePanel'

const TRADE_ENVS: readonly TradeEnv[] = ['dev', 'stg', 'prod']

function isTradeEnv(env: string): env is TradeEnv {
  return (TRADE_ENVS as readonly string[]).includes(env)
}

function busForViewer(
  data: SatelliteBusDeepResponse | { buses: SatelliteBusDeepResponse[] } | undefined,
  viewerEnv: string,
): SatelliteBusDeepResponse | undefined {
  if (data == null) return undefined
  if (isAllSatelliteBusDeep(data)) {
    return data.buses.find(bus => bus.environment === viewerEnv)
  }
  return data
}

function reachOf(value: string | undefined): 'ok' | 'degraded' | 'fail' | 'unknown' {
  if (value === 'ok' || value === 'degraded' || value === 'fail') return value
  return 'unknown'
}

function PathNode({ label, reach, detail }: { label: string; reach: string; detail: string }) {
  const lamp = reachOf(reach)
  return (
    <div className="flex min-w-0 flex-1 flex-col gap-1 rounded-[var(--card-radius)] border border-[var(--card-border)] bg-[var(--card-fill)] px-3 py-2">
      <div className="flex items-center gap-2">
        <StatusLamp value={lamp} kind="reach" />
        <span className="text-sm font-semibold">{label}</span>
        <DenseTag variant={lamp === 'ok' ? 'success' : lamp === 'fail' ? 'danger' : 'neutral'}>
          {reach || 'unknown'}
        </DenseTag>
      </div>
      <p className="m-0 text-[var(--text-dense-caption)] text-muted-foreground">{detail}</p>
    </div>
  )
}

/**
 * PROD seat bus: IB Gateway → redis-ib → daemon.
 * The request uses viewer_env only. There is no environment picker and no cross-env table.
 */
export function IbBusStatus() {
  const { canOperate } = usePlatformAuth()
  const healthQ = useQuery({
    queryKey: ['platform', 'self-health', 'ib-bus'],
    queryFn: fetchSelfHealth,
    refetchInterval: 30_000,
  })
  const viewerEnv = healthQ.data?.viewer_env?.trim() ?? ''
  const busQ = useQuery({
    queryKey: ['satellite', 'bus-deep', 'viewer', viewerEnv],
    queryFn: () => fetchSatelliteBusDeep(viewerEnv),
    enabled: viewerEnv !== '',
    refetchInterval: 30_000,
  })
  const tradeEnv = isTradeEnv(viewerEnv) ? viewerEnv : null
  const namespace = tradeEnv != null ? SOCKET_TRADE_NS[tradeEnv] : null
  const workloadsQ = useQuery({
    queryKey: ['cluster', 'workloads', namespace, 'ib-bus'],
    queryFn: () => fetchClusterWorkloads(namespace!),
    enabled: namespace != null,
    refetchInterval: 30_000,
  })

  const bus = busForViewer(busQ.data, viewerEnv)
  const gateway = bus?.monitor.socket.platform_ib_gateway
  const daemon = bus?.monitor.daemon
  const error =
    (healthQ.error instanceof Error ? healthQ.error.message : null) ??
    (busQ.error instanceof Error ? busQ.error.message : null)

  return (
    <div className="flex w-full min-w-0 flex-col gap-3">
      <OpsSection
        title="Bus status"
        description={`IB Gateway → redis-ib → daemon for viewer_env${viewerEnv !== '' ? ` ${viewerEnv}` : ''}. ${IB_SELF_HEALTH_PATH} then ${ibBusDeepPath(viewerEnv || undefined)}.`}
        bodyPadding="default"
        overflow="visible"
      >
        {viewerEnv === '' && healthQ.isLoading ? (
          <p className="m-0 text-sm text-muted-foreground">Reading viewer environment…</p>
        ) : null}
        {viewerEnv === '' && !healthQ.isLoading ? (
          <p className="m-0 text-sm text-muted-foreground">
            Self-health did not return viewer_env. Bus health stays unscoped.
          </p>
        ) : null}
        {error != null ? <p className="m-0 text-sm text-destructive">{error}</p> : null}
        {viewerEnv !== '' ? (
          <div className="flex flex-col gap-2 lg:flex-row">
            <PathNode
              label="IB Gateway"
              reach={gateway?.reachability ?? (busQ.isLoading ? '…' : 'unknown')}
              detail={gateway?.detail ?? 'platform_ib_gateway'}
            />
            <PathNode
              label="redis-ib"
              reach={gateway?.reachability === 'fail' ? 'unknown' : (gateway?.reachability ?? 'unknown')}
              detail="Shared IB data Redis. Derived from the gateway publish path on this seat."
            />
            <PathNode
              label="daemon"
              reach={daemon?.reachability ?? (busQ.isLoading ? '…' : 'unknown')}
              detail={daemon?.self_check ?? daemon?.lamp ?? 'Trade daemon on this seat'}
            />
          </div>
        ) : null}
        <div className="mt-3 flex flex-wrap gap-2">
          <RequestRestartDeployment namespace="data" name="ib-gateway" label="Request IB Gateway restart" />
        </div>
      </OpsSection>

      {tradeEnv != null && namespace != null ? (
        <TradeDaemonOperatePanel
          tradeEnv={tradeEnv}
          namespace={namespace}
          canOperate={canOperate}
          workloads={workloadsQ.data?.workloads ?? []}
          workloadsLoading={workloadsQ.isLoading}
        />
      ) : null}
    </div>
  )
}
