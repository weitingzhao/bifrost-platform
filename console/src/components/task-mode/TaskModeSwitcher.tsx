import type { ReactNode } from 'react'
import {
  Button,
  DenseTag,
  Popover,
  PopoverContent,
  PopoverTrigger,
  cn,
} from '@bifrost/ui'
import { ChevronDown } from 'lucide-react'
import { TaskModePickerContent } from '@/components/task-mode/TaskModePickerContent'
import { taskModeVisual } from '@/lib/task-mode/taskModeVisual'
import type { LoopArchetype, TaskModeId } from '@/lib/task-mode/types'
import { useTaskMode } from '@/lib/task-mode/TaskModeContext'

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

type TaskModeSwitcherProps = {
  onModeChange?: (landingTab: string, modeId: TaskModeId) => void
  variant?: 'bar' | 'sidebar-icon'
}

export function TaskModeSwitcher({ onModeChange, variant = 'bar' }: TaskModeSwitcherProps) {
  const { modeId, mode, setModeId } = useTaskMode()
  const visual = taskModeVisual(modeId)
  const ActiveIcon = visual.icon

  const pick = (next: TaskModeId) => {
    setModeId(next)
    onModeChange?.(next === 'system' ? 'control-room' : 'task-cc', next)
  }

  const popover = (trigger: ReactNode, align: 'start' | 'end' = 'start') => (
    <Popover>
      <PopoverTrigger asChild>{trigger}</PopoverTrigger>
      <PopoverContent align={align} className="w-80 p-2">
        <TaskModePickerContent activeId={modeId} onPick={pick} />
      </PopoverContent>
    </Popover>
  )

  if (variant === 'sidebar-icon') {
    return popover(
      <Button
        type="button"
        variant="ghost"
        size="icon"
        className="task-mode-sidebar-icon-trigger h-7 w-7 shrink-0"
        aria-label={`Task mode: ${mode.label}`}
      >
        <ActiveIcon size={16} />
      </Button>,
      'end',
    )
  }

  return popover(
    <button
      type="button"
      className={cn(
        'task-mode-switcher-trigger--accent inline-flex max-w-full items-center gap-2 rounded-md border px-2 py-1 text-left transition-colors',
        mode.loopArchetype === 'system' && 'border-border bg-card hover:bg-secondary/60',
      )}
      aria-label={`Task mode: ${mode.label}`}
    >
      <ActiveIcon size={14} className="shrink-0" style={{ color: 'var(--task-mode-accent)' }} />
      <span className="min-w-0 truncate text-[var(--text-dense-meta)] font-semibold">{mode.label}</span>
      {mode.loopArchetype !== 'system' && (
        <DenseTag variant={LOOP_VARIANT[mode.loopArchetype]} className="hidden shrink-0 sm:inline-flex">
          {LOOP_LABEL[mode.loopArchetype]}
        </DenseTag>
      )}
      <ChevronDown size={14} className="shrink-0 text-muted-foreground" />
    </button>,
  )
}
