import { useMemo } from 'react'
import { useQuery } from '@tanstack/react-query'
import { cn, DenseTag, Tooltip, TooltipContent, TooltipTrigger } from '@bifrost/ui'
import { fetchReleaseGate } from '@/api/promote'
import { taskModesForSwitcher } from '@/lib/task-mode/taskModeCatalog'
import { taskModeVisual } from '@/lib/task-mode/taskModeVisual'
import type { LoopArchetype, TaskModeDef, TaskModeId } from '@/lib/task-mode/types'
import { useTaskMode } from '@/lib/task-mode/TaskModeContext'

/**
 * Flat single-row rail: Home | Flight | Forge via hairline dividers only.
 * Idle glyphs small + muted; hover/active grow in-flow (push neighbors);
 * only active restores accent (System = Bifrost lime).
 * Non-active modes may show a cross-view attention dot (warn / error).
 */

const DECK_ORDER: LoopArchetype[] = ['system', 'ops', 'dev']

const ARCHETYPE_TOOLTIP: Record<
  LoopArchetype,
  { label: string; variant: 'neutral' | 'warning' | 'info' }
> = {
  system: { label: 'System', variant: 'neutral' },
  ops: { label: 'Ops', variant: 'warning' },
  dev: { label: 'Build', variant: 'info' },
}

export type ViewSignalLevel = 'warn' | 'error'

type TaskModeIconRailProps = {
  collapsed?: boolean
  onModeChange?: (landingTab: string, modeId: TaskModeId) => void
  /** Daily Ops — open operate-queue count (from parent; avoids duplicate heavy polling). */
  operateQueueOpen?: number
  /** Daily Ops — fleet NO-GO / mission fail. */
  fleetCritical?: boolean
}

function resolveDailyOpsSignal(
  operateQueueOpen: number,
  fleetCritical: boolean,
): ViewSignalLevel | null {
  if (fleetCritical) return 'error'
  if (operateQueueOpen > 0) return 'warn'
  return null
}

function resolveLaunchSignal(
  platformStg: string | undefined,
  tradeStg: string | undefined,
): ViewSignalLevel | null {
  const results = [platformStg, tradeStg].filter((r): r is string => r != null && r !== '')
  if (results.some(r => r === 'fail' || r === 'error')) return 'error'
  if (results.some(r => r === 'pending' || r === 'running' || r === 'unknown')) return 'warn'
  return null
}

export function TaskModeIconRail({
  collapsed = false,
  onModeChange,
  operateQueueOpen = 0,
  fleetCritical = false,
}: TaskModeIconRailProps) {
  const { modeId, setModeId, mode } = useTaskMode()
  const allModes = taskModesForSwitcher()

  // Light gate polls for Launch cross-view signal (≥20s). Skip when Launch is active
  // (TCC already owns denser polling) but still refresh so dots update after leave.
  const platformGateQ = useQuery({
    queryKey: ['task-mode-rail', 'platform-stg-gate'],
    queryFn: () => fetchReleaseGate('platform-stg'),
    refetchInterval: 20_000,
    staleTime: 15_000,
  })
  const tradeGateQ = useQuery({
    queryKey: ['task-mode-rail', 'trade-stg-gate'],
    queryFn: () => fetchReleaseGate('stg'),
    refetchInterval: 20_000,
    staleTime: 15_000,
  })

  const signals = useMemo((): Partial<Record<TaskModeId, ViewSignalLevel>> => {
    const out: Partial<Record<TaskModeId, ViewSignalLevel>> = {}
    const daily = resolveDailyOpsSignal(operateQueueOpen, fleetCritical)
    if (daily != null) out['daily-ops'] = daily
    const launch = resolveLaunchSignal(platformGateQ.data?.result, tradeGateQ.data?.result)
    if (launch != null) out['mission-launch'] = launch
    return out
  }, [
    operateQueueOpen,
    fleetCritical,
    platformGateQ.data?.result,
    tradeGateQ.data?.result,
  ])

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
                signal={m.id === modeId ? null : (signals[m.id] ?? null)}
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
  signal,
}: {
  mode: TaskModeDef
  active: boolean
  collapsed: boolean
  onPick: (id: TaskModeId) => void
  signal: ViewSignalLevel | null
}) {
  const visual = taskModeVisual(mode.id)
  const Icon = visual.icon
  const archetypeBadge = ARCHETYPE_TOOLTIP[mode.loopArchetype]
  return (
    <Tooltip>
      <TooltipTrigger asChild>
        <button
          type="button"
          data-task-mode={mode.id}
          data-active={active ? 'true' : undefined}
          className={cn(
            'task-mode-icon-rail__btn relative inline-flex shrink-0 items-center justify-center rounded-md',
            active && 'task-mode-icon-rail__btn--active',
          )}
          aria-label={
            signal == null
              ? mode.label
              : `${mode.label} (${signal === 'error' ? 'attention required' : 'needs review'})`
          }
          aria-pressed={active}
          onClick={() => onPick(mode.id)}
        >
          <Icon className="task-mode-icon-rail__icon" aria-hidden />
          {signal != null && (
            <span
              className={cn(
                'pointer-events-none absolute right-0.5 top-0.5 size-1.5 rounded-full',
                signal === 'error' ? 'bg-destructive' : 'bg-warning',
              )}
              aria-hidden
            />
          )}
        </button>
      </TooltipTrigger>
      <TooltipContent side={collapsed ? 'right' : 'bottom'} className="max-w-[14rem]">
        <div className="flex flex-wrap items-center gap-1.5">
          <p className="m-0 text-[var(--text-dense-label)] font-semibold">{mode.label}</p>
          {mode.loopArchetype !== 'system' && (
            <DenseTag variant={archetypeBadge.variant} className="text-[9px]">
              {archetypeBadge.label}
            </DenseTag>
          )}
          {signal != null && (
            <DenseTag variant={signal === 'error' ? 'danger' : 'warning'} className="text-[9px]">
              {signal === 'error' ? 'Attention' : 'Review'}
            </DenseTag>
          )}
        </div>
        <p className="m-0 mt-0.5 text-[var(--text-dense-caption)] text-muted-foreground">
          {mode.description}
        </p>
      </TooltipContent>
    </Tooltip>
  )
}
