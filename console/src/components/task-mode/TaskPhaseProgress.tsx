import { useEffect, useMemo, useState } from 'react'
import { Button, cn, DenseTag } from '@bifrost/ui'
import { Check, ChevronRight } from 'lucide-react'
import type { TaskPhaseDef, TaskPhaseStatus } from '@/lib/task-mode/types'
import type { TaskPhaseFixAction, TaskPhaseHint } from '@/lib/task-mode/taskPhaseDiagnostics'

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

const STEP_CIRCLE: Record<TaskPhaseStatus, string> = {
  done: 'border-2 border-success/40 text-success bg-success/10',
  active:
    'border-2 border-primary bg-primary text-primary-foreground shadow-[0_0_0_3px_color-mix(in_oklab,var(--primary)_15%,transparent)]',
  blocked: 'border-2 border-destructive text-destructive bg-destructive/10',
  planned: 'border-2 border-border text-muted-foreground/50 bg-transparent',
  unknown: 'border-2 border-border text-muted-foreground/40 bg-transparent',
}

const STEP_STATUS_TEXT: Record<TaskPhaseStatus, string> = {
  done: 'text-success/80',
  active: 'text-primary font-medium',
  blocked: 'text-destructive',
  planned: 'text-muted-foreground/50',
  unknown: 'text-muted-foreground/50',
}

function defaultSelectedIndex(
  phases: TaskPhaseDef[],
  statuses: Record<string, TaskPhaseStatus>,
): number {
  if (phases.length === 0) return 0
  const activeIdx = phases.findIndex(p => statuses[p.id] === 'active')
  if (activeIdx >= 0) return activeIdx
  const blockedIdx = phases.findIndex(p => statuses[p.id] === 'blocked')
  if (blockedIdx >= 0) return blockedIdx
  const incompleteIdx = phases.findIndex(p => statuses[p.id] !== 'done')
  if (incompleteIdx >= 0) return incompleteIdx
  return phases.length - 1
}

export type TaskPhaseProgressProps = {
  phases: TaskPhaseDef[]
  statuses: Record<string, TaskPhaseStatus>
  /** Phase selected by an external task-level affordance, when present. */
  selectedPhaseId?: string
  /** Per-step operator hints (block reason, root causes, fix actions). */
  hints?: Record<string, TaskPhaseHint>
  /** Secondary — open full Console tab for deep work on this phase. */
  onOpenFullPage?: (phase: TaskPhaseDef) => void
  onFixAction?: (action: TaskPhaseFixAction, phase: TaskPhaseDef) => void
}

export function TaskPhaseProgress({
  phases,
  statuses,
  selectedPhaseId,
  hints,
  onOpenFullPage,
  onFixAction,
}: TaskPhaseProgressProps) {
  const phaseKey = useMemo(() => phases.map(p => p.id).join(','), [phases])
  const statusKey = useMemo(
    () => phases.map(p => `${p.id}:${statuses[p.id] ?? 'unknown'}`).join(','),
    [phases, statuses],
  )

  const [selectedIndex, setSelectedIndex] = useState(() =>
    defaultSelectedIndex(phases, statuses),
  )

  useEffect(() => {
    setSelectedIndex(defaultSelectedIndex(phases, statuses))
  }, [phaseKey, statusKey, phases, statuses])

  useEffect(() => {
    if (selectedPhaseId == null) return
    const nextIndex = phases.findIndex(phase => phase.id === selectedPhaseId)
    if (nextIndex >= 0) setSelectedIndex(nextIndex)
  }, [phases, selectedPhaseId])

  if (phases.length === 0) return null

  const selectedPhase = phases[selectedIndex] ?? phases[0]
  const selectedStatus = statuses[selectedPhase.id] ?? 'unknown'
  const selectedHint = hints?.[selectedPhase.id]

  const dependsOnLabels =
    selectedPhase.dependsOn?.map(
      depId => phases.find(p => p.id === depId)?.title ?? depId,
    ) ?? []

  return (
    <div className="overflow-hidden rounded-md border border-border/60">
      <nav
        className="dense-scroll-x flex items-stretch gap-0 overflow-x-auto px-1 py-2"
        aria-label="Task phase progress"
      >
        {phases.map((phase, i) => {
          const status = statuses[phase.id] ?? 'unknown'
          const isSelected = i === selectedIndex
          const connectorDone = i > 0 && statuses[phases[i - 1].id] === 'done'

          return (
            <div key={phase.id} className="flex min-w-0 flex-1 items-center">
              {i > 0 && (
                <div
                  className={cn(
                    'mx-1 h-px min-w-[0.75rem] flex-1 shrink-0',
                    connectorDone ? 'bg-success/30' : 'bg-border',
                  )}
                  aria-hidden
                />
              )}
              <button
                type="button"
                onClick={() => setSelectedIndex(i)}
                aria-current={isSelected ? 'step' : undefined}
                className={cn(
                  'group flex min-w-[5.5rem] max-w-[9rem] flex-1 flex-col items-center gap-1 rounded-md px-1.5 py-1 transition-colors',
                  isSelected ? 'bg-primary/8 ring-1 ring-primary/25' : 'hover:bg-secondary/60',
                )}
              >
                <span
                  className={cn(
                    'flex h-6 w-6 shrink-0 items-center justify-center rounded-full text-[10px] font-bold leading-none',
                    STEP_CIRCLE[status],
                  )}
                >
                  {status === 'done' ? <Check className="h-3 w-3" aria-hidden /> : phase.seq}
                </span>
                <span
                  className={cn(
                    'w-full truncate text-center text-[var(--text-dense-caption)] leading-tight',
                    isSelected ? 'font-semibold text-foreground' : 'text-muted-foreground group-hover:text-foreground',
                  )}
                  title={phase.title}
                >
                  {phase.title}
                </span>
                <span className={cn('text-[9px] leading-none', STEP_STATUS_TEXT[status])}>
                  {STATUS_LABEL[status]}
                </span>
              </button>
            </div>
          )
        })}
      </nav>

      <div className="border-t border-border/60 bg-secondary/25 px-3 py-2.5">
        <div className="flex flex-wrap items-center gap-2">
          <span className="text-[var(--text-dense-label)] font-semibold">{selectedPhase.title}</span>
          <DenseTag variant={STATUS_VARIANT[selectedStatus]} className="text-[9px]">
            {STATUS_LABEL[selectedStatus]}
          </DenseTag>
          <span className="text-[var(--text-dense-caption)] text-muted-foreground">
            Step {selectedPhase.seq} of {phases.length}
          </span>
        </div>
        <p className="m-0 mt-1.5 text-[var(--text-dense-meta)] text-muted-foreground">
          {selectedPhase.summary}
        </p>
        {selectedHint != null && (
          <div
            className={cn(
              'mt-2 rounded-md border px-2.5 py-2',
              selectedStatus === 'blocked'
                ? 'border-destructive/35 bg-destructive/5'
                : 'border-[color-mix(in_srgb,var(--color-lamp-yellow)_35%,var(--border))] bg-[color-mix(in_srgb,var(--color-lamp-yellow)_6%,transparent)]',
            )}
          >
            <p
              className={cn(
                'm-0 text-[var(--text-dense-meta)] font-medium',
                selectedStatus === 'blocked'
                  ? 'text-destructive'
                  : 'text-[var(--color-lamp-yellow)]',
              )}
            >
              {selectedHint.reason}
            </p>
            {selectedHint.rootCauses.length > 0 && (
              <ul className="m-0 mt-1.5 list-none space-y-0.5 p-0">
                {selectedHint.rootCauses.slice(0, 5).map(line => (
                  <li key={line} className="text-[var(--text-dense-caption)] text-muted-foreground">
                    · {line}
                  </li>
                ))}
              </ul>
            )}
            {selectedHint.fixActions.length > 0 && (
              <div className="mt-2 flex flex-wrap gap-1.5">
                {selectedHint.fixActions.map(action => (
                  <Button
                    key={action.label}
                    variant={action.kind === 'agent-fix' ? 'default' : 'outline'}
                    size="xs"
                    className="h-7 text-[var(--text-dense-caption)]"
                    onClick={() => onFixAction?.(action, selectedPhase)}
                  >
                    {action.label}
                  </Button>
                ))}
              </div>
            )}
          </div>
        )}
        {dependsOnLabels.length > 0 && (
          <p className="m-0 mt-1 text-[var(--text-dense-caption)] text-muted-foreground">
            Depends on: {dependsOnLabels.join(' → ')}
          </p>
        )}
        {(selectedPhase.actions?.length ?? 0) > 0 && (
          <div className="mt-2 flex flex-wrap gap-1.5">
            {selectedPhase.actions!.map(action => (
              <Button
                key={action.label}
                variant="outline"
                size="xs"
                className="h-7 text-[var(--text-dense-caption)]"
                onClick={() => {
                  if (action.tabId != null) {
                    onOpenFullPage?.({ ...selectedPhase, navigateTab: action.tabId })
                  }
                }}
                disabled={action.tabId == null && action.externalHref == null}
              >
                {action.label}
                <ChevronRight size={12} className="ml-0.5" />
              </Button>
            ))}
          </div>
        )}
        {selectedPhase.navigateTab != null && onOpenFullPage != null && (
          <button
            type="button"
            className="mt-2 inline-flex items-center gap-0.5 text-[var(--text-dense-meta)] text-primary hover:underline"
            onClick={() => onOpenFullPage(selectedPhase)}
          >
            Open full page in Console
            <ChevronRight size={12} />
          </button>
        )}
      </div>
    </div>
  )
}
