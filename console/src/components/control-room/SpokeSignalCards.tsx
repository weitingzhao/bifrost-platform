import { useQuery } from '@tanstack/react-query'
import { Bot, ChevronRight, Plug, Satellite, type LucideIcon } from 'lucide-react'
import { StatusLamp } from '@/components/StatusLamp'
import { fetchClusterNodes } from '@/api/cluster'
import { fetchMatrix, fetchSatelliteBusDeep, isAllMatrices, isAllSatelliteBusDeep } from '@/api/core'
import { fetchNetworkAudit, fetchNetworkStatus } from '@/api/network'
import { fetchRemediationJobs } from '@/api/remediation'
import { fetchRetrospectiveReport } from '@/api/agentOps'
import type { MatrixResponse, Reachability } from '@/api/matrixTypes'
import { findActiveRemediationJobs } from '@/lib/remediation/remediationJobDisplay'
import { signalColor, worst, type Signal } from '@/lib/control-room/missionSignals'

const REFETCH_MS = 30_000

function tradeApiTargets(matrix: MatrixResponse | undefined): { ok: number; total: number } {
  if (matrix == null) return { ok: 0, total: 0 }
  const tradeTargets = matrix.targets.filter(
    t =>
      t.category === 'trade_api' ||
      t.category === 'trade_frontend' ||
      t.id === 'nginx-spa' ||
      t.id.startsWith('api-'),
  )
  const ok = tradeTargets.filter(t => t.reachability === 'ok').length
  return { ok, total: tradeTargets.length }
}

function reachToSignal(r: Reachability | undefined): Signal {
  if (r == null) return 'unknown'
  return r as Signal
}

function networkReach(
  status: Awaited<ReturnType<typeof fetchNetworkStatus>> | undefined,
  audit: Awaited<ReturnType<typeof fetchNetworkAudit>> | undefined,
  statusError: boolean,
): Signal {
  if (status == null && statusError) return 'unknown'
  if (status?.error != null && status.error !== '') {
    if (status.hint != null) return 'unknown'
    return 'fail'
  }
  if (status?.reachable !== true) return 'fail'
  if (audit?.classification === 'POLICY_NOMINAL') return 'ok'
  if (audit?.classification === 'POLICY_DRIFT') return 'degraded'
  return 'degraded'
}

function healthScoreSignal(score: number | undefined): Signal {
  if (score == null) return 'unknown'
  if (score >= 90) return 'ok'
  if (score >= 70) return 'degraded'
  return 'fail'
}

function SpokeCard({
  icon: Icon,
  title,
  signal,
  summary,
  detail,
  onOpen,
  linkLabel,
  secondaryLink,
}: {
  icon: LucideIcon
  title: string
  signal: Signal
  summary: string
  detail?: string
  onOpen: () => void
  linkLabel: string
  secondaryLink?: { label: string; onOpen: () => void }
}) {
  return (
    <div className="mission-rocket-card-wrap">
      <button type="button" className="mission-rocket-card" onClick={onOpen} title={detail ?? summary}>
        <Icon size={20} className="mission-rocket-card-icon" style={{ color: signalColor(signal) }} />
        <div className="mission-rocket-card-body">
          <div className="flex items-center gap-2">
            <StatusLamp value={signal} kind="reach" />
            <div className="mission-rocket-card-name">{title}</div>
          </div>
          <div className="mission-rocket-card-val">{summary}</div>
          {detail != null && detail !== '' && (
            <div className="mission-rocket-card-role">{detail}</div>
          )}
        </div>
        <ChevronRight size={14} className="shrink-0 text-muted-foreground" />
      </button>
      <div className="flex flex-wrap gap-x-3 px-3 pb-2">
        <button type="button" className="focus-strip-link text-[var(--text-dense-meta)]" onClick={onOpen}>
          {linkLabel}
        </button>
        {secondaryLink != null && (
          <button
            type="button"
            className="focus-strip-link text-[var(--text-dense-meta)]"
            onClick={secondaryLink.onOpen}
          >
            {secondaryLink.label}
          </button>
        )}
      </div>
    </div>
  )
}

export interface SpokeSignalCardsProps {
  onOpenSatelliteBus: () => void
  onOpenNetwork: () => void
  onOpenCluster: () => void
  onOpenAgentDesk: () => void
  onOpenDefects: () => void
}

export function SpokeSignalCards({
  onOpenSatelliteBus,
  onOpenNetwork,
  onOpenCluster,
  onOpenAgentDesk,
  onOpenDefects,
}: SpokeSignalCardsProps) {
  const matrixQuery = useQuery({
    queryKey: ['spoke', 'matrix'],
    queryFn: () => fetchMatrix(),
    refetchInterval: REFETCH_MS,
  })

  const busQuery = useQuery({
    queryKey: ['spoke', 'bus', 'stg'],
    queryFn: () => fetchSatelliteBusDeep('stg'),
    refetchInterval: REFETCH_MS,
  })

  const networkStatusQuery = useQuery({
    queryKey: ['spoke', 'network', 'status'],
    queryFn: fetchNetworkStatus,
    refetchInterval: REFETCH_MS,
    retry: 1,
  })

  const networkAuditQuery = useQuery({
    queryKey: ['spoke', 'network', 'audit'],
    queryFn: fetchNetworkAudit,
    refetchInterval: REFETCH_MS,
    retry: 1,
    enabled: networkStatusQuery.data?.reachable === true,
  })

  const nodesQuery = useQuery({
    queryKey: ['spoke', 'cluster', 'nodes'],
    queryFn: fetchClusterNodes,
    refetchInterval: REFETCH_MS,
  })

  const jobsQuery = useQuery({
    queryKey: ['spoke', 'remediation', 'jobs'],
    queryFn: fetchRemediationJobs,
    refetchInterval: REFETCH_MS,
  })

  const defectsQuery = useQuery({
    queryKey: ['spoke', 'retrospective', 'report'],
    queryFn: () => fetchRetrospectiveReport(),
    refetchInterval: REFETCH_MS,
    retry: 1,
  })

  const matrices = (() => {
    const data = matrixQuery.data
    if (data == null) return [] as MatrixResponse[]
    return isAllMatrices(data) ? data.matrices : [data]
  })()

  const stgMatrix = matrices.find((m): m is MatrixResponse => 'environment' in m && m.environment === 'stg')
  const tradeApi = tradeApiTargets(stgMatrix)

  const busData = busQuery.data
  const busReach =
    busData == null
      ? undefined
      : isAllSatelliteBusDeep(busData)
        ? busData.buses.find(b => b.environment === 'stg')?.reachability
        : busData.reachability

  const satelliteSignal = worst(
    reachToSignal(busReach),
    tradeApi.total === 0 ? 'unknown' : tradeApi.ok === tradeApi.total ? 'ok' : tradeApi.ok > 0 ? 'degraded' : 'fail',
  )

  const satelliteSummary =
    matrixQuery.isLoading || busQuery.isLoading
      ? 'Probing…'
      : `${String(busReach ?? 'unknown').toUpperCase()} bus · ${tradeApi.ok}/${tradeApi.total} APIs`

  const networkSignal = networkReach(
    networkStatusQuery.data,
    networkAuditQuery.data,
    networkStatusQuery.isError,
  )
  const nodes = nodesQuery.data?.nodes ?? []
  const readyNodes = nodes.filter(n => n.status === 'Ready').length
  const nodeSignal: Signal =
    nodes.length === 0 ? 'unknown' : readyNodes === nodes.length ? 'ok' : readyNodes > 0 ? 'degraded' : 'fail'
  const groundSignal = worst(networkSignal, nodeSignal)

  const networkHost = networkStatusQuery.data?.host ?? 'UCG'
  const groundSummary =
    networkStatusQuery.isLoading || nodesQuery.isLoading
      ? 'Probing…'
      : `${networkHost} · ${readyNodes}/${nodes.length} nodes`

  const activeJobs = findActiveRemediationJobs(jobsQuery.data?.jobs ?? [])
  const healthScore = defectsQuery.data?.health_score
  const engineerSignal = healthScoreSignal(healthScore)

  const engineerSummary =
    jobsQuery.isLoading || defectsQuery.isLoading
      ? 'Probing…'
      : `${activeJobs.length} active · score ${healthScore != null ? healthScore.toFixed(0) : '—'}`

  return (
    <div className="mission-rocket-grid">
      <SpokeCard
        icon={Satellite}
        title="Satellite"
        signal={satelliteSignal}
        summary={satelliteSummary}
        detail="Trade payload bus + L0 API probes (STG)"
        onOpen={onOpenSatelliteBus}
        linkLabel="Satellite → Bus Status"
      />
      <SpokeCard
        icon={Plug}
        title="Plugin"
        signal={groundSignal}
        summary={groundSummary}
        detail="UniFi Network · IB Gateway / Market Data manage pages"
        onOpen={onOpenNetwork}
        linkLabel="Plugin → Network"
        secondaryLink={{ label: 'Rocket → Cluster', onOpen: onOpenCluster }}
      />
      <SpokeCard
        icon={Bot}
        title="Engineer"
        signal={engineerSignal}
        summary={engineerSummary}
        detail="Remediation jobs + cross-job defect health"
        onOpen={activeJobs.length > 0 ? onOpenAgentDesk : onOpenDefects}
        linkLabel={activeJobs.length > 0 ? 'Engineer → Agent Desk' : 'Engineer → Defects'}
        secondaryLink={
          activeJobs.length > 0
            ? { label: 'Engineer → Defects', onOpen: onOpenDefects }
            : { label: 'Engineer → Agent Desk', onOpen: onOpenAgentDesk }
        }
      />
    </div>
  )
}
