import { DenseTag, Popover, PopoverContent, PopoverTrigger } from '@bifrost/ui'
import { ChevronDown } from 'lucide-react'
import type { LoopArchetype, TaskModeId } from '@/lib/task-mode/types'
import { useTaskMode } from '@/lib/task-mode/TaskModeContext'
import { taskModeVisual } from '@/lib/task-mode/taskModeVisual'
import { TaskModePickerContent } from '@/components/task-mode/TaskModePickerContent'

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

type TaskModeActiveBannerProps = {
  onModeChange?: (landingTab: string, modeId: TaskModeId) => void
}

/** Always-visible mode identity strip — accent rail + icon + label. */
export function TaskModeActiveBanner({ onModeChange }: TaskModeActiveBannerProps) {
  const { modeId, mode, setModeId } = useTaskMode()
  const visual = taskModeVisual(modeId)
  const Icon = visual.icon

  const pick = (next: TaskModeId) => {
    setModeId(next)
    onModeChange?.(next === 'system' ? 'control-room' : 'task-cc', next)
  }

  return (
    <div
      className="task-mode-active-banner flex flex-wrap items-center gap-2 border-b border-border px-3 py-1.5"
      role="status"
      aria-label={`Current task mode: ${mode.label}`}
    >
      <Icon size={16} className="task-mode-active-banner__icon shrink-0" aria-hidden />
      <span className="text-[var(--text-dense-label)] font-semibold tracking-tight">{mode.label}</span>
      <DenseTag variant={LOOP_VARIANT[mode.loopArchetype]} className="text-[9px]">
        {LOOP_LABEL[mode.loopArchetype]}
      </DenseTag>
      {mode.loopArchetype === 'system' ? (
        <span className="text-[var(--text-dense-caption)] text-muted-foreground">Full navigation</span>
      ) : (
        <span className="text-[var(--text-dense-caption)] text-muted-foreground hidden sm:inline">
          Focused lens active
        </span>
      )}
      <div className="flex-1" />
      <Popover>
        <PopoverTrigger asChild>
          <button
            type="button"
            className="inline-flex items-center gap-1 rounded-md border border-border bg-card/80 px-2 py-0.5 text-[var(--text-dense-caption)] font-medium text-foreground hover:bg-secondary"
          >
            Change mode
            <ChevronDown size={12} className="text-muted-foreground" />
          </button>
        </PopoverTrigger>
        <PopoverContent align="end" className="w-80 p-2">
          <TaskModePickerContent activeId={modeId} onPick={pick} />
        </PopoverContent>
      </Popover>
    </div>
  )
}
