import { useMemo, useState } from 'react'
import { useQuery } from '@tanstack/react-query'
import { ChevronDown, ChevronRight } from 'lucide-react'
import { DenseTag, EmptyState, type DenseTagVariant } from '@bifrost/ui'
import { fetchDevAgentProgramJobs } from '@/api/devAgent'
import type { DevAgentJob } from '@/api/devAgentTypes'
import { devAgentProgramJobsQueryKey } from '@/hooks/useDevAgentMutations'
import { OpsSection } from '@/components/layout/OpsSection'
import {
  collectTraceJobs,
  computeAgentExecutionKpis,
  formatDurationMs,
  formatRate,
  groupJobsByPhase,
  jobDurationMs,
} from '@/lib/delivery/agentExecutionTrace'

const EMPTY_TITLE = 'No agent runs recorded for this program.'

function jobStatusVariant(status: DevAgentJob['status']): DenseTagVariant {
  if (status === 'done') return 'success'
  if (status === 'failed') return 'danger'
  if (status === 'cancelled') return 'neutral'
  if (status === 'running' || status === 'awaiting_review') return 'warning'
  return 'neutral'
}

function truncateSummary(summary: string | undefined, max = 120): string {
  const text = (summary ?? '').trim()
  if (text === '') return '—'
  return text.length > max ? `${text.slice(0, max)}…` : text
}

function AgentJobRow({ job }: { job: DevAgentJob }) {
  const [open, setOpen] = useState(false)
  const hasOutput = job.output.trim() !== ''
  const stamp = job.completed_at?.trim() || (job.status === 'running' ? 'in flight' : '—')
  const duration = jobDurationMs(job)
  const approvedBy = job.approved_by?.trim() ?? ''

  return (
    <li className="border-b border-border/50 last:border-b-0">
      <button
        type="button"
        className="flex w-full items-start gap-2 px-0 py-1.5 text-left disabled:cursor-default"
        onClick={() => hasOutput && setOpen(v => !v)}
        disabled={!hasOutput}
        aria-expanded={hasOutput ? open : undefined}
      >
        {hasOutput ? (
          open ? (
            <ChevronDown className="mt-0.5 h-3.5 w-3.5 shrink-0 text-muted-foreground" />
          ) : (
            <ChevronRight className="mt-0.5 h-3.5 w-3.5 shrink-0 text-muted-foreground" />
          )
        ) : (
          <span className="mt-0.5 w-3.5 shrink-0" />
        )}
        <DenseTag variant={jobStatusVariant(job.status)}>{job.status}</DenseTag>
        <span className="shrink-0 font-mono text-dense-meta text-muted-foreground">{stamp}</span>
        {duration != null && (
          <span className="shrink-0 font-mono text-dense-meta text-muted-foreground tabular-nums">
            {formatDurationMs(duration)}
          </span>
        )}
        {approvedBy !== '' && (
          <span className="shrink-0 text-dense-meta text-muted-foreground">{approvedBy}</span>
        )}
        <span className="min-w-0 flex-1 truncate text-dense-meta">{truncateSummary(job.summary)}</span>
      </button>
      {open && hasOutput && (
        <pre className="dense-scroll-y mb-2 mt-0 max-h-40 overflow-y-auto whitespace-pre-wrap rounded-md bg-secondary px-2 py-1.5 font-mono text-dense-caption text-muted-foreground">
          {job.output}
        </pre>
      )}
    </li>
  )
}

export function ProgramAgentExecutionSection({ programId }: { programId: string }) {
  const jobsQuery = useQuery({
    queryKey: devAgentProgramJobsQueryKey(programId),
    queryFn: () => fetchDevAgentProgramJobs(programId),
    enabled: programId !== '',
    staleTime: 10_000,
    refetchInterval: q => (q.state.data != null ? 15_000 : false),
  })

  const jobs = useMemo(
    () => (jobsQuery.data != null ? collectTraceJobs(jobsQuery.data) : []),
    [jobsQuery.data],
  )
  const kpis = useMemo(() => computeAgentExecutionKpis(jobs), [jobs])
  const groups = useMemo(() => groupJobsByPhase(jobs), [jobs])

  const showEmpty =
    jobsQuery.isError || (jobsQuery.isSuccess && jobs.length === 0)

  return (
    <OpsSection
      title="Agent Execution"
      description="Per-program agent job history. Agent job status is not Owner sign-off."
    >
      {jobsQuery.isLoading && (
        <p className="m-0 text-dense-meta text-muted-foreground">Loading agent trace…</p>
      )}

      {showEmpty && !jobsQuery.isLoading && (
        <EmptyState title={EMPTY_TITLE} className="py-6" />
      )}

      {jobs.length > 0 && (
        <div className="flex flex-col gap-3">
          <div className="flex flex-wrap items-baseline gap-x-4 gap-y-1 font-mono text-dense-meta tabular-nums">
            <span>
              total_runs <span className="text-foreground">{kpis.totalRuns}</span>
            </span>
            <span>
              first_pass{' '}
              <span className="text-foreground">
                {kpis.firstPassRate == null ? '—' : formatRate(kpis.firstPassRate)}
              </span>
            </span>
            <span>
              avg_duration{' '}
              <span className="text-foreground">
                {kpis.avgDurationMs == null ? '—' : formatDurationMs(kpis.avgDurationMs)}
              </span>
            </span>
          </div>
          <p className="m-0 text-dense-caption text-muted-foreground">
            {kpis.firstPassHeuristic === 'per-phase-first'
              ? 'first_pass = first terminal job per phase is done (excludes in-flight / cancelled).'
              : 'first_pass heuristic = done / (done + failed).'}
            {kpis.avgDurationMs != null ? ' avg_duration uses started_at + completed_at.' : ''}
          </p>
          {groups.map(group => (
            <div key={group.phaseId}>
              <p className="m-0 mb-1 font-mono text-dense-caption text-muted-foreground">
                phase {group.phaseId}
              </p>
              <ul className="m-0 list-none p-0">
                {group.jobs.map(job => (
                  <AgentJobRow key={job.id} job={job} />
                ))}
              </ul>
            </div>
          ))}
        </div>
      )}
    </OpsSection>
  )
}
