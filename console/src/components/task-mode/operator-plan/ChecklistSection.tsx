/**
 * Daily Ops Checklist — table section: legend + per-step cards + item rows +
 * dispatch/remediation-job action badges.
 *
 * There is no standalone remediation-job list UI in this panel (jobs are only
 * consulted to render per-item dispatch status), so that display folds into
 * this file rather than a separate RemediationJobSection.
 */
import { useState } from 'react'
import {
  Collapsible,
  CollapsibleContent,
  CollapsibleTrigger,
  DenseDataTable,
  DenseTag,
  DenseTableBody,
  DenseTableCell,
  DenseTableHead,
  DenseTableHeader,
  DenseTableHeadRow,
  DenseTableRow,
  StatusLamp,
  cn,
} from '@bifrost/ui'
import {
  CheckCircle2,
  ChevronRight,
  Circle,
  AlertTriangle,
  Eye,
  Wrench,
  Bot,
  Hand,
  Copy,
  Sparkles,
  Loader2,
} from 'lucide-react'
import { ChecklistDispatchActionDto } from '@/api/checklist'
import type { RemediationJob } from '@/api/remediationTypes'
import { DAILY_OPS_CHECKLIST_RUN_SCOPE } from '@/lib/agent/agentScopes'
import {
  deriveChecklistItemProgress,
  fleetAgentSignalDisagree,
  type ChecklistItemProgress,
} from '@/lib/control-room/checklistProgress'
import {
  checklistItemNeedsAttention,
  checklistItemPlatformFixAllowed,
  buildChecklistItemPlatformFixPrompt,
  type ChecklistFailoverItemInput,
} from '@/lib/control-room/checklistCursorFailoverPrompt'
import type { DailyOpsChecklistStep, FixCapability } from '@/lib/control-room/dailyOpsChecklistCatalog'
import {
  coverageKeysForChecklistStep,
  type ChecklistCoverageIndex,
} from '@/lib/control-room/dailyOpsChecklistCoverage'
import type { FleetCellSignal, FleetEnvColumn } from '@/lib/control-room/fleetSnapshot'
import {
  FLEET_ROLE_COLOR,
  FLEET_ROLE_ICON,
  FLEET_ROLE_LABEL,
  primaryFleetRole,
} from '@/lib/control-room/fleetRoleVisuals'
import {
  ENV_LABEL,
  ENV_ORDER,
  deployEnvsFromMapping,
  envLampTitle,
  lampValue,
  toFailoverInput,
  unhealthyEnvs,
  type ChecklistItemFixHandler,
  type EnvRollup,
  type ResolvedItem,
  type ResolvedStep,
} from './checklistResolve'

// ---------------------------------------------------------------------------
// Visual mappings
// ---------------------------------------------------------------------------

const SIGNAL_VARIANT: Record<FleetCellSignal, 'success' | 'warning' | 'danger' | 'neutral'> = {
  ok: 'success',
  degraded: 'warning',
  fail: 'danger',
  unavailable: 'neutral',
  unknown: 'neutral',
}

const FIX_ICON: Record<FixCapability, typeof Bot> = {
  full_auto: Bot,
  semi_auto: Wrench,
  manual: Hand,
  observe: Eye,
}

const FIX_LABEL: Record<FixCapability, string> = {
  full_auto: 'Auto',
  semi_auto: 'Semi',
  manual: 'Manual',
  observe: 'Observe',
}

/**
 * Fix capability colors — intentional non-health palette.
 * Never use emerald / red / amber (reserved for status lamps).
 */
const FIX_TONE_CLASS: Record<FixCapability, string> = {
  full_auto: 'border-sky-500/50 text-sky-700 dark:text-sky-300',
  semi_auto: 'border-violet-500/50 text-violet-700 dark:text-violet-300',
  manual: 'border-fuchsia-500/45 text-fuchsia-700 dark:text-fuchsia-300',
  observe: 'border-slate-500/45 text-slate-600 dark:text-slate-400',
}

const FIX_TITLE: Record<FixCapability, string> = {
  full_auto: 'Fix path: Agent can remediate without operator gate',
  semi_auto: 'Fix path: Agent can attempt fix; may pause for operator approval',
  manual: 'Fix path: physical/GUI action required — Agent cannot finish alone',
  observe: 'Fix path: observe only — no remediation (e.g. D10-blocked feeds)',
}

const FIX_LEGEND: Array<{ capability: FixCapability; blurb: string }> = [
  { capability: 'full_auto', blurb: 'Agent remediates alone' },
  { capability: 'semi_auto', blurb: 'Agent may need your approval' },
  { capability: 'manual', blurb: 'Human / physical action' },
  { capability: 'observe', blurb: 'No fix path — observe only' },
]

/**
 * Env identity colors — distinct from health lamps (green/amber/red/gray).
 * Never use emerald / red / amber for env identity (those mean ok / fail / degraded).
 * DEV=sky · STG=violet · PROD=indigo
 */
const ENV_TONE_CLASS: Record<FleetEnvColumn, string> = {
  dev: 'text-sky-600 dark:text-sky-400',
  stg: 'text-violet-600 dark:text-violet-400',
  prod: 'text-indigo-600 dark:text-indigo-300',
}

/** Dense table cells default to max-w-0; override so compact cards don't collapse. */
const cellTight = '!max-w-none whitespace-nowrap'

// ---------------------------------------------------------------------------
// Section (legend + per-step cards)
// ---------------------------------------------------------------------------

export type ChecklistSectionProps = {
  showMeta: boolean
  visibleSteps: ResolvedStep[]
  failingOnly: boolean
  compactColumns: boolean
  activeFlashStepId: string | null
  remediatingStepIds: Set<string>
  checklistItemFixActiveId?: string | null
  ambientJobScope?: string | null
  dispatchByItem: Map<string, ChecklistDispatchActionDto>
  agentSignalByItem: Map<string, string>
  jobs: RemediationJob[]
  onOpenDispatchJob?: (jobId: string) => void
  onOpenOperateQueue?: (queueId?: string) => void
  onChecklistItemFix?: ChecklistItemFixHandler
  checklistItemFixPending?: boolean
  checklistItemFixDisabled?: boolean
  checklistItemFixTitle?: string
  copyState: 'idle' | 'copied' | 'error'
  copiedItemId: string | null
  onAskAiItem: (input: ChecklistFailoverItemInput) => void
  onFlashStep?: (stepId: string, coverageKeys: string[]) => void
  coverage?: ChecklistCoverageIndex | null
}

export function ChecklistSection({
  showMeta,
  visibleSteps,
  failingOnly,
  compactColumns,
  activeFlashStepId,
  remediatingStepIds,
  checklistItemFixActiveId = null,
  ambientJobScope = null,
  dispatchByItem,
  agentSignalByItem,
  jobs,
  onOpenDispatchJob,
  onOpenOperateQueue,
  onChecklistItemFix,
  checklistItemFixPending = false,
  checklistItemFixDisabled = false,
  checklistItemFixTitle,
  copyState,
  copiedItemId,
  onAskAiItem,
  onFlashStep,
  coverage,
}: ChecklistSectionProps) {
  return (
    <>
      {showMeta && <ChecklistLegend />}

      {/* Two columns from lg: dependency order flows top→bottom then left→right */}
      <div className={cn('columns-1 gap-x-3', !compactColumns && 'lg:columns-2')}>
        {visibleSteps.map(rs => (
          <div key={rs.step.id} className="mb-2 break-inside-avoid">
            <StepCard
              resolved={rs}
              active={activeFlashStepId === rs.step.id}
              remediating={remediatingStepIds.has(rs.step.id)}
              remediatingItemId={checklistItemFixActiveId}
              remediatingScope={
                ambientJobScope === DAILY_OPS_CHECKLIST_RUN_SCOPE ? null : ambientJobScope
              }
              dispatchByItem={dispatchByItem}
              agentSignalByItem={agentSignalByItem}
              jobs={jobs}
              onOpenDispatchJob={onOpenDispatchJob}
              onOpenOperateQueue={onOpenOperateQueue}
              onChecklistItemFix={onChecklistItemFix}
              checklistItemFixPending={checklistItemFixPending}
              checklistItemFixDisabled={checklistItemFixDisabled}
              checklistItemFixTitle={checklistItemFixTitle}
              checklistItemFixActiveId={checklistItemFixActiveId}
              copyState={copyState}
              copiedItemId={copiedItemId}
              onAskAiItem={onAskAiItem}
              onFlash={() => {
                const keys = coverageKeysForChecklistStep(coverage, rs.step.id)
                onFlashStep?.(rs.step.id, keys)
              }}
            />
          </div>
        ))}
        {failingOnly && visibleSteps.length === 0 && (
          <p className="m-0 text-[var(--text-dense-caption)] text-muted-foreground">
            No failing items — toggle All items to review the full checklist.
          </p>
        )}
      </div>
    </>
  )
}

// ---------------------------------------------------------------------------
// Env scope chips (step header)
// ---------------------------------------------------------------------------

function EnvScopeChips({ mapping }: { mapping: DailyOpsChecklistStep['fleetMapping'] }) {
  const deploy = deployEnvsFromMapping(mapping)
  if (deploy.length === 0) {
    return <span className="text-[var(--text-dense-caption)] text-muted-foreground">ALL</span>
  }
  return (
    <span className="inline-flex items-center gap-1 text-[var(--text-dense-caption)] font-semibold">
      {deploy.map((env, i) => (
        <span key={env} className="inline-flex items-center gap-1">
          {i > 0 && <span className="font-normal text-muted-foreground/50">·</span>}
          <span className={ENV_TONE_CLASS[env]}>{ENV_LABEL[env]}</span>
        </span>
      ))}
    </span>
  )
}

// ---------------------------------------------------------------------------
// Legend (collapsed by default — icon / status help)
// ---------------------------------------------------------------------------

const CHECKLIST_LEGEND_OPEN_KEY = 'daily-ops-checklist-legend-open'

function readChecklistLegendOpen(): boolean {
  try {
    return window.localStorage.getItem(CHECKLIST_LEGEND_OPEN_KEY) === '1'
  } catch {
    return false
  }
}

function writeChecklistLegendOpen(open: boolean) {
  try {
    window.localStorage.setItem(CHECKLIST_LEGEND_OPEN_KEY, open ? '1' : '0')
  } catch {
    /* ignore quota / private mode */
  }
}

function ChecklistLegend() {
  const [open, setOpen] = useState(readChecklistLegendOpen)

  return (
    <Collapsible
      open={open}
      onOpenChange={next => {
        setOpen(next)
        writeChecklistLegendOpen(next)
      }}
      className="group/legend mb-2"
    >
      <CollapsibleTrigger asChild>
        <button
          type="button"
          className="flex items-center gap-1 text-[var(--text-dense-caption)] text-muted-foreground hover:text-foreground"
          aria-expanded={open}
        >
          <ChevronRight
            className="size-3 shrink-0 transition-transform group-data-[state=open]/legend:rotate-90"
            aria-hidden
          />
          <span>{open ? 'Hide legend' : 'Show legend'}</span>
        </button>
      </CollapsibleTrigger>
      <CollapsibleContent>
        <div className="mt-1.5 flex flex-col gap-1 rounded border border-border/40 bg-muted/20 px-2 py-1.5 text-[var(--text-dense-caption)] text-muted-foreground">
          <div className="flex flex-wrap items-center gap-x-3 gap-y-1">
            <span className="font-semibold text-foreground/80">Legend</span>
            <span className="inline-flex items-center gap-1.5">
              <span className="inline-flex items-center gap-0.5">
                <StatusLamp value="ok" kind="reach" />
                <span>ok</span>
              </span>
              <span className="inline-flex items-center gap-0.5">
                <StatusLamp value="degraded" kind="reach" />
                <span>degraded</span>
              </span>
              <span className="inline-flex items-center gap-0.5">
                <StatusLamp value="fail" kind="reach" />
                <span>fail</span>
              </span>
              <span className="inline-flex items-center gap-0.5">
                <StatusLamp value="unknown" kind="reach" />
                <span>unknown / unavailable (no probe or not scored)</span>
              </span>
            </span>
            <span className="text-border">|</span>
            <span className="inline-flex items-center gap-1.5">
              <span>Env</span>
              {ENV_ORDER.map(env => (
                <span key={env} className={cn('font-mono text-[8px] font-semibold', ENV_TONE_CLASS[env])}>
                  {ENV_LABEL[env][0]}
                  <span className="font-normal text-muted-foreground">={ENV_LABEL[env]}</span>
                </span>
              ))}
            </span>
          </div>
          <div className="flex flex-wrap items-center gap-x-3 gap-y-1">
            <span className="inline-flex items-center gap-1">
              <DenseTag variant="neutral" className="text-[8px]">
                gate
              </DenseTag>
              <span>= catalog gate-critical (can block downstream) — not a live fail</span>
            </span>
            <span className="text-border">|</span>
            {FIX_LEGEND.map(({ capability, blurb }) => {
              const Icon = FIX_ICON[capability]
              return (
                <span key={capability} className="inline-flex items-center gap-1">
                  <DenseTag
                    variant="neutral"
                    className={cn('text-[8px]', FIX_TONE_CLASS[capability])}
                    title={FIX_TITLE[capability]}
                  >
                    <Icon className="mr-0.5 inline size-2.5" />
                    {FIX_LABEL[capability]}
                  </DenseTag>
                  <span>{blurb}</span>
                </span>
              )
            })}
            <span className="text-border">|</span>
            <span className="inline-flex items-center gap-1">
              <span className="font-semibold text-foreground/70">Do</span>
              <span className="inline-flex h-4 w-4 items-center justify-center rounded border border-sky-500/55 bg-sky-500/15">
                <Sparkles className="size-2.5 text-sky-700 dark:text-sky-300" aria-hidden />
              </span>
              <span>= Ops Fix</span>
              <span className="inline-flex h-4 w-4 items-center justify-center rounded border border-border bg-background">
                <Copy className="size-2.5" aria-hidden />
              </span>
              <span>= Ask AI (Cursor copy)</span>
            </span>
          </div>
        </div>
      </CollapsibleContent>
    </Collapsible>
  )
}

// ---------------------------------------------------------------------------
// Per-step compact card (own mini-table — fits two-column layout)
// ---------------------------------------------------------------------------

function StepCard({
  resolved,
  active,
  remediating = false,
  remediatingItemId = null,
  remediatingScope = null,
  onFlash,
  dispatchByItem,
  agentSignalByItem,
  jobs,
  onOpenDispatchJob,
  onOpenOperateQueue,
  onChecklistItemFix,
  checklistItemFixPending = false,
  checklistItemFixDisabled = false,
  checklistItemFixTitle,
  checklistItemFixActiveId = null,
  copyState,
  copiedItemId,
  onAskAiItem,
}: {
  resolved: ResolvedStep
  active?: boolean
  /** Ambient Agent is working on this step — pulse highlight. */
  remediating?: boolean
  remediatingItemId?: string | null
  remediatingScope?: string | null
  onFlash?: () => void
  dispatchByItem: Map<string, ChecklistDispatchActionDto>
  agentSignalByItem: Map<string, string>
  jobs: RemediationJob[]
  onOpenDispatchJob?: (jobId: string) => void
  onOpenOperateQueue?: (queueId?: string) => void
  onChecklistItemFix?: ChecklistItemFixHandler
  checklistItemFixPending?: boolean
  checklistItemFixDisabled?: boolean
  checklistItemFixTitle?: string
  checklistItemFixActiveId?: string | null
  copyState: 'idle' | 'copied' | 'error'
  copiedItemId: string | null
  onAskAiItem: (input: ChecklistFailoverItemInput) => void
}) {
  const { step, items, stepSignal, blockedByUpstream, envScopeLabel } = resolved
  const role = primaryFleetRole(step.fleetMapping)
  const RoleIcon = FLEET_ROLE_ICON[role]
  const roleLabel = FLEET_ROLE_LABEL[role]
  const focusLabel = step.label.includes('·')
    ? step.label.split('·').slice(1).join('·').trim()
    : step.label

  return (
    <div
      className={cn(
        'overflow-hidden rounded border border-border/50 bg-background/80 transition-[box-shadow,border-color]',
        active && !remediating && 'border-primary/60 ring-1 ring-primary/40',
        remediating &&
          'border-sky-500/80 ring-2 ring-sky-400/50 shadow-[0_0_12px_-2px] shadow-sky-500/35 animate-pulse',
        blockedByUpstream && !remediating && 'opacity-50',
      )}
      aria-busy={remediating || undefined}
    >
      <button
        type="button"
        className={cn(
          'flex w-full flex-wrap items-center gap-1.5 px-2 py-1 text-left hover:bg-primary/8',
          remediating ? 'bg-sky-500/15' : 'bg-muted/40',
        )}
        onClick={() => onFlash?.()}
        title={
          remediating
            ? `Agent remediating · ${roleLabel} · ${focusLabel}`
            : `Flash Fleet Board · ${roleLabel} · ${focusLabel} · ${envScopeLabel}`
        }
      >
        <StepIcon signal={stepSignal} blocked={blockedByUpstream} />
        <span className="text-[var(--text-dense-meta)] font-semibold text-muted-foreground">
          {step.order}.
        </span>
        <span className="inline-flex items-center gap-1">
          <RoleIcon className={cn('size-3.5 shrink-0', FLEET_ROLE_COLOR[role])} aria-hidden />
          <span className={cn('text-[var(--text-dense-meta)] font-semibold', FLEET_ROLE_COLOR[role])}>
            {roleLabel}
          </span>
        </span>
        <span className="text-[var(--text-dense-meta)] text-muted-foreground">·</span>
        <span className="text-[var(--text-dense-meta)] font-semibold text-foreground">
          {focusLabel}
        </span>
        <DenseTag variant={SIGNAL_VARIANT[stepSignal]} className="text-[9px]">
          {stepSignal}
        </DenseTag>
        {remediating && (
          <DenseTag
            variant="neutral"
            className="text-[8px] border-sky-500/50 text-sky-700 dark:text-sky-300"
          >
            Agent…
          </DenseTag>
        )}
        {step.blocksDownstream && (
          <span className="text-[9px] text-muted-foreground">(blocks ↓)</span>
        )}
        <span className="ml-auto">
          <EnvScopeChips mapping={step.fleetMapping} />
        </span>
      </button>

      <DenseDataTable wrapClassName="rounded-none border-0 border-t border-border/40">
        <DenseTableHeader>
          <DenseTableHeadRow>
            <DenseTableHead className={cn(cellTight, 'w-6')}> </DenseTableHead>
            <DenseTableHead className={cn(cellTight, 'min-w-[7rem]')}>Check</DenseTableHead>
            <DenseTableHead className={cn(cellTight, 'w-12')}>Gate</DenseTableHead>
            <DenseTableHead className={cn(cellTight, 'w-[4.75rem]')}>Path</DenseTableHead>
            <DenseTableHead
              className={cn(cellTight, 'w-[4.25rem]')}
              title="Ops Agent Fix · Cursor Ask for AI"
            >
              Do
            </DenseTableHead>
            <DenseTableHead className={cn(cellTight, 'w-16')} title="Dispatch / RemediationJob">
              Action
            </DenseTableHead>
            <DenseTableHead className={cn(cellTight, 'w-[4.5rem]')} title="DEV · STG · PROD">
              Env
            </DenseTableHead>
            <DenseTableHead className={cn(cellTight, 'min-w-[4rem]')}>Notes</DenseTableHead>
          </DenseTableHeadRow>
        </DenseTableHeader>
        <DenseTableBody>
          {items.map(ri => {
            const item = ri.checklistItem
            const rowRemediating =
              remediating &&
              (item.id === remediatingItemId ||
                (remediatingScope != null &&
                  remediatingScope !== '' &&
                  item.fixScope === remediatingScope &&
                  checklistItemNeedsAttention(ri.overallSignal)))
            return (
              <ItemTableRow
                key={item.id}
                step={step}
                resolved={ri}
                remediating={rowRemediating}
                dispatch={dispatchByItem.get(item.id)}
                agentSignal={agentSignalByItem.get(item.id)}
                jobs={jobs}
                onOpenDispatchJob={onOpenDispatchJob}
                onOpenOperateQueue={onOpenOperateQueue}
                onChecklistItemFix={onChecklistItemFix}
                checklistItemFixPending={checklistItemFixPending}
                checklistItemFixDisabled={checklistItemFixDisabled}
                checklistItemFixTitle={checklistItemFixTitle}
                checklistItemFixActiveId={checklistItemFixActiveId}
                copyState={copyState}
                copiedItemId={copiedItemId}
                onAskAiItem={onAskAiItem}
              />
            )
          })}
        </DenseTableBody>
      </DenseDataTable>
    </div>
  )
}

function StepIcon({ signal, blocked }: { signal: FleetCellSignal; blocked: boolean }) {
  if (blocked) {
    return <Circle className="size-3.5 text-muted-foreground/50" />
  }
  if (signal === 'ok') {
    return <CheckCircle2 className="size-3.5 text-success" />
  }
  if (signal === 'fail') {
    return <AlertTriangle className="size-3.5 text-destructive" />
  }
  return <Circle className="size-3.5 text-warning" />
}

// ---------------------------------------------------------------------------
// Item row
// ---------------------------------------------------------------------------

function EnvLamps({
  envRollups,
  spanOk,
}: {
  envRollups: EnvRollup[]
  spanOk?: boolean
}) {
  if (envRollups.length === 0) {
    return (
      <span className="text-[8px] text-muted-foreground" title="Span role · all envs">
        {spanOk ? 'ALL' : '—'}
      </span>
    )
  }
  return (
    <span className="inline-flex items-center gap-1">
      {ENV_ORDER.map(env => {
        const rollup = envRollups.find(e => e.env === env)
        if (rollup == null) {
          return (
            <span key={env} className="text-[8px] text-muted-foreground/40" title={`${ENV_LABEL[env]}: n/a`}>
              ·
            </span>
          )
        }
        return (
          <span
            key={env}
            className="inline-flex items-center gap-0.5"
            title={envLampTitle(rollup)}
          >
            <span className={cn('font-mono text-[8px] font-bold', ENV_TONE_CLASS[env])}>
              {ENV_LABEL[env][0]}
            </span>
            <StatusLamp value={lampValue(rollup.signal)} kind="reach" />
          </span>
        )
      })}
    </span>
  )
}

function truncateNote(text: string, max = 42): string {
  const t = text.trim()
  if (t.length <= max) return t
  return `${t.slice(0, max - 1)}…`
}

function ItemTableRow({
  step,
  resolved,
  remediating = false,
  dispatch,
  agentSignal,
  jobs,
  onOpenDispatchJob,
  onOpenOperateQueue,
  onChecklistItemFix,
  checklistItemFixPending = false,
  checklistItemFixDisabled = false,
  checklistItemFixTitle,
  checklistItemFixActiveId = null,
  copyState,
  copiedItemId,
  onAskAiItem,
}: {
  step: DailyOpsChecklistStep
  resolved: ResolvedItem
  remediating?: boolean
  dispatch?: ChecklistDispatchActionDto
  agentSignal?: string
  jobs: RemediationJob[]
  onOpenDispatchJob?: (jobId: string) => void
  onOpenOperateQueue?: (queueId?: string) => void
  onChecklistItemFix?: ChecklistItemFixHandler
  checklistItemFixPending?: boolean
  checklistItemFixDisabled?: boolean
  checklistItemFixTitle?: string
  checklistItemFixActiveId?: string | null
  copyState: 'idle' | 'copied' | 'error'
  copiedItemId: string | null
  onAskAiItem: (input: ChecklistFailoverItemInput) => void
}) {
  const { checklistItem: item, matchedStandards, overallSignal, envRollups } = resolved
  const FixIcon = FIX_ICON[item.fixCapability]
  const showEnvCols = envRollups.length > 0
  const badEnvs = unhealthyEnvs(envRollups)
  const needsAttention = checklistItemNeedsAttention(overallSignal)
  const canPlatformFix = checklistItemPlatformFixAllowed(item) && needsAttention
  const failoverInput = toFailoverInput(step, resolved, agentSignal, dispatch)
  const itemFixBusy =
    checklistItemFixPending &&
    (checklistItemFixActiveId == null || checklistItemFixActiveId === item.id)

  const linkedJob =
    dispatch?.job_id != null
      ? jobs.find(j => j.id === dispatch.job_id)
      : jobs.find(
          j =>
            j.status === 'running' &&
            item.fixScope != null &&
            j.scope === item.fixScope &&
            (j.actor === 'checklist-dispatch' || (j.init_brief ?? '').includes(item.id)),
        )

  const progress = deriveChecklistItemProgress({ dispatch, linkedJob })
  const disagree = fleetAgentSignalDisagree(overallSignal, agentSignal)

  const notes: string[] = []
  if (disagree) {
    notes.push('fleet≠agent')
  }
  if (badEnvs.length > 0) {
    notes.push(`failing: ${badEnvs.map(e => ENV_LABEL[e.env]).join(' · ')}`)
  }
  if (!showEnvCols && matchedStandards.length > 0 && overallSignal !== 'ok') {
    const n = matchedStandards.filter(s => s.signal !== 'ok').length
    notes.push(`${n}/${matchedStandards.length} fail`)
  }
  if (matchedStandards.length === 0) notes.push('no probes')
  if (
    matchedStandards.length > 0 &&
    matchedStandards.every(s => s.source === 'checklist')
  ) {
    notes.push('chk')
  } else if (
    matchedStandards.some(s => s.source === 'checklist') &&
    matchedStandards.some(s => (s.source ?? 'probe') === 'probe')
  ) {
    notes.push('probe+chk')
  }
  const showManualHint =
    (progress.state === 'notify' || item.fixCapability === 'manual') &&
    item.manualAction != null &&
    item.manualAction.trim() !== ''
  if (showManualHint) {
    notes.push(truncateNote(item.manualAction!))
  }
  if (progress.state === 'notify') {
    notes.push('Next: follow manual / physical step')
  }

  const noteTitle = [
    disagree
      ? `fleet≠agent — fleet=${overallSignal} · agent=${agentSignal ?? 'n/a'} (lamps stay fleet)`
      : null,
    showManualHint ? `Manual: ${item.manualAction}` : null,
    progress.state === 'notify' ? 'Next: follow manual / physical step' : null,
    badEnvs.length > 0
      ? `failing: ${badEnvs.map(e => ENV_LABEL[e.env]).join(' · ')}`
      : null,
  ]
    .filter(Boolean)
    .join('\n')

  const fixTitle = [
    FIX_TITLE[item.fixCapability],
    item.fixScope != null ? `Scope: ${item.fixScope}` : null,
    item.manualAction != null ? `Manual: ${item.manualAction}` : null,
  ]
    .filter(Boolean)
    .join('\n')

  const actionTitle =
    progress.state === 'notify' && item.manualAction != null
      ? [progress.title, `Manual: ${item.manualAction}`].join(' · ')
      : progress.title

  const rowFixDisabled =
    !canPlatformFix ||
    onChecklistItemFix == null ||
    checklistItemFixDisabled ||
    checklistItemFixPending
  const rowFixTitle = !needsAttention
    ? 'Item is healthy — no Fix needed'
    : item.fixCapability === 'observe'
      ? 'Observe-only (D10) — use Ask for AI for Cursor diagnosis; no Ops Agent Fix'
      : item.fixScope == null
        ? 'No fixScope — use Ask for AI (Cursor failover) or follow manualAction'
        : (checklistItemFixTitle ??
          `Start Ops Agent Fix · scope ${item.fixScope}`)

  return (
    <DenseTableRow
      className={cn(
        remediating && 'bg-sky-500/15 ring-2 ring-inset ring-sky-500/45',
      )}
      data-checklist-item-id={item.id}
      data-checklist-remediating={remediating ? 'true' : undefined}
    >
      <DenseTableCell className={cellTight}>
        <StatusLamp value={lampValue(overallSignal)} kind="reach" />
      </DenseTableCell>
      <DenseTableCell className={cn(cellTight, '!whitespace-normal')}>
        <span className="text-[var(--text-dense-caption)] font-medium text-foreground">
          {item.label}
        </span>
      </DenseTableCell>
      <DenseTableCell className={cellTight}>
        {item.critical ? (
          <DenseTag
            variant={
              overallSignal === 'fail' || overallSignal === 'degraded' ? 'danger' : 'neutral'
            }
            className="text-[8px]"
            title="Catalog: gate-critical check (can block downstream)."
          >
            gate
          </DenseTag>
        ) : (
          <span className="text-muted-foreground/40">—</span>
        )}
      </DenseTableCell>
      <DenseTableCell className={cellTight}>
        <DenseTag
          variant="neutral"
          className={cn('text-[8px]', FIX_TONE_CLASS[item.fixCapability])}
          title={fixTitle}
        >
          <FixIcon className="mr-0.5 inline size-2.5" />
          {FIX_LABEL[item.fixCapability]}
        </DenseTag>
      </DenseTableCell>
      <DenseTableCell className={cellTight}>
        {needsAttention ? (
          <span className="inline-flex h-6 flex-nowrap items-center gap-1">
            {canPlatformFix && onChecklistItemFix != null ? (
              <button
                type="button"
                disabled={rowFixDisabled}
                title={rowFixTitle}
                aria-label={`Ops Agent Fix · ${item.label}`}
                className={cn(
                  'inline-flex h-6 w-6 shrink-0 items-center justify-center rounded-md border shadow-sm transition-colors',
                  // Secondary to Ops loop primary CTA (P4)
                  'border-border/70 bg-background text-muted-foreground hover:bg-muted hover:text-foreground',
                  'disabled:cursor-not-allowed disabled:opacity-40 disabled:hover:bg-background',
                  'focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring/40',
                  remediating &&
                    'border-sky-500/55 bg-sky-500/15 text-sky-800 hover:bg-sky-500/25 dark:text-sky-200',
                )}
                onClick={e => {
                  e.stopPropagation()
                  if (item.fixScope == null) return
                  onChecklistItemFix({
                    itemId: item.id,
                    fixScope: item.fixScope,
                    label: `${item.label} Fix`,
                    prompt: buildChecklistItemPlatformFixPrompt(failoverInput),
                  })
                }}
              >
                {itemFixBusy ? (
                  <Loader2 className="size-3.5 animate-spin" aria-hidden />
                ) : (
                  <Sparkles className="size-3.5" aria-hidden />
                )}
              </button>
            ) : (
              <span
                className="inline-flex h-6 w-6 shrink-0 items-center justify-center rounded-md border border-dashed border-border/50 text-muted-foreground/35"
                title={rowFixTitle}
                aria-hidden
              >
                —
              </span>
            )}
            <button
              type="button"
              title={
                copyState === 'copied' && copiedItemId === item.id
                  ? 'Copied to clipboard'
                  : 'Copy Cursor IDE Agent failover pack for this item'
              }
              aria-label={`Ask for AI · copy Cursor pack · ${item.label}`}
              className={cn(
                'inline-flex h-6 w-6 shrink-0 items-center justify-center rounded-md border shadow-sm transition-colors',
                'border-border bg-background text-foreground hover:bg-muted',
                'focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring/40',
                copyState === 'copied' &&
                  copiedItemId === item.id &&
                  'border-emerald-500/50 bg-emerald-500/10 text-emerald-700 dark:text-emerald-300',
              )}
              onClick={e => {
                e.stopPropagation()
                onAskAiItem(failoverInput)
              }}
            >
              {copyState === 'copied' && copiedItemId === item.id ? (
                <CheckCircle2 className="size-3.5" aria-hidden />
              ) : (
                <Copy className="size-3.5" aria-hidden />
              )}
            </button>
          </span>
        ) : (
          <span className="text-muted-foreground/30">—</span>
        )}
      </DenseTableCell>
      <DenseTableCell className={cellTight}>
        <DispatchActionBadge
          progress={{ ...progress, title: actionTitle }}
          onOpenDispatchJob={onOpenDispatchJob}
          onOpenOperateQueue={onOpenOperateQueue}
        />
      </DenseTableCell>
      <DenseTableCell className={cellTight}>
        <EnvLamps envRollups={envRollups} spanOk={overallSignal === 'ok'} />
      </DenseTableCell>
      <DenseTableCell className={cn(cellTight, '!whitespace-normal')}>
        {notes.length > 0 ? (
          <span
            className={cn(
              'text-[var(--text-dense-caption)]',
              disagree || badEnvs.length > 0 ? 'text-destructive' : 'text-muted-foreground',
            )}
            title={noteTitle || notes.join(' · ')}
          >
            {notes.join(' · ')}
          </span>
        ) : (
          <span className="text-muted-foreground/30">—</span>
        )}
      </DenseTableCell>
    </DenseTableRow>
  )
}

const PROGRESS_TONE: Record<ChecklistItemProgress['state'], string> = {
  idle: '',
  checking: 'border-sky-500/45 text-sky-700 dark:text-sky-300',
  reported: 'border-sky-500/45 text-sky-700 dark:text-sky-300',
  queued: 'border-violet-500/45 text-violet-700 dark:text-violet-300',
  auto_running: 'border-sky-500/45 text-sky-700 dark:text-sky-300',
  notify: 'border-fuchsia-500/40 text-fuchsia-700 dark:text-fuchsia-300',
  skip: 'border-slate-500/40 text-slate-600 dark:text-slate-400',
  done: 'border-emerald-500/40 text-emerald-700 dark:text-emerald-300',
  failed: 'border-destructive/50 text-destructive',
}

function DispatchActionBadge({
  progress,
  onOpenDispatchJob,
  onOpenOperateQueue,
}: {
  progress: ChecklistItemProgress
  onOpenDispatchJob?: (jobId: string) => void
  onOpenOperateQueue?: (queueId?: string) => void
}) {
  if (progress.state === 'idle') {
    return <span className="text-muted-foreground/30">—</span>
  }
  const openJob =
    progress.openTarget === 'job' && progress.jobId != null && onOpenDispatchJob != null
  const openQueue = progress.openTarget === 'queue' && onOpenOperateQueue != null
  const clickable = openJob || openQueue
  const tone = PROGRESS_TONE[progress.state]
  const displayLabel =
    openQueue && !progress.label.includes('→') ? `${progress.label} →` : progress.label
  const tag = (
    <DenseTag
      variant={progress.state === 'failed' ? 'danger' : 'neutral'}
      className={cn('text-[8px]', tone, clickable && 'cursor-pointer hover:opacity-90')}
      title={progress.title}
    >
      {displayLabel}
    </DenseTag>
  )
  if (!clickable) return tag
  return (
    <button
      type="button"
      className="inline-flex border-0 bg-transparent p-0"
      title={progress.title}
      onClick={() => {
        if (openJob) onOpenDispatchJob?.(progress.jobId!)
        else if (openQueue) onOpenOperateQueue?.(progress.queueId)
      }}
    >
      {tag}
    </button>
  )
}
