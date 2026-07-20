import { Button, ConfirmDialog, DenseTag, SegmentControl, cn } from '@bifrost/ui'
import { useEffect, useMemo, useState } from 'react'
import type { OperateQueueItem } from '@/api/operateQueueTypes'
import { useCloseOperateQueueItem } from '@/hooks/useCloseOperateQueueItem'
import { useDismissOperateQueueItem } from '@/hooks/useDismissOperateQueueItem'
import { usePlatformAuth } from '@/hooks/usePlatformAuth'
import { OpsFeedback } from '@/components/feedback/OpsFeedback'
import { effectiveOperateLane } from '@/lib/operate/handoff'
import { catalogTaskById } from '@/lib/agent/agentTaskCatalog'
import { fetchVerifyMissionSnapshot } from '@/api/platform'

interface OperateQueueHandoffPanelProps {
  items: OperateQueueItem[]
  loading?: boolean
  onOpenSource?: (item: OperateQueueItem) => void
  onPrepareAgent?: (item: OperateQueueItem) => void
  onStartAgent?: (item: OperateQueueItem) => void
  onObserveJob?: (jobId: string) => void
  onNavigateSetup?: () => void
  /** Deep-link focus from TCC / Control Room — scroll + highlight, then consume. */
  focusHandoffId?: string | null
  onFocusHandoffConsumed?: () => void
}

export function OperateQueueHandoffPanel({
  items,
  loading,
  onOpenSource,
  onPrepareAgent,
  onStartAgent,
  onObserveJob,
  onNavigateSetup,
  focusHandoffId,
  onFocusHandoffConsumed,
}: OperateQueueHandoffPanelProps) {
  const closeMutation = useCloseOperateQueueItem()
  const dismissMutation = useDismissOperateQueueItem()
  const { canOperate } = usePlatformAuth()
  const [preparedItemId, setPreparedItemId] = useState<string | null>(null)
  const [laneFilter, setLaneFilter] = useState('all')
  const [closeItem, setCloseItem] = useState<OperateQueueItem | null>(null)
  const [dismissItem, setDismissItem] = useState<OperateQueueItem | null>(null)
  const [evidence, setEvidence] = useState('')
  const [dismissEvidence, setDismissEvidence] = useState('')
  const [dismissReason, setDismissReason] = useState<'stale' | 'resolved' | 'other'>('stale')
  const [closeVerificationError, setCloseVerificationError] = useState<string | null>(null)
  const [checkingVerification, setCheckingVerification] = useState(false)
  const [highlightedId, setHighlightedId] = useState<string | null>(null)

  useEffect(() => {
    if (focusHandoffId == null || focusHandoffId === '') return
    const id = focusHandoffId
    const match = items.find(item => item.id === id)
    if (match != null) {
      const lane = effectiveOperateLane(match)
      setLaneFilter(prev => (prev === 'all' || prev === lane ? prev : 'all'))
    }
    setHighlightedId(id)
    const frame = window.requestAnimationFrame(() => {
      const el = document.querySelector(`[data-operate-handoff-id="${CSS.escape(id)}"]`)
      if (el instanceof HTMLElement) {
        el.scrollIntoView({ behavior: 'smooth', block: 'nearest' })
      }
    })
    onFocusHandoffConsumed?.()
    return () => window.cancelAnimationFrame(frame)
  }, [focusHandoffId, items, onFocusHandoffConsumed])

  useEffect(() => {
    if (highlightedId == null) return
    const clearHighlight = window.setTimeout(() => setHighlightedId(null), 4000)
    return () => window.clearTimeout(clearHighlight)
  }, [highlightedId])
  const lanes = useMemo(
    () => Array.from(new Set(items.map(effectiveOperateLane))),
    [items],
  )
  const visibleItems = laneFilter === 'all'
    ? items
    : items.filter(item => effectiveOperateLane(item) === laneFilter)

  async function handleVerifiedClose() {
    if (closeItem == null || evidence.trim() === '') return
    setCloseVerificationError(null)
    if (closeItem.execution_job_id != null) {
      setCheckingVerification(true)
      try {
        const snapshot = await fetchVerifyMissionSnapshot()
        if (!snapshot.post_fix_verification.passed) {
          setCloseVerificationError('Post-fix verification is not passing; keep this handoff open.')
          return
        }
      } catch (error) {
        setCloseVerificationError((error as Error).message)
        return
      } finally {
        setCheckingVerification(false)
      }
    }
    const normalizedEvidence = /^(schedule|skill|operator):/i.test(evidence.trim())
      ? evidence.trim()
      : `operator: ${evidence.trim()}`
    closeMutation.mutate({
      itemId: closeItem.id,
      body: {
        completion_evidence: [
          normalizedEvidence,
          ...(closeItem.execution_job_id != null ? ['post_fix_verification:passed'] : []),
        ],
        post_fix_verification_passed: closeItem.execution_job_id != null,
      },
    }, {
      onSuccess: () => {
        setCloseItem(null)
        setEvidence('')
      },
    })
  }

  async function handleDismiss() {
    if (dismissItem == null || dismissEvidence.trim() === '') return
    const normalizedEvidence = /^(schedule|skill|operator|dismiss):/i.test(dismissEvidence.trim())
      ? dismissEvidence.trim()
      : `operator: ${dismissEvidence.trim()}`
    dismissMutation.mutate(
      {
        itemId: dismissItem.id,
        body: {
          completion_evidence: [normalizedEvidence],
          reason: dismissReason,
        },
      },
      {
        onSuccess: () => {
          setDismissItem(null)
          setDismissEvidence('')
          setDismissReason('stale')
        },
      },
    )
  }
  if (loading) {
    return (
      <div className="rounded-lg border border-[var(--border)] bg-[var(--background)] px-3 py-2">
        <p className="m-0 text-dense-meta text-[var(--muted-foreground)]">Loading operate queue…</p>
      </div>
    )
  }

  if (items.length === 0) {
    return null
  }

  return (
    <div className="rounded-lg border border-[var(--border)] bg-[var(--background)] px-3 py-2">
      <p className="m-0 text-dense-label font-medium">Operate queue handoffs</p>
      <p className="m-0 mt-0.5 text-dense-meta text-[var(--muted-foreground)]">
        Owner-approved handoffs awaiting execution. Source context is read-only; closing a handoff
        updates the queue through platform-api.
      </p>
      <p className="m-0 mt-0.5 text-dense-caption text-[var(--muted-foreground)]">
        A catalog-validated Agent task can start directly; all other handoffs stay Prepare-only.
      </p>
      {lanes.length > 1 && (
        <div className="mt-2">
          <SegmentControl
            ariaLabel="Operate lane filter"
            value={laneFilter}
            onChange={setLaneFilter}
            options={[{ value: 'all', label: 'All lanes' }, ...lanes.map(lane => ({ value: lane, label: lane }))]}
            size="sm"
          />
        </div>
      )}
      {preparedItemId != null && (
        <OpsFeedback variant="success" title="Handoff prepared" className="mt-2">
          Review the prefilled Agent Desk composer, choose a scope, then start when runner guards are green.
        </OpsFeedback>
      )}
      {closeMutation.isSuccess && (
        <OpsFeedback variant="success" title="Handoff closed" className="mt-2">
          The operate queue was refreshed from platform-api.
        </OpsFeedback>
      )}
      {dismissMutation.isSuccess && (
        <OpsFeedback variant="success" title="Handoff dismissed" className="mt-2">
          Stale or resolved handoff closed with evidence (no post-fix gate).
        </OpsFeedback>
      )}
      {closeMutation.isError && (
        <OpsFeedback variant="error" title="Handoff could not be closed" className="mt-2">
          {(closeMutation.error as Error).message}
        </OpsFeedback>
      )}
      {dismissMutation.isError && (
        <OpsFeedback variant="error" title="Handoff could not be dismissed" className="mt-2">
          {(dismissMutation.error as Error).message}
        </OpsFeedback>
      )}
      {closeVerificationError != null && (
        <OpsFeedback variant="error" title="Verification is not complete" className="mt-2">
          {closeVerificationError}
        </OpsFeedback>
      )}
      <ul className="m-0 mt-2 flex list-none flex-col gap-2 p-0">
        {visibleItems.map(item => (
          <li
            key={item.id}
            data-operate-handoff-id={item.id}
            className={cn(
              'flex flex-wrap items-start justify-between gap-2 rounded border px-2 py-1.5',
              highlightedId === item.id
                ? 'border-amber-500/60 bg-amber-500/10 ring-1 ring-amber-500/35'
                : 'border-[var(--border)]',
            )}
          >
            <div className="min-w-0">
              <div className="flex flex-wrap items-center gap-1.5">
                <span className="text-dense-label font-medium">{item.title}</span>
                <DenseTag variant={item.status === 'closed' ? 'success' : 'warning'}>{item.status}</DenseTag>
                {(item.operate_lane ?? item.lane) && <DenseTag variant="category">{item.operate_lane ?? item.lane}</DenseTag>}
                <DenseTag variant={item.risk_level === 'high' ? 'danger' : item.risk_level === 'medium' ? 'warning' : 'neutral'}>
                  {item.risk_level ?? 'low'} risk
                </DenseTag>
              </div>
              {item.description && (
                <p className="m-0 mt-0.5 text-dense-meta text-[var(--muted-foreground)]">{item.description}</p>
              )}
              <p className="m-0 mt-0.5 text-dense-meta text-[var(--muted-foreground)]">
                {item.reason ?? item.description ?? 'No reason recorded'}
              </p>
              <p className="m-0 mt-0.5 font-mono text-dense-caption text-[var(--muted-foreground)]">
                {item.program_id}{item.source_lane_id ? ` · ${item.source_lane_id}` : ''} · {item.handoff_kind ?? 'one_off'}
              </p>
              <p className="m-0 mt-0.5 text-dense-caption text-[var(--muted-foreground)]">
                Agent task: {item.agent_task_id ?? 'not validated'}
                {item.owner ? ` · Owner: ${item.owner}` : ''}
                {item.due_at ? ` · Due: ${item.due_at}` : ''}
              </p>
              {(item.acceptance_criteria?.length ?? 0) > 0 && (
                <p className="m-0 mt-1 text-dense-caption"><span className="font-medium">Acceptance:</span> {item.acceptance_criteria?.join(' · ')}</p>
              )}
              {(item.verification_steps?.length ?? 0) > 0 && (
                <p className="m-0 mt-0.5 text-dense-caption"><span className="font-medium">Verify:</span> {item.verification_steps?.join(' · ')}</p>
              )}
              {item.handoff_kind === 'recurring_setup' && item.status === 'open' && (
                <p className="m-0 mt-0.5 text-dense-caption text-[var(--muted-foreground)]">
                  Skills &amp; Schedules are read-only here; navigate and prepare the setup, then close with schedule/skill or explicit operator evidence.
                </p>
              )}
              {(item.completion_evidence?.length ?? 0) > 0 && (
                <p className="m-0 mt-0.5 text-dense-caption text-[var(--muted-foreground)]">Evidence: {item.completion_evidence?.join(' · ')}</p>
              )}
            </div>
            <div className="flex flex-wrap items-center justify-end gap-1.5">
              {onOpenSource != null && (
                <Button type="button" size="sm" variant="ghost" onClick={() => onOpenSource(item)}>
                  Open source
                </Button>
              )}
              {item.status === 'open' && item.agent_task_id != null && catalogTaskById(item.agent_task_id) != null && onStartAgent != null ? (
                <Button type="button" size="sm" variant="outline" disabled={!canOperate} onClick={() => onStartAgent(item)}>
                  Start Agent
                </Button>
              ) : item.status === 'open' && onPrepareAgent != null && (
                <Button
                  type="button"
                  size="sm"
                  variant="outline"
                  disabled={!canOperate}
                  title={
                    canOperate
                      ? 'Prefill Agent Desk without guessing an execution scope'
                      : 'Operator authentication required'
                  }
                  onClick={() => {
                    onPrepareAgent(item)
                    setPreparedItemId(item.id)
                  }}
                >
                  Prepare Agent
                </Button>
              )}
              {item.handoff_kind === 'recurring_setup' && item.status === 'open' && onNavigateSetup != null && (
                <Button type="button" size="sm" variant="ghost" onClick={onNavigateSetup}>
                  Navigate setup
                </Button>
              )}
              {item.execution_job_id && onObserveJob != null && (
                <Button type="button" size="sm" variant="ghost" onClick={() => onObserveJob(item.execution_job_id!)}>
                  Observe task
                </Button>
              )}
              {canOperate && item.status === 'open' && (
                <Button
                  type="button"
                  size="sm"
                  variant="outline"
                  disabled={closeMutation.isPending || dismissMutation.isPending}
                  title="Close this handoff through the existing operate queue API after execution is complete"
                  onClick={() => setCloseItem(item)}
                >
                  Close handoff
                </Button>
              )}
              {canOperate && item.status === 'open' && (
                <Button
                  type="button"
                  size="sm"
                  variant="ghost"
                  disabled={closeMutation.isPending || dismissMutation.isPending}
                  title="Dismiss stale or already-resolved handoff with evidence (skips job/post-fix gates)"
                  onClick={() => setDismissItem(item)}
                >
                  Dismiss
                </Button>
              )}
            </div>
          </li>
        ))}
      </ul>
      <div className={closeItem == null ? 'hidden' : 'mt-2'}>
        <label className="text-dense-caption font-medium text-[var(--muted-foreground)]" htmlFor="handoff-evidence">
          Completion evidence
        </label>
        <textarea
          id="handoff-evidence"
          className="mt-1 min-h-16 w-full rounded border border-[var(--border)] bg-[var(--background)] px-2 py-1 text-dense-meta"
          placeholder={closeItem?.handoff_kind === 'recurring_setup' ? 'schedule: …, skill: …, or operator: setup verified…' : 'Describe verification evidence…'}
          value={evidence}
          onChange={event => setEvidence(event.target.value)}
        />
      </div>
      <div className={dismissItem == null ? 'hidden' : 'mt-2'}>
        <label className="text-dense-caption font-medium text-[var(--muted-foreground)]" htmlFor="dismiss-reason">
          Dismiss reason
        </label>
        <SegmentControl
          ariaLabel="Dismiss reason"
          className="mt-1"
          size="sm"
          value={dismissReason}
          onChange={v => setDismissReason(v as 'stale' | 'resolved' | 'other')}
          options={[
            { value: 'stale', label: 'Stale' },
            { value: 'resolved', label: 'Resolved' },
            { value: 'other', label: 'Other' },
          ]}
        />
        <label
          className="mt-2 block text-dense-caption font-medium text-[var(--muted-foreground)]"
          htmlFor="dismiss-evidence"
        >
          Dismiss evidence
        </label>
        <textarea
          id="dismiss-evidence"
          className="mt-1 min-h-16 w-full rounded border border-[var(--border)] bg-[var(--background)] px-2 py-1 text-dense-meta"
          placeholder="operator: fleet already clean / handoff superseded…"
          value={dismissEvidence}
          onChange={event => setDismissEvidence(event.target.value)}
        />
      </div>
      <ConfirmDialog
        open={closeItem != null}
        title="Close verified handoff"
        message="Close only after execution and every verification step is complete. The evidence is persisted with the queue item."
        confirmLabel="Close verified handoff"
        confirming={closeMutation.isPending || checkingVerification}
        onConfirm={() => void handleVerifiedClose()}
        onCancel={() => {
          setCloseItem(null)
          setEvidence('')
          setCloseVerificationError(null)
        }}
      />
      <ConfirmDialog
        open={dismissItem != null}
        title="Dismiss handoff"
        message="Use when the item is stale or already resolved outside the verified Close path. Requires evidence; does not require a finished execution job."
        confirmLabel="Dismiss handoff"
        confirming={dismissMutation.isPending}
        onConfirm={() => void handleDismiss()}
        onCancel={() => {
          setDismissItem(null)
          setDismissEvidence('')
          setDismissReason('stale')
        }}
      />
    </div>
  )
}
