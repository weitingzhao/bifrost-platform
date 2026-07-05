import { cn } from '@bifrost/ui'
import { useQuery } from '@tanstack/react-query'
import { CheckCircle2, XCircle } from 'lucide-react'
import { fetchPipelineRunSteps } from '@/api/platform'
import type { DeliveryPipelineRunView, ReleaseGateResponse } from '@/api/types'
import {
  formatPipelineRunStatus,
  isPipelineRunFailed,
  isPipelineRunRunning,
  isPipelineRunSucceeded,
} from '@/lib/delivery/pipelineRunAskPack'

const PHASE_TEXT_CLASS: Record<string, string> = {
  succeeded: 'text-muted-foreground/50',
  running: 'text-primary font-medium',
  failed: 'text-destructive font-medium',
  pending: 'text-muted-foreground/30',
}

const DEPLOY_STATUS_CLASS: Record<string, string> = {
  running: 'text-primary',
  succeeded: 'text-muted-foreground',
  failed: 'text-destructive',
}

function InlinePhaseProgress({ run }: { run: DeliveryPipelineRunView }) {
  const running = isPipelineRunRunning(run)
  const stepsQuery = useQuery({
    queryKey: ['delivery', 'steps', run.name, run.namespace],
    queryFn: () => fetchPipelineRunSteps(run.name, run.namespace),
    staleTime: 0,
    refetchInterval: running ? 3_000 : false,
  })
  const phases = stepsQuery.data?.phases ?? []
  const taskCount = stepsQuery.data?.tasks?.length ?? 0
  if (phases.length === 0) {
    return stepsQuery.isLoading
      ? <span className="text-dense-caption text-muted-foreground/50">Loading phases…</span>
      : null
  }
  const succeeded = phases.filter(p => p.status === 'succeeded').length
  return (
    <div className="flex items-center gap-2 mt-1.5">
      <span className="text-dense-micro text-muted-foreground/50">
        {succeeded}/{phases.length} phases
        {taskCount > 0 && ` · ${taskCount} tasks`}
      </span>
      <span className="flex items-center gap-1 text-dense-micro font-mono">
        {phases.map((phase, i) => (
          <span key={phase.id} className="inline-flex items-center">
            {i > 0 && <span className="text-border mx-0.5">→</span>}
            <span
              className={cn(
                PHASE_TEXT_CLASS[phase.status] ?? 'text-muted-foreground/30',
                phase.status === 'running' && 'release-cc__running-phase',
              )}
            >
              {phase.label}
            </span>
          </span>
        ))}
      </span>
    </div>
  )
}

export function DeployStepSummary({ run }: { run: DeliveryPipelineRunView | undefined }) {
  if (run == null) {
    return (
      <div className="py-2 text-dense-caption text-muted-foreground/50">
        No runs yet — deploy to begin.
      </div>
    )
  }
  const running = isPipelineRunRunning(run)
  const ok = isPipelineRunSucceeded(run)
  const failed = isPipelineRunFailed(run)
  const statusText = formatPipelineRunStatus(run)
  const statusClass = failed
    ? DEPLOY_STATUS_CLASS.failed
    : running
      ? DEPLOY_STATUS_CLASS.running
      : ok
        ? DEPLOY_STATUS_CLASS.succeeded
        : 'text-muted-foreground'

  return (
    <div className="py-1.5">
      <div className="flex flex-wrap items-center gap-x-2.5 gap-y-1">
        <span className={cn('inline-flex items-center gap-1 text-dense-caption font-medium', statusClass)}>
          {ok && <CheckCircle2 className="h-3 w-3 text-success/50" />}
          {failed && <XCircle className="h-3 w-3" />}
          {running && <span className="release-cc__running-dot" aria-hidden />}
          {statusText}
        </span>
        {run.revision && (
          <span className="font-mono text-dense-micro text-muted-foreground/60">{run.revision}</span>
        )}
        <span className="font-mono text-dense-micro text-muted-foreground/40">{run.name}</span>
      </div>
      <div className="mt-0.5 text-dense-micro text-muted-foreground/40">
        {run.start_time != null && run.start_time !== ''
          ? `Started ${new Date(run.start_time).toLocaleString()}`
          : 'Start pending'}
        {run.completion_time != null && run.completion_time !== ''
          ? ` · Completed ${new Date(run.completion_time).toLocaleString()}`
          : running
            ? ' · Running'
            : ''}
      </div>
      <InlinePhaseProgress run={run} />
    </div>
  )
}

export function GateStepSummary({ gate }: { gate: ReleaseGateResponse | undefined }) {
  if (gate == null) {
    return (
      <div className="py-2 text-dense-caption text-muted-foreground/50">
        Gate not run yet.
      </div>
    )
  }
  const result = gate.result ?? ''
  const checks = gate.checks ?? []
  const passed = checks.filter(c => c.reachability === 'ok').length
  const blockers = gate.blockers ?? []
  const isPass = result === 'pass'
  const isFail = result === 'fail'

  return (
    <div className="py-1.5">
      <div className="flex flex-wrap items-center gap-x-2.5 gap-y-1">
        <span
          className={cn(
            'inline-flex items-center gap-1 text-dense-caption font-medium',
            isPass ? 'text-muted-foreground' : isFail ? 'text-destructive' : 'text-muted-foreground/50',
          )}
        >
          {isPass && <CheckCircle2 className="h-3 w-3 text-success/50" />}
          {isFail && <XCircle className="h-3 w-3" />}
          {isPass ? 'Passed' : isFail ? 'Failed' : 'Not run'}
        </span>
        {gate.revision && (
          <span className="font-mono text-dense-micro text-muted-foreground/60">{gate.revision}</span>
        )}
        {checks.length > 0 && (
          <span className="font-mono text-dense-micro text-muted-foreground/40">
            {passed}/{checks.length} checks
          </span>
        )}
      </div>
      {checks.length > 0 && (
        <div className="flex items-center gap-1 mt-1.5 text-dense-micro font-mono">
          {checks.map((c, i) => (
            <span key={c.id} className="inline-flex items-center">
              {i > 0 && <span className="text-border mx-0.5">·</span>}
              <span
                className={cn(
                  c.reachability === 'ok'
                    ? 'text-muted-foreground/40'
                    : c.reachability === 'fail'
                      ? 'text-destructive font-medium'
                      : 'text-muted-foreground/30',
                )}
              >
                {c.label}
              </span>
            </span>
          ))}
        </div>
      )}
      {blockers.length > 0 && (
        <div className="mt-1.5 text-dense-micro text-destructive/80">
          {blockers.slice(0, 2).join('; ')}
          {blockers.length > 2 && ` (+${blockers.length - 2} more)`}
        </div>
      )}
    </div>
  )
}
