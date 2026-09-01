import { Popover, PopoverContent, PopoverTrigger, cn } from '@bifrost/ui'
import { ChevronDown } from 'lucide-react'
import type { LoopArchetype, TaskModeId } from '@/lib/task-mode/types'
import { useTaskMode } from '@/lib/task-mode/useTaskMode'
import { taskModeVisual } from '@/lib/task-mode/taskModeVisual'
import { TaskModePickerContent } from '@/components/task-mode/TaskModePickerContent'

const LOOP_TITLE: Record<LoopArchetype, string> = {
  system: 'System',
  ops: 'Ops loop',
  dev: 'Dev loop',
  analysis: 'Analysis',
}

export type TaskModeCapsuleProps = {
  onModeChange?: (landingTab: string, modeId: TaskModeId) => void
}

/**
 * Compact mode identity in ConsoleHeader — replaces the full-width TaskModeActiveBanner.
 * Same picker + accent tokens; zero dedicated chrome row.
 */
export function TaskModeCapsule({ onModeChange }: TaskModeCapsuleProps) {
  const { modeId, mode, setModeId } = useTaskMode()
  const visual = taskModeVisual(modeId)
  const Icon = visual.icon
  const focused = mode.loopArchetype !== 'system'

  const pick = (next: TaskModeId) => {
    setModeId(next)
    onModeChange?.(next === 'system' ? 'control-room' : 'task-cc', next)
  }

  return (
    <Popover>
      <PopoverTrigger asChild>
        <button
          type="button"
          className={cn(
            'task-mode-capsule inline-flex h-7 max-w-[14rem] shrink-0 items-center gap-1.5 rounded-md border px-2',
            'whitespace-nowrap text-[var(--text-dense-caption)] font-medium shadow-sm',
            'border-[color-mix(in_oklab,var(--task-mode-accent)_55%,var(--border))] bg-[color-mix(in_oklab,var(--task-mode-accent)_14%,var(--card))]',
            'hover:bg-[color-mix(in_oklab,var(--task-mode-accent)_18%,var(--card))]',
          )}
          aria-label={`Task mode: ${mode.label}. Change mode`}
          title={
            focused
              ? `${mode.label} · ${LOOP_TITLE[mode.loopArchetype]} · Focused lens`
              : `${mode.label} · Full navigation`
          }
        >
          <Icon size={14} className="task-mode-capsule__icon shrink-0" aria-hidden />
          <span className="min-w-0 truncate font-semibold text-[var(--task-mode-accent)]">
            {visual.shortLabel}
          </span>
          <ChevronDown size={12} className="shrink-0 text-muted-foreground" aria-hidden />
        </button>
      </PopoverTrigger>
      <PopoverContent align="end" className="w-80 p-2">
        <TaskModePickerContent activeId={modeId} onPick={pick} />
      </PopoverContent>
    </Popover>
  )
}
