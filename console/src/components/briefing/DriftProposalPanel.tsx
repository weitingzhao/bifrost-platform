import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { Button, ConfirmDialog, DenseTag } from '@bifrost/ui'
import { useMemo, useState } from 'react'
import type { DriftProposal, RemediationJob } from '@/api/types'
import {
  approveDriftProposal,
  fetchAgentNightlyReport,
  fetchDriftProposals,
  fetchRemediationJobs,
  rejectDriftProposal,
} from '@/api/platform'
import { OpsFeedback } from '@/components/feedback/OpsFeedback'
import { usePlatformAuth } from '@/hooks/usePlatformAuth'

interface DriftProposalPanelProps {
  onOpenAgentDesk?: (jobId?: string) => void
}

function pickLatestDriftTask(jobs: RemediationJob[]): RemediationJob | null {
  const sorted = [...jobs].sort((a, b) => Date.parse(b.created_at) - Date.parse(a.created_at))
  for (const job of sorted) {
    const scope = job.scope ?? ''
    if (scope.startsWith('nightly-')) return job
  }
  return sorted[0] ?? null
}

function extractJobIdFromNightlyReport(content: string): string | null {
  for (const marker of ['## Agent remediation', '## Cluster verification']) {
    const blockStart = content.indexOf(marker)
    if (blockStart < 0) continue
    const slice = content.slice(blockStart)
    const match = slice.match(/"id"\s*:\s*"([0-9a-f-]{36})"/i)
    if (match?.[1] != null) return match[1]
  }
  return null
}

function extractProposalApiFromNightlyReport(content: string): string | null {
  const blockStart = content.indexOf('## Layer 4')
  if (blockStart < 0) return null
  const slice = content.slice(blockStart, blockStart + 4000)
  const match = slice.match(/"platform_api"\s*:\s*"([^"]+)"/)
  return match?.[1] ?? null
}

function statusVariant(status: DriftProposal['status']): 'success' | 'warning' | 'neutral' | 'danger' {
  switch (status) {
    case 'pending_approval':
      return 'warning'
    case 'running':
      return 'neutral'
    case 'done':
      return 'success'
    case 'rejected':
      return 'neutral'
    case 'failed':
      return 'danger'
    default:
      return 'neutral'
  }
}

export function DriftProposalPanel({ onOpenAgentDesk }: DriftProposalPanelProps) {
  const qc = useQueryClient()
  const { canOperate } = usePlatformAuth()
  const [rejectOpen, setRejectOpen] = useState(false)
  const [rejectTarget, setRejectTarget] = useState<DriftProposal | null>(null)
  const [approveOpen, setApproveOpen] = useState(false)
  const [approveTarget, setApproveTarget] = useState<DriftProposal | null>(null)

  const proposalsQuery = useQuery({
    queryKey: ['agent', 'drift-proposals'],
    queryFn: fetchDriftProposals,
    refetchInterval: 15_000,
  })

  const nightlyQuery = useQuery({
    queryKey: ['agent', 'nightly-report'],
    queryFn: fetchAgentNightlyReport,
    staleTime: 60_000,
  })

  const proposals = proposalsQuery.data?.proposals ?? []
  const pending = proposals.filter(p => p.status === 'pending_approval')

  const jobsQuery = useQuery({
    queryKey: ['remediation', 'jobs'],
    queryFn: fetchRemediationJobs,
    staleTime: 15_000,
    refetchInterval:
      pending.length > 0 ||
      proposals.some(p => p.status === 'running' || p.status === 'approved')
        ? 5_000
        : 30_000,
  })

  const latestDriftTask = useMemo(() => {
    const fromJobs = pickLatestDriftTask(jobsQuery.data?.jobs ?? [])
    if (fromJobs != null) return fromJobs
    const fromReport = nightlyQuery.data?.content
    if (fromReport != null) {
      const id = extractJobIdFromNightlyReport(fromReport)
      if (id != null) return { id, scope: 'nightly-health-check', status: 'running' } as RemediationJob
    }
    return null
  }, [jobsQuery.data?.jobs, nightlyQuery.data?.content])

  const activeFixProposal = proposals.find(
    p => p.status === 'running' || (p.status === 'approved' && p.remediation_job_id != null),
  )
  const activeFixJob =
    activeFixProposal?.remediation_job_id != null
      ? (jobsQuery.data?.jobs ?? []).find(j => j.id === activeFixProposal.remediation_job_id)
      : null

  const nightlyLayer4Hint = useMemo(() => {
    const content = nightlyQuery.data?.content ?? ''
    const l4Start = content.indexOf('## Layer 4')
    const l4End = content.indexOf('## Runner health', l4Start)
    const l4Block =
      l4Start >= 0 ? content.slice(l4Start, l4End > l4Start ? l4End : l4Start + 8000) : ''
    if (l4Block.includes('POST failed')) {
      return 'post_failed'
    }
    if (content.includes('No drift — skipping Layer 4 proposal')) {
      return 'no_drift'
    }
    if (content.includes('SKIP proposal POST')) {
      return 'post_skipped'
    }
    if (content.includes('Owner approval: Ops Console')) {
      return 'posted'
    }
    return null
  }, [nightlyQuery.data?.content])

  const proposalTargetApi = useMemo(
    () => extractProposalApiFromNightlyReport(nightlyQuery.data?.content ?? ''),
    [nightlyQuery.data?.content],
  )

  const approveMutation = useMutation({
    mutationFn: approveDriftProposal,
    onSuccess: data => {
      void qc.invalidateQueries({ queryKey: ['agent', 'drift-proposals'] })
      void qc.invalidateQueries({ queryKey: ['remediation', 'jobs'] })
      void qc.invalidateQueries({ queryKey: ['platform', 'audit'] })
      setApproveOpen(false)
      setApproveTarget(null)
      if (onOpenAgentDesk != null && data.remediation_job?.id != null) {
        onOpenAgentDesk(data.remediation_job.id)
      }
    },
  })

  const rejectMutation = useMutation({
    mutationFn: ({ id, note }: { id: string; note?: string }) => rejectDriftProposal(id, note),
    onSuccess: () => {
      void qc.invalidateQueries({ queryKey: ['agent', 'drift-proposals'] })
      setRejectOpen(false)
      setRejectTarget(null)
    },
  })

  return (
    <section id="drift-proposals" className="page-section panel-elevated px-4 py-3 mt-3">
      <div className="flex flex-wrap items-start justify-between gap-3">
        <div className="min-w-0 flex-1">
          <p className="briefing-section-kicker m-0">Layer 4 · Owner gate</p>
          <h2 className="m-0 mt-1 text-sm font-semibold">Drift auto-fix proposals</h2>
          <p className="m-0 mt-1 text-[var(--text-dense-meta)] text-[var(--muted-foreground)]">
            Nightly scan creates a proposal when Layer 1–3 fail. Approve to start an agent auto-fix
            branch/PR — no fixes run without Owner action.
          </p>
        </div>
        <div className="flex flex-wrap items-center gap-2 shrink-0">
          {onOpenAgentDesk != null && (
            <>
              <Button
                type="button"
                variant="outline"
                size="sm"
                disabled={latestDriftTask == null}
                onClick={() => onOpenAgentDesk(latestDriftTask?.id)}
              >
                View latest agent task
              </Button>
              <Button type="button" variant="outline" size="sm" onClick={() => onOpenAgentDesk()}>
                Agent Desk
              </Button>
            </>
          )}
          <Button
            type="button"
            variant="outline"
            size="sm"
            onClick={() => {
              void qc.invalidateQueries({ queryKey: ['agent', 'drift-proposals'] })
              void qc.invalidateQueries({ queryKey: ['remediation', 'jobs'] })
              void qc.invalidateQueries({ queryKey: ['agent', 'nightly-report'] })
            }}
          >
            Refresh
          </Button>
        </div>
      </div>

      {activeFixProposal != null && (
        <div className="mt-3 flex flex-wrap items-center gap-2 rounded border border-[var(--border)] bg-[var(--background)] px-3 py-2">
          <DenseTag variant={statusVariant(activeFixProposal.status)}>
            L4 fix · {activeFixProposal.status}
          </DenseTag>
          <span className="text-[var(--text-dense-meta)]">
            {activeFixProposal.layers_failed.join(', ')} · {activeFixProposal.findings_count} findings
          </span>
          {activeFixJob != null && (
            <span className="font-mono-tabular text-[var(--text-dense-caption)] text-[var(--muted-foreground)]">
              job {activeFixJob.id.slice(0, 8)} · {activeFixJob.status}
            </span>
          )}
          {onOpenAgentDesk != null && activeFixProposal.remediation_job_id != null && (
            <Button
              type="button"
              variant="ghost"
              size="sm"
              className="ml-auto h-auto px-0 py-0"
              onClick={() => onOpenAgentDesk(activeFixProposal.remediation_job_id)}
            >
              Track in Agent Desk →
            </Button>
          )}
        </div>
      )}

      {proposalsQuery.isLoading && (
        <p className="m-0 mt-3 text-[var(--text-dense-meta)] text-[var(--muted-foreground)]">
          Loading proposals…
        </p>
      )}
      {proposalsQuery.isError && (
        <OpsFeedback variant="error" title="Failed to load proposals" className="mt-3">
          {(proposalsQuery.error as Error).message}
        </OpsFeedback>
      )}

      {!proposalsQuery.isLoading && pending.length === 0 && (
        <div className="m-0 mt-3 text-[var(--text-dense-meta)] text-[var(--muted-foreground)]">
          <p className="m-0">No proposals awaiting approval.</p>
          {latestDriftTask != null && (
            <p className="m-0 mt-2 font-mono-tabular text-[var(--text-dense-caption)]">
              Latest agent task: {latestDriftTask.id.slice(0, 8)}
              {latestDriftTask.scope != null && ` · ${latestDriftTask.scope}`}
              {latestDriftTask.status != null && ` · ${latestDriftTask.status}`}
            </p>
          )}
          {nightlyLayer4Hint === 'no_drift' && (
            <p className="m-0 mt-2">
              Latest nightly scan: <strong className="text-[var(--foreground)]">Layer 1–3 passed</strong> — no
              drift, so Layer 4 did not create a proposal. Use{' '}
              <strong className="text-[var(--foreground)]">View latest agent task</strong> to open the
              remediation drawer (cluster verification / drift brief) with Copy report.
            </p>
          )}
          {nightlyLayer4Hint === 'post_skipped' && (
            <OpsFeedback variant="warning" title="Drift detected but proposal was not posted" className="mt-2">
              Check the nightly report Layer 4 section — usually{' '}
              <code className="font-mono-tabular">PLATFORM_OPERATOR_TOKEN</code> +{' '}
              <code className="font-mono-tabular">PLATFORM_API_URL</code> missing on the agent host that ran
              the scan.
            </OpsFeedback>
          )}
          {nightlyLayer4Hint === 'post_failed' && (
            <OpsFeedback variant="warning" title="Layer 4 proposal POST failed" className="mt-2">
              The scan detected drift but could not create a proposal on platform-api. Check agent host{' '}
              <code className="font-mono-tabular">PLATFORM_OPERATOR_TOKEN</code> and network reachability
              to the target API.
            </OpsFeedback>
          )}
          {nightlyLayer4Hint === 'posted' && (
            <OpsFeedback variant="warning" title="Proposal on a different platform-api" className="mt-2">
              Report shows a proposal was created on{' '}
              <code className="font-mono-tabular">{proposalTargetApi ?? 'remote platform-api'}</code>.
              This Console reads proposals from the local dev API (<code className="font-mono-tabular">:8780</code>
              ). Open the matching Ops Console (K3s ingress) to approve, or re-run scan with{' '}
              <code className="font-mono-tabular">PLATFORM_API_URL</code> pointing here.
            </OpsFeedback>
          )}
        </div>
      )}

      {pending.length > 0 && (
        <div className="mt-3 flex flex-col gap-3">
          {pending.map(p => (
            <div key={p.id} className="rounded-md border border-[var(--border)] bg-[var(--background)] px-3 py-2">
              <div className="flex flex-wrap items-center gap-2">
                <DenseTag variant={statusVariant(p.status)}>{p.status}</DenseTag>
                <span className="font-mono-tabular text-[var(--text-dense-meta)]">{p.id}</span>
                <span className="text-[var(--text-dense-caption)] text-[var(--muted-foreground)]">
                  layers: {p.layers_failed.join(', ')} · {p.findings_count} findings
                </span>
              </div>
              <pre className="llm-content-pre mt-2 max-h-40 overflow-auto font-mono-tabular text-[var(--text-dense-meta)]">
                {p.summary.slice(0, 4000)}
              </pre>
              {canOperate && (
                <div className="mt-2 flex flex-wrap gap-2">
                  <Button
                    type="button"
                    size="sm"
                    disabled={approveMutation.isPending}
                    onClick={() => {
                      setApproveTarget(p)
                      setApproveOpen(true)
                    }}
                  >
                    Approve auto-fix
                  </Button>
                  <Button
                    type="button"
                    variant="outline"
                    size="sm"
                    disabled={rejectMutation.isPending}
                    onClick={() => {
                      setRejectTarget(p)
                      setRejectOpen(true)
                    }}
                  >
                    Reject
                  </Button>
                </div>
              )}
            </div>
          ))}
        </div>
      )}

      {proposals.some(p => p.status !== 'pending_approval') && (
        <div className="mt-3">
          <p className="m-0 text-[var(--text-dense-label)] font-medium text-[var(--muted-foreground)]">
            Recent history
          </p>
          <ul className="m-0 mt-1 list-none pl-0 text-[var(--text-dense-meta)]">
            {proposals
              .filter(p => p.status !== 'pending_approval')
              .slice(0, 6)
              .map(p => (
                <li key={p.id} className="py-1">
                  <DenseTag variant={statusVariant(p.status)}>{p.status}</DenseTag>
                  <span className="ml-2 font-mono-tabular">{p.id.slice(0, 16)}</span>
                  {p.remediation_job_id != null && onOpenAgentDesk != null && (
                    <Button
                      type="button"
                      variant="ghost"
                      size="sm"
                      className="ml-2 h-auto py-0"
                      onClick={() => onOpenAgentDesk(p.remediation_job_id!)}
                    >
                      View task
                    </Button>
                  )}
                </li>
              ))}
          </ul>
        </div>
      )}

      <ConfirmDialog
        open={approveOpen}
        title="Approve drift auto-fix"
        message={
          approveTarget != null
            ? `Start agent auto-fix for ${approveTarget.layers_failed.join(', ')}? Creates a git branch and may open a PR. Cluster destructive actions are not allowed.`
            : ''
        }
        confirmLabel="Approve"
        confirming={approveMutation.isPending}
        onConfirm={() => {
          if (approveTarget != null) approveMutation.mutate(approveTarget.id)
        }}
        onCancel={() => {
          setApproveOpen(false)
          setApproveTarget(null)
        }}
      />

      <ConfirmDialog
        open={rejectOpen}
        title="Reject drift proposal"
        message={
          rejectTarget != null
            ? `Dismiss proposal ${rejectTarget.id}? No auto-fix will run.`
            : ''
        }
        confirmLabel="Reject"
        confirming={rejectMutation.isPending}
        onConfirm={() => {
          if (rejectTarget != null) rejectMutation.mutate({ id: rejectTarget.id })
        }}
        onCancel={() => {
          setRejectOpen(false)
          setRejectTarget(null)
        }}
      />
    </section>
  )
}
