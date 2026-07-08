import { useMemo } from 'react'
import { cn, DenseTag, Tooltip, TooltipContent, TooltipTrigger } from '@bifrost/ui'
import { Check, CircleHelp } from 'lucide-react'
import { taskModesForSwitcher } from '@/lib/task-mode/taskModeCatalog'
import { taskModeVisual } from '@/lib/task-mode/taskModeVisual'
import type { LoopArchetype, TaskModeDef, TaskModeId } from '@/lib/task-mode/types'

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

function ModeDescriptionHelp({ description, label }: { description: string; label: string }) {
  return (
    <Tooltip>
      <TooltipTrigger asChild>
        <button
          type="button"
          className="inline-flex shrink-0 rounded-sm text-muted-foreground hover:text-foreground"
          aria-label={`About ${label}`}
          onClick={event => event.stopPropagation()}
        >
          <CircleHelp className="size-3" aria-hidden />
        </button>
      </TooltipTrigger>
      <TooltipContent side="right" className="max-w-xs">
        {description}
      </TooltipContent>
    </Tooltip>
  )
}

function ModeOption({
  mode,
  active,
  onPick,
}: {
  mode: TaskModeDef
  active: boolean
  onPick: (id: TaskModeId) => void
}) {
  const visual = taskModeVisual(mode.id)
  const Icon = visual.icon
  return (
    <button
      type="button"
      data-task-mode={mode.id}
      data-active={active ? 'true' : undefined}
      className={cn(
        'task-mode-picker-option flex w-full items-center gap-2 rounded-md px-2 py-1 text-left transition-colors',
        !active && 'hover:bg-transparent',
      )}
      onClick={() => onPick(mode.id)}
    >
      <Icon size={14} className="task-mode-picker-option__icon shrink-0" />
      <span className="flex min-w-0 flex-1 items-center gap-1.5">
        <span className="task-mode-picker-option__label truncate text-[var(--text-dense-label)] font-medium">
          {mode.label}
        </span>
        {mode.loopArchetype !== 'system' && (
          <DenseTag variant={LOOP_VARIANT[mode.loopArchetype]} className="shrink-0 text-[9px]">
            {LOOP_LABEL[mode.loopArchetype]}
          </DenseTag>
        )}
        <ModeDescriptionHelp description={mode.description} label={mode.label} />
      </span>
      {active && <Check size={14} className="task-mode-picker-option__check shrink-0" />}
    </button>
  )
}

function ModeSection({
  title,
  modes,
  activeId,
  onPick,
}: {
  title: string
  modes: TaskModeDef[]
  activeId: TaskModeId
  onPick: (id: TaskModeId) => void
}) {
  if (modes.length === 0) return null
  return (
    <div>
      <p className="mb-0.5 px-2 text-[var(--text-dense-caption)] font-medium uppercase tracking-wide text-muted-foreground">
        {title}
      </p>
      <div className="flex flex-col">
        {modes.map(m => (
          <ModeOption key={m.id} mode={m} active={m.id === activeId} onPick={onPick} />
        ))}
      </div>
    </div>
  )
}

export function TaskModePickerContent({
  activeId,
  onPick,
}: {
  activeId: TaskModeId
  onPick: (id: TaskModeId) => void
}) {
  const allModes = taskModesForSwitcher()
  const grouped = useMemo(
    () => ({
      system: allModes.filter(m => m.loopArchetype === 'system'),
      ops: allModes.filter(m => m.loopArchetype === 'ops'),
      dev: allModes.filter(m => m.loopArchetype === 'dev'),
    }),
    [allModes],
  )

  return (
    <div className="flex max-h-[min(70vh,28rem)] flex-col gap-2 overflow-y-auto p-1">
      <ModeSection title="View" modes={grouped.system} activeId={activeId} onPick={onPick} />
      <ModeSection title="Ops playbooks" modes={grouped.ops} activeId={activeId} onPick={onPick} />
      <ModeSection title="Build playbooks" modes={grouped.dev} activeId={activeId} onPick={onPick} />
    </div>
  )
}
