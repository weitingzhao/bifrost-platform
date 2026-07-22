/**
 * Daily Ops Checklist — KPI cards + progress header row.
 *
 * Pass/fail tags, quiet-success streak, Re-check link, Ask for AI (all),
 * Failing-only toggle, meta/coverage strip, and the "Full Operator Plane" link.
 */
import { Button, DenseTag, cn } from '@bifrost/ui'
import { Copy, ListFilter } from 'lucide-react'
import type { ChecklistHeaderProgress } from '@/lib/control-room/checklistProgress'
import { formatDispatchHeaderStrip } from '@/lib/control-room/checklistProgress'
import {
  formatChecklistTouchAge,
  type ChecklistCoverageIndex,
} from '@/lib/control-room/dailyOpsChecklistCoverage'

export type ChecklistKPIHeaderProps = {
  okItems: number
  totalItems: number
  failItems: number
  streak: number
  newFailHint?: string | null
  onChecklistCheck?: () => void
  checkBusy: boolean
  proberHint: string | null
  checklistCheckTitle?: string
  checklistCheckDisabled?: boolean
  checklistCheckPending?: boolean
  checklistCheckError?: string | null
  attentionCount: number
  copyState: 'idle' | 'copied' | 'error'
  copiedItemId: string | null
  onAskAiAll: () => void
  checklistItemFixError?: string | null
  failingOnly: boolean
  onToggleFailingOnly: () => void
  remediatingPhase: boolean
  showMeta: boolean
  onToggleShowMeta: () => void
  compactColumns: boolean
  coverage?: ChecklistCoverageIndex | null
  nowMs: number
  headerProgress: ChecklistHeaderProgress
  onOpenFullOperatorPlane: () => void
}

export function ChecklistKPIHeader({
  okItems,
  totalItems,
  failItems,
  streak,
  newFailHint,
  onChecklistCheck,
  checkBusy,
  proberHint,
  checklistCheckTitle,
  checklistCheckDisabled = false,
  checklistCheckPending = false,
  checklistCheckError = null,
  attentionCount,
  copyState,
  copiedItemId,
  onAskAiAll,
  checklistItemFixError = null,
  failingOnly,
  onToggleFailingOnly,
  remediatingPhase,
  showMeta,
  onToggleShowMeta,
  compactColumns,
  coverage,
  nowMs,
  headerProgress,
  onOpenFullOperatorPlane,
}: ChecklistKPIHeaderProps) {
  const dispatchStrip = formatDispatchHeaderStrip(headerProgress)

  return (
    <>
      <div className="mb-2 flex flex-wrap items-center gap-2">
        <span className="text-[var(--text-dense-caption)] font-semibold text-foreground">
          Daily Ops Checklist
        </span>
        <DenseTag variant={failItems > 0 ? 'danger' : 'success'} className="text-[9px]">
          {okItems}/{totalItems} pass
        </DenseTag>
        {streak > 0 && failItems === 0 && (
          <DenseTag
            variant="neutral"
            className="text-[8px] border-sky-500/40 text-sky-700 dark:text-sky-300"
            title="Quiet success streak — consecutive checklist runs with zero fail/degraded"
          >
            quiet ×{streak}
          </DenseTag>
        )}
        {newFailHint != null && newFailHint !== '' && failItems > 0 && (
          <span className="text-[8px] text-destructive" title={newFailHint}>
            new fail
          </span>
        )}
        {/* Primary AI Check lives on the Process strip (stage CTA). Header keeps a muted Re-check link. */}
        {onChecklistCheck != null && (
          <button
            type="button"
            className={cn(
              'text-[var(--text-dense-meta)] text-muted-foreground hover:text-primary hover:underline',
              (checklistCheckDisabled || checklistCheckPending) &&
                'cursor-not-allowed opacity-50 hover:no-underline hover:text-muted-foreground',
            )}
            disabled={checklistCheckDisabled || checklistCheckPending}
            title={
              checklistCheckTitle ??
              'Re-run daily-ops-checklist-run (same as strip AI Check — not Operator Plane Fix)'
            }
            onClick={onChecklistCheck}
          >
            {checkBusy
              ? proberHint != null && proberHint !== ''
                ? `Checking… · ${proberHint}`
                : 'Checking…'
              : 'Re-check'}
          </button>
        )}
        {checklistCheckError != null && checklistCheckError !== '' && (
          <span className="text-[8px] text-destructive" title={checklistCheckError}>
            Check failed
          </span>
        )}
        {attentionCount > 0 && (
          <Button
            type="button"
            variant="outline"
            size="xs"
            className="h-6 gap-1 px-2 text-[10px]"
            title="Copy Cursor IDE Agent failover pack for all non-ok checklist items (paste into Cursor chat)"
            onClick={onAskAiAll}
          >
            <Copy className="size-3" aria-hidden />
            {copyState === 'copied' && copiedItemId == null
              ? 'Copied!'
              : copyState === 'error'
                ? 'Copy failed'
                : `Ask for AI (${attentionCount})`}
          </Button>
        )}
        {copyState === 'copied' && copiedItemId == null && (
          <span className="text-[8px] text-muted-foreground">
            Next: paste into Cursor · then re-check strip
          </span>
        )}
        {checklistItemFixError != null && checklistItemFixError !== '' && (
          <span className="text-[8px] text-destructive" title={checklistItemFixError}>
            Item Fix failed
          </span>
        )}
        <Button
          type="button"
          variant="outline"
          size="xs"
          className={cn(
            'h-6 cursor-pointer gap-1 px-2 text-[10px]',
            failingOnly
              ? 'border-primary/50 bg-primary/5 text-foreground hover:bg-primary/10'
              : 'text-muted-foreground hover:text-foreground',
          )}
          title={failingOnly ? 'Click to show all checklist items' : 'Click to show failing items only'}
          aria-label={failingOnly ? 'Showing fails. Click to show all' : 'Show fails only'}
          onClick={onToggleFailingOnly}
        >
          <ListFilter className="size-3 shrink-0" aria-hidden />
          {failingOnly ? (
            <>
              <span className="font-normal text-muted-foreground">showing fails</span>
              <span className="text-muted-foreground" aria-hidden>
                ·
              </span>
              <span className="font-semibold text-primary underline underline-offset-2">Show all</span>
            </>
          ) : (
            <span>Failing only</span>
          )}
        </Button>
        {remediatingPhase && (
          <button
            type="button"
            className="text-[var(--text-dense-meta)] text-muted-foreground hover:text-primary hover:underline"
            onClick={onToggleShowMeta}
          >
            {showMeta ? 'Hide meta' : 'Show meta'}
          </button>
        )}
        {showMeta && (
          <span className="text-[var(--text-dense-caption)] text-muted-foreground">
            {compactColumns
              ? 'Split layout · Path = capability · Do = Ops Fix / Ask AI'
              : 'Two-column · Path = capability · Do icons = Ops Fix / Ask AI'}
          </span>
        )}
        {showMeta && coverage != null && (
          <span
            className="text-[var(--text-dense-caption)] text-muted-foreground"
            title="Coverage: Checklist↔Fleet Board match ratio (excludes path + checklist-only virtuals)"
          >
            {' · Coverage '}
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
              <span className="text-emerald-700/80 dark:text-emerald-300/80"> · union ok</span>
            )}
            <span className="text-muted-foreground">
              {' · dry-run '}
              {formatChecklistTouchAge(coverage.dryRunAt, nowMs)}
            </span>
          </span>
        )}
        {!showMeta && remediatingPhase && coverage != null && (
          <span className="text-[var(--text-dense-caption)] text-muted-foreground">
            ✓d {coverage.boardMatchedCount}/{coverage.boardTotalCount}
          </span>
        )}
        <button
          type="button"
          className="ml-auto text-[var(--text-dense-meta)] text-muted-foreground hover:text-primary hover:underline"
          onClick={onOpenFullOperatorPlane}
          title="Full Operator Plane (MCP, host deploy, self-smoke) — distinct from Checklist AI Check"
        >
          Full Operator Plane →
        </button>
      </div>

      {showMeta && (headerProgress.proberLabel != null || dispatchStrip != null) && (
        <div className="mb-2 flex flex-wrap items-center gap-x-3 gap-y-0.5 text-[var(--text-dense-caption)] text-muted-foreground">
          {headerProgress.proberLabel != null && (
            <span className="font-medium text-sky-700 dark:text-sky-300" title="daily-ops-checklist-run">
              {headerProgress.proberLabel}
            </span>
          )}
          {dispatchStrip != null && <DispatchHeaderStrip header={headerProgress} />}
        </div>
      )}
    </>
  )
}

/** Colored last AI Check dispatch strip — numbers use semantic tones; labels stay muted. */
function DispatchHeaderStrip({ header }: { header: ChecklistHeaderProgress }) {
  type Part = { key: string; count: number; label: string; countClass: string }
  const parts: Part[] = []
  if (header.remediating > 0) {
    parts.push({
      key: 'remediating',
      count: header.remediating,
      label: 'remediating',
      countClass: 'text-sky-700 dark:text-sky-300',
    })
  }
  if (
    (header.dispatchAuto > 0 && header.remediating === 0) ||
    (header.dispatchAuto > header.remediating && header.remediating > 0)
  ) {
    parts.push({
      key: 'auto',
      count: header.dispatchAuto,
      label: 'auto',
      countClass: 'text-sky-700 dark:text-sky-300',
    })
  }
  if (header.dispatchQueued > 0) {
    parts.push({
      key: 'queued',
      count: header.dispatchQueued,
      label: 'queued',
      countClass: 'text-violet-700 dark:text-violet-300',
    })
  }
  if (header.dispatchNotify > 0) {
    parts.push({
      key: 'notify',
      count: header.dispatchNotify,
      label: 'notify',
      countClass: 'text-amber-700 dark:text-amber-300',
    })
  }
  if (header.dispatchSkip > 0) {
    parts.push({
      key: 'skip',
      count: header.dispatchSkip,
      label: 'skip',
      countClass: 'text-foreground/70',
    })
  }
  if (header.done > 0) {
    parts.push({
      key: 'done',
      count: header.done,
      label: 'done',
      countClass: 'text-emerald-700 dark:text-emerald-300',
    })
  }
  if (header.failed > 0) {
    parts.push({
      key: 'failed',
      count: header.failed,
      label: 'failed',
      countClass: 'text-destructive',
    })
  }
  if (parts.length === 0) return null
  return (
    <span title="Gates from the most recent AI Check report_checklist_signals last_dispatch + linked remediation jobs">
      <span className="text-muted-foreground">Last AI Check dispatch:</span>
      {parts.map((p, i) => (
        <span key={p.key}>
          {i > 0 ? <span className="text-muted-foreground"> · </span> : ' '}
          <span className={p.countClass}>{p.count}</span>
          <span className="text-muted-foreground"> {p.label}</span>
        </span>
      ))}
    </span>
  )
}
