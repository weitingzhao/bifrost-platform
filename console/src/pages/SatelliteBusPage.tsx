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
import type { MatrixResponse, Reachability, SatelliteBusDeepResponse } from '@/api/types'
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
  ingestRuntimeView,
  SATELLITE_BUS_INGEST_TRIAGE_SCOPE,
  summarizeIngestServices,
} from '@/lib/agent/satelliteBusIngestTriagePrompt'
import {
  buildPayloadReadinessRows,
  type PayloadReadinessRow,
} from '@/lib/control-room/payloadReadiness'
import {
  buildSocketHealthRows,
  summarizeSocketHealth,
  type SocketHealthRow,
  type SocketRequiredState,
} from '@/lib/satellite/socketHealthSemantics'
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

function BusPageGroup({
  title,
  description,
  sectionRef,
  highlight,
  children,
}: {
  title: string
  description?: string
  sectionRef?: Ref<HTMLDivElement>
  highlight?: boolean
  children: ReactNode
}) {
  return (
    <div
      ref={sectionRef}
      className={cn(
        'satellite-bus-group flex flex-col gap-1.5 rounded-sm transition-shadow',
        highlight && 'ring-1 ring-[var(--ring)] ring-offset-1 ring-offset-[var(--background)]',
      )}
    >
      <div className="flex flex-wrap items-baseline gap-x-2 gap-y-0.5">
        <OpsSubsectionTitle className="m-0">{title}</OpsSubsectionTitle>
        {description != null && description !== '' && (
          <span className="text-[var(--text-dense-caption)] text-muted-foreground">{description}</span>
        )}
      </div>
      {children}
    </div>
  )
}

function BusSummaryCard({
  label,
  signal,
  headline,
  selected,
  onClick,
}: {
  label: string
  signal: Signal
  headline: string
  selected?: boolean
  onClick: () => void
}) {
  return (
    <button
      type="button"
      onClick={onClick}
      aria-pressed={selected === true}
      className={cn(
        'flex min-w-[8.5rem] flex-1 flex-col gap-0.5 rounded-md border px-2 py-1.5 text-left transition-colors',
        selected
          ? 'border-[var(--ring)] bg-[var(--accent)]'
          : 'border-[var(--border)] bg-[var(--secondary)] hover:bg-[var(--accent)]',
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

function runtimeToneClass(tone: 'ok' | 'warn' | 'fail' | 'muted'): string {
  switch (tone) {
    case 'ok':
      return 'text-success'
    case 'warn':
      return 'text-warning'
    case 'fail':
      return 'text-danger'
    default:
      return 'text-muted-foreground'
  }
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

function SocketHealthTable({ rows, layerLabel }: { rows: SocketHealthRow[]; layerLabel: string }) {
  return (
    <div className="flex flex-col gap-0.5">
      <span className="text-[var(--text-dense-caption)] font-medium text-muted-foreground">{layerLabel}</span>
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
          {rows.map(row => (
            <DenseTableRow key={row.id}>
              <DenseTableCell className="font-medium text-[var(--text-dense-meta)]">{row.label}</DenseTableCell>
              <DenseTableCell>
                <RequiredTag state={row.required} />
              </DenseTableCell>
              <DenseTableCell>
                <StatusLamp
                  value={row.required === 'policy-off' ? 'ok' : row.reach}
                  kind="reach"
                />{' '}
                <span
                  className={cn(
                    'text-[var(--text-dense-caption)]',
                    row.required === 'policy-off' ? 'text-muted-foreground' : '',
                  )}
                >
                  {row.reachLabel}
                </span>
              </DenseTableCell>
              <DenseTableCell className="max-w-[14rem] text-[var(--text-dense-caption)] text-muted-foreground">
                {row.detail}
              </DenseTableCell>
            </DenseTableRow>
          ))}
        </DenseTableBody>
      </DenseDataTable>
    </div>
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
  const socketSectionRef = useRef<HTMLDivElement | null>(null)
  const ingestSectionRef = useRef<HTMLDivElement | null>(null)
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
        socket: socketSectionRef,
        ingest: ingestSectionRef,
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

  const busDeepQuery = useQuery({
    queryKey: ['satellite', 'bus-deep', tradeEnv],
    queryFn: () => fetchSatelliteBusDeep(tradeEnv),
    refetchInterval: 30_000,
  })

  const busDeep = useMemo((): SatelliteBusDeepResponse | undefined => {
    const data = busDeepQuery.data
    if (data == null) return undefined
    if (isAllSatelliteBusDeep(data)) return data.buses.find(b => b.environment === tradeEnv)
    return data
  }, [busDeepQuery.data, tradeEnv])

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

  const socketHealth = useMemo(
    () => buildSocketHealthRows(busDeep?.monitor.socket, tradeEnv, busDeep?.ingest.services),
    [busDeep?.ingest.services, busDeep?.monitor.socket, tradeEnv],
  )

  const socketSummary = useMemo(
    () => summarizeSocketHealth([...socketHealth.rocket, ...socketHealth.trade]),
    [socketHealth],
  )

  const socketHeadline = socketSummary.headline

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
    if (busDeepQuery.isLoading) return []
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
  }, [busDeep?.monitor.daemon, busDeepQuery.isLoading])

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
        description="Monitor + ops deep probe · namespace workloads · matrix L0."
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
          <span className="ml-auto flex flex-wrap items-center gap-x-3 gap-y-0.5">
            <AgentTriggerButton
              label="Agent Triage"
              size="xs"
              pending={aiIngestTriage.isPending}
              disabled={aiIngestTriage.disabled}
              title={
                aiIngestTriage.disabledReason ??
                'Cross-check ingest display vs monitor.socket vs ib-gateway (D10 safe)'
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
            Macro signals — click to jump · dock stays fixed
          </span>
        </div>
        <div className="flex flex-wrap gap-1.5">
          <BusSummaryCard
            label="Socket health"
            signal={socketSummary.signal}
            headline={socketHeadline}
            selected={highlightSection === 'socket'}
            onClick={() => scrollTo('socket')}
          />
          <BusSummaryCard
            label="Trade APIs"
            signal={tradeApiSignal}
            headline={`${tradeApi.ok}/${tradeApi.total} reachable`}
            selected={highlightSection === 'trade-apis'}
            onClick={() => scrollTo('trade-apis')}
          />
          <BusSummaryCard
            label="Market ingest"
            signal={ingestSummary.signal}
            headline={ingestSummary.headline}
            selected={highlightSection === 'ingest'}
            onClick={() => scrollTo('ingest')}
          />
          <BusSummaryCard
            label="Workers"
            signal={workersSummary.signal}
            headline={workersSummary.headline}
            selected={highlightSection === 'workers'}
            onClick={() => scrollTo('workers')}
          />
          <BusSummaryCard
            label="Cluster domains"
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
      <BusPageGroup
        title="Monitor probe"
        description="Daemon, socket, ingest from monitor /status + ops APIs"
        sectionRef={monitorSectionRef}
        highlight={highlightSection === 'monitor' || highlightSection === 'socket'}
      >
        <div className="grid gap-1.5 lg:grid-cols-2">
          <OpsSection title="Daemon FSM" bodyPadding="none" overflow="hidden" className="shadow-none">
            <MonitorKvTable rows={daemonRows} loading={busDeepQuery.isLoading} />
          </OpsSection>

          <div ref={socketSectionRef}>
            <OpsSection
              title="Socket health"
              bodyPadding="compact"
              overflow="hidden"
              className="shadow-none"
              description="Rocket platform bus vs Trade env consumers · policy-off ≠ degraded"
            >
              <div className="flex flex-col gap-2">
                <SocketHealthTable rows={socketHealth.rocket} layerLabel="Rocket · Platform socket bus (data/ib-gateway)" />
                <SocketHealthTable
                  rows={socketHealth.trade}
                  layerLabel={`Trade · ${tradeEnv.toUpperCase()} consumers (${ns})`}
                />
              </div>
            </OpsSection>
          </div>
        </div>

        <div className="grid gap-1.5 sm:grid-cols-2 xl:grid-cols-3">
          <OpsSection title="Celery" bodyPadding="none" overflow="hidden" className="shadow-none">
            <MonitorKvTable rows={celeryRows} loading={busDeepQuery.isLoading} />
          </OpsSection>
          <OpsSection title="Account sync" bodyPadding="none" overflow="hidden" className="shadow-none">
            <MonitorKvTable rows={accountSyncRows} loading={busDeepQuery.isLoading} />
          </OpsSection>
          <OpsSection title="Ops executor" bodyPadding="none" overflow="hidden" className="shadow-none">
            <MonitorKvTable rows={opsRows} loading={busDeepQuery.isLoading} />
          </OpsSection>
        </div>

        <div
          ref={ingestSectionRef}
          className={cn(
            'rounded-sm transition-shadow',
            highlightSection === 'ingest' && 'ring-1 ring-[var(--ring)] ring-offset-1 ring-offset-[var(--background)]',
          )}
        >
          <OpsSection
            title="Market ingest"
            bodyPadding="none"
            overflow="hidden"
            className="shadow-none"
            description="Runtime semantics · source of truth (not local systemd)"
          >
            <DenseDataTable>
              <DenseTableHeader>
                <DenseTableHeadRow>
                  <DenseTableHead className="w-[28%]">Service</DenseTableHead>
                  <DenseTableHead className="w-[18%]">Reach</DenseTableHead>
                  <DenseTableHead className="w-[22%]">Runtime</DenseTableHead>
                  <DenseTableHead>Source</DenseTableHead>
                </DenseTableHeadRow>
              </DenseTableHeader>
              <DenseTableBody>
                {(busDeep?.ingest.services ?? []).length === 0 ? (
                  <DenseTableRow>
                    <DenseTableCell colSpan={4} className="text-[var(--muted-foreground)]">
                      {busDeepQuery.isLoading ? 'Loading…' : '—'}
                    </DenseTableCell>
                  </DenseTableRow>
                ) : (
                  busDeep?.ingest.services.map(svc => {
                    const view = ingestRuntimeView(svc)
                    return (
                      <DenseTableRow key={svc.id}>
                        <DenseTableCell className="font-mono-tabular text-[var(--text-dense-meta)] font-medium">
                          {svc.id}
                        </DenseTableCell>
                        <DenseTableCell>
                          <StatusLamp value={svc.reachability} kind="reach" />{' '}
                          <span className="text-[var(--text-dense-caption)] text-muted-foreground">
                            {renderText(svc.reachability)}
                          </span>
                        </DenseTableCell>
                        <DenseTableCell>
                          <span
                            className={cn(
                              'font-mono-tabular text-[var(--text-dense-meta)]',
                              runtimeToneClass(view.tone),
                            )}
                          >
                            {view.runtime}
                          </span>
                          {view.note != null && (
                            <span className="ml-1.5 text-[var(--text-dense-caption)] text-muted-foreground">
                              {view.note}
                            </span>
                          )}
                        </DenseTableCell>
                        <DenseTableCell className="font-mono-tabular text-[var(--text-dense-caption)] text-muted-foreground">
                          {view.source}
                        </DenseTableCell>
                      </DenseTableRow>
                    )
                  })
                )}
              </DenseTableBody>
            </DenseDataTable>
          </OpsSection>
        </div>
      </BusPageGroup>

      <BusPageGroup
        title={`Namespace · ${ns}`}
        description="Critical workloads in selected trade namespace"
        sectionRef={workersSectionRef}
        highlight={highlightSection === 'workers'}
      >
        <OpsSection
          title="Critical processes"
          bodyPadding="none"
          overflow="hidden"
          className="shadow-none"
        >
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
        description="Service domains · matrix L0 · observability"
        sectionRef={clusterSectionRef}
        highlight={highlightSection === 'cluster' || highlightSection === 'trade-apis'}
      >
        <div
          ref={tradeApisSectionRef}
          className={cn(
            'rounded-sm transition-shadow',
            highlightSection === 'trade-apis' && 'ring-1 ring-[var(--ring)] ring-offset-1 ring-offset-[var(--background)]',
          )}
        >
          <OpsSection title="Payload readiness (matrix L0)" bodyPadding="none" overflow="hidden" className="shadow-none mb-1.5">
            <PayloadReadinessTable rows={payloadRows} />
          </OpsSection>
        </div>
        <div className="grid gap-1.5 lg:grid-cols-2">
          <OpsSection title="Service domains" bodyPadding="none" overflow="hidden" className="shadow-none">
            {SATELLITE_DOMAIN_IDS.map(domainId => (
              <div key={domainId} className="border-t border-[var(--border)] first:border-t-0">
                <ClusterServiceReadinessPanel
                  data={serviceReadinessQuery.data}
                  isLoading={serviceReadinessQuery.isLoading}
                  compact
                  domainFilter={domainId}
                />
              </div>
            ))}
          </OpsSection>
          <OpsSection title="Observability" bodyPadding="none" overflow="hidden" className="shadow-none">
            <SatelliteObservabilityStrip
              metrics={metricsQuery.data}
              observability={observabilityQuery.data}
              metricsLoading={metricsQuery.isLoading}
              observabilityLoading={observabilityQuery.isLoading}
              onOpenCluster={onOpenCluster}
              onOpenTelemetry={onOpenTelemetry}
            />
          </OpsSection>
        </div>
      </BusPageGroup>
      </div>
      </div>
    </div>
  )
}
