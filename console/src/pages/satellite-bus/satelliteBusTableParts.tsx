import type { ReactNode, Ref, SyntheticEvent } from 'react'
import {
  DenseDataTable,
  DenseTableBody,
  DenseTableCell,
  DenseTableHead,
  DenseTableHeadRow,
  DenseTableHeader,
  DenseTableRow,
  DenseTag,
  cn,
} from '@bifrost/ui'
import type { Target } from '@/api/matrixTypes'
import { StatusLamp } from '@/components/StatusLamp'
import {
  SOCKET_MATRIX_LABELS,
  type SocketHealthEnvCell,
  type SocketHealthMatrixRow,
  type SocketHealthRow,
  type SocketRequiredState,
  type TradeEnvId,
} from '@/lib/satellite/socketHealthSemantics'
import { busScopeGroupClass, type BusStatusScope } from '@/lib/satellite/busStatusScope'
import {
  busNodeHealthToReach,
  type BusAttentionIssue,
  type BusConsumerRow,
  type BusPathNode,
} from '@/lib/satellite-bus/satelliteBusViewModel'
import type { CriticalProcessRow, MonitorKvRow } from '@/pages/satellite-bus/useSatelliteBusQueries'
import { displayReachLabel, healthTagVariant } from '@/pages/satellite-bus/satelliteBusTableUtils'

export function TradeApiReachTable({
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

function BusScopeBadge({ scope }: { scope: BusStatusScope }) {
  const label =
    scope === 'rocket'
      ? 'Shared · Rocket'
      : scope === 'trade-multi-env'
        ? 'All envs'
        : scope === 'trade-single-env'
          ? 'Selected NS'
          : 'Shared · Ground'
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

export function SocketHealthMatrixTable({
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

export function RocketSocketBusRow({ row }: { row: SocketHealthRow }) {
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

export function MonitorKvTable({ rows, loading }: { rows: MonitorKvRow[]; loading?: boolean }) {
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

export function BusPathNodeCard({
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

export function AttentionIssueRow({
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

export function ConsumerTable({
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

export function CriticalProcessesTable({
  rows,
  loading,
}: {
  rows: CriticalProcessRow[]
  loading: boolean
}) {
  return (
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
        {loading ? (
          <DenseTableRow>
            <DenseTableCell colSpan={4} className="text-[var(--muted-foreground)]">
              Loading…
            </DenseTableCell>
          </DenseTableRow>
        ) : (
          rows.map(row => (
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
  )
}

export function SecondaryGroup({
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
