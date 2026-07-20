import { useEffect, useMemo, useState } from 'react'
import { Button, DenseTag, cn } from '@bifrost/ui'
import { ListFilter } from 'lucide-react'
import { AgentTriggerButton } from '@/components/agent/AgentTriggerButton'
import { StatusLamp } from '@/components/StatusLamp'
import type {
  FleetCell,
  FleetCellGate,
  FleetCellSignal,
  FleetEnvColumn,
  FleetRole,
  FleetSnapshot,
  FleetStandard,
} from '@/lib/control-room/fleetSnapshot'
import {
  cellKey,
  fleetRoleNavigateTab,
  groupStandards,
  resolveCellGate,
} from '@/lib/control-room/fleetSnapshot'
import { cellAllowsAgentFix } from '@/lib/control-room/fleetCellFix'
import {
  describeCoverageEntry,
  formatChecklistTouchAge,
  formatChecklistTouchAgeCompact,
  lookupCoverage,
  coverageKey,
  touchKindShortLabel,
  type ChecklistCoverageIndex,
} from '@/lib/control-room/dailyOpsChecklistCoverage'
import type { DailyOpsWorkflowPhase } from '@/lib/control-room/dailyOpsWorkflow'
import { useNowMs } from '@/hooks/useNowMs'
import {
  FLEET_ROLE_COLOR,
  FLEET_ROLE_ICON,
  FLEET_ROLE_LABEL,
} from '@/lib/control-room/fleetRoleVisuals'

const ROLE_LABEL = FLEET_ROLE_LABEL
const ROLE_ICON = FLEET_ROLE_ICON
const ROLE_COLOR = FLEET_ROLE_COLOR

/** Board row order — issues-only mode re-sorts problem roles to the top. */
const BOARD_ROLE_ORDER: FleetRole[] = ['rocket', 'satellite', 'engineer', 'ground', 'vendor']
const SPAN_ROLES: ReadonlySet<FleetRole> = new Set(['engineer', 'ground', 'vendor'])

const COL_LABEL: Record<FleetEnvColumn, string> = {
  dev: 'DEV',
  stg: 'STG',
  prod: 'PROD',
}

const ISSUES_ONLY_STORAGE_PREFIX = 'daily-ops-fleet-issues-only:'

function cellHasIssues(cell: FleetCell): boolean {
  const gate = resolveCellGate(cell)
  if (gate === 'NO-GO') return true
  const required = cell.standards.filter(s => s.required !== false)
  if (required.length === 0) return cell.signal !== 'ok' && gate !== 'N/A'
  return required.some(s => s.signal !== 'ok')
}

function roleCells(snap: FleetSnapshot, role: FleetRole): FleetCell[] {
  return snap.cells.filter(c => c.role === role)
}

function roleHasIssues(snap: FleetSnapshot, role: FleetRole): boolean {
  return roleCells(snap, role).some(cellHasIssues)
}

/** One-line quiet summary for an all-green role (issues-only mode). */
function roleGoSummaryLabel(snap: FleetSnapshot, role: FleetRole): string {
  const cells = roleCells(snap, role)
  if (SPAN_ROLES.has(role)) {
    return `${ROLE_LABEL[role]} · GO`
  }
  const envCount = cells.length > 0 ? cells.length : snap.columns.length
  return `${ROLE_LABEL[role]} · GO · ${envCount} envs`
}

function readStoredIssuesOnly(phase: DailyOpsWorkflowPhase | undefined): boolean | null {
  if (phase == null || typeof window === 'undefined') return null
  try {
    const raw = window.localStorage.getItem(`${ISSUES_ONLY_STORAGE_PREFIX}${phase}`)
    if (raw === '1') return true
    if (raw === '0') return false
  } catch {
    /* ignore quota / private mode */
  }
  return null
}

function writeStoredIssuesOnly(phase: DailyOpsWorkflowPhase | undefined, value: boolean): void {
  if (phase == null || typeof window === 'undefined') return
  try {
    window.localStorage.setItem(`${ISSUES_ONLY_STORAGE_PREFIX}${phase}`, value ? '1' : '0')
  } catch {
    /* ignore */
  }
}

/** Remediate defaults to issues-first; Discover / Verify / Clear default Show all. */
function defaultIssuesOnly(phase: DailyOpsWorkflowPhase | undefined): boolean {
  return phase === 'remediate'
}

function gateTagVariant(gate: FleetCellGate): 'success' | 'danger' | 'category' {
  if (gate === 'GO') return 'success'
  if (gate === 'NO-GO') return 'danger'
  return 'category'
}

/** Short chip label — keep board dense while every condition stays visible. */
function chipLabel(s: FleetStandard): string {
  const id = s.id.toLowerCase()
  if (id.startsWith('api-')) return id.slice(4)
  if (id === 'nginx-spa') return 'nginx'
  if (id === 'postgres' || id === 'redis') return id
  if (id === 'runners') return 'runners'
  if (id === 'git-bridge') return 'git'
  if (id === 'mac-seat') return 'mac'
  if (id === 'cluster-api') return 'api'
  if (id === 'nodes-ready') return 'nodes'
  if (id === 'failing-pods') return 'pods'
  if (id === 'hermes') return 'hermes'
  if (id === 'ib-feed') return 'ib'
  if (id === 'deliver-stg') return 'deliver'
  if (id === 'stg-smoke') return 'smoke'
  if (id.startsWith('platform-api')) return id.includes('prod') ? 'api·prod' : id.includes('stg') ? 'api·stg' : 'api'
  if (id.startsWith('platform-console')) {
    return id.includes('prod') ? 'ui·prod' : id.includes('stg') ? 'ui·stg' : 'ui'
  }
  if (id.includes('argo')) {
    if (id.includes('prod')) return 'argo·prod'
    if (id.includes('stg')) return 'argo·stg'
    return 'argo'
  }
  const raw = s.label
  return raw.length > 16 ? `${raw.slice(0, 15)}…` : raw
}

function chipTone(signal: FleetCellSignal, required: boolean): string {
  if (required === false) {
    return 'border-border/60 bg-muted/30 text-muted-foreground'
  }
  switch (signal) {
    case 'ok':
      return 'border-emerald-500/35 bg-emerald-500/10 text-emerald-700 dark:text-emerald-300'
    case 'degraded':
      return 'border-amber-500/45 bg-amber-500/15 text-amber-800 dark:text-amber-200'
    case 'fail':
      return 'border-destructive/50 bg-destructive/15 text-destructive'
    case 'unavailable':
      return 'border-border/50 bg-muted/40 text-muted-foreground line-through decoration-muted-foreground/50'
    default:
      return 'border-border/60 bg-muted/40 text-muted-foreground'
  }
}

function StandardChip({
  s,
  cellKey,
  coverage,
  flash,
  nowMs,
}: {
  s: FleetStandard
  cellKey: string
  coverage?: ChecklistCoverageIndex | null
  flash?: boolean
  nowMs: number
}) {
  const required = s.required !== false
  const isVirtual = s.source === 'checklist'
  const entry = lookupCoverage(coverage, { key: cellKey }, s)
  const hit = entry?.hit
  const excluded = entry?.excluded === true
  const uncovered = entry != null && !excluded && hit == null
  const isRun = hit?.touchKind === 'run'
  const touchAgeCompact =
    hit != null ? formatChecklistTouchAgeCompact(hit.touchedAt, nowMs) : null
  const coverageLine = describeCoverageEntry(entry, nowMs)
  const sourceLine = isVirtual
    ? 'Checklist projection · not from matrix probe'
    : 'Matrix / bridge probe'

  return (
    <span
      className={cn(
        'inline-flex max-w-full items-center gap-0.5 truncate rounded border px-1 py-px font-mono text-[9px] leading-tight',
        chipTone(s.signal, required),
        isVirtual && 'border-dashed border-violet-500/45 bg-violet-500/10',
        uncovered && 'ring-1 ring-amber-500/50',
        isRun && 'ring-1 ring-sky-500/45',
        flash && 'checklist-fleet-chip-flash relative',
      )}
      title={`${s.label}: ${s.signal}${required ? '' : ' (info)'}\n${s.reason}\n${sourceLine}\n${coverageLine}`}
    >
      {isVirtual && (
        <span className="shrink-0 font-sans text-[8px] text-violet-700 dark:text-violet-300" aria-hidden>
          chk·
        </span>
      )}
      {chipLabel(s)}
      {hit != null && (
        <span
          className={cn(
            'inline-flex shrink-0 items-center gap-px font-sans text-[8px] leading-none',
            isRun
              ? 'text-sky-700 dark:text-sky-300'
              : 'text-emerald-700/90 dark:text-emerald-300/90',
          )}
          aria-label={`Checklist ${hit.touchKind} touched ${formatChecklistTouchAge(hit.touchedAt, nowMs)}`}
        >
          <span aria-hidden>✓</span>
          <span className="font-semibold uppercase" aria-hidden>
            {touchKindShortLabel(hit.touchKind)}
          </span>
          <span className="opacity-80" aria-hidden>
            {touchAgeCompact}
          </span>
        </span>
      )}
      {uncovered && (
        <span
          className="shrink-0 font-sans text-[8px] text-amber-700 dark:text-amber-300"
          aria-label="Not in Daily Ops Checklist"
        >
          ?
        </span>
      )}
    </span>
  )
}

function FleetCellCard({
  cell,
  highlight,
  selected,
  prodWeight,
  wide,
  canOperate,
  agentFixPending,
  coverage,
  flashKeys,
  flashNonce,
  nowMs,
  onAgentFix,
  onSelect,
}: {
  cell: FleetCell
  highlight?: boolean
  selected?: boolean
  prodWeight?: boolean
  /** Span rows — lay groups horizontally to use width */
  wide?: boolean
  canOperate?: boolean
  agentFixPending?: boolean
  coverage?: ChecklistCoverageIndex | null
  /** Coverage keys currently flashing from Checklist section click */
  flashKeys?: ReadonlySet<string>
  /** Remount animation when the same keys flash again */
  flashNonce?: number
  nowMs: number
  onAgentFix?: (cell: FleetCell) => void
  onSelect: (cell: FleetCell) => void
}) {
  const gate = resolveCellGate(cell)
  const allowFix = cellAllowsAgentFix(cell)
  const showFix = gate === 'NO-GO'
  const sections = groupStandards(cell.standards)
  // GO cells: collapse green pills by default; Detail expands chips (and opens side panel).
  const showChips = gate !== 'GO' || selected === true
  const cellFlashing =
    flashKeys != null &&
    cell.standards.some(s => flashKeys.has(coverageKey(cell.key, s.id)))

  const required = cell.standards.filter(s => s.required !== false)
  const okCount = required.filter(s => s.signal === 'ok').length
  const totalCount = required.length > 0 ? required.length : cell.standards.length

  return (
    <div
      className={cn(
        'flex min-w-0 max-w-full flex-col gap-1 overflow-hidden rounded border px-1.5 py-1',
        highlight ? 'border-primary/50 bg-primary/5' : 'border-border/70 bg-background/60',
        selected && 'ring-1 ring-primary/60 border-primary/50',
        prodWeight && !highlight && !selected && 'border-border bg-background/80',
        gate === 'NO-GO' && 'border-destructive/40',
        gate === 'N/A' && 'opacity-80',
        cellFlashing && 'checklist-fleet-cell-flash',
      )}
      data-flash-nonce={cellFlashing ? flashNonce : undefined}
    >
      {/* Header: gate + actions on one row */}
      <div className="flex min-w-0 flex-wrap items-center gap-1">
        <StatusLamp
          value={gate === 'GO' ? 'ok' : gate === 'NO-GO' ? 'fail' : 'unknown'}
          kind="reach"
        />
        <DenseTag variant={gateTagVariant(gate)} className="text-[8px]">
          {gate}
        </DenseTag>
        <span className="min-w-0 truncate font-mono text-[var(--text-dense-micro)] text-muted-foreground">
          {cell.value}
        </span>
        {!showChips && gate === 'GO' && totalCount > 0 && (
          <span className="font-mono text-[8px] text-muted-foreground">
            {okCount}/{totalCount} ok
          </span>
        )}
        <span className="ml-auto flex shrink-0 items-center gap-1">
          <button
            type="button"
            className="text-[var(--text-dense-caption)] font-medium text-primary hover:underline"
            onClick={() => onSelect(cell)}
          >
            {selected ? 'Hide' : 'Detail'}
          </button>
          {showFix && allowFix && onAgentFix != null && (
            <AgentTriggerButton
              label="Fix"
              size="xs"
              pending={agentFixPending}
              disabled={!canOperate || agentFixPending}
              title={
                !canOperate
                  ? 'Authenticate as operator'
                  : cell.agentFixDisabledReason ?? cell.detail
              }
              onClick={() => onAgentFix(cell)}
            />
          )}
        </span>
      </div>

      {/* Every condition as colored chip, grouped — GO defaults collapsed */}
      {showChips && (
        <div
          className={cn(
            'flex min-w-0 gap-1.5',
            wide ? 'flex-row flex-wrap' : 'flex-col',
          )}
        >
          {sections.map(section => {
            const scored = section.items.filter(i => i.required !== false)
            const ok = scored.filter(i => i.signal === 'ok').length
            const total = scored.length > 0 ? scored.length : section.items.length
            const groupBad = scored.some(i => i.signal !== 'ok')
            return (
              <div
                key={section.group}
                className={cn('min-w-0', wide ? 'min-w-[9rem] flex-1' : '')}
              >
                <div
                  className={cn(
                    'mb-0.5 font-mono text-[8px] font-semibold uppercase tracking-wide',
                    groupBad ? 'text-foreground' : 'text-muted-foreground',
                  )}
                >
                  {section.label}{' '}
                  <span className="font-normal text-muted-foreground">
                    {ok}/{total}
                  </span>
                </div>
                <div className="flex min-w-0 flex-wrap gap-0.5">
                  {[...section.items]
                    .sort((a, b) => {
                      const rank = (s: FleetStandard) =>
                        s.signal === 'fail'
                          ? 3
                          : s.signal === 'degraded'
                            ? 2
                            : s.signal === 'unknown'
                              ? 1
                              : 0
                      return rank(b) - rank(a)
                    })
                    .map(s => (
                      <StandardChip
                        key={`${s.id}-${flashKeys?.has(coverageKey(cell.key, s.id)) ? flashNonce : 0}`}
                        s={s}
                        cellKey={cell.key}
                        coverage={coverage}
                        flash={flashKeys?.has(coverageKey(cell.key, s.id)) === true}
                        nowMs={nowMs}
                      />
                    ))}
                </div>
              </div>
            )
          })}
        </div>
      )}
    </div>
  )
}

function findCell(
  snap: FleetSnapshot,
  role: FleetRole,
  env: FleetEnvColumn | 'span',
): FleetCell | undefined {
  return snap.cells.find(c => c.key === cellKey(role, env))
}

export function DailyOpsFleetBoard({
  fleet,
  isLoading,
  canOperate,
  agentFixPending,
  selectedCellKey,
  coverage,
  flashKeys,
  flashNonce = 0,
  workflowPhase,
  onAgentFix,
  onSelectCell,
  onNavigate,
}: {
  fleet: FleetSnapshot
  isLoading?: boolean
  canOperate?: boolean
  agentFixPending?: boolean
  selectedCellKey?: string | null
  /** Dry-run checklist coverage + last touch times */
  coverage?: ChecklistCoverageIndex | null
  /** Coverage keys flashing from Checklist section click */
  flashKeys?: ReadonlySet<string>
  flashNonce?: number
  /** Process strip phase — Remediate defaults to issues-first; others Show all. */
  workflowPhase?: DailyOpsWorkflowPhase
  onAgentFix?: (cell: FleetCell) => void
  onSelectCell: (cell: FleetCell | null) => void
  onNavigate: (tabId: string) => void
}) {
  const nowMs = useNowMs()
  const worstKey = fleet.verdict.worstCell?.key
  const remediatingPhase = workflowPhase === 'remediate'
  const [issuesOnly, setIssuesOnly] = useState(() => {
    const stored = readStoredIssuesOnly(workflowPhase)
    return stored ?? defaultIssuesOnly(workflowPhase)
  })
  /** Issues-only: which all-green roles are expanded past the quiet summary. */
  const [expandedGoRoles, setExpandedGoRoles] = useState<ReadonlySet<FleetRole>>(
    () => new Set(),
  )

  useEffect(() => {
    const stored = readStoredIssuesOnly(workflowPhase)
    setIssuesOnly(stored ?? defaultIssuesOnly(workflowPhase))
    setExpandedGoRoles(new Set())
  }, [workflowPhase])

  const toggleIssuesOnly = () => {
    setIssuesOnly(prev => {
      const next = !prev
      writeStoredIssuesOnly(workflowPhase, next)
      if (!next) setExpandedGoRoles(new Set())
      return next
    })
  }

  const toggleGoRoleExpanded = (role: FleetRole) => {
    setExpandedGoRoles(prev => {
      const next = new Set(prev)
      if (next.has(role)) next.delete(role)
      else next.add(role)
      return next
    })
  }

  // Stable role order across Viewer seats (DEV/STG/PROD). "Issues only" collapses
  // green roles to a summary row — it must not reorder ROLE, or seats look inconsistent.
  const orderedRoles = BOARD_ROLE_ORDER

  const issueRoleCount = useMemo(
    () => BOARD_ROLE_ORDER.filter(r => roleHasIssues(fleet, r)).length,
    [fleet],
  )

  const envColCount = Math.max(fleet.columns.length, 1)
  // Fixed role gutter so labels stay readable in split layout (~556px pane).
  // Percent-only (was 9%) truncates "Satellite" / "Engineer" under table-fixed.
  const roleColWidth = '5.75rem'
  const envColPct = 100 / envColCount
  const colSpanAll = fleet.columns.length

  const renderRoleLabel = (role: FleetRole, opts?: { onHide?: () => void }) => {
    const Icon = ROLE_ICON[role]
    return (
      <div className="flex max-w-full flex-col items-start gap-0.5">
        <button
          type="button"
          className="inline-flex max-w-full items-center gap-1 text-[var(--text-dense-meta)] font-medium text-foreground hover:text-primary hover:underline"
          title={`Open ${ROLE_LABEL[role]}`}
          onClick={() => onNavigate(fleetRoleNavigateTab(role))}
        >
          <Icon className={cn('size-3 shrink-0', ROLE_COLOR[role])} aria-hidden />
          <span className="whitespace-nowrap">{ROLE_LABEL[role]}</span>
        </button>
        {opts?.onHide != null && (
          <button
            type="button"
            className="text-[var(--text-dense-caption)] font-medium text-primary hover:underline"
            onClick={opts.onHide}
          >
            Hide
          </button>
        )}
      </div>
    )
  }

  const renderGoSummaryRow = (role: FleetRole) => {
    return (
      <tr key={role} className="border-t border-border/40">
        <td className="whitespace-nowrap px-1 py-1 align-middle">{renderRoleLabel(role)}</td>
        <td className="min-w-0 px-0.5 py-1 align-middle" colSpan={colSpanAll}>
          <div className="flex min-w-0 items-center gap-1.5 rounded border border-border/50 bg-background/40 px-1.5 py-1">
            <StatusLamp value="ok" kind="reach" />
            <DenseTag variant="success" className="text-[8px]">
              GO
            </DenseTag>
            <span className="min-w-0 truncate text-[var(--text-dense-caption)] text-muted-foreground">
              {roleGoSummaryLabel(fleet, role)}
            </span>
            <button
              type="button"
              className="ml-auto shrink-0 text-[var(--text-dense-caption)] font-medium text-primary hover:underline"
              onClick={() => toggleGoRoleExpanded(role)}
            >
              Detail
            </button>
          </div>
        </td>
      </tr>
    )
  }

  const renderEnvRoleRow = (role: FleetRole, opts?: { onHide?: () => void }) => (
    <tr key={role}>
      <td className="whitespace-nowrap px-1 py-1 align-top">
        {renderRoleLabel(role, opts?.onHide != null ? { onHide: opts.onHide } : undefined)}
      </td>
      {fleet.columns.map(col => {
        const cell = findCell(fleet, role, col)
        if (cell == null) {
          return (
            <td key={col} className="min-w-0 px-0.5 py-1 align-top">
              <div className="rounded border border-dashed border-border/50 px-2 py-3 text-[var(--text-dense-caption)] text-muted-foreground">
                —
              </div>
            </td>
          )
        }
        return (
          <td key={col} className="min-w-0 px-0.5 py-1 align-top">
            <FleetCellCard
              cell={cell}
              highlight={cell.key === worstKey}
              selected={cell.key === selectedCellKey}
              prodWeight={col === 'prod'}
              canOperate={canOperate}
              agentFixPending={agentFixPending}
              coverage={coverage}
              flashKeys={flashKeys}
              flashNonce={flashNonce}
              nowMs={nowMs}
              onAgentFix={onAgentFix}
              onSelect={c => onSelectCell(selectedCellKey === c.key ? null : c)}
            />
          </td>
        )
      })}
    </tr>
  )

  const renderSpanRoleRow = (role: FleetRole, opts?: { onHide?: () => void }) => {
    const cell = findCell(fleet, role, 'span')
    return (
      <tr key={role}>
        <td className="whitespace-nowrap px-1 py-1 align-top">
          {renderRoleLabel(role, opts?.onHide != null ? { onHide: opts.onHide } : undefined)}
        </td>
        <td className="min-w-0 px-0.5 py-1 align-top" colSpan={colSpanAll}>
          {cell != null ? (
            <FleetCellCard
              cell={cell}
              wide
              highlight={cell.key === worstKey}
              selected={cell.key === selectedCellKey}
              canOperate={canOperate}
              agentFixPending={agentFixPending}
              coverage={coverage}
              flashKeys={flashKeys}
              flashNonce={flashNonce}
              nowMs={nowMs}
              onAgentFix={onAgentFix}
              onSelect={c => onSelectCell(selectedCellKey === c.key ? null : c)}
            />
          ) : (
            <div className="rounded border border-dashed border-border/50 px-2 py-3 text-[var(--text-dense-caption)] text-muted-foreground">
              —
            </div>
          )}
        </td>
      </tr>
    )
  }

  return (
    <div
      data-daily-ops-fleet-board
      className="min-w-0 max-w-full overflow-x-auto rounded-lg border border-border bg-secondary px-3 py-2.5"
    >
      <div className="mb-2 flex min-w-0 flex-wrap items-center gap-2">
        <span className="text-[var(--text-dense-label)] font-semibold">Fleet board</span>
        <DenseTag variant="neutral" className="text-[9px]">
          {issuesOnly ? 'Issues only' : 'All checks · grouped'}
        </DenseTag>
        <Button
          type="button"
          variant="outline"
          size="xs"
          className={cn(
            'h-6 cursor-pointer gap-1 px-2 text-[10px]',
            issuesOnly
              ? 'border-primary/50 bg-primary/5 text-foreground hover:bg-primary/10'
              : 'text-muted-foreground hover:text-foreground',
          )}
          title={
            issuesOnly
              ? 'Click to show all roles'
              : 'Click to show failing / non-green roles only'
          }
          aria-label={
            issuesOnly ? 'Showing issues. Click to show all roles' : 'Show issues only'
          }
          onClick={toggleIssuesOnly}
        >
          <ListFilter className="size-3 shrink-0" aria-hidden />
          {issuesOnly ? (
            <>
              <span className="font-normal text-muted-foreground">showing issues</span>
              <span className="text-muted-foreground" aria-hidden>
                ·
              </span>
              <span className="font-semibold text-primary underline underline-offset-2">
                Show all roles
              </span>
            </>
          ) : (
            <span>Issues only</span>
          )}
        </Button>
        {issuesOnly && remediatingPhase && issueRoleCount > 0 && (
          <span className="text-[var(--text-dense-caption)] text-muted-foreground">
            {issueRoleCount} role{issueRoleCount === 1 ? '' : 's'} need attention
          </span>
        )}
        {isLoading && (
          <span className="text-[var(--text-dense-caption)] text-muted-foreground">Probing…</span>
        )}
        {!fleet.fleetClear && (
          <DenseTag variant="warning" className="text-[9px]">
            Fleet not clear
          </DenseTag>
        )}
        {coverage != null && (
          <span
            className="text-[var(--text-dense-caption)] text-muted-foreground"
            title="Coverage: Checklist↔Fleet Board match ratio (excludes path + checklist-only virtuals)"
          >
            <span className="opacity-80">Coverage </span>
            <span className="text-emerald-600 dark:text-emerald-300">
              ✓d {coverage.boardMatchedCount}/{coverage.boardTotalCount}
            </span>
            {coverage.runTouchedCount > 0 && (
              <>
                {' · '}
                <span className="text-sky-700 dark:text-sky-300">
                  ✓r {coverage.runTouchedCount}
                </span>
              </>
            )}
            {coverage.virtualCount > 0 && (
              <>
                {' · '}
                <span className="text-violet-700 dark:text-violet-300">
                  chk {coverage.virtualCount}
                </span>
              </>
            )}
            {coverage.uncoveredCount > 0 ? (
              <>
                {' · '}
                <span className="text-amber-700 dark:text-amber-300">
                  ?{coverage.uncoveredCount} gap
                </span>
              </>
            ) : (
              <span className="ml-1 text-emerald-700/80 dark:text-emerald-300/80">· union ok</span>
            )}
            <span
              className="ml-1 opacity-70"
              title="d=dry-run match/total board · r=run · chk=checklist virtual (not in denominator)"
            >
              · ✓d|r · chk· = checklist-only
            </span>
          </span>
        )}
      </div>

      <table className="w-full min-w-[28rem] max-w-full table-fixed border-collapse text-left">
        <colgroup>
          <col style={{ width: roleColWidth }} />
          {fleet.columns.map(col => (
            <col key={col} style={{ width: `${envColPct}%` }} />
          ))}
        </colgroup>
        <thead>
          <tr>
            <th className="whitespace-nowrap px-1 py-1 text-[var(--text-dense-micro)] font-medium uppercase tracking-wide text-muted-foreground">
              Role
            </th>
            {fleet.columns.map(col => (
              <th
                key={col}
                className={cn(
                  'truncate px-1 py-1 uppercase tracking-wide',
                  col === 'prod'
                    ? 'text-[var(--text-dense-meta)] font-semibold text-foreground'
                    : 'text-[var(--text-dense-micro)] font-medium text-muted-foreground',
                )}
              >
                {COL_LABEL[col]}
              </th>
            ))}
          </tr>
        </thead>
        <tbody>
          {orderedRoles.map(role => {
            const problem = roleHasIssues(fleet, role)
            if (issuesOnly && !problem) {
              if (expandedGoRoles.has(role)) {
                const onHide = () => toggleGoRoleExpanded(role)
                if (SPAN_ROLES.has(role)) {
                  return renderSpanRoleRow(role, { onHide })
                }
                return renderEnvRoleRow(role, { onHide })
              }
              return renderGoSummaryRow(role)
            }
            if (SPAN_ROLES.has(role)) {
              return renderSpanRoleRow(role)
            }
            return renderEnvRoleRow(role)
          })}
        </tbody>
      </table>
    </div>
  )
}
