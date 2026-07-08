import { useMemo } from 'react'
import { useQuery } from '@tanstack/react-query'
import { DenseTag, StatusLamp } from '@bifrost/ui'
import { AlertTriangle, Gauge, Rocket, Satellite, type LucideIcon } from 'lucide-react'
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
  SelfHealthProbe,
} from '@/api/types'
import type { MatrixResponse, Reachability } from '@/api/types'
import { OpsFeedback } from '@/components/feedback/OpsFeedback'
import { useMissionSnapshot } from '@/hooks/useMissionSnapshot'
import {
  infraSignal,
  missionStatus,
  missionStatusColor,
  worst,
  type ModuleState,
  type Signal,
} from '@/lib/control-room/missionSignals'
import type { TaskModeId } from '@/lib/task-mode/types'

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
}

function ReadinessChip({ label, signal, detail }: ReadinessChipProps) {
  return (
    <div className="rounded border border-border/60 bg-card px-2 py-1.5">
      <div className="flex items-center gap-1.5">
        <StatusLamp value={signal} kind="reach" />
        <span className="text-[var(--text-dense-meta)] font-medium">{label}</span>
      </div>
      <p className="m-0 mt-0.5 truncate text-[var(--text-dense-caption)] text-muted-foreground">{detail}</p>
    </div>
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

function ibSocketSignal(
  ingestReach: Reachability | undefined,
  components: Array<{ reachability?: Reachability } | undefined>,
): { signal: Signal; detail: string } {
  const signals = [ingestReach, ...components.map(c => c?.reachability)].filter(Boolean) as Reachability[]
  if (signals.length === 0) return { signal: 'unknown', detail: 'probing' }
  const signal = worst(...signals.map(r => r as Signal))
  const ok = signals.filter(r => r === 'ok').length
  return { signal, detail: `${ok}/${signals.length} IB processes OK` }
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
  linkLabel: string
  onLink: () => void
}

function EnvironmentReadinessPanel({
  title,
  icon: Icon,
  overall,
  isLoading,
  chips,
  compact = false,
  linkLabel,
  onLink,
}: EnvironmentReadinessPanelProps) {
  const tag = stripOverallTag(overall, isLoading)
  const color = missionStatusColor(missionStatus(overall))

  return (
    <div className="rounded-lg border border-border bg-secondary px-3 py-2.5">
      <div className="flex flex-wrap items-center gap-2">
        <Icon size={16} style={{ color }} />
        <span className="text-[var(--text-dense-label)] font-semibold">{title}</span>
        <StatusLamp value={overall} kind="reach" />
        <DenseTag variant={tag.variant}>{tag.label}</DenseTag>
      </div>
      <div className={`mt-2 grid gap-2 ${compact ? 'grid-cols-1' : 'sm:grid-cols-2'}`}>
        {chips.map(chip => (
          <ReadinessChip key={chip.label} label={chip.label} signal={chip.signal} detail={chip.detail} />
        ))}
      </div>
      <button
        type="button"
        className="mt-2 text-[var(--text-dense-meta)] text-primary hover:underline"
        onClick={onLink}
      >
        {linkLabel}
      </button>
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

  const ib = useMemo(() => {
    if (busReach == null) return { signal: 'unknown' as Signal, detail: 'probing' }
    const socket = busReach.monitor.socket
    return ibSocketSignal(busReach.ingest.reachability, [
      socket.ib_ingestor,
      socket.ib_account_agent,
      socket.ib_operator,
      socket.massive,
    ])
  }, [busReach])

  const tradeApis = useMemo(() => tradeApiSummary(prodMatrix), [prodMatrix])
  const tradeSnapshot = snapshot.tradeProd
  const gate = useMemo(() => releaseGateSignal(prodGateQ.data), [prodGateQ.data])

  const prodOverall = worst(
    k8s.signal,
    datastore.signal,
    ib.signal,
    tradeApis.signal,
    tradeSnapshot.signal,
    gate.signal,
  )

  const isLoading =
    missionLoading || clusterQ.isLoading || busQ.isLoading || prodGateQ.isLoading

  const prodBlocked = isProdReleaseBlocked(prodOverall)

  return {
    prodOverall,
    prodBlocked,
    isLoading,
    prodDisabledReason: prodBlocked ? 'Prod readiness blocked — fix environment first' : undefined,
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
  }
}

function RocketReadinessStrip({
  compact = false,
  onNavigate,
}: {
  compact?: boolean
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

  const showProdBanner = isProdReleaseBlocked(prodOverallLocal) || isProdReleaseBlocked(prodOverall)

  return (
    <div className="flex flex-col gap-2">
      {showProdBanner && <ProdBlockedBanner context="rocket" />}
      <EnvironmentReadinessPanel
        title="Platform STG readiness"
        icon={Rocket}
        overall={stgOverall}
        isLoading={stgLoading}
        compact={compact}
        chips={[
          { label: 'K8s · Platform STG NS', signal: k8sStg.signal, detail: k8sStg.detail },
          { label: 'CI/CD', signal: cicdSignal, detail: cicdDetail },
          { label: 'Self-health STG', signal: selfStg.signal, detail: selfStg.detail },
          { label: 'STG release gate', signal: stgGate.signal, detail: stgGate.detail },
          { label: 'Supply chain', signal: snapshot.release.signal, detail: snapshot.release.detail },
        ]}
        linkLabel="Platform Release →"
        onLink={() => onNavigate('platform-release')}
      />
      <EnvironmentReadinessPanel
        title="PROD environment readiness"
        icon={Rocket}
        overall={prodOverallLocal}
        isLoading={prodPanelLoading}
        compact={compact}
        chips={[
          { label: 'K8s · Platform PROD NS', signal: k8sProd.signal, detail: k8sProd.detail },
          { label: 'Self-health PROD', signal: selfProd.signal, detail: selfProd.detail },
          { label: 'PROD release gate', signal: prodGate.signal, detail: prodGate.detail },
          { label: 'Supply chain', signal: snapshot.release.signal, detail: snapshot.release.detail },
        ]}
        linkLabel="Platform Release →"
        onLink={() => onNavigate('platform-release')}
      />
    </div>
  )
}

function SatelliteReadinessStrip({
  compact = false,
  onNavigate,
}: {
  compact?: boolean
  onNavigate: (tabId: string) => void
}) {
  const { snapshot, matrices, isLoading: missionLoading } = useMissionSnapshot()
  const { prodOverall, prodBlocked } = useSatelliteProdReadiness()

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

  const ibForBus = (busReach: SatelliteBusDeepResponse | undefined) => {
    if (busReach == null) return { signal: 'unknown' as Signal, detail: 'probing' }
    const socket = busReach.monitor.socket
    return ibSocketSignal(busReach.ingest.reachability, [
      socket.ib_ingestor,
      socket.ib_account_agent,
      socket.ib_operator,
      socket.massive,
    ])
  }

  const stgIb = useMemo(() => ibForBus(stgBusReach), [stgBusReach])
  const prodIb = useMemo(() => ibForBus(prodBusReach), [prodBusReach])

  const stgTradeApis = useMemo(() => tradeApiSummary(stgMatrix), [stgMatrix])
  const prodTradeApis = useMemo(() => tradeApiSummary(prodMatrix), [prodMatrix])
  const prodGate = useMemo(() => releaseGateSignal(prodGateQ.data), [prodGateQ.data])

  const stgOverall = worst(stgK8s.signal, stgDatastore.signal, stgIb.signal, stgTradeApis.signal)
  const prodOverallLocal = worst(
    prodK8s.signal,
    prodDatastore.signal,
    prodIb.signal,
    prodTradeApis.signal,
    snapshot.tradeProd.signal,
    prodGate.signal,
  )

  const stgLoading =
    missionLoading || stgBusQ.isLoading || clusterDetailQ.isLoading
  const prodLoading =
    missionLoading || prodBusQ.isLoading || clusterDetailQ.isLoading || prodGateQ.isLoading

  const showProdBanner = prodBlocked || isProdReleaseBlocked(prodOverallLocal) || isProdReleaseBlocked(prodOverall)

  return (
    <div className="flex flex-col gap-2">
      {showProdBanner && <ProdBlockedBanner context="satellite" />}
      <EnvironmentReadinessPanel
        title="STG environment readiness"
        icon={Satellite}
        overall={stgOverall}
        isLoading={stgLoading}
        compact={compact}
        chips={[
          { label: 'K8s · STG NS', signal: stgK8s.signal, detail: stgK8s.detail },
          { label: 'PG / Redis', signal: stgDatastore.signal, detail: stgDatastore.detail },
          { label: 'IB socket', signal: stgIb.signal, detail: stgIb.detail },
          { label: 'Trade APIs', signal: stgTradeApis.signal, detail: stgTradeApis.detail },
        ]}
        linkLabel="Satellite Bus →"
        onLink={() => onNavigate('satellite-bus')}
      />
      <EnvironmentReadinessPanel
        title="PROD environment readiness"
        icon={Satellite}
        overall={prodOverallLocal}
        isLoading={prodLoading}
        compact={compact}
        chips={[
          { label: 'K8s · PROD NS', signal: prodK8s.signal, detail: prodK8s.detail },
          { label: 'PG / Redis', signal: prodDatastore.signal, detail: prodDatastore.detail },
          { label: 'IB socket', signal: prodIb.signal, detail: prodIb.detail },
          { label: 'Trade APIs', signal: prodTradeApis.signal, detail: prodTradeApis.detail },
          { label: 'Trade prod matrix', signal: snapshot.tradeProd.signal, detail: snapshot.tradeProd.detail },
          { label: 'PROD release gate', signal: prodGate.signal, detail: prodGate.detail },
        ]}
        linkLabel="Trade Release →"
        onLink={() => onNavigate('trade-release')}
      />
    </div>
  )
}

export type TaskModeReadinessStripProps = {
  modeId: TaskModeId
  onNavigate: (tabId: string) => void
  compact?: boolean
}

/** Mode-scoped environment readiness — replaces generic mission signals in playbook ops modes. */
export function TaskModeReadinessStrip({ modeId, onNavigate, compact = false }: TaskModeReadinessStripProps) {
  if (modeId === 'rocket-launch') {
    return <RocketReadinessStrip compact={compact} onNavigate={onNavigate} />
  }
  if (modeId === 'satellite-deploy') {
    return <SatelliteReadinessStrip compact={compact} onNavigate={onNavigate} />
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
        <ReadinessChip label="Rocket" signal={snapshot.rocketOverall} detail={snapshot.release.detail} />
        <ReadinessChip label="Payload" signal={snapshot.payloadOverall} detail={snapshot.tradeProd.detail} />
        <ReadinessChip label="Infra" signal={snapshot.infra.signal} detail={snapshot.infra.detail} />
      </div>
    </div>
  )
}
