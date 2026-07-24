import type { ReactNode } from 'react'
import type { LoopArchetype, TaskModeDef } from '@/lib/task-mode/types'
import { taskModeVisual } from '@/lib/task-mode/taskModeVisual'
import { OpsVerdictStrip } from '@/components/layout/OpsVerdictStrip'

const LOOP_VARIANT: Record<LoopArchetype, 'neutral' | 'warning' | 'info'> = {
  system: 'neutral',
  ops: 'warning',
  dev: 'info',
}

const LOOP_LABEL: Record<LoopArchetype, string> = {
  system: 'System',
  ops: 'Ops loop',
  dev: 'Dev loop',
}

export type TaskCCVerdictProps = {
  mode: TaskModeDef
  /** Phase progress for modes with a playbook (e.g. 3/7 complete). */
  phaseCaption?: string | null
  /** One-line situational cause / hint. */
  summary?: string | null
  lamp?: 'ok' | 'degraded' | 'fail' | 'unknown'
  tagLabel?: string
  tagVariant?: 'success' | 'warning' | 'danger' | 'neutral' | 'info'
  actions?: ReactNode
  className?: string
}

/**
 * Unified Task Control Center verdict — always at top for every task mode.
 */
export function TaskCCVerdict({
  mode,
  phaseCaption,
  summary,
  lamp = 'unknown',
  tagLabel,
  tagVariant,
  actions,
  className,
}: TaskCCVerdictProps) {
  const visual = taskModeVisual(mode.id)
  const Icon = visual.icon
  const resolvedTag = tagLabel ?? LOOP_LABEL[mode.loopArchetype]
  const resolvedVariant = tagVariant ?? LOOP_VARIANT[mode.loopArchetype]

  return (
    <OpsVerdictStrip
      className={className}
      ariaLabel="Task control center verdict"
      title={`TASK VERDICT · ${mode.label.toUpperCase()}`}
      lamp={lamp}
      tagLabel={resolvedTag}
      tagVariant={resolvedVariant}
      leading={<Icon size={14} className="task-mode-capsule__icon shrink-0" aria-hidden />}
      summary={
        summary ??
        (mode.loopArchetype === 'system'
          ? 'Full navigation — pick a focused lens from the header Task Mode capsule.'
          : mode.description)
      }
      actions={actions}
      meta={
        phaseCaption != null && phaseCaption !== '' ? (
          <span className="font-mono-tabular">{phaseCaption}</span>
        ) : undefined
      }
    />
  )
}
