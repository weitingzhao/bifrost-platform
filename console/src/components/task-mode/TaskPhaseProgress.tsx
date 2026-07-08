import { DenseTag } from '@bifrost/ui'
import { ChevronRight } from 'lucide-react'
import type { TaskPhaseDef, TaskPhaseStatus } from '@/lib/task-mode/types'

const STATUS_VARIANT: Record<
  TaskPhaseStatus,
  'success' | 'warning' | 'danger' | 'neutral' | 'info'
> = {
  done: 'success',
  active: 'warning',
  blocked: 'danger',
  planned: 'neutral',
  unknown: 'info',
}

const STATUS_LABEL: Record<TaskPhaseStatus, string> = {
  done: 'Done',
  active: 'Active',
  blocked: 'Blocked',
  planned: 'Planned',
  unknown: 'Unknown',
}

export type TaskPhaseProgressProps = {
  phases: TaskPhaseDef[]
  statuses: Record<string, TaskPhaseStatus>
  onNavigatePhase?: (phase: TaskPhaseDef) => void
}

export function TaskPhaseProgress({
  phases,
  statuses,
  onNavigatePhase,
}: TaskPhaseProgressProps) {
  if (phases.length === 0) return null

  return (
    <ol className="m-0 flex list-none flex-col gap-1.5 p-0" aria-label="Task phase progress">
      {phases.map(phase => {
        const status = statuses[phase.id] ?? 'unknown'
        const clickable = onNavigatePhase != null && phase.navigateTab != null
        return (
          <li
            key={phase.id}
            className="rounded-md border border-border bg-secondary px-3 py-2"
          >
            <div className="flex flex-wrap items-center gap-2">
              <span className="text-[var(--text-dense-caption)] font-mono tabular-nums text-muted-foreground">
                {phase.seq}
              </span>
              <span className="text-[var(--text-dense-label)] font-semibold">{phase.title}</span>
              <DenseTag variant={STATUS_VARIANT[status]}>{STATUS_LABEL[status]}</DenseTag>
              {clickable && (
                <button
                  type="button"
                  className="ml-auto inline-flex items-center gap-0.5 text-[var(--text-dense-meta)] text-primary hover:underline"
                  onClick={() => onNavigatePhase(phase)}
                >
                  Open
                  <ChevronRight size={12} />
                </button>
              )}
            </div>
            <p className="m-0 mt-1 text-[var(--text-dense-meta)] text-muted-foreground">
              {phase.summary}
            </p>
          </li>
        )
      })}
    </ol>
  )
}
