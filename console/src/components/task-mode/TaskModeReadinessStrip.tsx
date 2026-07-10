import { useMemo } from 'react'
import { useQuery } from '@tanstack/react-query'
import { cn, DenseTag, StatusLamp } from '@bifrost/ui'
import { AlertTriangle, ChevronRight, Gauge, Rocket, Satellite, type LucideIcon } from 'lucide-react'
import {
  fetchCluster,
  fetchClusterServiceReadiness,
  fetchReleaseGate,
  fetchSatelliteBusDeep,
  fetchSelfHealth,
  isAllSatelliteBusDeep,
} from '@/api/platform'
import type {
  ClusterSummary,
  ReleaseGateResponse,
  SatelliteBusDeepResponse,
  SatelliteBusSocketComponent,
  SelfHealthProbe,
} from '@/api/types'
import type { MatrixResponse, Reachability } from '@/api/types'
import { OpsFeedback } from '@/components/feedback/OpsFeedback'
import { ReadinessFixBar } from '@/components/task-mode/ReadinessFixBar'
import { useMissionSnapshot } from '@/hooks/useMissionSnapshot'
import {
  primaryChipNavigation,
  setSatelliteApiEnv,
  setSatelliteBusFocus,
  type ReadinessChipContext,
} from '@/lib/task-mode/readinessChipActions'
import { readinessAnchorDomId } from '@/lib/task-mode/satelliteLaunchVerdict'
import {
  infraSignal,
  missionStatus,
  missionStatusColor,
  worst,
  type ModuleState,
  type Signal,
} from '@/lib/control-room/missionSignals'
import { classifyPlatformIbGateway } from '@/lib/satellite/socketHealthSemantics'
import type { TaskModeId } from '@/lib/task-mode/types'
import type { ProdFixSignal } from '@/lib/agent/prodEnvironmentFixPrompt'

const REFETCH_MS = 20_000
const STG_NS = 'bifrost-stg'
const PROD_NS = 'bifrost-prod'
const PLATFORM_STG = 'bifrost-platform-stg'
const PLATFORM_PROD = 'bifrost-platform-prod'

export { STG_NS, PROD_NS, PLATFORM_STG, PLATFORM_PROD }

type ReadinessChipProps = {
  label: string
  signal: Signal
  detail: string
  onDrillDown?: () => void
  title?: string
}

function ReadinessChip({ label, signal, detail, onDrillDown, title }: ReadinessChipProps) {
  const failing = signal !== 'ok'
  const inner = (
    <>
      <div className="flex items-center gap-1.5">
        <StatusLamp value={signal} kind="reach" />
        <span className="min-w-0 flex-1 truncate text-[var(--text-dense-meta)] font-medium">{label}</span>
        {onDrillDown != null && (
          <ChevronRight
            size={12}
            className={cn('shrink-0', failing ? 'text-warning' : 'text-muted-foreground')}
            aria-hidden
          />
        )}
      </div>
      <p className="m-0 mt-0.5 truncate text-[var(--text-dense-caption)] text-muted-foreground">{detail}</p>
    </>
  )
  if (onDrillDown == null) {
    return (
      <div className="rounded border border-border/60 bg-card px-2 py-1.5" title={title}>
        {inner}
      </div>
    )
  }
  return (
    <button
      type="button"
      className={cn(
        'w-full rounded border bg-card px-2 py-1.5 text-left transition-colors hover:border-primary/40 hover:bg-primary/5',
        failing ? 'border-warning/40' : 'border-border/60',
      )}
      title={title ?? `Open details: ${label}`}
      onClick={onDrillDown}
    >
      {inner}
    </button>
  )
}

function stripOverallTag(signal: Signal, isLoading: boolean) {
  if (isLoading) return { variant: 'category' as const, label: 'Probing…' }
  const status = missionStatus(signal)
  return {
    variant: (status === 'NOMINAL' ? 'success' : status === 'CRITICAL' ? 'danger' : 'warning') as
      | 'success'
      | 'danger'
      | 'warning',
    label: status,
  }
}

function findMatrixTarget(matrices: MatrixResponse[], env: string, targetId: string) {
  return matrices.find(m => m.environment === env)?.targets.find(t => t.id === targetId)
}

function datastoreEnvSignal(matrices: MatrixResponse[], env: string): Signal {
  const pg = findMatrixTarget(matrices, env, 'postgres')
  const redis = findMatrixTarget(matrices, env, 'redis')
  const signals = [pg?.reachability, redis?.reachability].filter(Boolean) as Reachability[]
  if (signals.length === 0) return 'unknown'
  return worst(...signals.map(r => r as Signal))
}

function datastoreDetail(matrices: MatrixResponse[], env: string): string {
  const pg = findMatrixTarget(matrices, env, 'postgres')
  const redis = findMatrixTarget(matrices, env, 'redis')
  const parts: string[] = []
  if (pg != null) parts.push(`PG ${pg.reachability}`)
  if (redis != null) parts.push(`Redis ${redis.reachability}`)
  return parts.length > 0 ? parts.join(' · ') : 'probing'
}

function tradeApiSummary(matrix: MatrixResponse | undefined): { signal: Signal; detail: string } {
  if (matrix == null) return { signal: 'unknown', detail: 'probing' }
  const tradeTargets = matrix.targets.filter(
    t =>
      t.category === 'trade_api' ||
      t.category === 'trade_frontend' ||
      t.id === 'nginx-spa' ||
      t.id.startsWith('api-'),
  )
  const total = tradeTargets.length
  if (total === 0) return { signal: 'unknown', detail: 'no API targets' }
  const ok = tradeTargets.filter(t => t.reachability === 'ok').length
  const signal: Signal = ok === total ? 'ok' : ok === 0 ? 'fail' : 'degraded'
  return { signal, detail: `${ok}/${total} APIs reachable` }
}

export function namespacePods(cluster: ClusterSummary | undefined, ns: string): ModuleState {
  if (cluster == null) return { signal: 'unknown', value: '…', detail: `${ns}: probing` }
  const failing = (cluster.failing_pod_details ?? []).filter(p => p.namespace === ns)
  if (failing.length > 0) {
    return {
      signal: 'degraded',
      value: `${failing.length} failing`,
      detail: `${ns}: ${failing.length} failing pod${failing.length === 1 ? '' : 's'}`,
    }
  }
  const clusterFail = cluster.failing_pods
  if (cluster.reachability === 'fail') {
    return { signal: 'fail', value: 'down', detail: 'Cluster API unreachable' }
  }
  return {
    signal: cluster.reachability === 'degraded' ? 'degraded' : 'ok',
    value: ns,
    detail:
      clusterFail > 0
        ? `${ns} OK · ${clusterFail} failing elsewhere`
        : `${ns} workloads nominal`,
  }
}

/**
 * Shared Rocket IB bus — monitor.socket only (no ingest.reachability).
 * Gateway uses classifyPlatformIbGateway so observe/partial ≠ silent fail.
 */
function sharedRocketFromSocket(socket: {
  ib_ingestor?: SatelliteBusSocketComponent
  ib_account_agent?: SatelliteBusSocketComponent
  ib_operator?: SatelliteBusSocketComponent
  platform_ib_gateway?: SatelliteBusSocketComponent
} | undefined): { signal: Signal; detail: string } {
  if (socket == null) return { signal: 'unknown', detail: 'probing' }
  const gateway = classifyPlatformIbGateway(socket.platform_ib_gateway)
  const reaches: Signal[] = [
    (socket.ib_ingestor?.reachability ?? 'unknown') as Signal,
    (socket.ib_account_agent?.reachability ?? 'unknown') as Signal,
    (socket.ib_operator?.reachability ?? 'unknown') as Signal,
    gateway.reach as Signal,
  ]
  const signal = worst(...reaches)
  const ok = reaches.filter(r => r === 'ok').length
  const gwHint = gateway.reach !== 'ok' ? ` · gateway ${gateway.reachLabel}` : ''
  return { signal, detail: `${ok}/4 socket OK${gwHint}` }
}

function releaseGateSignal(gate: ReleaseGateResponse | undefined): { signal: Signal; detail: string } {
  if (gate == null) return { signal: 'unknown', detail: 'probing' }
  if (gate.result === 'pass') return { signal: 'ok', detail: 'Gate passed' }
  if (gate.result === 'fail') return { signal: 'fail', detail: gate.detail?.trim() || 'Gate failed' }
  return { signal: 'degraded', detail: 'Gate not run' }
}

function selfHealthEnvSignal(
  probes: SelfHealthProbe[] | undefined,
  env: 'stg' | 'prod',
): { signal: Signal; detail: string } {
  const filtered = probes?.filter(p => p.env === env) ?? []
  if (filtered.length === 0) return { signal: 'unknown', detail: 'probing' }
  const ok = filtered.filter(p => p.status === 'ok').length
  const signal = worst(...filtered.map(p => p.status as Signal))
  return { signal, detail: `${ok}/${filtered.length} probes OK (${env})` }
}

export function isProdReleaseBlocked(signal: Signal): boolean {
  return signal === 'fail' || signal === 'degraded'
}

type EnvChip = { label: string; signal: Signal; detail: string }

type EnvironmentReadinessPanelProps = {
  title: string
  icon: LucideIcon
  overall: Signal
  isLoading: boolean
  chips: EnvChip[]
  compact?: boolean
  summaryColumn?: boolean
  /** Scroll target from LaunchGateBar checkpoint (1:1 lamp ↔ panel). */
  readinessAnchor?: 'rocket' | 'trade-prod' | 'platform-prod' | 'stg'
  linkLabel: string
  onLink: () => void
  onNavigate: (tabId: string) => void
  fixCtx?: ReadinessChipContext
  canOperate?: boolean
  onAgentFix?: () => void
  agentFixPending?: boolean
  agentFixDisabled?: boolean
  agentFixTitle?: string
}

function EnvironmentReadinessPanel({
  title,
  icon: Icon,
  overall,
  isLoading,
  chips,
  compact = false,
  summaryColumn = false,
  readinessAnchor,
  linkLabel,
  onLink,
  onNavigate,
  fixCtx,
  canOperate = false,
  onAgentFix,
  agentFixPending,
  agentFixDisabled,
  agentFixTitle,
}: EnvironmentReadinessPanelProps) {
  const tag = stripOverallTag(overall, isLoading)
  const color = missionStatusColor(missionStatus(overall))
  const dense = compact || summaryColumn

  return (
    <div
      id={readinessAnchor != null ? readinessAnchorDomId(readinessAnchor) : undefined}
      className={cn(
        'rounded-lg border border-border bg-secondary scroll-mt-2 transition-shadow',
        dense ? 'px-2.5 py-2' : 'px-3 py-2.5',
        summaryColumn && 'flex h-full min-h-0 flex-col',
      )}
    >
      <div className="flex flex-wrap items-center gap-1.5">
        <Icon size={dense ? 14 : 16} style={{ color }} />
        <span className={cn('font-semibold', dense ? 'text-[var(--text-dense-meta)]' : 'text-[var(--text-dense-label)]')}>
          {title}
        </span>
        <StatusLamp value={overall} kind="reach" />
        <DenseTag variant={tag.variant} className={dense ? 'text-[9px]' : undefined}>
          {tag.label}
        </DenseTag>
      </div>
      <div
        className={cn(
          'mt-1.5 grid gap-1.5',
          dense ? 'grid-cols-2' : 'sm:grid-cols-2',
        )}
      >
        {chips.map(chip => {
          const nav = fixCtx != null ? primaryChipNavigation(chip.label, fixCtx) : null
          return (
            <ReadinessChip
              key={chip.label}
              label={chip.label}
              signal={chip.signal}
              detail={chip.detail}
              onDrillDown={
                nav != null
                  ? () => {
                      if (nav.tabId === 'satellite-bus') {
                        setSatelliteBusFocus(nav.busFocus)
                      }
                      if (nav.tabId === 'satellite-api') {
                        setSatelliteApiEnv(nav.apiEnv)
                      }
                      onNavigate(nav.tabId)
                    }
                  : undefined
              }
            />
          )
        })}
      </div>
      {fixCtx != null && (
        <ReadinessFixBar
          chips={chips}
          ctx={fixCtx}
          canOperate={canOperate}
          onNavigate={onNavigate}
          onAgentFix={onAgentFix}
          agentFixPending={agentFixPending}
          agentFixDisabled={agentFixDisabled}
          agentFixTitle={agentFixTitle}
          dense={dense}
        />
      )}
      {!summaryColumn && (
        <button
          type="button"
          className="mt-2 text-[var(--text-dense-meta)] text-primary hover:underline"
          onClick={onLink}
        >
          {linkLabel}
        </button>
      )}
    </div>
  )
}

function ProdBlockedBanner({ context }: { context: 'satellite' | 'rocket' }) {
  const copy =
    context === 'satellite'
      ? 'Satellite deploy and promote require a healthy Trade Prod stack. Resolve Prod failures before deploying or releasing.'
      : 'Platform release requires healthy Prod namespaces and gates. Resolve Prod failures before launching release agents.'
  return (
    <OpsFeedback variant="warning" title="Fix Prod environment before release">
      <span className="inline-flex items-start gap-1.5">
        <AlertTriangle size={14} className="mt-0.5 shrink-0" aria-hidden />
        {copy}
      </span>
    </OpsFeedback>
  )
}

export type SatelliteBlockKind = 'rocket' | 'prod' | 'both' | null

export function useSatelliteProdReadiness(enabled = true) {
  const { snapshot, matrices, isLoading: missionLoading } = useMissionSnapshot()

  const clusterQ = useQuery({
    queryKey: ['task-cc', 'cluster'],
    queryFn: fetchCluster,
    refetchInterval: REFETCH_MS,
    enabled,
  })

  const busQ = useQuery({
    queryKey: ['task-cc', 'satellite-bus', 'prod'],
    queryFn: () => fetchSatelliteBusDeep('prod'),
    refetchInterval: REFETCH_MS,
    enabled,
  })

  const prodGateQ = useQuery({
    queryKey: ['task-cc', 'trade-prod-gate'],
    queryFn: () => fetchReleaseGate('prod'),
    refetchInterval: REFETCH_MS,
    enabled,
  })

  const prodMatrix = useMemo(() => matrices.find(m => m.environment === 'prod'), [matrices])
  const cluster = clusterQ.data

  const k8s = useMemo(() => {
    const infra = infraSignal(cluster)
    const ns = namespacePods(cluster, PROD_NS)
    return { signal: worst(infra.signal, ns.signal), detail: `${infra.detail} · ${ns.detail}` }
  }, [cluster])

  const datastore = useMemo(
    () => ({
      signal: datastoreEnvSignal(matrices, 'prod'),
      detail: datastoreDetail(matrices, 'prod'),
    }),
    [matrices],
  )

  const busReach = useMemo((): SatelliteBusDeepResponse | undefined => {
    const data = busQ.data
    if (data == null) return undefined
    if (isAllSatelliteBusDeep(data)) return data.buses.find(b => b.environment === 'prod')
    return data
  }, [busQ.data])

  const rocket = useMemo(
    () => sharedRocketFromSocket(busReach?.monitor.socket),
    [busReach],
  )

  const tradeApis = useMemo(() => tradeApiSummary(prodMatrix), [prodMatrix])
  const tradeSnapshot = snapshot.tradeProd
  const gate = useMemo(() => releaseGateSignal(prodGateQ.data), [prodGateQ.data])

  /** Trade Prod only — shared Rocket is reported separately. */
  const tradeProdOverall = worst(
    k8s.signal,
    datastore.signal,
    tradeApis.signal,
    tradeSnapshot.signal,
    gate.signal,
  )

  const releaseOverall = worst(tradeProdOverall, rocket.signal)

  const isLoading =
    missionLoading || clusterQ.isLoading || busQ.isLoading || prodGateQ.isLoading

  const tradeProdBlocked = isProdReleaseBlocked(tradeProdOverall)
  const rocketBlocked = isProdReleaseBlocked(rocket.signal)
  const prodBlocked = isProdReleaseBlocked(releaseOverall)

  const blockKind: SatelliteBlockKind = prodBlocked
    ? rocketBlocked && tradeProdBlocked
      ? 'both'
      : rocketBlocked
        ? 'rocket'
        : 'prod'
    : null

  const prodDisabledReason = !prodBlocked
    ? undefined
    : blockKind === 'rocket'
      ? 'Rocket IB bus blocked — fix shared gateway before deploy'
      : blockKind === 'both'
        ? 'Rocket IB bus and Trade Prod readiness blocked'
        : 'Prod readiness blocked — fix environment first'

  return {
    prodOverall: releaseOverall,
    tradeProdOverall,
    rocketSignal: rocket.signal,
    rocketDetail: rocket.detail,
    tradeProdBlocked,
    rocketBlocked,
    blockKind,
    prodBlocked,
    isLoading,
    prodDisabledReason,
    fixSignals: [
      { label: 'Trade · K8s PROD', signal: k8s.signal, detail: k8s.detail },
      { label: 'Ground · PG / Redis', signal: datastore.signal, detail: datastore.detail },
      { label: 'Trade · APIs PROD', signal: tradeApis.signal, detail: tradeApis.detail },
      { label: 'Trade · PROD matrix', signal: tradeSnapshot.signal, detail: tradeSnapshot.detail },
      { label: 'Trade · PROD gate', signal: gate.signal, detail: gate.detail },
    ] as ProdFixSignal[],
    rocketFixSignal: {
      label: 'Rocket · IB socket',
      signal: rocket.signal,
      detail: rocket.detail,
    } as ProdFixSignal,
    prodNamespace: PROD_NS,
    stgNamespace: STG_NS,
  }
}

export function useRocketProdReadiness(enabled = true) {
  const { snapshot, isLoading: missionLoading } = useMissionSnapshot()

  const clusterQ = useQuery({
    queryKey: ['task-cc', 'cluster'],
    queryFn: fetchCluster,
    refetchInterval: REFETCH_MS,
    enabled,
  })

  const selfQ = useQuery({
    queryKey: ['cockpit', 'self-health'],
    queryFn: fetchSelfHealth,
    refetchInterval: REFETCH_MS,
    enabled,
  })

  const prodGateQ = useQuery({
    queryKey: ['task-cc', 'platform-prod-gate'],
    queryFn: () => fetchReleaseGate('platform-prod'),
    refetchInterval: REFETCH_MS,
    enabled,
  })

  const cluster = clusterQ.data
  const k8sProd = useMemo(() => namespacePods(cluster, PLATFORM_PROD), [cluster])
  const selfProd = useMemo(() => selfHealthEnvSignal(selfQ.data?.probes, 'prod'), [selfQ.data?.probes])
  const gate = useMemo(() => releaseGateSignal(prodGateQ.data), [prodGateQ.data])

  const prodOverall = worst(k8sProd.signal, selfProd.signal, gate.signal, snapshot.release.signal)

  const isLoading = missionLoading || clusterQ.isLoading || selfQ.isLoading || prodGateQ.isLoading
  const prodBlocked = isProdReleaseBlocked(prodOverall)

  return {
    prodOverall,
    prodBlocked,
    isLoading,
    prodDisabledReason: prodBlocked ? 'Prod readiness blocked — fix environment first' : undefined,
    fixSignals: [
      { label: 'K8s · Platform PROD NS', signal: k8sProd.signal, detail: k8sProd.detail },
      { label: 'Self-health PROD', signal: selfProd.signal, detail: selfProd.detail },
      { label: 'PROD release gate', signal: gate.signal, detail: gate.detail },
      { label: 'Supply chain', signal: snapshot.release.signal, detail: snapshot.release.detail },
    ] as ProdFixSignal[],
    prodNamespace: PLATFORM_PROD,
  }
}

export type LaunchViewOverall = {
  overall: Signal
  stgOverall: Signal
  prodOverall: Signal
  rocketSignal?: Signal
  rocketDetail?: string
  isLoading: boolean
  fixSignals: ProdFixSignal[]
}

/** Headline readiness for Rocket Launch view — worst(STG, PROD) panels in Task CC. */
export function useRocketLaunchOverall(enabled = true): LaunchViewOverall {
  const { snapshot, isLoading: missionLoading } = useMissionSnapshot()

  const serviceQ = useQuery({
    queryKey: ['task-cc', 'service-readiness'],
    queryFn: fetchClusterServiceReadiness,
    refetchInterval: REFETCH_MS,
    enabled,
  })

  const clusterQ = useQuery({
    queryKey: ['task-cc', 'cluster'],
    queryFn: fetchCluster,
    refetchInterval: REFETCH_MS,
    enabled,
  })

  const selfQ = useQuery({
    queryKey: ['cockpit', 'self-health'],
    queryFn: fetchSelfHealth,
    refetchInterval: REFETCH_MS,
    enabled,
  })

  const stgGateQ = useQuery({
    queryKey: ['task-cc', 'platform-stg-gate'],
    queryFn: () => fetchReleaseGate('platform-stg'),
    refetchInterval: REFETCH_MS,
    enabled,
  })

  const prodGateQ = useQuery({
    queryKey: ['task-cc', 'platform-prod-gate'],
    queryFn: () => fetchReleaseGate('platform-prod'),
    refetchInterval: REFETCH_MS,
    enabled,
  })

  const cicdDomain = serviceQ.data?.domains.find(d => d.id === 'cicd')
  const cicdSignal = (cicdDomain?.reachability ?? 'unknown') as Signal
  const cicdDetail = cicdDomain?.summary ?? serviceQ.data?.detail ?? 'Tekton · platform namespaces'

  const cluster = clusterQ.data
  const clusterInfra = useMemo(() => infraSignal(cluster), [cluster])
  const k8sStgNs = useMemo(() => namespacePods(cluster, PLATFORM_STG), [cluster])
  const k8sProd = useMemo(() => namespacePods(cluster, PLATFORM_PROD), [cluster])
  const selfStg = useMemo(() => selfHealthEnvSignal(selfQ.data?.probes, 'stg'), [selfQ.data?.probes])
  const selfProd = useMemo(() => selfHealthEnvSignal(selfQ.data?.probes, 'prod'), [selfQ.data?.probes])
  const stgGate = useMemo(() => releaseGateSignal(stgGateQ.data), [stgGateQ.data])
  const prodGate = useMemo(() => releaseGateSignal(prodGateQ.data), [prodGateQ.data])

  const stgOverall = worst(
    clusterInfra.signal,
    k8sStgNs.signal,
    cicdSignal,
    selfStg.signal,
    stgGate.signal,
    snapshot.release.signal,
  )
  const prodOverall = worst(k8sProd.signal, selfProd.signal, prodGate.signal, snapshot.release.signal)
  const overall = worst(stgOverall, prodOverall)

  const isLoading =
    missionLoading ||
    serviceQ.isLoading ||
    clusterQ.isLoading ||
    selfQ.isLoading ||
    stgGateQ.isLoading ||
    prodGateQ.isLoading

  const fixSignals: ProdFixSignal[] = [
    { label: 'Cluster · infra', signal: clusterInfra.signal, detail: clusterInfra.detail },
    { label: 'STG · K8s NS', signal: k8sStgNs.signal, detail: k8sStgNs.detail },
    { label: 'STG · CI/CD', signal: cicdSignal, detail: cicdDetail },
    { label: 'STG · Release gate', signal: stgGate.signal, detail: stgGate.detail },
    { label: 'Supply chain', signal: snapshot.release.signal, detail: snapshot.release.detail },
    { label: 'STG · Self-health', signal: selfStg.signal, detail: selfStg.detail },
    { label: 'PROD · Platform NS', signal: k8sProd.signal, detail: k8sProd.detail },
    { label: 'PROD · Self-health', signal: selfProd.signal, detail: selfProd.detail },
    { label: 'PROD · Release gate', signal: prodGate.signal, detail: prodGate.detail },
  ]

  return { overall, stgOverall, prodOverall, isLoading, fixSignals }
}

/** Headline readiness for Satellite Deploy view — worst(STG, PROD) panels in Task CC. */
export function useSatelliteDeployOverall(enabled = true): LaunchViewOverall {
  const { snapshot, matrices, isLoading: missionLoading } = useMissionSnapshot()

  const stgBusQ = useQuery({
    queryKey: ['task-cc', 'satellite-bus', 'stg'],
    queryFn: () => fetchSatelliteBusDeep('stg'),
    refetchInterval: REFETCH_MS,
    enabled,
  })

  const prodBusQ = useQuery({
    queryKey: ['task-cc', 'satellite-bus', 'prod'],
    queryFn: () => fetchSatelliteBusDeep('prod'),
    refetchInterval: REFETCH_MS,
    enabled,
  })

  const prodGateQ = useQuery({
    queryKey: ['task-cc', 'trade-prod-gate'],
    queryFn: () => fetchReleaseGate('prod'),
    refetchInterval: REFETCH_MS,
    enabled,
  })

  const clusterQ = useQuery({
    queryKey: ['task-cc', 'cluster'],
    queryFn: fetchCluster,
    refetchInterval: REFETCH_MS,
    enabled,
  })

  const stgMatrix = useMemo(() => matrices.find(m => m.environment === 'stg'), [matrices])
  const prodMatrix = useMemo(() => matrices.find(m => m.environment === 'prod'), [matrices])
  const cluster = clusterQ.data
  const clusterInfra = useMemo(() => infraSignal(cluster), [cluster])
  const stgK8sNs = useMemo(() => namespacePods(cluster, STG_NS), [cluster])
  const prodK8sNs = useMemo(() => namespacePods(cluster, PROD_NS), [cluster])

  const stgDatastore = useMemo(
    () => ({ signal: datastoreEnvSignal(matrices, 'stg'), detail: datastoreDetail(matrices, 'stg') }),
    [matrices],
  )

  const prodDatastore = useMemo(
    () => ({ signal: datastoreEnvSignal(matrices, 'prod'), detail: datastoreDetail(matrices, 'prod') }),
    [matrices],
  )

  const busForEnv = (data: typeof stgBusQ.data, env: 'stg' | 'prod'): SatelliteBusDeepResponse | undefined => {
    if (data == null) return undefined
    if (isAllSatelliteBusDeep(data)) return data.buses.find(b => b.environment === env)
    return data
  }

  const stgBus = useMemo(() => busForEnv(stgBusQ.data, 'stg'), [stgBusQ.data])
  const prodBus = useMemo(() => busForEnv(prodBusQ.data, 'prod'), [prodBusQ.data])

  const rocket = useMemo(() => {
    // Prefer prod probe of shared gateway; fall back to stg.
    const fromProd = sharedRocketFromSocket(prodBus?.monitor.socket)
    if (prodBus != null) return fromProd
    return sharedRocketFromSocket(stgBus?.monitor.socket)
  }, [stgBus, prodBus])

  const stgTradeApis = useMemo(() => tradeApiSummary(stgMatrix), [stgMatrix])
  const prodTradeApis = useMemo(() => tradeApiSummary(prodMatrix), [prodMatrix])
  const prodGate = useMemo(() => releaseGateSignal(prodGateQ.data), [prodGateQ.data])

  const stgOverall = worst(
    clusterInfra.signal,
    stgK8sNs.signal,
    stgDatastore.signal,
    stgTradeApis.signal,
  )
  const prodOverall = worst(
    clusterInfra.signal,
    prodK8sNs.signal,
    prodDatastore.signal,
    prodTradeApis.signal,
    snapshot.tradeProd.signal,
    prodGate.signal,
  )
  const overall = worst(stgOverall, prodOverall, rocket.signal)

  const isLoading =
    missionLoading ||
    stgBusQ.isLoading ||
    prodBusQ.isLoading ||
    clusterQ.isLoading ||
    prodGateQ.isLoading

  const fixSignals: ProdFixSignal[] = [
    { label: 'Ground · Cluster infra', signal: clusterInfra.signal, detail: clusterInfra.detail },
    { label: 'Trade · K8s STG', signal: stgK8sNs.signal, detail: stgK8sNs.detail },
    { label: 'Ground · PG / Redis STG', signal: stgDatastore.signal, detail: stgDatastore.detail },
    { label: 'Trade · APIs STG', signal: stgTradeApis.signal, detail: stgTradeApis.detail },
    { label: 'Trade · K8s PROD', signal: prodK8sNs.signal, detail: prodK8sNs.detail },
    { label: 'Ground · PG / Redis PROD', signal: prodDatastore.signal, detail: prodDatastore.detail },
    { label: 'Trade · APIs PROD', signal: prodTradeApis.signal, detail: prodTradeApis.detail },
    { label: 'Trade · PROD matrix', signal: snapshot.tradeProd.signal, detail: snapshot.tradeProd.detail },
    { label: 'Trade · PROD gate', signal: prodGate.signal, detail: prodGate.detail },
    { label: 'Rocket · IB socket', signal: rocket.signal, detail: rocket.detail },
  ]

  return {
    overall,
    stgOverall,
    prodOverall,
    rocketSignal: rocket.signal,
    rocketDetail: rocket.detail,
    isLoading,
    fixSignals,
  }
}

function RocketReadinessStrip({
  compact = false,
  summaryColumn = false,
  suppressProdBlockedBanner = false,
  onNavigate,
}: {
  compact?: boolean
  summaryColumn?: boolean
  suppressProdBlockedBanner?: boolean
  onNavigate: (tabId: string) => void
}) {
  const { snapshot, isLoading: missionLoading } = useMissionSnapshot()
  const { prodOverall, isLoading: prodLoading } = useRocketProdReadiness()

  const serviceQ = useQuery({
    queryKey: ['task-cc', 'service-readiness'],
    queryFn: fetchClusterServiceReadiness,
    refetchInterval: REFETCH_MS,
  })

  const clusterQ = useQuery({
    queryKey: ['task-cc', 'cluster'],
    queryFn: fetchCluster,
    refetchInterval: REFETCH_MS,
  })

  const selfQ = useQuery({
    queryKey: ['cockpit', 'self-health'],
    queryFn: fetchSelfHealth,
    refetchInterval: REFETCH_MS,
  })

  const stgGateQ = useQuery({
    queryKey: ['task-cc', 'platform-stg-gate'],
    queryFn: () => fetchReleaseGate('platform-stg'),
    refetchInterval: REFETCH_MS,
  })

  const prodGateQ = useQuery({
    queryKey: ['task-cc', 'platform-prod-gate'],
    queryFn: () => fetchReleaseGate('platform-prod'),
    refetchInterval: REFETCH_MS,
  })

  const cicdDomain = serviceQ.data?.domains.find(d => d.id === 'cicd')
  const cicdSignal = (cicdDomain?.reachability ?? 'unknown') as Signal
  const cicdDetail = cicdDomain?.summary ?? serviceQ.data?.detail ?? 'Tekton · platform namespaces'

  const cluster = clusterQ.data
  const k8sStg = useMemo(() => {
    const infra = infraSignal(cluster)
    const ns = namespacePods(cluster, PLATFORM_STG)
    return { signal: worst(infra.signal, ns.signal), detail: `${infra.detail} · ${ns.detail}` }
  }, [cluster])

  const k8sProd = useMemo(() => namespacePods(cluster, PLATFORM_PROD), [cluster])
  const selfStg = useMemo(() => selfHealthEnvSignal(selfQ.data?.probes, 'stg'), [selfQ.data?.probes])
  const selfProd = useMemo(() => selfHealthEnvSignal(selfQ.data?.probes, 'prod'), [selfQ.data?.probes])
  const stgGate = useMemo(() => releaseGateSignal(stgGateQ.data), [stgGateQ.data])
  const prodGate = useMemo(() => releaseGateSignal(prodGateQ.data), [prodGateQ.data])

  const stgOverall = worst(k8sStg.signal, cicdSignal, selfStg.signal, stgGate.signal, snapshot.release.signal)
  const prodOverallLocal = worst(k8sProd.signal, selfProd.signal, prodGate.signal, snapshot.release.signal)

  const stgLoading =
    missionLoading ||
    serviceQ.isLoading ||
    clusterQ.isLoading ||
    selfQ.isLoading ||
    stgGateQ.isLoading
  const prodPanelLoading = prodLoading || clusterQ.isLoading || selfQ.isLoading || prodGateQ.isLoading

  const showProdBanner =
    !suppressProdBlockedBanner &&
    (isProdReleaseBlocked(prodOverallLocal) || isProdReleaseBlocked(prodOverall))

  const stgChips: EnvChip[] = summaryColumn
    ? [
        { label: 'Rocket · K8s STG', signal: k8sStg.signal, detail: k8sStg.detail },
        { label: 'CI/CD', signal: cicdSignal, detail: cicdDetail },
        { label: 'Self-health STG', signal: selfStg.signal, detail: selfStg.detail },
        { label: 'STG gate', signal: stgGate.signal, detail: stgGate.detail },
      ]
    : [
        { label: 'Rocket · K8s STG', signal: k8sStg.signal, detail: k8sStg.detail },
        { label: 'CI/CD', signal: cicdSignal, detail: cicdDetail },
        { label: 'Self-health STG', signal: selfStg.signal, detail: selfStg.detail },
        { label: 'STG release gate', signal: stgGate.signal, detail: stgGate.detail },
        { label: 'Supply chain', signal: snapshot.release.signal, detail: snapshot.release.detail },
      ]

  return (
    <div className={cn('flex flex-col', summaryColumn ? 'gap-1.5' : 'gap-2')}>
      {showProdBanner && <ProdBlockedBanner context="rocket" />}
      <EnvironmentReadinessPanel
        title={summaryColumn ? 'STG readiness' : 'Platform STG readiness'}
        icon={Rocket}
        overall={stgOverall}
        isLoading={stgLoading}
        compact={compact}
        summaryColumn={summaryColumn}
        readinessAnchor="stg"
        chips={stgChips}
        linkLabel="Platform Release →"
        onLink={() => onNavigate('platform-release')}
        onNavigate={onNavigate}
        fixCtx={{ modeId: 'rocket-launch', env: 'platform-stg' }}
      />
      <EnvironmentReadinessPanel
        title={summaryColumn ? 'Platform Prod' : 'PROD environment readiness'}
        icon={Rocket}
        overall={prodOverallLocal}
        isLoading={prodPanelLoading}
        compact={compact}
        summaryColumn={summaryColumn}
        readinessAnchor="platform-prod"
        chips={[
          { label: 'Rocket · K8s PROD', signal: k8sProd.signal, detail: k8sProd.detail },
          { label: 'Self-health PROD', signal: selfProd.signal, detail: selfProd.detail },
          { label: 'PROD gate', signal: prodGate.signal, detail: prodGate.detail },
          { label: 'Supply chain', signal: snapshot.release.signal, detail: snapshot.release.detail },
        ]}
        linkLabel="Platform Release →"
        onLink={() => onNavigate('platform-release')}
        onNavigate={onNavigate}
        fixCtx={{ modeId: 'rocket-launch', env: 'platform-prod' }}
      />
    </div>
  )
}

function SharedRocketStrip({
  rocket,
  isLoading,
  compact = false,
  onNavigate,
  canOperate = false,
}: {
  rocket: { signal: Signal; detail: string }
  isLoading: boolean
  compact?: boolean
  onNavigate: (tabId: string) => void
  canOperate?: boolean
}) {
  const tag = stripOverallTag(rocket.signal, isLoading)
  const chips: EnvChip[] = [
    { label: 'Rocket · IB socket', signal: rocket.signal, detail: rocket.detail },
  ]
  const openBus = () => {
    setSatelliteBusFocus('rocket')
    onNavigate('satellite-bus')
  }

  return (
    <div
      id={readinessAnchorDomId('rocket')}
      className={cn(
        'rounded-lg border border-border bg-secondary scroll-mt-2 transition-shadow',
        compact ? 'px-2.5 py-2' : 'px-3 py-2.5',
      )}
    >
      <div className="flex flex-wrap items-center gap-1.5">
        <Rocket size={compact ? 14 : 16} className="text-muted-foreground" />
        <span
          className={cn(
            'font-semibold',
            compact ? 'text-[var(--text-dense-meta)]' : 'text-[var(--text-dense-label)]',
          )}
        >
          Rocket IB bus
        </span>
        <StatusLamp value={rocket.signal} kind="reach" />
        <DenseTag variant={tag.variant} className={compact ? 'text-[9px]' : undefined}>
          {tag.label}
        </DenseTag>
        <DenseTag variant="neutral" className="text-[9px]">
          SHARED
        </DenseTag>
      </div>
      <div className="mt-1.5 grid gap-1.5 grid-cols-1">
        <ReadinessChip
          label="Rocket · IB socket"
          signal={rocket.signal}
          detail={rocket.detail}
          onDrillDown={openBus}
        />
      </div>
      <ReadinessFixBar
        chips={chips}
        ctx={{ modeId: 'satellite-deploy', env: 'prod' }}
        canOperate={canOperate}
        onNavigate={onNavigate}
        dense={compact}
      />
      <button
        type="button"
        className="mt-1.5 text-[var(--text-dense-caption)] text-primary hover:underline"
        onClick={openBus}
      >
        Bus Status → Rocket
      </button>
    </div>
  )
}

function SatelliteReadinessStrip({
  compact = false,
  summaryColumn = false,
  suppressProdBlockedBanner = false,
  onNavigate,
  canOperate = false,
  onAgentFixStg,
  onAgentFixProd,
  agentFixPending = false,
  agentFixDisabled = false,
  agentFixTitle,
}: {
  compact?: boolean
  summaryColumn?: boolean
  suppressProdBlockedBanner?: boolean
  onNavigate: (tabId: string) => void
  canOperate?: boolean
  onAgentFixStg?: () => void
  onAgentFixProd?: () => void
  agentFixPending?: boolean
  agentFixDisabled?: boolean
  agentFixTitle?: string
}) {
  const { snapshot, matrices, isLoading: missionLoading } = useMissionSnapshot()
  const {
    tradeProdOverall,
    tradeProdBlocked,
    rocketBlocked,
    rocketSignal,
    rocketDetail,
  } = useSatelliteProdReadiness()

  const stgBusQ = useQuery({
    queryKey: ['task-cc', 'satellite-bus', 'stg'],
    queryFn: () => fetchSatelliteBusDeep('stg'),
    refetchInterval: REFETCH_MS,
  })

  const prodBusQ = useQuery({
    queryKey: ['task-cc', 'satellite-bus', 'prod'],
    queryFn: () => fetchSatelliteBusDeep('prod'),
    refetchInterval: REFETCH_MS,
  })

  const prodGateQ = useQuery({
    queryKey: ['task-cc', 'trade-prod-gate'],
    queryFn: () => fetchReleaseGate('prod'),
    refetchInterval: REFETCH_MS,
  })

  const stgMatrix = useMemo(() => matrices.find(m => m.environment === 'stg'), [matrices])
  const prodMatrix = useMemo(() => matrices.find(m => m.environment === 'prod'), [matrices])

  const clusterDetailQ = useQuery({
    queryKey: ['task-cc', 'cluster'],
    queryFn: fetchCluster,
    refetchInterval: REFETCH_MS,
  })

  const cluster = clusterDetailQ.data

  const stgK8s = useMemo(() => {
    const infra = infraSignal(cluster)
    const ns = namespacePods(cluster, STG_NS)
    return { signal: worst(infra.signal, ns.signal), detail: `${infra.detail} · ${ns.detail}` }
  }, [cluster])

  const prodK8s = useMemo(() => {
    const infra = infraSignal(cluster)
    const ns = namespacePods(cluster, PROD_NS)
    return { signal: worst(infra.signal, ns.signal), detail: `${infra.detail} · ${ns.detail}` }
  }, [cluster])

  const stgDatastore = useMemo(
    () => ({
      signal: datastoreEnvSignal(matrices, 'stg'),
      detail: datastoreDetail(matrices, 'stg'),
    }),
    [matrices],
  )

  const prodDatastore = useMemo(
    () => ({
      signal: datastoreEnvSignal(matrices, 'prod'),
      detail: datastoreDetail(matrices, 'prod'),
    }),
    [matrices],
  )

  const busForEnv = (data: typeof stgBusQ.data, env: 'stg' | 'prod'): SatelliteBusDeepResponse | undefined => {
    if (data == null) return undefined
    if (isAllSatelliteBusDeep(data)) return data.buses.find(b => b.environment === env)
    return data
  }

  const stgBusReach = useMemo(() => busForEnv(stgBusQ.data, 'stg'), [stgBusQ.data])
  const prodBusReach = useMemo(() => busForEnv(prodBusQ.data, 'prod'), [prodBusQ.data])

  const rocket = useMemo(() => {
    if (prodBusReach != null) return sharedRocketFromSocket(prodBusReach.monitor.socket)
    if (stgBusReach != null) return sharedRocketFromSocket(stgBusReach.monitor.socket)
    return { signal: rocketSignal, detail: rocketDetail }
  }, [prodBusReach, stgBusReach, rocketSignal, rocketDetail])

  const stgTradeApis = useMemo(() => tradeApiSummary(stgMatrix), [stgMatrix])
  const prodTradeApis = useMemo(() => tradeApiSummary(prodMatrix), [prodMatrix])
  const prodGate = useMemo(() => releaseGateSignal(prodGateQ.data), [prodGateQ.data])

  const stgOverall = worst(stgK8s.signal, stgDatastore.signal, stgTradeApis.signal)
  const prodOverallLocal = worst(
    prodK8s.signal,
    prodDatastore.signal,
    prodTradeApis.signal,
    snapshot.tradeProd.signal,
    prodGate.signal,
  )

  const stgLoading = missionLoading || clusterDetailQ.isLoading
  const prodLoading =
    missionLoading || clusterDetailQ.isLoading || prodGateQ.isLoading
  const rocketLoading = stgBusQ.isLoading || prodBusQ.isLoading

  const showProdBanner =
    !suppressProdBlockedBanner &&
    (tradeProdBlocked || isProdReleaseBlocked(prodOverallLocal) || isProdReleaseBlocked(tradeProdOverall))

  const prodChips: EnvChip[] = [
    { label: 'Trade · K8s PROD', signal: prodK8s.signal, detail: prodK8s.detail },
    { label: 'Ground · PG / Redis', signal: prodDatastore.signal, detail: prodDatastore.detail },
    { label: 'Trade · APIs PROD', signal: prodTradeApis.signal, detail: prodTradeApis.detail },
    { label: 'Trade · PROD matrix', signal: snapshot.tradeProd.signal, detail: snapshot.tradeProd.detail },
    { label: 'Trade · PROD gate', signal: prodGate.signal, detail: prodGate.detail },
  ]

  return (
    <div className={cn('flex min-h-0 flex-col', summaryColumn ? 'h-full gap-1.5' : 'gap-2')}>
      {showProdBanner && <ProdBlockedBanner context="satellite" />}
      <SharedRocketStrip
        rocket={rocket}
        isLoading={rocketLoading}
        compact={compact || summaryColumn}
        onNavigate={onNavigate}
        canOperate={canOperate}
      />
      <div
        className={cn(
          'grid min-h-0 gap-2',
          summaryColumn ? 'flex-1 md:grid-cols-2 [&>*]:h-full' : 'gap-2',
        )}
      >
        <EnvironmentReadinessPanel
          title={summaryColumn ? 'STG trade' : 'STG environment readiness'}
          icon={Satellite}
          overall={stgOverall}
          isLoading={stgLoading}
          compact={compact}
          summaryColumn={summaryColumn}
          readinessAnchor="stg"
          chips={[
            { label: 'Trade · K8s STG', signal: stgK8s.signal, detail: stgK8s.detail },
            { label: 'Ground · PG / Redis', signal: stgDatastore.signal, detail: stgDatastore.detail },
            { label: 'Trade · APIs STG', signal: stgTradeApis.signal, detail: stgTradeApis.detail },
          ]}
          linkLabel="Satellite Bus →"
          onLink={() => onNavigate('satellite-bus')}
          onNavigate={onNavigate}
          fixCtx={{ modeId: 'satellite-deploy', env: 'stg' }}
          canOperate={canOperate}
          onAgentFix={onAgentFixStg}
          agentFixPending={agentFixPending}
          agentFixDisabled={agentFixDisabled}
          agentFixTitle={agentFixTitle}
        />
        <EnvironmentReadinessPanel
          title={summaryColumn ? 'Trade Prod' : 'PROD environment readiness'}
          icon={Satellite}
          overall={prodOverallLocal}
          isLoading={prodLoading}
          compact={compact}
          summaryColumn={summaryColumn}
          readinessAnchor="trade-prod"
          chips={prodChips}
          linkLabel="Trade Release →"
          onLink={() => onNavigate('trade-release')}
          onNavigate={onNavigate}
          fixCtx={{ modeId: 'satellite-deploy', env: 'prod' }}
          canOperate={canOperate}
          onAgentFix={onAgentFixProd}
          agentFixPending={agentFixPending}
          agentFixDisabled={agentFixDisabled}
          agentFixTitle={agentFixTitle}
        />
      </div>
      {!suppressProdBlockedBanner && rocketBlocked && !showProdBanner && (
        <OpsFeedback variant="warning" title="Fix Rocket IB bus before release">
          Shared Rocket readiness is {missionStatus(rocket.signal)} — resolve Platform IB Gateway /
          socket consumers before deploying.
        </OpsFeedback>
      )}
    </div>
  )
}

export type TaskModeReadinessStripProps = {
  modeId: TaskModeId
  onNavigate: (tabId: string) => void
  compact?: boolean
  summaryColumn?: boolean
  suppressProdBlockedBanner?: boolean
  canOperate?: boolean
  onAgentFixStg?: () => void
  onAgentFixProd?: () => void
  agentFixPending?: boolean
  agentFixDisabled?: boolean
  agentFixTitle?: string
}

/** Mode-scoped environment readiness — replaces generic mission signals in playbook ops modes. */
export function TaskModeReadinessStrip({
  modeId,
  onNavigate,
  compact = false,
  summaryColumn = false,
  suppressProdBlockedBanner = false,
  canOperate = false,
  onAgentFixStg,
  onAgentFixProd,
  agentFixPending = false,
  agentFixDisabled = false,
  agentFixTitle,
}: TaskModeReadinessStripProps) {
  if (modeId === 'rocket-launch') {
    return (
      <RocketReadinessStrip
        compact={compact}
        summaryColumn={summaryColumn}
        suppressProdBlockedBanner={suppressProdBlockedBanner}
        onNavigate={onNavigate}
      />
    )
  }
  if (modeId === 'satellite-deploy') {
    return (
      <SatelliteReadinessStrip
        compact={compact}
        summaryColumn={summaryColumn}
        suppressProdBlockedBanner={suppressProdBlockedBanner}
        onNavigate={onNavigate}
        canOperate={canOperate}
        onAgentFixStg={onAgentFixStg}
        onAgentFixProd={onAgentFixProd}
        agentFixPending={agentFixPending}
        agentFixDisabled={agentFixDisabled}
        agentFixTitle={agentFixTitle}
      />
    )
  }
  return null
}

/** Condensed mission signals for Daily Ops (unchanged generic strip). */
export function DailyOpsMissionStrip({ compact = false }: { compact?: boolean }) {
  const { snapshot, isLoading } = useMissionSnapshot()
  const status = missionStatus(snapshot.missionOverall)
  const color = missionStatusColor(status)

  return (
    <div className="rounded-lg border border-border bg-secondary px-3 py-2.5">
      <div className="flex flex-wrap items-center gap-2">
        <Gauge size={16} style={{ color }} />
        <span className="text-[var(--text-dense-label)] font-semibold">Mission signals</span>
        <StatusLamp value={snapshot.missionOverall} kind="reach" />
        <DenseTag variant={status === 'NOMINAL' ? 'success' : status === 'CRITICAL' ? 'danger' : 'warning'}>
          {isLoading ? 'Probing…' : status}
        </DenseTag>
      </div>
      <div className={`mt-2 grid gap-2 ${compact ? 'grid-cols-1' : 'sm:grid-cols-3'}`}>
        <ReadinessChip
          label="Rocket"
          signal={snapshot.rocketOverall}
          detail={snapshot.release.detail}
          title="Rocket scope — Platform release, IB Gateway, supply chain"
        />
        <ReadinessChip
          label="Payload"
          signal={snapshot.payloadOverall}
          detail={snapshot.tradeProd.detail}
          title="Trade scope — per-env APIs, sockets, prod matrix"
        />
        <ReadinessChip
          label="Infra"
          signal={snapshot.infra.signal}
          detail={snapshot.infra.detail}
          title="Ground scope — cluster domains, PG/Redis, observability"
        />
      </div>
    </div>
  )
}
