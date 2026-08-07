import { Button, ConfirmDialog, DenseTag } from '@bifrost/ui'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { useState } from 'react'
import {
  approvePostCompletionItem,
  fetchPendingPostCompletion,
  fetchProgramDetail,
  PROGRAMS_BOARD_QUERY_KEY,
  recordNoPostCompletionHandoff,
  rejectPostCompletionItem,
  submitProgramPostCompletion,
} from '@/api/programs'
import { OPERATE_QUEUE_QUERY_KEY } from '@/api/operateQueue'
import { usePlatformAuth } from '@/hooks/usePlatformAuth'
import { useOperateQueue } from '@/hooks/useOperateQueue'
import type { PostCompletionItem } from '@/api/programsTypes'
import { deriveAssessmentLabel } from '@/lib/operate/handoff'

function DetailList({ label, values }: { label: string; values?: string[] }) {
  if (!values?.length) return null
  return (
    <div>
      <p className="m-0 text-dense-caption font-medium text-muted-foreground">{label}</p>
      <ul className="m-0 list-disc pl-4 text-dense-meta text-muted-foreground">
        {values.map(value => <li key={value}>{value}</li>)}
      </ul>
    </div>
  )
}

function ItemDetail({ item }: { item: PostCompletionItem }) {
  return (
    <div className="mt-1 grid gap-1.5 rounded border border-border/60 bg-background/60 p-2 sm:grid-cols-2">
      <p className="m-0 text-dense-meta"><span className="text-muted-foreground">Reason:</span> {item.reason ?? item.description ?? '—'}</p>
      <p className="m-0 text-dense-meta"><span className="text-muted-foreground">Operate lane:</span> {item.operate_lane ?? '—'}</p>
      <p className="m-0 text-dense-meta"><span className="text-muted-foreground">Agent task:</span> {item.agent_task_id ?? 'Prepare manually'}</p>
      <p className="m-0 text-dense-meta"><span className="text-muted-foreground">Risk:</span> {item.risk_level ?? 'low'} · {item.handoff_kind ?? 'one_off'}</p>
      <DetailList label="Acceptance criteria" values={item.acceptance_criteria} />
      <DetailList label="Verification steps" values={item.verification_steps} />
    </div>
  )
}

export function PostCompletionPendingPanel({
  programId,
  programIds,
  allowApprove = true,
  emphasize = false,
}: {
  programId?: string
  /** Filter global pending items to these program ids (Briefing Session lane handoff). */
  programIds?: string[]
  allowApprove?: boolean
  /** Highlight shell for Session ownership (e.g. governance handoff). */
  emphasize?: boolean
}) {
  const { canAdmin } = usePlatformAuth()
  const queryClient = useQueryClient()
  const [approveId, setApproveId] = useState<string | null>(null)
  const [rejectId, setRejectId] = useState<string | null>(null)
  const [decisionReason, setDecisionReason] = useState('')
  const [confirmNoHandoff, setConfirmNoHandoff] = useState(false)
  const [dismissedDraft, setDismissedDraft] = useState(false)
  const operateQueue = useOperateQueue()

  const pendingQuery = useQuery({
    queryKey: ['programs', 'post-completion', 'pending'],
    queryFn: fetchPendingPostCompletion,
    refetchInterval: 30_000,
    enabled: programId == null,
  })

  const detailQuery = useQuery({
    queryKey: ['programs', programId ?? ''],
    queryFn: () => fetchProgramDetail(programId!),
    enabled: programId != null,
  })

  const approveMutation = useMutation({
    mutationFn: (itemId: string) => approvePostCompletionItem(itemId),
    onSuccess: () => {
      void queryClient.invalidateQueries({ queryKey: ['programs', 'post-completion', 'pending'] })
      void queryClient.invalidateQueries({ queryKey: OPERATE_QUEUE_QUERY_KEY })
      if (programId) void queryClient.invalidateQueries({ queryKey: ['programs', programId] })
      void queryClient.invalidateQueries({ queryKey: PROGRAMS_BOARD_QUERY_KEY })
      setApproveId(null)
    },
  })
  const rejectMutation = useMutation({
    mutationFn: ({ itemId, reason }: { itemId: string; reason: string }) =>
      rejectPostCompletionItem(itemId, { reason }),
    onSuccess: () => {
      void queryClient.invalidateQueries({ queryKey: ['programs', 'post-completion', 'pending'] })
      if (programId) void queryClient.invalidateQueries({ queryKey: ['programs', programId] })
      setRejectId(null)
      setDecisionReason('')
    },
  })
  const noHandoffMutation = useMutation({
    mutationFn: (reason: string) => recordNoPostCompletionHandoff(programId!, { reason }),
    onSuccess: () => {
      void queryClient.invalidateQueries({ queryKey: ['programs', programId] })
      void queryClient.invalidateQueries({ queryKey: ['programs', 'post-completion', 'pending'] })
      setConfirmNoHandoff(false)
      setDecisionReason('')
    },
  })
  const acceptDraftMutation = useMutation({
    mutationFn: () =>
      submitProgramPostCompletion(programId!, {
        new_capabilities: detailQuery.data?.post_completion?.new_capabilities,
        new_risks: detailQuery.data?.post_completion?.new_risks,
        operate_queue_items: detailQuery.data?.post_completion?.suggested_items,
      }),
    onSuccess: () => {
      void queryClient.invalidateQueries({ queryKey: ['programs', programId] })
      void queryClient.invalidateQueries({ queryKey: ['programs', 'post-completion', 'pending'] })
    },
  })

  const globalItems = pendingQuery.data?.items ?? []
  const programItems = detailQuery.data?.pending_post_completion_items ?? []
  const idSet = programIds != null ? new Set(programIds) : null
  const items =
    programId != null
      ? programItems
      : globalItems.filter(i => {
          if (i.status !== 'pending_review') return false
          if (idSet != null) return idSet.has(i.program_id)
          return true
        })
  const detail = detailQuery.data
  const assessmentStatus = deriveAssessmentLabel(
    detail?.post_completion?.assessment_status,
    operateQueue.data,
    programId ?? '',
  )
  const suggestions = detail?.post_completion?.suggested_items ?? []

  if (items.length === 0 && programId == null) {
    return null
  }

  return (
    <div
      className={[
        'flex flex-col gap-2 rounded-md border px-3 py-2',
        emphasize
          ? 'border-[var(--color-lamp-yellow)]/45 bg-[color-mix(in_srgb,var(--color-lamp-yellow)_12%,transparent)]'
          : 'border-border bg-secondary/30',
      ].join(' ')}
    >
      <div className="flex flex-wrap items-center justify-between gap-2">
        <p className="text-dense-label font-medium m-0">Post-completion operational handoff</p>
        <DenseTag variant={assessmentStatus === 'NO HANDOFF' || assessmentStatus === 'CLOSED' ? 'success' : assessmentStatus === 'NOT ASSESSED' ? 'neutral' : 'warning'}>
          {assessmentStatus}
        </DenseTag>
      </div>
      <p className="text-dense-meta text-muted-foreground m-0">
        Program completion does not enter Operate automatically. Owner approval creates a structured Agent Desk handoff.
      </p>
      {detail?.post_completion?.no_handoff_reason && (
        <p className="m-0 text-dense-meta text-muted-foreground">No handoff: {detail.post_completion.no_handoff_reason}</p>
      )}
      <ul className="m-0 flex flex-col gap-2 p-0 list-none">
        {items.map(item => (
          <li key={item.id} className="rounded border border-border/60 bg-secondary/20 p-2">
            <div className="flex flex-wrap items-start justify-between gap-2">
              <div className="min-w-0 flex-1">
              <span className="font-medium">{item.title}</span>
              {item.description && (
                <p className="text-dense-meta text-muted-foreground m-0 mt-0.5">{item.description}</p>
              )}
              <DenseTag variant={item.status === 'approved' || item.status === 'closed' ? 'success' : item.status === 'rejected' ? 'neutral' : 'warning'} className="mt-1">
                {item.status.replace(/_/g, ' ')}
              </DenseTag>
              <ItemDetail item={item} />
              {item.decision_note && <p className="m-0 mt-1 text-dense-caption text-muted-foreground">{item.decision_note}</p>}
            </div>
              {canAdmin && allowApprove && item.status === 'pending_review' && (
                <div className="flex gap-1.5">
                  <Button type="button" size="sm" variant="outline" onClick={() => setRejectId(item.id)}>Reject</Button>
                  <Button type="button" size="sm" variant="outline" onClick={() => setApproveId(item.id)}>Approve handoff</Button>
                </div>
              )}
            </div>
          </li>
        ))}
      </ul>
      {programId != null && assessmentStatus === 'NOT ASSESSED' &&
        (suggestions.length > 0 || detail?.post_completion?.suggested_assessment === 'no_handoff') &&
        !dismissedDraft && (
        <div className="rounded border border-dashed border-border p-2">
          <p className="m-0 text-dense-label font-medium">Suggested draft — Owner decision required</p>
          <p className="m-0 mt-0.5 text-dense-meta text-muted-foreground">
            This deterministic suggestion is not approved and has not entered Agent Desk.
          </p>
          {detail?.post_completion?.suggested_assessment === 'no_handoff' && suggestions.length === 0 && (
            <p className="m-0 mt-2 text-dense-meta">
              Suggested: NO HANDOFF. Owner must still record an explicit reason below.
            </p>
          )}
          {suggestions.map(item => (
            <div key={item.id ?? item.title} className="mt-2">
              <p className="m-0 text-dense-label font-medium">{item.title}</p>
              <p className="m-0 text-dense-meta text-muted-foreground">{item.reason}</p>
            </div>
          ))}
          {canAdmin && allowApprove && (
            <div className="mt-2 flex gap-1.5">
              <Button size="sm" variant="ghost" onClick={() => setDismissedDraft(true)}>Reject suggestion</Button>
              {suggestions.length > 0 && (
                <Button size="sm" variant="outline" disabled={acceptDraftMutation.isPending} onClick={() => acceptDraftMutation.mutate()}>
                  Accept as pending review
                </Button>
              )}
            </div>
          )}
        </div>
      )}
      {programId != null && canAdmin && allowApprove && assessmentStatus !== 'NO HANDOFF' && assessmentStatus !== 'CLOSED' && (
        <div className="flex flex-col gap-2 border-t border-border/60 pt-3">
          <p className="m-0 text-dense-label font-medium">Close this program — Owner decision</p>
          <p className="m-0 text-dense-meta text-muted-foreground">
            All phases are signed. Record that no ongoing operational handoff is needed to close.
          </p>
          <label className="text-dense-caption font-medium text-muted-foreground" htmlFor={`no-handoff-${programId}`}>Reason</label>
          <textarea
            id={`no-handoff-${programId}`}
            className="min-h-16 rounded border border-border bg-background px-2 py-1.5 text-dense-meta"
            placeholder="e.g. All phases delivered. No ongoing operational responsibility."
            value={decisionReason}
            onChange={event => setDecisionReason(event.target.value)}
          />
          <Button size="sm" variant="default" className="self-start" disabled={decisionReason.trim() === ''} onClick={() => setConfirmNoHandoff(true)}>
            Record no handoff & close
          </Button>
        </div>
      )}

      <ConfirmDialog
        open={approveId != null}
        title="Approve post-completion item"
        message="Approve this structured handoff and send it to Agent Desk? It remains open until execution and verification evidence are complete."
        confirmLabel="Approve handoff"
        confirming={approveMutation.isPending}
        onConfirm={() => {
          if (approveId) approveMutation.mutate(approveId)
        }}
        onCancel={() => setApproveId(null)}
      />
      <ConfirmDialog
        open={rejectId != null}
        title="Reject operational handoff"
        message={decisionReason.trim() === '' ? 'Enter an Owner decision reason before rejecting this handoff.' : `Reject this proposed handoff? Reason: ${decisionReason}`}
        confirmLabel="Reject handoff"
        confirming={rejectMutation.isPending}
        onConfirm={() => {
          if (rejectId && decisionReason.trim() !== '') rejectMutation.mutate({ itemId: rejectId, reason: decisionReason.trim() })
        }}
        onCancel={() => setRejectId(null)}
      />
      <ConfirmDialog
        open={confirmNoHandoff}
        title="Record no operational handoff"
        message={`Record an explicit NO HANDOFF decision for this Program? Reason: ${decisionReason}`}
        confirmLabel="Record no handoff"
        confirming={noHandoffMutation.isPending}
        onConfirm={() => noHandoffMutation.mutate(decisionReason.trim())}
        onCancel={() => setConfirmNoHandoff(false)}
      />
    </div>
  )
}
