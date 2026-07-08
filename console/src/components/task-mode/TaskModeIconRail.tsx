import { cn, Tooltip, TooltipContent, TooltipTrigger } from '@bifrost/ui'
import { taskModesForSwitcher } from '@/lib/task-mode/taskModeCatalog'
import { taskModeVisual } from '@/lib/task-mode/taskModeVisual'
import type { LoopArchetype, TaskModeId } from '@/lib/task-mode/types'
import { useTaskMode } from '@/lib/task-mode/TaskModeContext'

const LOOP_HINT: Record<LoopArchetype, string> = {
  system: 'Full navigation',
  ops: 'Ops playbook',
  dev: 'Build playbook',
}

type TaskModeIconRailProps = {
  collapsed?: boolean
  onModeChange?: (landingTab: string, modeId: TaskModeId) => void
}

export function TaskModeIconRail({ collapsed = false, onModeChange }: TaskModeIconRailProps) {
  const { modeId, setModeId } = useTaskMode()
  const allModes = taskModesForSwitcher()
  const groups = [
    allModes.filter(m => m.loopArchetype === 'system'),
    allModes.filter(m => m.loopArchetype === 'ops'),
    allModes.filter(m => m.loopArchetype === 'dev'),
  ].filter(g => g.length > 0)

  const pick = (next: TaskModeId) => {
    setModeId(next)
    onModeChange?.(next === 'system' ? 'control-room' : 'task-cc', next)
  }

  return (
    <div
      className={cn(
        'task-mode-icon-rail',
        collapsed ? 'flex flex-col items-center gap-1 py-2' : 'px-2 py-2',
      )}
      role="toolbar"
      aria-label="Task mode views"
    >
      {!collapsed && (
        <p className="mb-1.5 px-0.5 text-[var(--text-dense-caption)] font-medium uppercase tracking-wide text-muted-foreground">
          Views
        </p>
      )}
      <div
        className={cn(
          'flex items-center gap-1',
          collapsed ? 'flex-col' : 'flex-wrap justify-start',
        )}
      >
        {groups.map((modes, groupIndex) => (
          <div key={groupIndex} className={cn('flex items-center gap-0.5', collapsed && 'flex-col')}>
            {groupIndex > 0 && (
              <div
                className={cn(
                  'shrink-0 bg-sidebar-border/70',
                  collapsed ? 'my-0.5 h-px w-5' : 'mx-0.5 h-5 w-px',
                )}
                aria-hidden
              />
            )}
            {modes.map(mode => {
              const visual = taskModeVisual(mode.id)
              const Icon = visual.icon
              const active = mode.id === modeId
              return (
                <Tooltip key={mode.id}>
                  <TooltipTrigger asChild>
                    <button
                      type="button"
                      data-task-mode={mode.id}
                      data-active={active ? 'true' : undefined}
                      className={cn(
                        'task-mode-icon-rail__btn inline-flex shrink-0 items-center justify-center rounded-md transition-colors',
                        collapsed ? 'h-8 w-8' : 'h-8 w-8',
                      )}
                      aria-label={mode.label}
                      aria-pressed={active}
                      onClick={() => pick(mode.id)}
                    >
                      <Icon size={16} className="task-mode-icon-rail__icon" />
                    </button>
                  </TooltipTrigger>
                  <TooltipContent side={collapsed ? 'right' : 'bottom'} className="max-w-[14rem]">
                    <p className="m-0 text-[var(--text-dense-label)] font-semibold">{mode.label}</p>
                    <p className="m-0 mt-0.5 text-[var(--text-dense-caption)] text-muted-foreground">
                      {LOOP_HINT[mode.loopArchetype]}
                    </p>
                  </TooltipContent>
                </Tooltip>
              )
            })}
          </div>
        ))}
      </div>
    </div>
  )
}
