import type { ReactNode, Ref, RefObject, SyntheticEvent } from 'react'
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
  Sheet,
  SheetContent,
  SheetDescription,
  SheetHeader,
  SheetTitle,
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
import { PayloadReadinessTable } from '@/components/control-room/PayloadDepthPanel'
import { useFleetSnapshot } from '@/hooks/useFleetSnapshot'
import { projectPayloadReadinessRows } from '@/lib/control-room/payloadReadiness'
import {
  buildSocketHealthMatrix,
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
import { filterTradeApiTargets, tradeApiTargetCounts } from '@/lib/satellite/tradeApiTargets'
import {
  buildSatelliteBusViewModel,
  busHealthToReach,
  busNodeHealthToReach,
  type BusAttentionIssue,
  type BusConsumerRow,
  type BusHealth,
  type BusNodeHealth,
  type BusPathNode,
} from '@/lib/satellite-bus/satelliteBusViewModel'
import { consumeSatelliteBusFocus } from '@/lib/task-mode/readinessChipActions'

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

/** `policy-off` must always surface as EXPECTED OFF in visible copy. */
function displayReachLabel(label: string): string {
  return label === 'policy-off' ? 'expected off' : label
}

function healthTagVariant(health: BusNodeHealth): 'success' | 'warning' | 'danger' | 'neutral' {
  switch (health) {
    case 'ok':
      return 'success'
    case 'degraded':
      return 'warning'
    case 'fail':
      return 'danger'
    default:
      return 'neutral'
  }
}

function busHealthTagVariant(health: BusHealth): 'success' | 'warning' | 'danger' | 'neutral' {
  switch (health) {
    case 'healthy':
      return 'success'
    case 'degraded':
      return 'warning'
    case 'unavailable':
      return 'danger'
    default:
      return 'neutral'
  }
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

function renderText(value: unknown): string {
  if (value == null) return '—'
  if (typeof value === 'string' && value.trim() === '') return '—'
  if (typeof value === 'object') return JSON.stringify(value)
  return String(value)
}

function BusScopeBadge({ scope }: { scope: BusStatusScope }) {
  const label =
    scope === 'rocket'
      ? 'Shared · Rocket'
      : scope === 'trade-multi-env'
        ? 'All envs'
        : scope === 'trade-single-env'
          ? 'Selected NS'
          : 'Shared · Ground'
  // Scope is expressed with neutral text badges — status colors are reserved for health.
  return (
    <DenseTag variant="neutral" className="shrink-0 text-[10px] uppercase tracking-wide">
      {label}
    </DenseTag>
  )
}

function RequirementTag({ state }: { state: SocketRequiredState }) {
  const label = state === 'policy-off' ? 'EXPECTED OFF' : state.toUpperCase()
  return (
    <DenseTag variant="neutral" className="text-[9px] uppercase">
      {label}
    </DenseTag>
  )
}

function SocketHealthEnvCellView({
  cell,
  selected,
}: {
  cell: SocketHealthEnvCell
  selected?: boolean
}) {
  const lampReach = cell.required === 'policy-off' ? 'unknown' : cell.reach
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
          {displayReachLabel(cell.reachLabel)}
        </span>
        {cell.required === 'policy-off' && (
          <DenseTag variant="neutral" className="text-[9px]">
            EXPECTED OFF
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
          <DenseTableHead className="whitespace-nowrap">Requirement</DenseTableHead>
          <DenseTableHead>Reach</DenseTableHead>
          <DenseTableHead>Detail</DenseTableHead>
        </DenseTableHeadRow>
      </DenseTableHeader>
      <DenseTableBody>
        <DenseTableRow>
          <DenseTableCell className="font-medium text-[var(--text-dense-meta)]">{row.label}</DenseTableCell>
          <DenseTableCell>
            <RequirementTag state={row.required} />
          </DenseTableCell>
          <DenseTableCell>
            <StatusLamp value={row.reach} kind="reach" />{' '}
            <span className="text-[var(--text-dense-caption)]">{displayReachLabel(row.reachLabel)}</span>
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
  ref: RefObject<HTMLElement | null>,
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

/* ── First-screen building blocks ── */

function BusPathNodeCard({
  node,
  onInspect,
}: {
  node: BusPathNode
  onInspect: () => void
}) {
  return (
    <button
      type="button"
      onClick={onInspect}
      title={`Inspect ${node.label}`}
      className="flex min-w-[10.5rem] flex-1 flex-col gap-0.5 rounded-md border border-[var(--border)] bg-[var(--secondary)] px-2 py-1.5 text-left transition-colors hover:bg-[var(--accent)]"
    >
      <span className="flex items-center gap-1.5">
        <StatusLamp value={busNodeHealthToReach(node.health)} kind="reach" />
        <span className="text-[var(--text-dense-caption)] font-medium">{node.label}</span>
        <DenseTag variant="neutral" className="ml-auto text-[9px] uppercase tracking-wide">
          {node.scopeLabel}
        </DenseTag>
      </span>
      <span className="flex items-center gap-1.5">
        <DenseTag variant={healthTagVariant(node.health)} className="text-[9px]">
          {node.stateLabel}
        </DenseTag>
        <span className="text-[var(--text-dense-caption)] text-muted-foreground line-clamp-1" title={node.headline}>
          {node.headline}
        </span>
      </span>
    </button>
  )
}

function AttentionIssueRow({
  issue,
  onInspect,
}: {
  issue: BusAttentionIssue
  onInspect: () => void
}) {
  return (
    <li className="flex flex-wrap items-center gap-x-2 gap-y-0.5 px-3 py-1.5">
      <StatusLamp value={issue.severity === 'critical' ? 'fail' : 'degraded'} kind="reach" />
      <DenseTag
        variant="neutral"
        className={cn('text-[9px] uppercase', issue.scope === 'cross-env' && 'opacity-80')}
      >
        {issue.scope === 'shared' ? 'SHARED' : issue.envLabel}
      </DenseTag>
      <span className="text-[var(--text-dense-caption)] font-medium">{issue.title}</span>
      <span className="min-w-0 flex-1 truncate text-[var(--text-dense-caption)] text-muted-foreground" title={issue.detail}>
        {issue.detail}
      </span>
      <button type="button" className="focus-strip-link text-[var(--text-dense-caption)]" onClick={onInspect}>
        Inspect
      </button>
    </li>
  )
}

function ConsumerStateCell({ row }: { row: BusConsumerRow }) {
  const lamp = row.health === 'expected-off' ? 'unknown' : busNodeHealthToReach(row.health)
  return (
    <span className="flex items-center gap-1.5">
      <StatusLamp value={lamp} kind="reach" />
      <DenseTag variant={healthTagVariant(row.health)} className="text-[9px]">
        {row.stateLabel}
      </DenseTag>
    </span>
  )
}

function ConsumerRequirementTag({ row }: { row: BusConsumerRow }) {
  return (
    <DenseTag variant="neutral" className="text-[9px] uppercase">
      {row.requirement === 'expected-off' ? 'EXPECTED OFF' : row.requirement.toUpperCase()}
    </DenseTag>
  )
}

function ConsumerTable({
  rows,
  loading,
  onInspect,
}: {
  rows: BusConsumerRow[]
  loading: boolean
  onInspect: (row: BusConsumerRow) => void
}) {
  return (
    <DenseDataTable>
      <DenseTableHeader>
        <DenseTableHeadRow>
          <DenseTableHead>Consumer</DenseTableHead>
          <DenseTableHead className="whitespace-nowrap">Requirement</DenseTableHead>
          <DenseTableHead>State</DenseTableHead>
          <DenseTableHead>Detail</DenseTableHead>
          <DenseTableHead className="w-14" />
        </DenseTableHeadRow>
      </DenseTableHeader>
      <DenseTableBody>
        {loading ? (
          <DenseTableRow>
            <DenseTableCell colSpan={5} className="text-[var(--muted-foreground)]">
              Loading…
            </DenseTableCell>
          </DenseTableRow>
        ) : rows.length === 0 ? (
          <DenseTableRow>
            <DenseTableCell colSpan={5} className="text-[var(--muted-foreground)]">
              —
            </DenseTableCell>
          </DenseTableRow>
        ) : (
          rows.map(row => (
            <DenseTableRow key={row.id}>
              <DenseTableCell className="font-medium text-[var(--text-dense-meta)]">{row.label}</DenseTableCell>
              <DenseTableCell>
                <ConsumerRequirementTag row={row} />
              </DenseTableCell>
              <DenseTableCell>
                <ConsumerStateCell row={row} />
              </DenseTableCell>
              <DenseTableCell className="text-[var(--text-dense-caption)] text-muted-foreground">
                {row.detail}
              </DenseTableCell>
              <DenseTableCell>
                <button
                  type="button"
                  className="focus-strip-link text-[var(--text-dense-caption)]"
                  onClick={() => onInspect(row)}
                >
                  Inspect
                </button>
              </DenseTableCell>
            </DenseTableRow>
          ))
        )}
      </DenseTableBody>
    </DenseDataTable>
  )
}

/* ── Secondary (collapsed) group ── */

function SecondaryGroup({
  title,
  description,
  scope,
  open,
  onOpenChange,
  sectionRef,
  highlight,
  children,
}: {
  title: string
  description?: string
  scope: BusStatusScope
  open: boolean
  onOpenChange: (open: boolean) => void
  sectionRef?: Ref<HTMLDetailsElement>
  highlight?: boolean
  children: ReactNode
}) {
  return (
    <details
      ref={sectionRef}
      open={open}
      data-scope={scope}
      className={cn(
        'satellite-bus-group panel-elevated overflow-hidden rounded-md transition-shadow',
        busScopeGroupClass(scope),
        highlight && 'ring-1 ring-[var(--ring)] ring-offset-1 ring-offset-[var(--background)]',
      )}
    >
      <summary
        className="satellite-bus-group-header cursor-pointer list-none [&::-webkit-details-marker]:hidden"
        onClick={(e: SyntheticEvent<HTMLElement>) => {
          // Controlled toggle — React 18 does not reliably deliver onToggle for <details>.
          e.preventDefault()
          onOpenChange(!open)
        }}
      >
        <div className="flex min-w-0 flex-1 flex-wrap items-center gap-x-2 gap-y-0.5">
          <BusScopeBadge scope={scope} />
          <h3 className="satellite-bus-group-title">{title}</h3>
          {description != null && description !== '' && (
            <span className="text-[var(--text-dense-caption)] text-muted-foreground">{description}</span>
          )}
        </div>
        <span className="text-[var(--text-dense-caption)] text-muted-foreground">
          {open ? 'Collapse' : 'Expand'}
        </span>
      </summary>
      <div className="satellite-bus-group-body flex flex-col">{children}</div>
    </details>
  )
}

/* ── Inspect sheet ── */

type InspectTarget =
  | { kind: 'node'; node: BusPathNode }
  | { kind: 'consumer'; row: BusConsumerRow }
  | { kind: 'issue'; issue: BusAttentionIssue }

type InspectView = {
  title: string
  scopeLabel: string
  health: BusNodeHealth
  stateLabel: string
  headline?: string
  detail: string
  probePath: string
  raw?: unknown
}

function inspectView(target: InspectTarget): InspectView {
  if (target.kind === 'node') {
    const n = target.node
    return {
      title: n.label,
      scopeLabel: n.scopeLabel,
      health: n.health,
      stateLabel: n.stateLabel,
      headline: n.headline,
      detail: n.detail,
      probePath: n.probePath,
      raw: n.raw,
    }
  }
  if (target.kind === 'consumer') {
    const r = target.row
    return {
      title: r.label,
      scopeLabel: r.kind === 'data-path' ? 'DATA PATH' : 'RUNTIME',
      health: r.health,
      stateLabel: r.stateLabel,
      detail: r.detail,
      probePath: r.probePath,
      raw: r.raw,
    }
  }
  const i = target.issue
  return {
    title: i.title,
    scopeLabel: i.scope === 'shared' ? 'SHARED' : i.scope === 'cross-env' ? `CROSS-ENV · ${i.envLabel}` : i.envLabel,
    health: i.severity === 'critical' ? 'fail' : 'degraded',
    stateLabel: i.severity.toUpperCase(),
    detail: i.detail,
    probePath: i.probePath,
    raw: i.raw,
  }
}

export function SatelliteBusPage({
  onOpenCluster,
  onOpenTelemetry,
  onOpenObservability,
  onOpenPluginGallery,
  onOpenApiHealth,
  ambientJobId,
  onStartAgentJob,
}: {
  onOpenCluster?: () => void
  onOpenTelemetry?: () => void
  onOpenObservability?: () => void
  onOpenPluginGallery?: () => void
  onOpenApiHealth?: () => void
} & AmbientAgentShellProps) {
  const { canOperate } = usePlatformAuth()
  const [tradeEnv, setTradeEnv] = useState<TradeEnv>('stg')
  const [highlightSection, setHighlightSection] = useState<string | null>(null)
  const [inspect, setInspect] = useState<InspectTarget | null>(null)
  const [sharedOpen, setSharedOpen] = useState(false)
  const [otherEnvsOpen, setOtherEnvsOpen] = useState(false)
  const [evidenceOpen, setEvidenceOpen] = useState(false)
  const ns = TRADE_NS[tradeEnv]
  const pageRootRef = useRef<HTMLDivElement | null>(null)
  const detailScrollRef = useRef<HTMLDivElement | null>(null)
  const selectedSectionRef = useRef<HTMLDivElement | null>(null)
  const sharedSectionRef = useRef<HTMLDetailsElement | null>(null)
  const otherEnvsSectionRef = useRef<HTMLDetailsElement | null>(null)
  const evidenceSectionRef = useRef<HTMLDetailsElement | null>(null)

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

  /** Legacy chip focus keys → new sections (opens collapsed groups when needed). */
  const focusTargets = useMemo(
    () =>
      ({
        monitor: { ref: selectedSectionRef, open: null },
        rocket: { ref: sharedSectionRef, open: setSharedOpen },
        cluster: { ref: sharedSectionRef, open: setSharedOpen },
        socket: { ref: otherEnvsSectionRef, open: setOtherEnvsOpen },
        ingest: { ref: otherEnvsSectionRef, open: setOtherEnvsOpen },
        'trade-apis': { ref: evidenceSectionRef, open: setEvidenceOpen },
        workers: { ref: evidenceSectionRef, open: setEvidenceOpen },
      }) as const satisfies Record<
        string,
        { ref: RefObject<HTMLElement | null>; open: ((open: boolean) => void) | null }
      >,
    [],
  )

  useEffect(() => {
    const focus = consumeSatelliteBusFocus()
    if (focus == null) return
    const target = focusTargets[focus]
    if (target == null || target.ref.current == null) return
    target.open?.(true)
    requestAnimationFrame(() => {
      scrollToBusSection(target.ref, detailScrollRef, setHighlightSection, focus)
    })
  }, [focusTargets])

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

  const { fleet } = useFleetSnapshot()
  const payloadRows = useMemo(() => projectPayloadReadinessRows(fleet), [fleet])
  const envMatrix = matrices.find(m => m.environment === tradeEnv)
  const tradeApi = tradeApiTargetCounts(envMatrix)

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

  /** Single source of truth for the first screen — pure derived view model. */
  const viewModel = useMemo(
    () =>
      buildSatelliteBusViewModel({
        selectedEnv: tradeEnv,
        buses: busesByEnv,
        tradeApi,
      }),
    [busesByEnv, tradeApi, tradeEnv],
  )

  const busLoading = busDeepAllQuery.isLoading

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

  const tradeApiTargetRows = useMemo(
    () => (envMatrix != null ? filterTradeApiTargets(envMatrix) : []),
    [envMatrix],
  )

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
        socketHeadline: socketSummary.headline,
        busReachability: busDeep?.reachability,
      }),
    }),
  })

  const openInspect = useCallback((target: InspectTarget) => {
    setInspect(target)
  }, [])

  const daemonRows = useMemo((): MonitorKvRow[] => {
    if (busLoading) return []
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
  }, [busDeep?.monitor.daemon, busLoading])

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

  const selectedIssues = viewModel.attention
  const crossEnvIssues = viewModel.crossEnvIssues
  const inspectData = inspect != null ? inspectView(inspect) : null

  return (
    <div
      ref={pageRootRef}
      className="satellite-bus-page flex w-full min-w-0 flex-col overflow-hidden"
    >
      <div className="flex shrink-0 flex-col gap-2">
      <PageHeader
        title="Satellite Bus"
        titleSize="default"
        description="Bus health for the selected Trade namespace — shared dependencies (Platform IB Gateway → redis-ib) feed every environment."
      />

      <section className="page-section panel-elevated px-2.5 py-1.5">
        <div className="flex flex-wrap items-center gap-x-3 gap-y-1.5">
          <span className="text-[var(--text-dense-caption)] font-medium text-muted-foreground shrink-0">Trade NS</span>
          <SegmentControl
            value={tradeEnv}
            options={[...TRADE_ENV_OPTIONS]}
            onChange={v => setTradeEnv(v as TradeEnv)}
          />
          <span className="text-[var(--text-dense-caption)] text-muted-foreground">
            {ns} · selector drives the verdict and Selected Environment below — other envs surface under
            Cross-env attention only
          </span>
          <DenseTag variant="neutral">Probe {probeTime}</DenseTag>
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
                API & Auth Probes
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

      <section className="page-section panel-elevated px-2.5 py-2">
        <div className="flex flex-wrap items-center gap-x-3 gap-y-1">
          <StatusLamp value={busHealthToReach(viewModel.health)} kind="reach" />
          <span className="text-[var(--text-dense-label)] font-semibold tracking-wide">
            BUS HEALTH · {tradeEnv.toUpperCase()}
          </span>
          <DenseTag variant={busHealthTagVariant(viewModel.health)} className="text-[10px] font-semibold">
            {busLoading ? 'PROBING' : viewModel.healthLabel}
          </DenseTag>
          <span className="min-w-0 flex-1 truncate text-[var(--text-dense-meta)] text-foreground/90" title={viewModel.topReason}>
            {busLoading ? 'Probing bus-deep endpoints…' : viewModel.topReason}
          </span>
        </div>
        <div className="mt-1 flex flex-wrap items-center gap-x-3 gap-y-0.5 text-[var(--text-dense-caption)] text-muted-foreground">
          <span className="font-mono-tabular">
            required {viewModel.metrics.requiredOk}/{viewModel.metrics.requiredTotal}
          </span>
          <span className="font-mono-tabular">expected off {viewModel.metrics.expectedOff}</span>
          <span className="font-mono-tabular">
            APIs {viewModel.metrics.apiOk}/{viewModel.metrics.apiTotal}
          </span>
          <span className="font-mono-tabular">
            monitor consumers {viewModel.metrics.runtimeOk}/{viewModel.metrics.runtimeTotal}
          </span>
          <span className="ml-auto">Bus health only — not Launch/Fleet GO&#8201;/&#8201;NO-GO</span>
        </div>
      </section>
      </div>

      <div ref={detailScrollRef} className="min-h-0 flex-1 overflow-y-auto">
      <div className="flex flex-col gap-2 pb-2">

      <section className="page-section panel-elevated px-2.5 py-1.5">
        <div className="mb-1 flex flex-wrap items-baseline gap-x-2 gap-y-0.5">
          <OpsSubsectionTitle className="m-0">Data path</OpsSubsectionTitle>
          <span className="text-[var(--text-dense-caption)] text-muted-foreground">
            Shared Platform IB Gateway → redis-ib → socket consumers → {ns} · click a hop to inspect
          </span>
        </div>
        <div className="flex flex-wrap items-stretch gap-1.5">
          {viewModel.path.map((node, idx) => (
            <div key={node.id} className="flex min-w-0 flex-1 items-center gap-1.5">
              <BusPathNodeCard node={node} onInspect={() => openInspect({ kind: 'node', node })} />
              {idx < viewModel.path.length - 1 && (
                <span aria-hidden className="shrink-0 text-muted-foreground">
                  →
                </span>
              )}
            </div>
          ))}
        </div>
      </section>

      <section className="page-section panel-elevated overflow-hidden">
        <div className="flex flex-wrap items-baseline gap-x-2 gap-y-0.5 px-2.5 pt-1.5">
          <OpsSubsectionTitle className="m-0">Issues requiring attention</OpsSubsectionTitle>
          <span className="text-[var(--text-dense-caption)] text-muted-foreground">
            Selected env + shared dependencies first · cross-env is informational
          </span>
        </div>
        {busLoading ? (
          <p className="m-0 px-3 py-2 text-[var(--text-dense-caption)] text-muted-foreground">Probing…</p>
        ) : selectedIssues.length === 0 && crossEnvIssues.length === 0 ? (
          <p className="m-0 flex items-center gap-1.5 px-3 py-1.5 text-[var(--text-dense-caption)] text-muted-foreground">
            <StatusLamp value="ok" kind="reach" />
            No issues requiring attention — {tradeEnv.toUpperCase()} bus and shared dependencies are clear.
          </p>
        ) : (
          <>
            {selectedIssues.length > 0 && (
              <ul className="m-0 flex list-none flex-col divide-y divide-[var(--border)] p-0">
                {selectedIssues.map(issue => (
                  <AttentionIssueRow
                    key={issue.id}
                    issue={issue}
                    onInspect={() => openInspect({ kind: 'issue', issue })}
                  />
                ))}
              </ul>
            )}
            {crossEnvIssues.length > 0 && (
              <>
                <p className="m-0 border-t border-[var(--border)] px-3 py-1 text-[var(--text-dense-caption)] font-medium text-muted-foreground">
                  Cross-env attention — does not affect the {tradeEnv.toUpperCase()} verdict
                </p>
                <ul className="m-0 flex list-none flex-col divide-y divide-[var(--border)] p-0">
                  {crossEnvIssues.map(issue => (
                    <AttentionIssueRow
                      key={issue.id}
                      issue={issue}
                      onInspect={() => openInspect({ kind: 'issue', issue })}
                    />
                  ))}
                </ul>
              </>
            )}
          </>
        )}
      </section>

      <div
        ref={selectedSectionRef}
        data-scope="trade-single-env"
        className={cn(
          'satellite-bus-group panel-elevated flex flex-col overflow-hidden rounded-md transition-shadow',
          busScopeGroupClass('trade-single-env'),
          highlightSection === 'monitor' && 'ring-1 ring-[var(--ring)] ring-offset-1 ring-offset-[var(--background)]',
        )}
      >
        <header className="satellite-bus-group-header">
          <div className="flex min-w-0 flex-1 flex-wrap items-center gap-x-2 gap-y-0.5">
            <DenseTag variant="neutral" className="shrink-0 text-[10px] uppercase tracking-wide">
              Selected
            </DenseTag>
            <h3 className="satellite-bus-group-title">Selected Environment · {tradeEnv.toUpperCase()}</h3>
            <span className="text-[var(--text-dense-caption)] text-muted-foreground">
              {singleEnvScope} · {tradeSingleEnvProbeSource(tradeEnv)}
            </span>
          </div>
        </header>
        <div className="satellite-bus-group-body flex flex-col">
          <OpsSection
            variant="flat"
            title="Data path consumers"
            bodyPadding="none"
            overflow="hidden"
            description="Socket consumers reading the shared bus — REQUIRED must be up; EXPECTED OFF is intentional env policy (never a fault)"
          >
            <ConsumerTable
              rows={viewModel.dataPathConsumers}
              loading={busLoading}
              onInspect={row => openInspect({ kind: 'consumer', row })}
            />
          </OpsSection>
          <OpsSection
            variant="flat"
            title="Runtime consumers"
            bodyPadding="none"
            overflow="hidden"
            description="Monitor consumers — trading daemon / Trade APIs / Celery workers / account sync. Issues here degrade the bus verdict but never mark it unavailable. K8s workload readiness is Evidence and does not affect Bus Health"
          >
            <ConsumerTable
              rows={viewModel.runtimeConsumers}
              loading={busLoading}
              onInspect={row => openInspect({ kind: 'consumer', row })}
            />
          </OpsSection>
        </div>
      </div>

      <SecondaryGroup
        title="Shared dependencies"
        description="Rocket IB socket bus + Ground cluster — shared by all trade namespaces"
        scope="rocket"
        open={sharedOpen}
        onOpenChange={setSharedOpen}
        sectionRef={sharedSectionRef}
        highlight={highlightSection === 'rocket' || highlightSection === 'cluster'}
      >
        <OpsSection
          variant="flat"
          title="Rocket · Platform IB Gateway"
          bodyPadding="compact"
          overflow="hidden"
          description="Authoritative quote/account/operator path for all trade namespaces (data/ib-gateway @ redis-ib)"
        >
          <RocketSocketBusRow row={socketHealthMatrix.rocket} />
        </OpsSection>
        <OpsSection variant="flat" title="Payload readiness (Fleet projection)" bodyPadding="none" overflow="hidden">
          <PayloadReadinessTable rows={payloadRows} showActions={false} />
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
            onOpenObservability={onOpenObservability}
          />
        </div>
      </SecondaryGroup>

      <SecondaryGroup
        title="Other environments"
        description={`Socket consumers across all envs — highlight column = ${tradeEnv.toUpperCase()}`}
        scope="trade-multi-env"
        open={otherEnvsOpen}
        onOpenChange={setOtherEnvsOpen}
        sectionRef={otherEnvsSectionRef}
        highlight={highlightSection === 'socket' || highlightSection === 'ingest'}
      >
        <OpsSection
          variant="flat"
          title="Socket matrix · all envs"
          bodyPadding="compact"
          overflow="hidden"
          description="monitor.socket + bus semantics per namespace · cross-env issues above come from this data"
        >
          <div className="flex flex-col gap-2">
            <SocketHealthMatrixTable rows={socketHealthMatrix.tradeRows} selectedEnv={tradeEnv} />
            <p className="text-[var(--text-dense-caption)] text-muted-foreground m-0">
              Trading daemon row uses bus semantics (observe / paused / expected off). K3s Dev = bifrost-dev @
              :30882. Mac = satellite-probe-bridge on this workstation.
            </p>
          </div>
        </OpsSection>
      </SecondaryGroup>

      <SecondaryGroup
        title="Evidence"
        description={`Raw monitor FSM · Trade API reachability · K8s workloads for ${ns}`}
        scope="trade-single-env"
        open={evidenceOpen}
        onOpenChange={setEvidenceOpen}
        sectionRef={evidenceSectionRef}
        highlight={highlightSection === 'trade-apis' || highlightSection === 'workers'}
      >
        <OpsSection
          variant="flat"
          title="Raw monitor FSM"
          description="Strict trading-arm semantics — may differ from Bus health when observe / pause / expected-off is healthy"
          bodyPadding="none"
          overflow="hidden"
        >
          <MonitorKvTable rows={daemonRows} loading={busLoading} />
        </OpsSection>
        <div className="grid divide-x divide-[var(--border)] border-t border-[var(--border)] sm:grid-cols-2 xl:grid-cols-3">
          <OpsSection variant="flat" title="Celery" bodyPadding="none" overflow="hidden">
            <MonitorKvTable rows={celeryRows} loading={busLoading} />
          </OpsSection>
          <OpsSection variant="flat" title="Account sync" bodyPadding="none" overflow="hidden">
            <MonitorKvTable rows={accountSyncRows} loading={busLoading} />
          </OpsSection>
          <OpsSection variant="flat" title="Ops executor" bodyPadding="none" overflow="hidden">
            <MonitorKvTable rows={opsRows} loading={busLoading} />
          </OpsSection>
        </div>
        <OpsSection
          variant="flat"
          title="Trade API reachability"
          bodyPadding="none"
          overflow="hidden"
          description={`Matrix L0 HTTP probes for ${tradeEnv.toUpperCase()} · full detail on API & Auth Probes`}
        >
          <TradeApiReachTable targets={tradeApiTargetRows} loading={matrixQuery.isLoading} />
        </OpsSection>
        <OpsSection
          variant="flat"
          title="Critical processes"
          bodyPadding="none"
          overflow="hidden"
          description={`K8s workload readiness in ${ns} — evidence only, not part of the Bus Health verdict`}
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
      </SecondaryGroup>
      </div>
      </div>

      <Sheet open={inspect != null} onOpenChange={open => !open && setInspect(null)}>
        <SheetContent side="right" className="w-full overflow-y-auto sm:max-w-md">
          {inspectData != null && (
            <>
              <SheetHeader>
                <SheetTitle className="flex flex-wrap items-center gap-2">
                  <StatusLamp
                    value={
                      inspectData.health === 'expected-off'
                        ? 'unknown'
                        : busNodeHealthToReach(inspectData.health)
                    }
                    kind="reach"
                  />
                  {inspectData.title}
                </SheetTitle>
                <SheetDescription className="flex flex-wrap items-center gap-1.5">
                  <DenseTag variant="neutral" className="text-[9px] uppercase tracking-wide">
                    {inspectData.scopeLabel}
                  </DenseTag>
                  <DenseTag variant={healthTagVariant(inspectData.health)} className="text-[9px]">
                    {inspectData.stateLabel}
                  </DenseTag>
                </SheetDescription>
              </SheetHeader>
              <div className="flex flex-col gap-3 px-4 pb-4">
                {inspectData.headline != null && (
                  <p className="m-0 text-[var(--text-dense-meta)]">{inspectData.headline}</p>
                )}
                <div>
                  <p className="m-0 mb-0.5 text-[var(--text-dense-caption)] font-medium text-muted-foreground">
                    Detail
                  </p>
                  <p className="m-0 text-[var(--text-dense-meta)]">{inspectData.detail}</p>
                </div>
                <div>
                  <p className="m-0 mb-0.5 text-[var(--text-dense-caption)] font-medium text-muted-foreground">
                    Source / probe path
                  </p>
                  <p className="m-0 font-mono-tabular text-[var(--text-dense-caption)]">{inspectData.probePath}</p>
                </div>
                {inspectData.raw != null && (
                  <div>
                    <p className="m-0 mb-0.5 text-[var(--text-dense-caption)] font-medium text-muted-foreground">
                      Raw status
                    </p>
                    <pre className="m-0 max-h-72 overflow-auto rounded-md border border-[var(--border)] bg-[var(--secondary)] p-2 text-[10px] leading-snug">
                      {JSON.stringify(inspectData.raw, null, 2)}
                    </pre>
                  </div>
                )}
                <div className="flex flex-wrap items-center gap-2 border-t border-[var(--border)] pt-2">
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
                  {onOpenPluginGallery != null && (
                    <button
                      type="button"
                      className="focus-strip-link text-[var(--text-dense-caption)]"
                      onClick={onOpenPluginGallery}
                    >
                      IB Gateway plugin
                    </button>
                  )}
                  {onOpenApiHealth != null && (
                    <button
                      type="button"
                      className="focus-strip-link text-[var(--text-dense-caption)]"
                      onClick={onOpenApiHealth}
                    >
                      API & Auth Probes
                    </button>
                  )}
                </div>
              </div>
            </>
          )}
        </SheetContent>
      </Sheet>
    </div>
  )
}
