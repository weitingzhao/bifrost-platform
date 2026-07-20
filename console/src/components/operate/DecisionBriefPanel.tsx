import { useEffect, useState } from 'react'
import { Button, ConfirmDialog, DenseTag, cn } from '@bifrost/ui'
import type { BriefDecision, DecisionBrief } from '@/api/operateBriefs'
import { useDecideOnBrief, usePendingDecisionBriefs, isHeldDecisionBrief } from '@/hooks/useDecisionBriefs'
import { usePlatformAuth } from '@/hooks/usePlatformAuth'
import { OpsFeedback } from '@/components/feedback/OpsFeedback'

function suggestionVariant(
  suggestion: string,
): 'success' | 'warning' | 'danger' | 'neutral' | 'category' {
  const s = suggestion.toUpperCase()
  if (s === 'RUN') return 'success'
  if (s === 'DISMISS') return 'neutral'
  if (s === 'HOLD') return 'warning'
  return 'category'
}

function fleetSignalVariant(
  signal: string,
): 'success' | 'warning' | 'danger' | 'neutral' {
  const s = signal.toUpperCase()
  if (s === 'GO') return 'success'
  if (s === 'NO-GO' || s === 'NOGO') return 'danger'
  if (s === 'HOLD') return 'warning'
  return 'neutral'
}

type PendingAction = {
  brief: DecisionBrief
  decision: BriefDecision
}

export function DecisionBriefPanel({
  focusBriefId,
  onFocusBriefConsumed,
}: {
  focusBriefId?: string | null
  onFocusBriefConsumed?: () => void
} = {}) {
  const { canOperate } = usePlatformAuth()
  const briefsQuery = usePendingDecisionBriefs()
  const decideMutation = useDecideOnBrief()
  const [pendingAction, setPendingAction] = useState<PendingAction | null>(null)
  const [copiedId, setCopiedId] = useState<string | null>(null)
  const [copyError, setCopyError] = useState<string | null>(null)

  const pending = briefsQuery.pending
  const [highlightedId, setHighlightedId] = useState<string | null>(null)

  useEffect(() => {
    if (focusBriefId == null || focusBriefId === '') return
    const id = focusBriefId
    if (!pending.some(b => b.id === id)) {
      onFocusBriefConsumed?.()
      return
    }
    setHighlightedId(id)
    const frame = window.requestAnimationFrame(() => {
      const el = document.querySelector(`[data-decision-brief-id="${CSS.escape(id)}"]`)
      if (el instanceof HTMLElement) {
        el.scrollIntoView({ behavior: 'smooth', block: 'nearest' })
      }
    })
    onFocusBriefConsumed?.()
    return () => window.cancelAnimationFrame(frame)
  }, [focusBriefId, pending, onFocusBriefConsumed])

  useEffect(() => {
    if (highlightedId == null) return
    const clearHighlight = window.setTimeout(() => setHighlightedId(null), 4000)
    return () => window.clearTimeout(clearHighlight)
  }, [highlightedId])

  async function handleCopy(brief: DecisionBrief) {
    setCopyError(null)
    try {
      await navigator.clipboard.writeText(brief.full_brief || brief.title)
      setCopiedId(brief.id)
      window.setTimeout(() => setCopiedId(prev => (prev === brief.id ? null : prev)), 2000)
    } catch {
      setCopyError('Could not copy brief to clipboard')
    }
  }

  function confirmLabel(decision: BriefDecision): string {
    if (decision === 'approved_run') return 'Approve RUN'
    if (decision === 'dismissed') return 'Dismiss'
    return 'Hold'
  }

  function confirmTitle(decision: BriefDecision): string {
    if (decision === 'approved_run') return 'Approve RUN'
    if (decision === 'dismissed') return 'Dismiss decision brief'
    return 'Hold decision brief'
  }

  function confirmMessage(action: PendingAction): string {
    const title = action.brief.title
    const held = isHeldDecisionBrief(action.brief)
    if (action.decision === 'approved_run') {
      return held
        ? `Release hold and Approve RUN for “${title}”? Platform will start remediation for the linked operate-queue item.`
        : `Approve RUN for “${title}”? Platform will start remediation for the linked operate-queue item.`
    }
    if (action.decision === 'dismissed') {
      return held
        ? `Release hold and Dismiss “${title}”? The linked operate-queue item will be closed as resolved/stale.`
        : `Dismiss “${title}”? The linked operate-queue item will be closed as resolved/stale.`
    }
    return held
      ? `Extend Hold for “${title}”? Sweep will keep skipping this item until the new hold window expires.`
      : `Hold “${title}”? Next sweep will skip this item until the hold expires or you decide again.`
  }

  if (briefsQuery.isLoading) {
    return (
      <div className="rounded-lg border border-[var(--border)] bg-[var(--background)] px-3 py-2">
        <p className="m-0 text-dense-meta text-[var(--muted-foreground)]">
          Loading decision briefs…
        </p>
      </div>
    )
  }

  if (briefsQuery.isError) {
    return (
      <div className="rounded-lg border border-[var(--border)] bg-[var(--background)] px-3 py-2">
        <OpsFeedback variant="error" title="Decision briefs unavailable">
          {(briefsQuery.error as Error).message}
        </OpsFeedback>
      </div>
    )
  }

  if (pending.length === 0) {
    return null
  }

  return (
    <div className="rounded-lg border border-[var(--border)] bg-[var(--background)] px-3 py-2">
      <p className="m-0 text-dense-label font-medium">
        Decision briefs ({pending.length} pending)
      </p>
      <p className="m-0 mt-0.5 text-dense-meta text-[var(--muted-foreground)]">
        Owner decisions for ambiguous or high-risk queue items. Approve RUN starts remediation;
        Dismiss closes the handoff; Hold skips until expiry (held briefs stay visible so you can
        re-decide).
      </p>

      {decideMutation.isSuccess && (
        <OpsFeedback variant="success" title="Decision recorded" className="mt-2">
          Briefs and operate queue refreshed from platform-api.
        </OpsFeedback>
      )}
      {decideMutation.isError && (
        <OpsFeedback variant="error" title="Decision failed" className="mt-2">
          {(decideMutation.error as Error).message}
        </OpsFeedback>
      )}
      {copyError != null && (
        <OpsFeedback variant="error" title="Copy failed" className="mt-2">
          {copyError}
        </OpsFeedback>
      )}

      <ul className="m-0 mt-2 flex list-none flex-col gap-2 p-0">
        {pending.map(brief => (
          <li
            key={brief.id}
            data-decision-brief-id={brief.id}
            className={cn(
              'flex flex-wrap items-start justify-between gap-2 rounded border px-2 py-1.5',
              highlightedId === brief.id
                ? 'border-amber-500/60 bg-amber-500/10 ring-1 ring-amber-500/35'
                : 'border-[var(--border)]',
            )}
          >
            <div className="min-w-0 flex-1">
              <div className="flex flex-wrap items-center gap-1.5">
                <span className="text-dense-label font-medium">{brief.title}</span>
                {isHeldDecisionBrief(brief) && (
                  <DenseTag variant="warning">
                    Held
                    {brief.hold_until
                      ? ` until ${new Date(brief.hold_until).toLocaleString()}`
                      : ''}
                  </DenseTag>
                )}
                <DenseTag variant={fleetSignalVariant(brief.fleet_signal)}>
                  {brief.fleet_signal || 'unknown'}
                </DenseTag>
                <DenseTag
                  variant={
                    brief.risk_level === 'high'
                      ? 'danger'
                      : brief.risk_level === 'medium'
                        ? 'warning'
                        : 'neutral'
                  }
                >
                  {brief.risk_level || 'low'} risk
                </DenseTag>
                <DenseTag variant={suggestionVariant(brief.suggestion)}>
                  Suggest: {brief.suggestion}
                </DenseTag>
              </div>
              <p className="m-0 mt-0.5 text-dense-meta text-[var(--muted-foreground)]">
                Age {brief.item_age || '—'}
                {brief.fix_scope ? ` · Scope: ${brief.fix_scope}` : ' · Scope: none'}
              </p>
              {brief.fleet_detail !== '' && (
                <p className="m-0 mt-0.5 text-dense-caption text-[var(--muted-foreground)]">
                  {brief.fleet_detail}
                </p>
              )}
              {brief.suggestion_reason !== '' && (
                <p className="m-0 mt-0.5 text-dense-caption text-[var(--muted-foreground)]">
                  {brief.suggestion_reason}
                </p>
              )}
              {brief.open_question != null && brief.open_question !== '' && (
                <p className="m-0 mt-0.5 text-dense-caption text-[var(--muted-foreground)]">
                  <span className="font-medium">Open question:</span> {brief.open_question}
                </p>
              )}
            </div>
            <div className="flex flex-wrap items-center justify-end gap-1.5">
              <Button
                type="button"
                size="sm"
                variant="outline"
                disabled={!canOperate || decideMutation.isPending}
                title={
                  canOperate
                    ? 'Approve RUN — start remediation for the linked queue item'
                    : 'Operator authentication required'
                }
                onClick={() => setPendingAction({ brief, decision: 'approved_run' })}
              >
                Approve RUN
              </Button>
              <Button
                type="button"
                size="sm"
                variant="ghost"
                disabled={!canOperate || decideMutation.isPending}
                title={
                  canOperate
                    ? 'Dismiss — close the linked handoff as stale/resolved'
                    : 'Operator authentication required'
                }
                onClick={() => setPendingAction({ brief, decision: 'dismissed' })}
              >
                Dismiss
              </Button>
              <Button
                type="button"
                size="sm"
                variant="ghost"
                disabled={!canOperate || decideMutation.isPending}
                title={
                  canOperate
                    ? 'Hold — skip this item on the next sweep'
                    : 'Operator authentication required'
                }
                onClick={() => setPendingAction({ brief, decision: 'hold' })}
              >
                {isHeldDecisionBrief(brief) ? 'Extend Hold' : 'Hold'}
              </Button>
              <Button
                type="button"
                size="sm"
                variant="ghost"
                title="Copy full brief markdown for IDE Agent"
                onClick={() => void handleCopy(brief)}
              >
                {copiedId === brief.id ? 'Copied' : 'Copy'}
              </Button>
            </div>
          </li>
        ))}
      </ul>

      <ConfirmDialog
        open={pendingAction != null}
        title={pendingAction != null ? confirmTitle(pendingAction.decision) : ''}
        message={pendingAction != null ? confirmMessage(pendingAction) : ''}
        confirmLabel={pendingAction != null ? confirmLabel(pendingAction.decision) : 'Confirm'}
        confirming={decideMutation.isPending}
        onConfirm={() => {
          if (pendingAction == null) return
          decideMutation.mutate(
            { id: pendingAction.brief.id, decision: pendingAction.decision },
            { onSuccess: () => setPendingAction(null) },
          )
        }}
        onCancel={() => setPendingAction(null)}
      />
    </div>
  )
}
