import type { ReactNode, Ref, RefObject } from 'react'
import { useQuery } from '@tanstack/react-query'
import { useCallback, useEffect, useMemo, useRef, useState } from 'react'
import {
  DenseDataTable,
  DenseTableBody,
  DenseTableCell,
  DenseTableHead,
  DenseTableHeadRow,
  DenseTableHeader,
  DenseTableRow,
  DenseTag,
  PageHeader,
  SegmentControl,
  cn,
} from '@bifrost/ui'
import {
  fetchSatelliteBusDeep,
  fetchClusterMetrics,
  fetchClusterObservability,
  fetchClusterServiceReadiness,
  fetchClusterWorkloads,
  fetchMatrix,
  isAllMatrices,
  isAllSatelliteBusDeep,
} from '@/api/platform'
import type { MatrixResponse, Reachability, SatelliteBusDeepResponse, Target } from '@/api/types'
import { AgentTriggerButton } from '@/components/agent/AgentTriggerButton'
import { ClusterServiceReadinessPanel } from '@/components/cluster/ClusterServiceReadinessPanel'
import { OpsSection, OpsSubsectionTitle } from '@/components/layout/OpsSection'
import { SatelliteObservabilityStrip } from '@/components/satellite/SatelliteObservabilityStrip'
import { StatusLamp } from '@/components/StatusLamp'
import { useAmbientAgentTask } from '@/hooks/useAmbientAgentTask'
import { usePlatformAuth } from '@/hooks/usePlatformAuth'
import type { AmbientAgentShellProps } from '@/lib/agent/ambientAgent'
import { scopeToLabel } from '@/lib/agent/agentTaskCatalog'
import {
  buildSatelliteBusIngestTriagePrompt,
  SATELLITE_BUS_INGEST_TRIAGE_SCOPE,
  summarizeIngestServices,
} from '@/lib/agent/satelliteBusIngestTriagePrompt'
import {
  buildPayloadReadinessRows,
  type PayloadReadinessRow,
} from '@/lib/control-room/payloadReadiness'
import {
  buildSocketHealthMatrix,
  classifyTradingDaemon,
  formatBusProbeDetail,
  SOCKET_MATRIX_LABELS,
  summarizeSocketHealthAllEnvs,
  type BusEnvId,
  type SocketHealthEnvCell,
  type SocketHealthMatrixRow,
  type SocketHealthRow,
  type SocketRequiredState,
  type TradeEnvId,
} from '@/lib/satellite/socketHealthSemantics'
import {
  busScopeGroupClass,
  tradeSingleEnvProbeSource,
  tradeSingleEnvScope,
  type BusStatusScope,
} from '@/lib/satellite/busStatusScope'
import { consumeSatelliteBusFocus } from '@/lib/task-mode/readinessChipActions'
import { worst, type Signal } from '@/lib/control-room/missionSignals'

const TRADE_ENV_OPTIONS = [
  { value: 'dev', label: 'Dev' },
  { value: 'stg', label: 'Stg' },
  { value: 'prod', label: 'Prod' },
] as const

type TradeEnv = (typeof TRADE_ENV_OPTIONS)[number]['value']

const TRADE_NS: Record<TradeEnv, string> = {
  dev: 'bifrost-dev',
  stg: 'bifrost-stg',
  prod: 'bifrost-prod',
}

const SATELLITE_DOMAIN_IDS = ['workers', 'applications', 'database', 'redis'] as const

const CRITICAL_PROCESS_PATTERNS = [
  { pattern: /daemon/i, label: 'GsTrading daemon' },
  { pattern: /ingestor|ib-ingest/i, label: 'IB Ingestor' },
  { pattern: /operator|ib-operator/i, label: 'IB Operator' },
  { pattern: /account/i, label: 'IB Account Agent' },
  { pattern: /massive/i, label: 'Massive WS' },
  { pattern: /celery|worker/i, label: 'Celery worker' },
  { pattern: /flower/i, label: 'Flower' },
]

function tradeApiTargets(matrix: MatrixResponse | undefined): { ok: number; total: number } {
  if (matrix == null) return { ok: 0, total: 0 }
  const tradeTargets = filterTradeApiTargets(matrix)
  const ok = tradeTargets.filter(t => t.reachability === 'ok').length
  return { ok, total: tradeTargets.length }
}

function filterTradeApiTargets(matrix: MatrixResponse): Target[] {
  return matrix.targets.filter(
    t =>
      t.category === 'trade_api' ||
      t.category === 'trade_frontend' ||
      t.id === 'nginx-spa' ||
      t.id.startsWith('api-'),
  )
}

function TradeApiReachTable({
  targets,
  loading,
}: {
  targets: Target[]
  loading: boolean
}) {
  return (
    <DenseDataTable>
      <DenseTableHeader>
        <DenseTableHeadRow>
          <DenseTableHead>Target</DenseTableHead>
          <DenseTableHead>Category</DenseTableHead>
          <DenseTableHead>Reach</DenseTableHead>
          <DenseTableHead>Detail</DenseTableHead>
        </DenseTableHeadRow>
      </DenseTableHeader>
      <DenseTableBody>
        {loading ? (
          <DenseTableRow>
            <DenseTableCell colSpan={4} className="text-[var(--muted-foreground)]">
              Loading…
            </DenseTableCell>
          </DenseTableRow>
        ) : targets.length === 0 ? (
          <DenseTableRow>
            <DenseTableCell colSpan={4} className="text-[var(--muted-foreground)]">
              —
            </DenseTableCell>
          </DenseTableRow>
        ) : (
          targets.map(t => (
            <DenseTableRow key={t.id}>
              <DenseTableCell className="font-mono-tabular text-[var(--text-dense-meta)] font-medium">
                {t.id}
              </DenseTableCell>
              <DenseTableCell className="text-[var(--text-dense-caption)] text-muted-foreground">
                {t.category}
              </DenseTableCell>
              <DenseTableCell>
                <StatusLamp value={t.reachability} kind="reach" />{' '}
                <span className="text-[var(--text-dense-caption)]">{t.reachability}</span>
              </DenseTableCell>
              <DenseTableCell className="text-[var(--text-dense-caption)] text-muted-foreground">
                {t.detail ?? '—'}
              </DenseTableCell>
            </DenseTableRow>
          ))
        )}
      </DenseTableBody>
    </DenseDataTable>
  )
}

function signalFromReach(r: Reachability | undefined): Signal {
  if (r == null) return 'unknown'
  return r as Signal
}

function signalFromLamp(lamp: string | undefined): Signal {
  switch ((lamp ?? '').toLowerCase()) {
    case 'green':
      return 'ok'
    case 'yellow':
      return 'degraded'
    case 'red':
      return 'fail'
    default:
      return 'unknown'
  }
}

function renderText(value: unknown): string {
  if (value == null) return '—'
  if (typeof value === 'string' && value.trim() === '') return '—'
  if (typeof value === 'object') return JSON.stringify(value)
  return String(value)
}

function BusScopeBadge({ scope }: { scope: BusStatusScope }) {
  const label =
    scope === 'rocket'
      ? 'Rocket'
      : scope === 'trade-multi-env'
        ? 'Trade · all envs'
        : scope === 'trade-single-env'
          ? 'Trade · selected NS'
          : 'Ground'
  const variant: 'info' | 'category' | 'neutral' | 'warning' =
    scope === 'rocket'
      ? 'info'
      : scope === 'trade-multi-env'
        ? 'warning'
        : scope === 'trade-single-env'
          ? 'category'
          : 'neutral'
  return (
    <DenseTag variant={variant} className="shrink-0 text-[10px] uppercase tracking-wide">
      {label}
    </DenseTag>
  )
}

function BusEnvScopePill({ scope, env }: { scope: BusStatusScope; env: TradeEnv }) {
  if (scope === 'rocket') {
    return (
      <DenseTag variant="info" className="text-[10px] uppercase tracking-wide">
        SHARED
      </DenseTag>
    )
  }
  if (scope === 'trade-multi-env') {
    return (
      <DenseTag variant="warning" className="text-[10px] uppercase tracking-wide">
        ALL ENVS
      </DenseTag>
    )
  }
  if (scope === 'ground') {
    return (
      <DenseTag variant="neutral" className="text-[10px] uppercase tracking-wide">
        PLATFORM
      </DenseTag>
    )
  }
  return (
    <DenseTag variant="success" className="text-[10px] uppercase tracking-wide">
      {env.toUpperCase()}
    </DenseTag>
  )
}

function BusPageGroup({
  title,
  description,
  scope,
  tradeEnv,
  sectionRef,
  highlight,
  children,
}: {
  title: string
  description?: string
  scope: BusStatusScope
  tradeEnv?: TradeEnv
  sectionRef?: Ref<HTMLDivElement>
  highlight?: boolean
  children: ReactNode
}) {
  return (
    <div
      ref={sectionRef}
      data-scope={scope}
      className={cn(
        'satellite-bus-group panel-elevated flex flex-col overflow-hidden rounded-md transition-shadow',
        busScopeGroupClass(scope),
        highlight && 'ring-1 ring-[var(--ring)] ring-offset-1 ring-offset-[var(--background)]',
      )}
    >
      <header className="satellite-bus-group-header">
        <div className="flex min-w-0 flex-1 flex-wrap items-center gap-x-2 gap-y-0.5">
          <BusScopeBadge scope={scope} />
          <h3 className="satellite-bus-group-title">{title}</h3>
          {description != null && description !== '' && (
            <span className="text-[var(--text-dense-caption)] text-muted-foreground">{description}</span>
          )}
        </div>
        <BusEnvScopePill scope={scope} env={tradeEnv ?? 'stg'} />
      </header>
      <div className="satellite-bus-group-body flex flex-col">{children}</div>
    </div>
  )
}

function DaemonBusStatusStrip({
  row,
  loading,
}: {
  row: SocketHealthRow | null
  loading: boolean
}) {
  if (loading) {
    return (
      <p className="m-0 px-3 py-2 text-[var(--text-dense-caption)] text-muted-foreground border-b border-[var(--border)]">
        Loading bus interpretation…
      </p>
    )
  }
  if (row == null) {
    return (
      <p className="m-0 px-3 py-2 text-[var(--text-dense-caption)] text-muted-foreground border-b border-[var(--border)]">
        No daemon probe for this environment.
      </p>
    )
  }
  return (
    <div className="flex flex-wrap items-center gap-2 border-b border-[var(--border)] px-3 py-2">
      <span className="text-[var(--text-dense-caption)] font-medium text-muted-foreground shrink-0">Bus status</span>
      <StatusLamp value={row.reach} kind="reach" />
      <DenseTag
        variant={
          row.reach === 'ok' ? 'success' : row.reach === 'fail' ? 'danger' : row.reach === 'degraded' ? 'warning' : 'neutral'
        }
        className="text-[9px]"
      >
        {row.reachLabel}
      </DenseTag>
      <span className="text-[var(--text-dense-meta)] text-muted-foreground">{row.detail}</span>
    </div>
  )
}

function BusSummaryCard({
  label,
  signal,
  headline,
  scope,
  selected,
  onClick,
}: {
  label: string
  signal: Signal
  headline: string
  scope: BusStatusScope
  selected?: boolean
  onClick: () => void
}) {
  return (
    <button
      type="button"
      onClick={onClick}
      aria-pressed={selected === true}
      className={cn(
        'satellite-bus-summary-card flex min-w-[8.5rem] flex-1 flex-col gap-0.5 rounded-md border border-[var(--border)] px-2 py-1.5 text-left transition-colors',
        `satellite-bus-summary-card--${scope}`,
        selected
          ? 'border-[var(--ring)] bg-[var(--accent)]'
          : 'bg-[var(--secondary)] hover:bg-[var(--accent)]',
      )}
    >
      <span className="flex items-center gap-1.5 text-[var(--text-dense-caption)] font-medium text-muted-foreground">
        <StatusLamp value={signal} kind="reach" />
        {label}
      </span>
      <span className="text-[var(--text-dense-caption)] leading-snug text-foreground/90">{headline}</span>
    </button>
  )
}

function RequiredTag({ state }: { state: SocketRequiredState }) {
  if (state === 'policy-off') {
    return <DenseTag variant="neutral">policy-off</DenseTag>
  }
  if (state === 'optional') {
    return <DenseTag variant="neutral">optional</DenseTag>
  }
  return <DenseTag variant="success">required</DenseTag>
}

function SocketHealthEnvCellView({
  cell,
  selected,
}: {
  cell: SocketHealthEnvCell
  selected?: boolean
}) {
  const lampReach = cell.required === 'policy-off' ? 'ok' : cell.reach
  return (
    <div
      className={cn(
        'flex flex-col gap-0.5',
        selected && 'rounded-sm bg-[var(--accent)]/40 px-1 -mx-1',
      )}
    >
      <span className="flex items-center gap-1">
        <StatusLamp value={lampReach} kind="reach" />
        <span
          className={cn(
            'text-[var(--text-dense-caption)]',
            cell.required === 'policy-off' ? 'text-muted-foreground' : '',
          )}
        >
          {cell.reachLabel}
        </span>
        {cell.required === 'policy-off' && (
          <DenseTag variant="neutral" className="text-[9px]">
            policy-off
          </DenseTag>
        )}
      </span>
      <span className="text-[var(--text-dense-caption)] text-muted-foreground line-clamp-2" title={cell.detail}>
        {cell.detail}
      </span>
    </div>
  )
}

function SocketHealthMatrixTable({
  rows,
  selectedEnv,
}: {
  rows: SocketHealthMatrixRow[]
  selectedEnv: TradeEnvId
}) {
  return (
    <DenseDataTable>
      <DenseTableHeader>
        <DenseTableHeadRow>
          <DenseTableHead>Service</DenseTableHead>
          <DenseTableHead className={selectedEnv === 'dev' ? 'bg-[var(--accent)]/30' : undefined}>
            {SOCKET_MATRIX_LABELS.dev}
          </DenseTableHead>
          <DenseTableHead className={selectedEnv === 'stg' ? 'bg-[var(--accent)]/30' : undefined}>
            {SOCKET_MATRIX_LABELS.stg}
          </DenseTableHead>
          <DenseTableHead className={selectedEnv === 'prod' ? 'bg-[var(--accent)]/30' : undefined}>
            {SOCKET_MATRIX_LABELS.prod}
          </DenseTableHead>
          <DenseTableHead>Local</DenseTableHead>
        </DenseTableHeadRow>
      </DenseTableHeader>
      <DenseTableBody>
        {rows.map(row => (
          <DenseTableRow key={row.id}>
            <DenseTableCell className="font-medium text-[var(--text-dense-meta)]">
              {row.label}
              {row.envDiverges && (
                <DenseTag variant="warning" className="ml-1.5 text-[9px]">
                  Diverged
                </DenseTag>
              )}
            </DenseTableCell>
            <DenseTableCell>
              <SocketHealthEnvCellView cell={row.dev} selected={selectedEnv === 'dev'} />
            </DenseTableCell>
            <DenseTableCell>
              <SocketHealthEnvCellView cell={row.stg} selected={selectedEnv === 'stg'} />
            </DenseTableCell>
            <DenseTableCell>
              <SocketHealthEnvCellView cell={row.prod} selected={selectedEnv === 'prod'} />
            </DenseTableCell>
            <DenseTableCell>
              <SocketHealthEnvCellView cell={row.local} />
            </DenseTableCell>
          </DenseTableRow>
        ))}
      </DenseTableBody>
    </DenseDataTable>
  )
}

function RocketSocketBusRow({ row }: { row: SocketHealthRow }) {
  return (
    <DenseDataTable>
      <DenseTableHeader>
        <DenseTableHeadRow>
          <DenseTableHead>Service</DenseTableHead>
          <DenseTableHead>Required</DenseTableHead>
          <DenseTableHead>Reach</DenseTableHead>
          <DenseTableHead>Detail</DenseTableHead>
        </DenseTableHeadRow>
      </DenseTableHeader>
      <DenseTableBody>
        <DenseTableRow>
          <DenseTableCell className="font-medium text-[var(--text-dense-meta)]">{row.label}</DenseTableCell>
          <DenseTableCell>
            <RequiredTag state={row.required} />
          </DenseTableCell>
          <DenseTableCell>
            <StatusLamp value={row.reach} kind="reach" />{' '}
            <span className="text-[var(--text-dense-caption)]">{row.reachLabel}</span>
          </DenseTableCell>
          <DenseTableCell className="text-[var(--text-dense-caption)] text-muted-foreground">{row.detail}</DenseTableCell>
        </DenseTableRow>
      </DenseTableBody>
    </DenseDataTable>
  )
}

function updateSatelliteBusPageHeight(root: HTMLDivElement | null) {
  if (root == null) return
  const top = root.getBoundingClientRect().top
  root.style.height = `calc(100dvh - ${Math.ceil(top)}px)`
}

function scrollToBusSection(
  ref: RefObject<HTMLDivElement | null>,
  scrollContainerRef: RefObject<HTMLDivElement | null>,
  setHighlight: (key: string | null) => void,
  key: string,
) {
  const container = scrollContainerRef.current
  const el = ref.current
  if (container == null || el == null) return
  setHighlight(key)
  const containerTop = container.getBoundingClientRect().top
  const elTop = el.getBoundingClientRect().top
  const nextTop = container.scrollTop + (elTop - containerTop) - 8
  container.scrollTo({ top: Math.max(0, nextTop), behavior: 'smooth' })
  window.setTimeout(() => setHighlight(null), 1800)
}

type MonitorKvRow = { label: string; value: ReactNode }

function MonitorKvTable({ rows, loading }: { rows: MonitorKvRow[]; loading?: boolean }) {
  return (
    <DenseDataTable>
      <DenseTableHeader>
        <DenseTableHeadRow>
          <DenseTableHead className="w-[38%]">Field</DenseTableHead>
          <DenseTableHead>Value</DenseTableHead>
        </DenseTableHeadRow>
      </DenseTableHeader>
      <DenseTableBody>
        {loading ? (
          <DenseTableRow>
            <DenseTableCell colSpan={2} className="text-[var(--muted-foreground)]">
              Loading…
            </DenseTableCell>
          </DenseTableRow>
        ) : (
          rows.map(row => (
            <DenseTableRow key={row.label}>
              <DenseTableCell className="font-medium text-[var(--text-dense-meta)]">{row.label}</DenseTableCell>
              <DenseTableCell className="text-[var(--text-dense-meta)]">{row.value}</DenseTableCell>
            </DenseTableRow>
          ))
        )}
      </DenseTableBody>
    </DenseDataTable>
  )
}

function PayloadReadinessTable({ rows }: { rows: PayloadReadinessRow[] }) {
  return (
    <DenseDataTable>
      <DenseTableHeader>
        <DenseTableHeadRow>
          <DenseTableHead>Subsystem</DenseTableHead>
          <DenseTableHead>Role</DenseTableHead>
          <DenseTableHead>Dev</DenseTableHead>
          <DenseTableHead>Prod</DenseTableHead>
        </DenseTableHeadRow>
      </DenseTableHeader>
      <DenseTableBody>
        {rows.map(row => (
          <DenseTableRow key={row.id}>
            <DenseTableCell className="font-medium">{row.label}</DenseTableCell>
            <DenseTableCell className="text-[var(--muted-foreground)]">{row.role}</DenseTableCell>
            <DenseTableCell>
              <StatusLamp value={row.dev.signal} kind="reach" />{' '}
              <span className="text-[var(--text-dense-meta)]">{row.dev.detail}</span>
            </DenseTableCell>
            <DenseTableCell>
              <StatusLamp value={row.prod.signal} kind="reach" />{' '}
              <span className="text-[var(--text-dense-meta)]">{row.prod.detail}</span>
              {row.envDiverges && <DenseTag variant="warning" className="ml-2">Diverged</DenseTag>}
            </DenseTableCell>
          </DenseTableRow>
        ))}
      </DenseTableBody>
    </DenseDataTable>
  )
}

export function SatelliteBusPage({
  onOpenCluster,
  onOpenTelemetry,
  onOpenPluginGallery,
  onOpenApiHealth,
  ambientJobId,
  onStartAgentJob,
}: {
  onOpenCluster?: () => void
  onOpenTelemetry?: () => void
  onOpenPluginGallery?: () => void
  onOpenApiHealth?: () => void
} & AmbientAgentShellProps) {
  const { canOperate } = usePlatformAuth()
  const [tradeEnv, setTradeEnv] = useState<TradeEnv>('stg')
  const [highlightSection, setHighlightSection] = useState<string | null>(null)
  const ns = TRADE_NS[tradeEnv]
  const pageRootRef = useRef<HTMLDivElement | null>(null)
  const detailScrollRef = useRef<HTMLDivElement | null>(null)
  const monitorSectionRef = useRef<HTMLDivElement | null>(null)
  const rocketSectionRef = useRef<HTMLDivElement | null>(null)
  const socketSectionRef = useRef<HTMLDivElement | null>(null)
  const tradeApisSectionRef = useRef<HTMLDivElement | null>(null)
  const workersSectionRef = useRef<HTMLDivElement | null>(null)
  const clusterSectionRef = useRef<HTMLDivElement | null>(null)

  useEffect(() => {
    const root = pageRootRef.current
    const update = () => updateSatelliteBusPageHeight(root)
    update()
    window.addEventListener('resize', update)
    const ro = new ResizeObserver(update)
    if (root?.parentElement != null) ro.observe(root.parentElement)
    const chrome = document.querySelector('.console-shell-chrome')
    if (chrome instanceof HTMLElement) ro.observe(chrome)
    return () => {
      window.removeEventListener('resize', update)
      ro.disconnect()
    }
  }, [])

  const focusRefs = useMemo(
    () =>
      ({
        monitor: monitorSectionRef,
        rocket: rocketSectionRef,
        socket: socketSectionRef,
        // Legacy chip focus — Market ingest table removed; Socket matrix is authoritative.
        ingest: socketSectionRef,
        'trade-apis': tradeApisSectionRef,
        workers: workersSectionRef,
        cluster: clusterSectionRef,
      }) as const,
    [],
  )

  useEffect(() => {
    const focus = consumeSatelliteBusFocus()
    if (focus == null) return
    if (focusRefs[focus]?.current == null) return
    requestAnimationFrame(() => {
      scrollToBusSection(focusRefs[focus], detailScrollRef, setHighlightSection, focus)
    })
  }, [focusRefs])

  const matrixQuery = useQuery({
    queryKey: ['matrix', 'all'],
    queryFn: () => fetchMatrix(),
    refetchInterval: 30_000,
  })

  const serviceReadinessQuery = useQuery({
    queryKey: ['cluster', 'service-readiness'],
    queryFn: fetchClusterServiceReadiness,
    refetchInterval: 30_000,
  })

  const workloadsQuery = useQuery({
    queryKey: ['cluster', 'workloads', ns, 'satellite-bus'],
    queryFn: () => fetchClusterWorkloads(ns),
    refetchInterval: 30_000,
  })

  const metricsQuery = useQuery({
    queryKey: ['cluster', 'metrics'],
    queryFn: () => fetchClusterMetrics(8),
    refetchInterval: 30_000,
  })

  const observabilityQuery = useQuery({
    queryKey: ['cluster', 'observability'],
    queryFn: fetchClusterObservability,
    refetchInterval: 30_000,
    retry: false,
  })

  const matrices = useMemo((): MatrixResponse[] => {
    const data = matrixQuery.data
    if (data == null) return []
    if (isAllMatrices(data)) return data.matrices
    return [data]
  }, [matrixQuery.data])

  const payloadRows = useMemo(() => buildPayloadReadinessRows(matrices), [matrices])
  const envMatrix = matrices.find(m => m.environment === tradeEnv)
  const tradeApi = tradeApiTargets(envMatrix)

  const busDeepAllQuery = useQuery({
    queryKey: ['satellite', 'bus-deep', 'all'],
    queryFn: () => fetchSatelliteBusDeep(),
    refetchInterval: 30_000,
  })

  const busesByEnv = useMemo((): Partial<Record<BusEnvId, SatelliteBusDeepResponse>> => {
    const data = busDeepAllQuery.data
    if (data == null) return {}
    if (isAllSatelliteBusDeep(data)) {
      return Object.fromEntries(
        data.buses.map(b => [b.environment as BusEnvId, b]),
      ) as Partial<Record<BusEnvId, SatelliteBusDeepResponse>>
    }
    const env = data.environment as BusEnvId
    if (env === 'dev' || env === 'stg' || env === 'prod' || env === 'dev-local') {
      return { [env]: data }
    }
    return {}
  }, [busDeepAllQuery.data])

  const busDeep = busesByEnv[tradeEnv]

  const busSignal = useMemo((): Signal => {
    const workloadSignals = (workloadsQuery.data?.workloads ?? []).map(w => w.reachability as Signal)
    const domainSignals = (serviceReadinessQuery.data?.domains ?? [])
      .filter(d => SATELLITE_DOMAIN_IDS.includes(d.id as (typeof SATELLITE_DOMAIN_IDS)[number]))
      .map(d => signalFromReach(d.reachability))
    const deepSignal = signalFromReach(busDeep?.reachability)
    return worst(
      deepSignal,
      tradeApi.total === 0 ? 'unknown' : tradeApi.ok === tradeApi.total ? 'ok' : tradeApi.ok > 0 ? 'degraded' : 'fail',
      ...workloadSignals,
      ...domainSignals,
    )
  }, [busDeep?.reachability, serviceReadinessQuery.data?.domains, tradeApi.ok, tradeApi.total, workloadsQuery.data?.workloads])

  const criticalProcesses = useMemo(() => {
    const workloads = workloadsQuery.data?.workloads ?? []
    return CRITICAL_PROCESS_PATTERNS.map(({ pattern, label }) => {
      const match = workloads.find(w => pattern.test(w.name))
      return {
        label,
        name: match?.name ?? '—',
        namespace: match?.namespace ?? ns,
        reachability: match?.reachability ?? ('unknown' as Reachability),
        ready: match?.ready ?? '—',
        status: match?.status ?? 'not deployed',
      }
    })
  }, [ns, workloadsQuery.data?.workloads])

  const ingestSummary = useMemo(
    () => summarizeIngestServices(busDeep?.ingest.services ?? []),
    [busDeep?.ingest.services],
  )

  const tradeApiSignal = useMemo((): Signal => {
    if (tradeApi.total === 0) return 'unknown'
    if (tradeApi.ok === tradeApi.total) return 'ok'
    if (tradeApi.ok > 0) return 'degraded'
    return 'fail'
  }, [tradeApi.ok, tradeApi.total])

  const workersSummary = useMemo(() => {
    const rows = criticalProcesses
    const ok = rows.filter(r => r.reachability === 'ok').length
    const fail = rows.filter(r => r.reachability === 'fail').length
    const degraded = rows.filter(r => r.reachability === 'degraded').length
    let signal: Signal = 'ok'
    if (fail > 0) signal = 'fail'
    else if (degraded > 0) signal = 'degraded'
    else if (rows.length === 0) signal = 'unknown'
    const headline =
      rows.length === 0
        ? 'No workloads'
        : fail + degraded > 0
          ? `${ok}/${rows.length} ready · ${fail + degraded} attention`
          : `${ok}/${rows.length} critical ready`
    return { signal, headline, ok, total: rows.length }
  }, [criticalProcesses])

  const clusterDomainSummary = useMemo(() => {
    const domains = (serviceReadinessQuery.data?.domains ?? []).filter(d =>
      SATELLITE_DOMAIN_IDS.includes(d.id as (typeof SATELLITE_DOMAIN_IDS)[number]),
    )
    const ok = domains.filter(d => d.reachability === 'ok').length
    const fail = domains.filter(d => d.reachability === 'fail').length
    const degraded = domains.filter(d => d.reachability === 'degraded').length
    let signal: Signal = 'ok'
    if (fail > 0) signal = 'fail'
    else if (degraded > 0) signal = 'degraded'
    else if (domains.length === 0) signal = 'unknown'
    const headline =
      domains.length === 0
        ? 'Domains loading'
        : `${ok}/${domains.length} domains ok` +
          (fail + degraded > 0 ? ` · ${fail + degraded} degraded` : '')
    return { signal, headline }
  }, [serviceReadinessQuery.data?.domains])

  const socketHealthMatrix = useMemo(() => {
    const probeDetailFor = (env: BusEnvId): string | undefined => {
      const bus = busesByEnv[env]
      if (bus == null) return undefined
      return formatBusProbeDetail(bus)
    }
    const slices = Object.fromEntries(
      (['dev', 'stg', 'prod', 'dev-local'] as const).map(env => [
        env,
        busesByEnv[env] != null
          ? {
              socket: busesByEnv[env]?.monitor.socket,
              ingest: busesByEnv[env]?.ingest.services,
              daemon: busesByEnv[env]?.monitor.daemon,
              probeDetail: probeDetailFor(env),
            }
          : undefined,
      ]),
    )
    return buildSocketHealthMatrix(slices)
  }, [busesByEnv])

  const socketSummary = useMemo(
    () => summarizeSocketHealthAllEnvs(socketHealthMatrix),
    [socketHealthMatrix],
  )

  const socketHeadline = socketSummary.headline

  const daemonBusRow = useMemo((): SocketHealthRow | null => {
    const bus = busesByEnv[tradeEnv]
    if (bus == null) return null
    const ingest = bus.ingest.services.find(s => s.id === 'trading_engine')
    return classifyTradingDaemon(tradeEnv, ingest, bus.monitor.daemon, bus.monitor.socket)
  }, [busesByEnv, tradeEnv])

  const daemonSummary = useMemo((): { signal: Signal; headline: string } => {
    if (busDeepAllQuery.isLoading) return { signal: 'unknown', headline: 'Loading…' }
    if (daemonBusRow == null) return { signal: 'unknown', headline: 'No probe' }
    return {
      signal: daemonBusRow.reach as Signal,
      headline: `${daemonBusRow.reachLabel} — ${daemonBusRow.detail}`,
    }
  }, [busDeepAllQuery.isLoading, daemonBusRow])

  const tradeApiTargetRows = useMemo(
    () => (envMatrix != null ? filterTradeApiTargets(envMatrix) : []),
    [envMatrix],
  )

  const rocketSummary = useMemo((): { signal: Signal; headline: string } => {
    const row = socketHealthMatrix.rocket
    return {
      signal: row.reach as Signal,
      headline: `${row.reachLabel} — ${row.detail}`,
    }
  }, [socketHealthMatrix.rocket])

  const singleEnvScope = tradeSingleEnvScope(tradeEnv)

  const aiIngestTriage = useAmbientAgentTask({
    canOperate,
    ambientJobId,
    onStartAgentJob,
    scope: SATELLITE_BUS_INGEST_TRIAGE_SCOPE,
    label: scopeToLabel(SATELLITE_BUS_INGEST_TRIAGE_SCOPE),
    buildRequest: () => ({
      prompt: buildSatelliteBusIngestTriagePrompt({
        env: tradeEnv,
        namespace: ns,
        ingestHeadline: ingestSummary.headline,
        socketHeadline,
        busReachability: busDeep?.reachability,
      }),
    }),
  })

  const scrollTo = useCallback(
    (key: keyof typeof focusRefs) => {
      scrollToBusSection(focusRefs[key], detailScrollRef, setHighlightSection, key)
    },
    [focusRefs],
  )

  const daemonRows = useMemo((): MonitorKvRow[] => {
    if (busDeepAllQuery.isLoading) return []
    const daemon = busDeep?.monitor.daemon
    return [
      {
        label: 'Reachability',
        value: (
          <>
            <StatusLamp value={daemon?.reachability ?? 'unknown'} kind="reach" />{' '}
            {renderText(daemon?.reachability)}
          </>
        ),
      },
      { label: 'Self check', value: renderText(daemon?.self_check) },
      { label: 'Lamp', value: renderText(daemon?.lamp) },
      {
        label: 'Block reasons',
        value:
          (daemon?.block_reasons ?? []).length === 0
            ? '—'
            : (daemon?.block_reasons ?? []).map(reason => (
                <DenseTag key={reason} variant="warning" className="mr-1 text-[9px]">
                  {reason}
                </DenseTag>
              )),
      },
      { label: 'Trading suspended', value: renderText(daemon?.trading?.trading_suspended) },
      { label: 'daemon_alive', value: renderText(daemon?.heartbeat?.daemon_alive) },
      { label: 'ib_connected', value: renderText(daemon?.heartbeat?.ib_connected) },
      { label: 'seconds_until_retry', value: renderText(daemon?.heartbeat?.seconds_until_retry) },
    ]
  }, [busDeep?.monitor.daemon, busDeepAllQuery.isLoading])

  const celeryRows = useMemo((): MonitorKvRow[] => {
    const celery = busDeep?.monitor.celery
    return [
      {
        label: 'Reachability',
        value: (
          <>
            <StatusLamp value={celery?.reachability ?? 'unknown'} kind="reach" />{' '}
            {renderText(celery?.reachability)}
          </>
        ),
      },
      { label: 'broker_connected', value: renderText(celery?.broker_connected) },
      { label: 'workers', value: (celery?.workers ?? []).join(', ') || '—' },
      { label: 'worker_ib_connected', value: renderText(celery?.worker_ib_connected) },
      { label: 'worker_ib_client_id', value: renderText(celery?.worker_ib_client_id) },
    ]
  }, [busDeep?.monitor.celery])

  const accountSyncRows = useMemo((): MonitorKvRow[] => {
    const sync = busDeep?.monitor.account_sync
    return [
      {
        label: 'Reachability',
        value: (
          <>
            <StatusLamp value={sync?.reachability ?? 'unknown'} kind="reach" />{' '}
            {renderText(sync?.reachability)}
          </>
        ),
      },
      { label: 'daemon_alive', value: renderText(sync?.daemon_alive) },
      { label: 'stream_lag', value: renderText(sync?.stream_lag) },
    ]
  }, [busDeep?.monitor.account_sync])

  const opsRows = useMemo((): MonitorKvRow[] => {
    const ops = busDeep?.ops
    return [
      {
        label: 'Reachability',
        value: (
          <>
            <StatusLamp value={ops?.reachability ?? 'unknown'} kind="reach" /> {renderText(ops?.reachability)}
          </>
        ),
      },
      { label: 'executor_mode', value: renderText(ops?.executor_mode) },
      { label: 'k8s_reachable', value: renderText(ops?.k8s_reachable) },
      { label: 'status', value: renderText(ops?.status) },
    ]
  }, [busDeep?.ops])

  const probeTime =
    busDeep?.generated_at != null
      ? new Date(busDeep.generated_at).toLocaleTimeString()
      : envMatrix?.generated_at != null
        ? new Date(envMatrix.generated_at).toLocaleTimeString()
        : '—'

  return (
    <div
      ref={pageRootRef}
      className="satellite-bus-page flex w-full min-w-0 flex-col overflow-hidden"
    >
      <div className="flex shrink-0 flex-col gap-2">
      <PageHeader
        title="Bus Status"
        titleSize="default"
        description="Rocket (Platform) vs Trade satellite vs Ground cluster — scoped sections below."
      />

      <section className="page-section panel-elevated px-2.5 py-1.5">
        <div className="flex flex-wrap items-center gap-x-3 gap-y-1.5">
          <span className="text-[var(--text-dense-caption)] font-medium text-muted-foreground shrink-0">Trade NS</span>
          <SegmentControl
            value={tradeEnv}
            options={[...TRADE_ENV_OPTIONS]}
            onChange={v => setTradeEnv(v as TradeEnv)}
          />
          <StatusLamp value={busSignal} kind="reach" />
          <span className="text-[var(--text-dense-caption)] text-muted-foreground">
            {tradeApi.ok}/{tradeApi.total} APIs · {ns}
          </span>
          {busDeep != null && (
            <DenseTag variant={signalFromLamp(busDeep.monitor.health.status_lamp) === 'ok' ? 'success' : signalFromLamp(busDeep.monitor.health.status_lamp) === 'fail' ? 'danger' : 'warning'}>
              Monitor {String(busDeep.monitor.health.status_lamp ?? 'unknown').toUpperCase()}
            </DenseTag>
          )}
          <DenseTag variant={busSignal === 'ok' ? 'success' : busSignal === 'fail' ? 'danger' : 'warning'}>
            Bus {busSignal.toUpperCase()}
          </DenseTag>
          <DenseTag variant="neutral">Probe {probeTime}</DenseTag>
          <span className="text-[var(--text-dense-caption)] text-muted-foreground">
            Drives <strong className="font-medium text-foreground/80">Trade · selected NS</strong> sections only
          </span>
          <span className="ml-auto flex flex-wrap items-center gap-x-3 gap-y-0.5">
            <AgentTriggerButton
              label="Agent Triage"
              size="xs"
              pending={aiIngestTriage.isPending}
              disabled={aiIngestTriage.disabled}
              title={
                aiIngestTriage.disabledReason ??
                'Cross-check Socket matrix vs monitor.socket vs ib-gateway (D10 safe)'
              }
              onClick={() => aiIngestTriage.trigger()}
            />
            {onOpenApiHealth != null && (
              <button type="button" className="focus-strip-link text-[var(--text-dense-caption)]" onClick={onOpenApiHealth}>
                API Health
              </button>
            )}
            {onOpenPluginGallery != null && (
              <button type="button" className="focus-strip-link text-[var(--text-dense-caption)]" onClick={onOpenPluginGallery}>
                IB Gateway
              </button>
            )}
          </span>
        </div>
      </section>

      <section className="page-section panel-elevated px-2.5 py-1.5">
        <div className="mb-1 flex flex-wrap items-baseline gap-x-2 gap-y-0.5">
          <OpsSubsectionTitle className="m-0">Summary</OpsSubsectionTitle>
          <span className="text-[var(--text-dense-caption)] text-muted-foreground">
            Color bar = scope · click to jump
          </span>
        </div>
        <div className="flex flex-wrap gap-1.5">
          <BusSummaryCard
            label="Rocket · IB Gateway"
            scope="rocket"
            signal={rocketSummary.signal}
            headline={rocketSummary.headline}
            selected={highlightSection === 'rocket'}
            onClick={() => scrollTo('rocket')}
          />
          <BusSummaryCard
            label="Socket · all envs"
            scope="trade-multi-env"
            signal={socketSummary.signal}
            headline={socketHeadline}
            selected={highlightSection === 'socket'}
            onClick={() => scrollTo('socket')}
          />
          <BusSummaryCard
            label={`Daemon · ${tradeEnv.toUpperCase()}`}
            scope="trade-single-env"
            signal={daemonSummary.signal}
            headline={daemonSummary.headline}
            selected={highlightSection === 'monitor'}
            onClick={() => scrollTo('monitor')}
          />
          <BusSummaryCard
            label={`APIs · ${tradeEnv.toUpperCase()}`}
            scope="trade-single-env"
            signal={tradeApiSignal}
            headline={`${tradeApi.ok}/${tradeApi.total} reachable`}
            selected={highlightSection === 'trade-apis'}
            onClick={() => scrollTo('trade-apis')}
          />
          <BusSummaryCard
            label={`Workers · ${tradeEnv.toUpperCase()}`}
            scope="trade-single-env"
            signal={workersSummary.signal}
            headline={workersSummary.headline}
            selected={highlightSection === 'workers'}
            onClick={() => scrollTo('workers')}
          />
          <BusSummaryCard
            label="Ground · cluster"
            scope="ground"
            signal={clusterDomainSummary.signal}
            headline={clusterDomainSummary.headline}
            selected={highlightSection === 'cluster'}
            onClick={() => scrollTo('cluster')}
          />
        </div>
      </section>
      </div>

      <div ref={detailScrollRef} className="min-h-0 flex-1 overflow-y-auto">
      <div className="flex flex-col gap-2 pb-2">
      <details className="page-section panel-elevated px-2.5 py-1 text-[var(--text-dense-caption)] text-muted-foreground">
        <summary className="cursor-pointer font-medium text-foreground/80">Scope guide</summary>
        <ul className="m-0 mt-1 flex list-none flex-col gap-1 p-0">
          <li>
            <BusScopeBadge scope="rocket" /> Platform IB Gateway — shared across Dev / Stg / Prod (not Trade NS)
          </li>
          <li>
            <BusScopeBadge scope="trade-multi-env" /> Socket matrix — all env columns + Mac bridge
          </li>
          <li>
            <BusScopeBadge scope="trade-single-env" /> Follows Trade NS selector — monitor, ingest, APIs, workloads
          </li>
          <li>
            <BusScopeBadge scope="ground" /> Cluster matrix L0, service domains, observability
          </li>
        </ul>
      </details>

      <BusPageGroup
        title="Platform IB socket bus"
        scope="rocket"
        description="data/ib-gateway @ redis-ib"
        sectionRef={rocketSectionRef}
        highlight={highlightSection === 'rocket'}
      >
        <OpsSection
          variant="flat"
          title="Rocket · Platform IB Gateway"
          bodyPadding="compact"
          overflow="hidden"
          description="Authoritative quote/account/operator path for all trade namespaces"
        >
          <RocketSocketBusRow row={socketHealthMatrix.rocket} />
        </OpsSection>
      </BusPageGroup>

      <BusPageGroup
        title="Socket consumers"
        scope="trade-multi-env"
        description="Per-namespace consumers vs Platform gateway — authoritative bus health (replaces legacy Market ingest table)"
        sectionRef={socketSectionRef}
        highlight={highlightSection === 'socket' || highlightSection === 'ingest'}
      >
        <OpsSection
          variant="flat"
          title="Trade · socket matrix"
          bodyPadding="compact"
          overflow="hidden"
          description={`Highlight column = ${tradeEnv.toUpperCase()} (Trade NS) · monitor.socket + bus semantics`}
        >
          <div className="flex flex-col gap-2">
            <SocketHealthMatrixTable rows={socketHealthMatrix.tradeRows} selectedEnv={tradeEnv} />
            <p className="text-[var(--text-dense-caption)] text-muted-foreground m-0">
              Trading daemon row uses bus semantics (observe / paused / policy-off). K3s Dev = bifrost-dev @
              :30882. Mac = satellite-probe-bridge on this workstation.
            </p>
          </div>
        </OpsSection>
      </BusPageGroup>

      <BusPageGroup
        title={`Monitor · ${singleEnvScope}`}
        scope="trade-single-env"
        tradeEnv={tradeEnv}
        description={tradeSingleEnvProbeSource(tradeEnv)}
        sectionRef={monitorSectionRef}
        highlight={
          highlightSection === 'monitor' ||
          highlightSection === 'trade-apis'
        }
      >
        <OpsSection
          variant="flat"
          title="GsTrading trading daemon"
          description="Bus interpretation + raw monitor FSM"
          bodyPadding="none"
          overflow="hidden"
        >
          <DaemonBusStatusStrip row={daemonBusRow} loading={busDeepAllQuery.isLoading} />
          <div className="border-t border-[var(--border)] px-3 py-1.5">
            <span className="text-[var(--text-dense-caption)] font-medium text-muted-foreground">
              Raw monitor FSM
            </span>
            <span className="ml-2 text-[var(--text-dense-caption)] text-muted-foreground">
              Strict trading-arm semantics — may differ from Bus status when observe / pause is healthy
            </span>
          </div>
          <MonitorKvTable rows={daemonRows} loading={busDeepAllQuery.isLoading} />
        </OpsSection>

        <div className="grid divide-x divide-[var(--border)] border-t border-[var(--border)] sm:grid-cols-2 xl:grid-cols-3">
          <OpsSection variant="flat" title="Celery" bodyPadding="none" overflow="hidden">
            <MonitorKvTable rows={celeryRows} loading={busDeepAllQuery.isLoading} />
          </OpsSection>
          <OpsSection variant="flat" title="Account sync" bodyPadding="none" overflow="hidden">
            <MonitorKvTable rows={accountSyncRows} loading={busDeepAllQuery.isLoading} />
          </OpsSection>
          <OpsSection variant="flat" title="Ops executor" bodyPadding="none" overflow="hidden">
            <MonitorKvTable rows={opsRows} loading={busDeepAllQuery.isLoading} />
          </OpsSection>
        </div>

        <div ref={tradeApisSectionRef}>
          <OpsSection
            variant="flat"
            title="Trade API reachability"
            bodyPadding="none"
            overflow="hidden"
            description={`Matrix L0 HTTP probes for ${tradeEnv.toUpperCase()} · full detail on API Health`}
          >
            <TradeApiReachTable targets={tradeApiTargetRows} loading={matrixQuery.isLoading} />
          </OpsSection>
        </div>
      </BusPageGroup>

      <BusPageGroup
        title={`Namespace · ${ns}`}
        scope="trade-single-env"
        tradeEnv={tradeEnv}
        description={`K8s workloads in ${singleEnvScope}`}
        sectionRef={workersSectionRef}
        highlight={highlightSection === 'workers'}
      >
        <OpsSection variant="flat" title="Critical processes" bodyPadding="none" overflow="hidden">
          <DenseDataTable>
            <DenseTableHeader>
              <DenseTableHeadRow>
                <DenseTableHead>Process</DenseTableHead>
                <DenseTableHead>Workload</DenseTableHead>
                <DenseTableHead>Ready</DenseTableHead>
                <DenseTableHead>Status</DenseTableHead>
              </DenseTableHeadRow>
            </DenseTableHeader>
            <DenseTableBody>
              {workloadsQuery.isLoading ? (
                <DenseTableRow>
                  <DenseTableCell colSpan={4} className="text-[var(--muted-foreground)]">
                    Loading…
                  </DenseTableCell>
                </DenseTableRow>
              ) : (
                criticalProcesses.map(row => (
                  <DenseTableRow key={row.label}>
                    <DenseTableCell className="font-medium text-[var(--text-dense-meta)]">{row.label}</DenseTableCell>
                    <DenseTableCell className="font-mono-tabular text-[var(--text-dense-caption)]">{row.name}</DenseTableCell>
                    <DenseTableCell className="font-mono-tabular text-[var(--text-dense-caption)]">{row.ready}</DenseTableCell>
                    <DenseTableCell>
                      <StatusLamp value={row.reachability} kind="reach" />{' '}
                      <span className="font-mono-tabular text-[var(--text-dense-caption)]">{row.status}</span>
                    </DenseTableCell>
                  </DenseTableRow>
                ))
              )}
            </DenseTableBody>
          </DenseDataTable>
        </OpsSection>
      </BusPageGroup>

      <BusPageGroup
        title="Cluster readiness"
        scope="ground"
        description="Matrix L0 · service domains · observability"
        sectionRef={clusterSectionRef}
        highlight={highlightSection === 'cluster'}
      >
        <OpsSection variant="flat" title="Payload readiness (matrix L0)" bodyPadding="none" overflow="hidden">
          <PayloadReadinessTable rows={payloadRows} />
        </OpsSection>
        <div className="grid lg:grid-cols-2 lg:divide-x lg:divide-[var(--border)]">
          <OpsSection variant="flat" title="Service domains" bodyPadding="none" overflow="hidden">
            {SATELLITE_DOMAIN_IDS.map(domainId => (
              <ClusterServiceReadinessPanel
                key={domainId}
                data={serviceReadinessQuery.data}
                isLoading={serviceReadinessQuery.isLoading}
                compact
                variant="flat"
                domainFilter={domainId}
              />
            ))}
          </OpsSection>
          <SatelliteObservabilityStrip
            variant="flat"
            metrics={metricsQuery.data}
            observability={observabilityQuery.data}
            metricsLoading={metricsQuery.isLoading}
            observabilityLoading={observabilityQuery.isLoading}
            onOpenCluster={onOpenCluster}
            onOpenTelemetry={onOpenTelemetry}
          />
        </div>
      </BusPageGroup>
      </div>
      </div>
    </div>
  )
}
