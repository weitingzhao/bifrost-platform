import { cn, DenseTag } from '@bifrost/ui'
import { useQuery } from '@tanstack/react-query'
import { fetchPipelineRunSteps } from '@/api/delivery'
import type { PipelinePhaseView } from '@/api/deliveryTypes'
import { phaseStatusVariant } from '@/lib/delivery/deliverStgPhases'

function phasesTerminal(phases: PipelinePhaseView[]): boolean {
  if (phases.length === 0) return false
  return phases.every(p => p.status === 'succeeded' || p.status === 'failed')
}

function PhaseChip({
  phase,
  active,
  selected,
  onSelect,
}: {
  phase: PipelinePhaseView
  active: boolean
  selected: boolean
  onSelect: () => void
}) {
  const status = phase.status as 'pending' | 'running' | 'succeeded' | 'failed'
  const isRunning = status === 'running'
  return (
    <button
      type="button"
      onClick={onSelect}
      title={`Show logs for ${phase.label}`}
      className={cn(
        'flex min-w-0 flex-col items-center gap-1 rounded-md p-1 text-left transition-colors',
        'hover:bg-secondary/60 focus-visible:outline-none focus-visible:ring-1 focus-visible:ring-ring',
        selected && 'ring-1 ring-primary bg-primary/10',
        !selected && active && isRunning && 'ring-1 ring-primary/50 bg-primary/5',
        !selected && active && !isRunning && 'ring-1 ring-[var(--warning)] bg-[var(--secondary)]/50',
      )}
    >
      <DenseTag
        variant={phaseStatusVariant(status)}
        className={cn(
          'w-full justify-center font-mono-tabular',
          isRunning && (selected || active) && 'release-cc__running-phase',
        )}
      >
        {phase.label}
      </DenseTag>
      <span className="text-[var(--text-dense-caption)] text-[var(--muted-foreground)] tabular-nums">
        {phase.detail ?? status}
      </span>
    </button>
  )
}

interface DeliveryPipelineStepProgressProps {
  runName: string | undefined
  namespace?: string
  /** Poll every 3s until all phases reach succeeded/failed. */
  pollUntilTerminal: boolean
  /** When the PipelineRun is already terminal, suppress misleading "in progress" labels. */
  runTerminal?: 'succeeded' | 'failed'
  /** PipelineRun still active — show live phase animation. */
  runRunning?: boolean
  /** Phase whose logs are shown in Log Tail (null = all). */
  selectedPhaseId?: string | null
  onSelectPhase?: (phaseId: string | null) => void
}

export function DeliveryPipelineStepProgress({
  runName,
  namespace,
  pollUntilTerminal,
  runTerminal,
  runRunning = false,
  selectedPhaseId = null,
  onSelectPhase,
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
        {onSelectPhase != null && (
          <button
            type="button"
            className={cn(
              'normal-case font-normal underline-offset-2 hover:underline',
              selectedPhaseId == null ? 'text-foreground' : 'text-muted-foreground',
            )}
            onClick={() => onSelectPhase(null)}
            title="Show full log tail"
          >
            All logs
          </button>
        )}
        {pollUntilTerminal && runRunning && !phasesTerminal(phases) && (
          <span className="normal-case inline-flex items-center gap-1 text-primary">
            <span className="release-cc__running-dot scale-75" aria-hidden />
            live · 3s
          </span>
        )}
        {pollUntilTerminal && runTerminal == null && !runRunning && !phasesTerminal(phases) && (
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
          <PhaseChip
            key={phase.id}
            phase={phase}
            active={idx === currentIdx && (phase.status === 'running' || phase.status === 'failed')}
            selected={selectedPhaseId === phase.id}
            onSelect={() => {
              if (onSelectPhase == null) return
              onSelectPhase(selectedPhaseId === phase.id ? null : phase.id)
            }}
          />
        ))}
      </div>
      {stepsQuery.isFetching && !stepsQuery.isLoading && (
        <span className="sr-only">Refreshing pipeline phases…</span>
      )}
      {stepsQuery.error instanceof Error && (
        <p className="m-0 mt-2 text-[var(--text-dense-caption)] text-[var(--destructive)]">
          {stepsQuery.error.message}
        </p>
      )}
    </div>
  )
}
