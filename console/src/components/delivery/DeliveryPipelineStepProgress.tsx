import { DenseTag } from '@bifrost/ui'
import { useQuery } from '@tanstack/react-query'
import { fetchPipelineRunSteps } from '@/api/platform'
import type { PipelinePhaseView } from '@/api/types'
import { phaseStatusVariant } from '@/lib/delivery/deliverStgPhases'

function phasesTerminal(phases: PipelinePhaseView[]): boolean {
  if (phases.length === 0) return false
  return phases.every(p => p.status === 'succeeded' || p.status === 'failed')
}

function PhaseChip({ phase, active }: { phase: PipelinePhaseView; active: boolean }) {
  const status = phase.status as 'pending' | 'running' | 'succeeded' | 'failed'
  return (
    <div
      className={`flex min-w-0 flex-col items-center gap-1 rounded-md p-1 ${active ? 'ring-1 ring-[var(--warning)] bg-[var(--secondary)]/50' : ''}`}
    >
      <DenseTag variant={phaseStatusVariant(status)} className="w-full justify-center font-mono-tabular">
        {phase.label}
      </DenseTag>
      <span className="text-[var(--text-dense-caption)] text-[var(--muted-foreground)] tabular-nums">
        {phase.detail ?? status}
      </span>
    </div>
  )
}

interface DeliveryPipelineStepProgressProps {
  runName: string | undefined
  namespace?: string
  /** Poll every 3s until all phases reach succeeded/failed. */
  pollUntilTerminal: boolean
  /** When the PipelineRun is already terminal, suppress misleading "in progress" labels. */
  runTerminal?: 'succeeded' | 'failed'
}

export function DeliveryPipelineStepProgress({
  runName,
  namespace,
  pollUntilTerminal,
  runTerminal,
}: DeliveryPipelineStepProgressProps) {
  const stepsQuery = useQuery({
    queryKey: ['delivery', 'steps', runName, namespace],
    queryFn: () => fetchPipelineRunSteps(runName!, namespace),
    enabled: runName != null && runName !== '',
    staleTime: 0,
    refetchIntervalInBackground: true,
    refetchInterval: query => {
      if (!pollUntilTerminal) return false
      const phases = query.state.data?.phases ?? []
      if (phasesTerminal(phases)) return false
      return 3_000
    },
  })

  const phases = stepsQuery.data?.phases ?? []
  const taskCount = stepsQuery.data?.tasks?.length ?? 0

  if (runName == null || runName === '') return null

  if (stepsQuery.isLoading && phases.length === 0) {
    return (
      <p className="m-0 mb-3 text-[var(--text-dense-meta)] text-[var(--muted-foreground)]">Loading pipeline steps…</p>
    )
  }

  if (phases.length === 0) {
    return (
      <p className="m-0 mb-3 text-[var(--text-dense-meta)] text-[var(--muted-foreground)]">
        Pipeline steps pending — TaskRuns not scheduled yet.
      </p>
    )
  }

  const activeIdx = phases.findIndex(p => p.status === 'running' || p.status === 'failed')
  const currentIdx =
    activeIdx >= 0 ? activeIdx : phases.findIndex(p => p.status === 'pending')

  const updatedAt = stepsQuery.dataUpdatedAt
    ? new Date(stepsQuery.dataUpdatedAt).toLocaleTimeString()
    : null

  return (
    <div className="mb-3">
      <p className="m-0 mb-2 flex flex-wrap items-center gap-x-2 gap-y-1 text-[var(--text-dense-caption)] font-medium uppercase tracking-wide text-[var(--muted-foreground)]">
        <span>Pipeline phases</span>
        {pollUntilTerminal && runTerminal == null && !phasesTerminal(phases) && (
          <span className="normal-case text-[var(--warning)]">· updating every 3s</span>
        )}
        {runTerminal === 'succeeded' && (
          <span className="normal-case text-[var(--success)]">· completed</span>
        )}
        {runTerminal === 'failed' && (
          <span className="normal-case text-[var(--destructive)]">· failed</span>
        )}
        {runTerminal == null && currentIdx >= 0 && phases[currentIdx]?.status === 'running' && (
          <span className="normal-case">· {phases[currentIdx].label} in progress</span>
        )}
        {updatedAt != null && (
          <span className="normal-case font-normal">· {taskCount} tasks · refreshed {updatedAt}</span>
        )}
      </p>
      <div className="grid grid-cols-3 gap-2 sm:grid-cols-6">
        {phases.map((phase, idx) => (
          <PhaseChip key={phase.id} phase={phase} active={idx === currentIdx && phase.status === 'running'} />
        ))}
      </div>
      {stepsQuery.isFetching && !stepsQuery.isLoading && (
        <p className="m-0 mt-1 text-[var(--text-dense-caption)] text-[var(--muted-foreground)]">Refreshing…</p>
      )}
      {stepsQuery.error instanceof Error && (
        <p className="m-0 mt-2 text-[var(--text-dense-caption)] text-[var(--destructive)]">
          {stepsQuery.error.message}
        </p>
      )}
    </div>
  )
}
