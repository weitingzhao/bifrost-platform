import { useState } from 'react'
import { useMutation, useQueryClient } from '@tanstack/react-query'
import { ConfirmDialog } from '@bifrost/ui'
import type { RemediationJob } from '@/api/types'
import { closeBriefingSession } from '@/api/platform'
import {
  clearBriefingActiveSession,
  loadBriefingActiveSession,
} from '@/lib/briefing/briefingActiveSession'

interface CloseBriefingSessionDialogProps {
  job: RemediationJob | null
  open: boolean
  onDone: () => void
  onCancel: () => void
}

function defaultOutcome(job: RemediationJob): 'done' | 'failed' | 'cancelled' {
  if (job.status === 'done') return 'done'
  if (job.status === 'cancelled') return 'cancelled'
  return 'failed'
}

function defaultSummary(job: RemediationJob): string {
  const brief = job.init_brief?.trim()
  if (brief != null && brief !== '') {
    const firstLine = brief.split('\n').find(l => l.trim() !== '') ?? ''
    return firstLine.slice(0, 240)
  }
  return `Agent Desk session ${job.id.slice(0, 8)} — ${job.status}`
}

export function CloseBriefingSessionDialog({
  job,
  open,
  onDone,
  onCancel,
}: CloseBriefingSessionDialogProps) {
  const qc = useQueryClient()
  const briefingSession = loadBriefingActiveSession()
  const [summary, setSummary] = useState('')
  const [spineNote, setSpineNote] = useState('')

  const mutation = useMutation({
    mutationFn: closeBriefingSession,
    onSuccess: () => {
      clearBriefingActiveSession()
      void qc.invalidateQueries({ queryKey: ['platform', 'audit'] })
      void qc.invalidateQueries({ queryKey: ['briefing', 'session-results'] })
      onDone()
    },
  })

  if (job == null) return null

  const outcome = defaultOutcome(job)
  const effectiveSummary = summary || defaultSummary(job)

  return (
    <ConfirmDialog
      open={open}
      title="Close briefing session"
      message="Records outcome to audit (briefing.session.close) and the session-results store."
      confirmLabel={mutation.isPending ? 'Recording…' : 'Close session'}
      confirming={mutation.isPending}
      bodyExtra={
        <div className="flex flex-col gap-3">
          <label className="flex flex-col gap-1">
            <span className="text-dense-meta text-muted-foreground">Outcome</span>
            <span className="font-mono-tabular text-sm">{outcome}</span>
          </label>
          <label className="flex flex-col gap-1">
            <span className="text-dense-meta text-muted-foreground">Summary</span>
            <textarea
              className="min-h-[72px] rounded-md border border-[var(--border)] bg-[var(--background)] px-2 py-1.5 text-sm"
              value={effectiveSummary}
              onChange={e => setSummary(e.target.value)}
            />
          </label>
          <label className="flex flex-col gap-1">
            <span className="text-dense-meta text-muted-foreground">Spine note (optional)</span>
            <input
              className="rounded-md border border-[var(--border)] bg-[var(--background)] px-2 py-1.5 text-sm"
              value={spineNote}
              placeholder="e.g. W3 verify complete — ready for Owner sign-off"
              onChange={e => setSpineNote(e.target.value)}
            />
          </label>
          {briefingSession != null && (
            <p className="m-0 text-[var(--text-dense-caption)] text-[var(--muted-foreground)]">
              Briefing context: {briefingSession.track} / {briefingSession.lane} · intent{' '}
              {briefingSession.intent}
            </p>
          )}
        </div>
      }
      onConfirm={() => {
        mutation.mutate({
          job_id: job.id,
          outcome,
          summary: effectiveSummary,
          track: briefingSession?.track,
          lane: briefingSession?.lane,
          intent: briefingSession?.intent,
          spine_note: spineNote || undefined,
          request_spine_update: spineNote.trim() !== '',
        })
      }}
      onCancel={onCancel}
    />
  )
}
