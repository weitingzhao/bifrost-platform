import { useMemo, useState } from 'react'
import {
  Button,
  Collapsible,
  CollapsibleContent,
  CollapsibleTrigger,
  ConfirmDialog,
  DenseTag,
  cn,
} from '@bifrost/ui'
import { ChevronRight, Copy } from 'lucide-react'
import { AgentTriggerButton } from '@/components/agent/AgentTriggerButton'
import { StatusLamp } from '@/components/StatusLamp'
import { postIbGatewayControl } from '@/api/network'
import { cellAllowsAgentFix } from '@/lib/control-room/fleetCellFix'
import type { DailyOpsBlocker } from '@/lib/control-room/dailyOpsPrimaryBlocker'
import {
  blockerRequiresManualPath,
  manualPrimaryCtaLabel,
} from '@/lib/control-room/dailyOpsPrimaryBlocker'
import { matchStandardToChecklistItem } from '@/lib/control-room/dailyOpsChecklistCatalog'
import type { FleetCell, FleetCellSignal, FleetStandard } from '@/lib/control-room/fleetSnapshot'
import {
  fleetCellNavigateTab,
  groupStandards,
  resolveCellGate,
  rollupStandards,
} from '@/lib/control-room/fleetSnapshot'
import {
  formatChecklistTouchAge,
  lookupCoverage,
  touchKindLabel,
  type ChecklistCoverageIndex,
} from '@/lib/control-room/dailyOpsChecklistCoverage'
import { useNowMs } from '@/hooks/useNowMs'
import {
  FLEET_ROLE_COLOR,
  FLEET_ROLE_ICON,
  FLEET_ROLE_LABEL,
} from '@/lib/control-room/fleetRoleVisuals'

const ROLE_LABEL = FLEET_ROLE_LABEL
const ROLE_ICON = FLEET_ROLE_ICON
const ROLE_COLOR = FLEET_ROLE_COLOR

/** Probe age above this is highlighted as stale (1h). */
const STALE_MS = 60 * 60 * 1000

/** Reasons longer than this stay collapsed by default (passing rows only). */
const LONG_NOTE_CHARS = 72

function lampValue(signal: FleetCellSignal): 'ok' | 'degraded' | 'fail' | 'unknown' {
  if (signal === 'unavailable') return 'unknown'
  return signal
}

function navigateTabLabel(tabId: string, role: FleetCell['role']): string {
  switch (tabId) {
    case 'plugin-gallery':
      return 'Open Plugin Gallery'
    case 'operator-plane':
      return 'Open Operator Plane'
    case 'queue':
    case 'agent-desk':
      return 'Open Queue'
    case 'satellite-bus':
      return 'Open Satellite Bus'
    case 'cluster':
      return 'Open Cluster'
    default:
      return `Open ${ROLE_LABEL[role]} page`
  }
}

function isFailingRequired(s: FleetStandard): boolean {
  return s.required !== false && s.signal !== 'ok'
}

function StandardDenseRow({
  s,
  cell,
  coverage,
  nowMs,
  forceExpandReason = false,
}: {
  s: FleetStandard
  cell: FleetCell
  coverage?: ChecklistCoverageIndex | null
  nowMs: number
  forceExpandReason?: boolean
}) {
  const failing = isFailingRequired(s)
  const cov = lookupCoverage(coverage, cell, s)
  const hit = cov?.hit
  const reason = s.reason?.trim() ?? ''
  const hasLongNote = reason.length > LONG_NOTE_CHARS
  const [noteOpen, setNoteOpen] = useState(forceExpandReason || failing)
  const showReason = reason !== '' && (forceExpandReason || failing || !hasLongNote || noteOpen)

  return (
    <div
      className={cn(
        'border-b border-border/40 px-1.5 py-1 last:border-0',
        failing && 'bg-destructive/5',
      )}
      data-standard-id={s.id}
      data-standard-failing={failing ? 'true' : undefined}
    >
      <div className="flex min-w-0 items-center gap-1.5">
        <StatusLamp value={lampValue(s.signal)} kind="reach" />
        <span className="min-w-0 flex-1 truncate text-[var(--text-dense-caption)] text-foreground">
          {s.label}
          <span className="ml-1 font-mono text-[8px] text-muted-foreground">{s.id}</span>
          {s.required === false && (
            <span className="ml-1 text-[8px] text-muted-foreground">info</span>
          )}
        </span>
        <span
          className={cn(
            'shrink-0 font-mono text-[var(--text-dense-micro)] uppercase',
            failing ? 'font-semibold text-destructive' : 'text-muted-foreground',
          )}
        >
          {s.signal}
        </span>
        <span
          className={cn(
            'shrink-0 text-[8px]',
            s.source === 'checklist'
              ? 'text-violet-700 dark:text-violet-300'
              : 'text-muted-foreground',
          )}
        >
          {s.source === 'checklist' ? 'chk' : 'probe'}
        </span>
        {cov?.excluded ? (
          <span className="shrink-0 text-[8px] text-muted-foreground">excl</span>
        ) : hit != null ? (
          <span
            className={cn(
              'shrink-0 text-[8px]',
              hit.touchKind === 'run'
                ? 'text-sky-700 dark:text-sky-300'
                : 'text-emerald-700 dark:text-emerald-300',
            )}
            title={`${hit.itemLabel} · ${touchKindLabel(hit.touchKind)} · ${formatChecklistTouchAge(hit.touchedAt, nowMs)}`}
          >
            ✓{hit.touchKind === 'run' ? 'r' : 'd'} {formatChecklistTouchAge(hit.touchedAt, nowMs)}
          </span>
        ) : (
          <span className="shrink-0 text-[8px] text-amber-700 dark:text-amber-300">?</span>
        )}
        {hasLongNote && !forceExpandReason && !failing && (
          <button
            type="button"
            className="inline-flex shrink-0 items-center gap-0.5 text-[8px] text-muted-foreground hover:text-foreground"
            aria-expanded={noteOpen}
            onClick={() => setNoteOpen(v => !v)}
          >
            <ChevronRight
              className={cn('size-2.5 transition-transform', noteOpen && 'rotate-90')}
              aria-hidden
            />
            note
          </button>
        )}
      </div>
      {showReason && (
        <p
          className={cn(
            'm-0 mt-0.5 pl-4 text-[var(--text-dense-caption)]',
            failing ? 'text-foreground' : 'text-muted-foreground',
          )}
        >
          {reason}
        </p>
      )}
    </div>
  )
}

export function DailyOpsFleetCellDetail({
  cell,
  canOperate,
  agentFixPending,
  coverage,
  dataUpdatedAt,
  primaryBlocker,
  primaryActionLabel,
  suppressSuggestedNext = false,
  onAgentFix,
  onNavigate,
  onReprobe,
  onClose,
  onProposeCommit,
  onProposeStash,
  proposeCommitPending = false,
  proposeCommitDisabled = false,
  proposeCommitTitle,
}: {
  cell: FleetCell
  canOperate?: boolean
  agentFixPending?: boolean
  coverage?: ChecklistCoverageIndex | null
  /** Fleet cockpit last update (ms). */
  dataUpdatedAt?: number
  primaryBlocker?: DailyOpsBlocker
  primaryActionLabel?: string
  /** P7 — remediating: Ops loop already shows Next:; skip duplicate Suggested next. */
  suppressSuggestedNext?: boolean
  onAgentFix?: (cell: FleetCell) => void
  onNavigate: (tabId: string) => void
  onReprobe?: () => void
  onClose: () => void
  /** git-dirty-remediate — propose commit (operator approval). */
  onProposeCommit?: () => void
  onProposeStash?: () => void
  proposeCommitPending?: boolean
  proposeCommitDisabled?: boolean
  proposeCommitTitle?: string
}) {
  const nowMs = useNowMs()
  const gate = resolveCellGate(cell)
  const allowFix = cellAllowsAgentFix(cell)
  const detailTab = fleetCellNavigateTab(cell)
  const envLabel = cell.env != null ? cell.env.toUpperCase() : 'ALL'
  const required = cell.standards.filter(s => s.required !== false)
  const failing = required.filter(s => s.signal !== 'ok')
  const passing = required.filter(s => s.signal === 'ok')
  const sections = groupStandards(cell.standards)
  const rollups = rollupStandards(cell.standards)
  const RoleIcon = ROLE_ICON[cell.role]
  const isSpan = cell.span || cell.env == null
  const probeAgeMs =
    dataUpdatedAt != null && dataUpdatedAt > 0 ? Math.max(0, nowMs - dataUpdatedAt) : null
  const isStale = probeAgeMs != null && probeAgeMs > STALE_MS
  const nextAction =
    primaryBlocker != null && primaryBlocker.cellKey === cell.key
      ? blockerRequiresManualPath(primaryBlocker)
        ? manualPrimaryCtaLabel(primaryBlocker)
        : (primaryActionLabel ?? `AI Fix · ${primaryBlocker.label}`)
      : primaryActionLabel

  const [probeNoteOpen, setProbeNoteOpen] = useState(false)
  const [copyFeedback, setCopyFeedback] = useState<string | null>(null)
  const [reconnectOpen, setReconnectOpen] = useState(false)
  const [reconnectPending, setReconnectPending] = useState(false)
  const [reconnectMsg, setReconnectMsg] = useState<string | null>(null)
  const cellDetail = cell.detail?.trim() ?? ''
  const hasProbeNote = cellDetail.length > 0

  const primaryFail = failing[0]
  const ibFeedFailing = failing.some(s => s.id === 'ib-feed')
  const gitBridgeFailing = failing.some(s => s.id === 'git-bridge')
  const primaryManual = useMemo(() => {
    if (primaryFail == null) return null
    const matched = matchStandardToChecklistItem(primaryFail.id, primaryFail.group, {
      role: cell.role,
      env: cell.env ?? 'span',
    })
    return matched?.item.manualAction?.trim() || null
  }, [primaryFail, cell.role, cell.env])

  const suggestedNext =
    ibFeedFailing
      ? 'Reconnect Gateway (TWS OK → refresh plugin session)'
      : gitBridgeFailing
        ? 'Propose commit (git-dirty-remediate · approval required)'
        : nextAction

  const failureBrief = useMemo(() => {
    const lines = [
      `## Fleet Cell · ${ROLE_LABEL[cell.role]} · ${envLabel}`,
      `Gate: ${gate} (${passing.length}/${required.length} required ok)`,
      '',
      '### Failing standards',
    ]
    if (failing.length === 0) {
      lines.push('(none)')
    } else {
      for (const s of failing) {
        lines.push(`- **${s.label}** (\`${s.id}\`) · ${s.signal}`)
        if (s.reason?.trim()) lines.push(`  - ${s.reason.trim()}`)
      }
    }
    if (primaryManual) {
      lines.push('', '### Manual action', primaryManual)
    }
    if (cell.agentFixDisabledReason) {
      lines.push('', '### Agent Fix', cell.agentFixDisabledReason)
    }
    if (ibFeedFailing) {
      lines.push(
        '',
        '### Operator actuation',
        'Reconnect Gateway via plugin control (IB Gateway reconnect) — does not touch live trading.',
      )
    }
    if (gitBridgeFailing) {
      lines.push(
        '',
        '### Operator actuation',
        'Start remediation scope git-dirty-remediate — Propose commit or Stash; request_operator_approval before write. Never auto-discard WIP.',
      )
    }
    return lines.join('\n')
  }, [
    cell.role,
    cell.agentFixDisabledReason,
    envLabel,
    gate,
    passing.length,
    required.length,
    failing,
    primaryManual,
    ibFeedFailing,
    gitBridgeFailing,
  ])

  async function handleCopyFailure() {
    try {
      await navigator.clipboard.writeText(failureBrief)
      setCopyFeedback('Copied')
      window.setTimeout(() => setCopyFeedback(null), 2000)
    } catch {
      setCopyFeedback('Copy failed')
      window.setTimeout(() => setCopyFeedback(null), 2000)
    }
  }

  async function handleReconnect() {
    setReconnectPending(true)
    setReconnectMsg(null)
    try {
      const resp = await postIbGatewayControl('reconnect')
      setReconnectMsg(resp.ok ? resp.message : `Failed: ${resp.message}`)
      if (resp.ok) {
        // Give rollout a moment, then refresh fleet probes.
        window.setTimeout(() => onReprobe?.(), 2500)
      }
    } catch (e) {
      setReconnectMsg(e instanceof Error ? e.message : 'Reconnect failed')
    } finally {
      setReconnectPending(false)
      setReconnectOpen(false)
    }
  }

  return (
    <div
      data-daily-ops-cell-detail
      className="flex max-h-[min(28rem,50vh)] min-w-0 max-w-full flex-col overflow-hidden rounded-lg border border-border bg-secondary"
    >
      {/* Sticky chrome: title + Scope / Suggested next / Re-probe */}
      <div className="shrink-0 border-b border-border/60 px-3 py-2">
        <div className="mb-1.5 flex min-w-0 flex-wrap items-center justify-between gap-2">
          <div className="flex min-w-0 flex-wrap items-center gap-2">
            <span className="text-[var(--text-dense-label)] font-semibold">Cell detail</span>
            <DenseTag variant="neutral" className="inline-flex items-center gap-1 text-[9px]">
              <RoleIcon className={cn('size-2.5', ROLE_COLOR[cell.role])} aria-hidden />
              {ROLE_LABEL[cell.role]} · {envLabel}
            </DenseTag>
            <DenseTag
              variant={gate === 'GO' ? 'success' : gate === 'NO-GO' ? 'danger' : 'category'}
              className="text-[9px]"
            >
              {gate}
            </DenseTag>
            <span className="font-mono text-[var(--text-dense-micro)] text-muted-foreground">
              {cell.value}
              <span className="ml-1 text-muted-foreground/80">
                ({passing.length}/{required.length} required)
              </span>
            </span>
          </div>
          <button
            type="button"
            className="text-[var(--text-dense-caption)] text-muted-foreground hover:text-foreground"
            onClick={onClose}
          >
            Close
          </button>
        </div>

        <div className="rounded border border-border/60 bg-background/70 px-2 py-1.5">
          <p className="m-0 text-[var(--text-dense-caption)] font-medium text-foreground">
            Scope:{' '}
            <span className="font-normal text-muted-foreground">
              {isSpan ? 'all envs (span)' : `${ROLE_LABEL[cell.role]} · ${envLabel}`}
            </span>
          </p>
          {suggestedNext != null && suggestedNext !== '' && !suppressSuggestedNext && (
            <p className="m-0 mt-0.5 text-[var(--text-dense-caption)] text-muted-foreground">
              Suggested next: <span className="text-foreground">{suggestedNext}</span>
            </p>
          )}
          {suppressSuggestedNext && (
            <p className="m-0 mt-0.5 text-[var(--text-dense-micro)] text-muted-foreground/80">
              Next: see Ops loop
            </p>
          )}
          <p
            className={cn(
              'm-0 mt-0.5 text-[var(--text-dense-caption)]',
              isStale ? 'text-amber-700 dark:text-amber-300' : 'text-muted-foreground',
            )}
          >
            {probeAgeMs != null ? (
              <>
                {isStale ? 'Probe stale' : 'Probe age'}{' '}
                {dataUpdatedAt != null
                  ? formatChecklistTouchAge(new Date(dataUpdatedAt).toISOString(), nowMs)
                  : 'unknown'}
              </>
            ) : (
              'Probe age unknown'
            )}
            {onReprobe != null && (
              <>
                {' · '}
                <button type="button" className="text-primary hover:underline" onClick={onReprobe}>
                  Re-probe
                </button>
              </>
            )}
          </p>
        </div>
      </div>

      <div className="min-h-0 flex-1 overflow-y-auto px-3 py-2">
        <p className="m-0 mb-1.5 text-[var(--text-dense-caption)] text-muted-foreground">
          {gate === 'GO'
            ? 'All required standards are green.'
            : gate === 'N/A'
              ? 'Structural path unavailable — excluded from fleet GO / NO-GO.'
              : `${failing.length} of ${required.length} required standard(s) not green → NO-GO.`}
        </p>

        {/* Failures first — always visible, reason expanded */}
        {failing.length > 0 && (
          <div
            className="mb-2 overflow-hidden rounded border border-destructive/40 bg-destructive/5"
            data-cell-failing-block
          >
            <div className="border-b border-destructive/30 px-1.5 py-0.5 text-[var(--text-dense-micro)] font-semibold uppercase tracking-wide text-destructive">
              Failing now · {failing.length}
            </div>
            {failing.map(s => (
              <StandardDenseRow
                key={`fail-${s.id}`}
                s={s}
                cell={cell}
                coverage={coverage}
                nowMs={nowMs}
                forceExpandReason
              />
            ))}
            {primaryManual != null && (
              <p className="m-0 border-t border-destructive/20 px-1.5 py-1.5 text-[var(--text-dense-caption)] text-foreground">
                <span className="font-medium">Manual: </span>
                {primaryManual}
              </p>
            )}
            {cell.agentFixDisabledReason != null && cell.agentFixDisabledReason !== '' && (
              <p className="m-0 border-t border-destructive/20 px-1.5 py-1 text-[var(--text-dense-caption)] text-muted-foreground">
                Agent Fix disabled: {cell.agentFixDisabledReason}
              </p>
            )}
          </div>
        )}

        <div className="mb-1.5 flex min-w-0 flex-wrap gap-1.5">
          {rollups.map(r => (
            <DenseTag
              key={r.group}
              variant={r.signal === 'ok' ? 'success' : r.signal === 'fail' ? 'danger' : 'warning'}
              className="text-[9px]"
            >
              {r.label} {r.ok}/{r.total}
            </DenseTag>
          ))}
        </div>

        <div className="mb-2 flex min-w-0 flex-col gap-1.5">
          {sections.map(section => {
            // Keep failing rows in group list too, but de-emphasize duplicates by
            // sorting failing to top within the section for scanability.
            const items = [...section.items].sort((a, b) => {
              const af = isFailingRequired(a) ? 0 : 1
              const bf = isFailingRequired(b) ? 0 : 1
              return af - bf
            })
            return (
              <div
                key={section.group}
                className="overflow-hidden rounded border border-border/70 bg-background/60"
              >
                <div className="border-b border-border/60 px-1.5 py-0.5 text-[var(--text-dense-micro)] font-semibold uppercase tracking-wide text-muted-foreground">
                  {section.label}
                </div>
                {items.map(s => (
                  <StandardDenseRow
                    key={s.id}
                    s={s}
                    cell={cell}
                    coverage={coverage}
                    nowMs={nowMs}
                    forceExpandReason={isFailingRequired(s)}
                  />
                ))}
              </div>
            )
          })}
        </div>

        {hasProbeNote && (
          <Collapsible open={probeNoteOpen} onOpenChange={setProbeNoteOpen} className="mb-2">
            <CollapsibleTrigger asChild>
              <button
                type="button"
                className="flex items-center gap-1 text-[var(--text-dense-caption)] text-muted-foreground hover:text-foreground"
                aria-expanded={probeNoteOpen}
              >
                <ChevronRight
                  className={cn('size-3 shrink-0 transition-transform', probeNoteOpen && 'rotate-90')}
                  aria-hidden
                />
                {probeNoteOpen ? 'Hide probe note' : 'Show probe note'}
              </button>
            </CollapsibleTrigger>
            <CollapsibleContent>
              <p className="m-0 mt-1 rounded border border-border/50 bg-background/50 px-2 py-1.5 text-[var(--text-dense-caption)] text-muted-foreground">
                {cellDetail}
              </p>
            </CollapsibleContent>
          </Collapsible>
        )}
      </div>

      {/* Sticky manual actions — always reachable without scrolling past feeds */}
      <div className="shrink-0 border-t border-border/60 bg-secondary px-3 py-2">
        <div className="flex flex-wrap items-center gap-2">
          {ibFeedFailing && (
            <Button
              type="button"
              size="xs"
              variant="default"
              disabled={!canOperate || reconnectPending}
              title={
                canOperate
                  ? 'Rollout restart data/ib-gateway — use when TWS is fine but snapshot/heartbeat is stale'
                  : 'Operator authentication required'
              }
              onClick={() => setReconnectOpen(true)}
            >
              {reconnectPending ? 'Reconnecting…' : 'Reconnect Gateway'}
            </Button>
          )}
          {gitBridgeFailing && onProposeCommit != null && (
            <Button
              type="button"
              size="xs"
              variant="default"
              disabled={
                !canOperate || proposeCommitPending || proposeCommitDisabled || reconnectPending
              }
              title={
                !canOperate
                  ? 'Operator authentication required'
                  : (proposeCommitTitle ??
                    'Start git-dirty-remediate — approval required before commit/stash')
              }
              onClick={() => onProposeCommit()}
            >
              {proposeCommitPending ? 'Starting…' : 'Propose commit'}
            </Button>
          )}
          {gitBridgeFailing && onProposeStash != null && (
            <Button
              type="button"
              size="xs"
              variant="outline"
              disabled={
                !canOperate || proposeCommitPending || proposeCommitDisabled || reconnectPending
              }
              title={
                !canOperate
                  ? 'Operator authentication required'
                  : 'Start git-dirty-remediate toward stash (approval required; never drop WIP)'
              }
              onClick={() => onProposeStash()}
            >
              Stash
            </Button>
          )}
          {gate === 'NO-GO' && allowFix && onAgentFix != null && (
            <AgentTriggerButton
              label="Agent Fix"
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
          <button
            type="button"
            className="inline-flex items-center gap-1 rounded border border-border bg-background px-2 py-0.5 text-[var(--text-dense-caption)] font-medium text-primary hover:bg-accent"
            onClick={() => onNavigate(detailTab)}
          >
            <RoleIcon className={cn('size-2.5', ROLE_COLOR[cell.role])} aria-hidden />
            {navigateTabLabel(detailTab, cell.role)} →
          </button>
          {onReprobe != null && (
            <button
              type="button"
              className="inline-flex items-center rounded border border-border bg-background px-2 py-0.5 text-[var(--text-dense-caption)] text-foreground hover:bg-accent"
              onClick={onReprobe}
            >
              Re-probe
            </button>
          )}
          {failing.length > 0 && (
            <button
              type="button"
              className="inline-flex items-center gap-1 rounded border border-border bg-background px-2 py-0.5 text-[var(--text-dense-caption)] text-muted-foreground hover:text-foreground hover:bg-accent"
              onClick={() => void handleCopyFailure()}
              title="Copy failing standards + manual action for IDE Agent"
            >
              <Copy className="size-2.5" aria-hidden />
              {copyFeedback ?? 'Copy failure'}
            </button>
          )}
        </div>
        {reconnectMsg != null && (
          <p className="m-0 mt-1 text-[var(--text-dense-caption)] text-muted-foreground">
            {reconnectMsg}
          </p>
        )}
        {gate === 'NO-GO' && !allowFix && cell.agentFixDisabledReason != null && (
          <p className="m-0 mt-1 text-[var(--text-dense-micro)] text-muted-foreground">
            {ibFeedFailing
              ? 'No Agent Fix for IB — if TWS is already running, use Reconnect Gateway above (plugin session refresh).'
              : gitBridgeFailing
                ? 'No cell Agent Fix for Engineer — use Propose commit / Stash above (git-dirty-remediate; approval required).'
                : 'No Agent Fix for this cell — use the manual path above.'}
          </p>
        )}
      </div>

      <ConfirmDialog
        open={reconnectOpen}
        title="Reconnect IB Gateway"
        message="Rollout restart deployment/ib-gateway in data NS. Use when TWS is already running but account snapshot / heartbeat is stale (ghost or hung API client). Does not place orders (D10)."
        confirmLabel="Confirm reconnect"
        confirming={reconnectPending}
        onConfirm={() => void handleReconnect()}
        onCancel={() => setReconnectOpen(false)}
      />
    </div>
  )
}
