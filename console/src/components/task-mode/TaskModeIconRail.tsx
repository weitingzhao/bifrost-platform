import { cn, Tooltip, TooltipContent, TooltipTrigger } from '@bifrost/ui'
import { taskModesForSwitcher } from '@/lib/task-mode/taskModeCatalog'
import { taskModeVisual } from '@/lib/task-mode/taskModeVisual'
import type { LoopArchetype, TaskModeDef, TaskModeId } from '@/lib/task-mode/types'
import { useTaskMode } from '@/lib/task-mode/TaskModeContext'

/**
 * Flat single-row rail: Home | Flight | Forge via hairline dividers only.
 * Idle glyphs small + muted; hover/active grow in-flow (push neighbors);
 * only active restores accent (System = Bifrost lime).
 */

const DECK_ORDER: LoopArchetype[] = ['system', 'ops', 'dev']

type TaskModeIconRailProps = {
  collapsed?: boolean
  onModeChange?: (landingTab: string, modeId: TaskModeId) => void
}

export function TaskModeIconRail({ collapsed = false, onModeChange }: TaskModeIconRailProps) {
  const { modeId, setModeId, mode } = useTaskMode()
  const allModes = taskModesForSwitcher()

  const decks = DECK_ORDER.map(archetype => ({
    archetype,
    modes: allModes.filter(m => m.loopArchetype === archetype),
  })).filter(d => d.modes.length > 0)

  const pick = (next: TaskModeId) => {
    setModeId(next)
    onModeChange?.(next === 'system' ? 'control-room' : 'task-cc', next)
  }

  return (
    <div
      className={cn(
        'task-mode-icon-rail',
        collapsed
          ? 'flex flex-col items-center gap-0.5 py-2'
          : 'flex flex-row flex-nowrap items-center gap-0.5 px-1.5 py-1',
      )}
      role="toolbar"
      aria-label="Task mode views"
      data-active-archetype={mode.loopArchetype}
      data-collapsed={collapsed ? 'true' : undefined}
    >
      {decks.map(({ archetype, modes }, groupIndex) => (
        <div key={archetype} className="contents">
          {groupIndex > 0 && (
            <div
              className={cn(
                'task-mode-icon-rail__rule shrink-0 bg-sidebar-border/80',
                collapsed ? 'my-0.5 h-px w-4' : 'mx-0.5 h-3.5 w-px',
              )}
              aria-hidden
            />
          )}
          <div
            className={cn(
              'task-mode-icon-rail__group flex items-center',
              collapsed ? 'flex-col gap-0.5' : 'flex-row gap-px',
            )}
            role="group"
            aria-label={
              archetype === 'system'
                ? 'Overview'
                : archetype === 'ops'
                  ? 'Ops playbooks'
                  : 'Build playbooks'
            }
          >
            {modes.map(m => (
              <ModeGlyph
                key={m.id}
                mode={m}
                active={m.id === modeId}
                collapsed={collapsed}
                onPick={pick}
              />
            ))}
          </div>
        </div>
      ))}
    </div>
  )
}

function ModeGlyph({
  mode,
  active,
  collapsed,
  onPick,
}: {
  mode: TaskModeDef
  active: boolean
  collapsed: boolean
  onPick: (id: TaskModeId) => void
}) {
  const visual = taskModeVisual(mode.id)
  const Icon = visual.icon
  return (
    <Tooltip>
      <TooltipTrigger asChild>
        <button
          type="button"
          data-task-mode={mode.id}
          data-active={active ? 'true' : undefined}
          className={cn(
            'task-mode-icon-rail__btn inline-flex shrink-0 items-center justify-center rounded-md',
            active && 'task-mode-icon-rail__btn--active',
          )}
          aria-label={mode.label}
          aria-pressed={active}
          onClick={() => onPick(mode.id)}
        >
          <Icon className="task-mode-icon-rail__icon" aria-hidden />
        </button>
      </TooltipTrigger>
      <TooltipContent side={collapsed ? 'right' : 'bottom'} className="max-w-[14rem]">
        <p className="m-0 text-[var(--text-dense-label)] font-semibold">{mode.label}</p>
        <p className="m-0 mt-0.5 text-[var(--text-dense-caption)] text-muted-foreground">
          {mode.description}
        </p>
      </TooltipContent>
    </Tooltip>
  )
}
