import { useMemo } from 'react'
import { useQuery } from '@tanstack/react-query'
import { cn, DenseTag, Tooltip, TooltipContent, TooltipTrigger } from '@bifrost/ui'
import { fetchSelfHealth } from '@/api/core'
import { fetchReleaseGate } from '@/api/promote'
import { taskModesForSwitcher } from '@/lib/task-mode/taskModeCatalog'
import { taskModeVisual } from '@/lib/task-mode/taskModeVisual'
import type { LoopArchetype, TaskModeDef, TaskModeId } from '@/lib/task-mode/types'
import { useTaskMode } from '@/lib/task-mode/TaskModeContext'

/**
 * Flat single-row rail: System | [Daily Ops · Launch · Build] via hairline divider.
 * Expanded row uses full sidebar width — focused lenses justify-evenly.
 * Launch carries a horizontal D/S/P env strip (not a single attention dot).
 * Other modes may show a cross-view attention dot (warn / error) when inactive.
 */

const ARCHETYPE_TOOLTIP: Record<
  LoopArchetype,
  { label: string; variant: 'neutral' | 'warning' | 'info' }
> = {
  system: { label: 'System', variant: 'neutral' },
  ops: { label: 'Ops', variant: 'warning' },
  dev: { label: 'Build', variant: 'info' },
}

export type ViewSignalLevel = 'warn' | 'error'

/** Compact env lamp for Launch glyph — always visible. */
export type EnvLampLevel = 'ok' | 'degraded' | 'fail' | 'unknown'

export type LaunchEnvLamps = {
  dev: EnvLampLevel
  stg: EnvLampLevel
  prod: EnvLampLevel
}

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

function gateToLamp(result: string | undefined): EnvLampLevel {
  if (result == null || result === '') return 'unknown'
  if (result === 'pass' || result === 'ok') return 'ok'
  if (result === 'fail' || result === 'error') return 'fail'
  if (result === 'pending' || result === 'running') return 'degraded'
  return 'unknown'
}

function worseEnvLamp(a: EnvLampLevel, b: EnvLampLevel): EnvLampLevel {
  const rank: Record<EnvLampLevel, number> = { ok: 0, unknown: 1, degraded: 2, fail: 3 }
  return rank[a] >= rank[b] ? a : b
}

function combineGates(a: string | undefined, b: string | undefined): EnvLampLevel {
  return worseEnvLamp(gateToLamp(a), gateToLamp(b))
}

function selfHealthToLamp(overall: string | undefined): EnvLampLevel {
  if (overall == null || overall === '') return 'unknown'
  if (overall === 'ok') return 'ok'
  if (overall === 'fail') return 'fail'
  if (overall === 'degraded') return 'degraded'
  return 'unknown'
}

function lampWord(level: EnvLampLevel): string {
  if (level === 'ok') return 'green'
  if (level === 'fail') return 'red'
  if (level === 'degraded') return 'amber'
  return 'gray'
}

function buildLaunchEnvSummary(lamps: LaunchEnvLamps): string {
  return `DEV ${lampWord(lamps.dev)} · STG ${lampWord(lamps.stg)} · PROD ${lampWord(lamps.prod)}`
}

function envCellClass(level: EnvLampLevel): string {
  if (level === 'ok') return 'task-mode-icon-rail__env-cell--ok'
  if (level === 'fail') return 'task-mode-icon-rail__env-cell--fail'
  if (level === 'degraded') return 'task-mode-icon-rail__env-cell--degraded'
  return 'task-mode-icon-rail__env-cell--unknown'
}

export function TaskModeIconRail({
  collapsed = false,
  onModeChange,
  operateQueueOpen = 0,
  fleetCritical = false,
}: TaskModeIconRailProps) {
  const { modeId, setModeId, mode } = useTaskMode()
  const allModes = taskModesForSwitcher()

  // Light polls for Launch env stack (≥20s). TCC owns denser polling when Launch is open.
  const platformStgQ = useQuery({
    queryKey: ['task-mode-rail', 'platform-stg-gate'],
    queryFn: () => fetchReleaseGate('platform-stg'),
    refetchInterval: 20_000,
    staleTime: 15_000,
  })
  const tradeStgQ = useQuery({
    queryKey: ['task-mode-rail', 'trade-stg-gate'],
    queryFn: () => fetchReleaseGate('stg'),
    refetchInterval: 20_000,
    staleTime: 15_000,
  })
  const platformProdQ = useQuery({
    queryKey: ['task-mode-rail', 'platform-prod-gate'],
    queryFn: () => fetchReleaseGate('platform-prod'),
    refetchInterval: 20_000,
    staleTime: 15_000,
  })
  const tradeProdQ = useQuery({
    queryKey: ['task-mode-rail', 'trade-prod-gate'],
    queryFn: () => fetchReleaseGate('prod'),
    refetchInterval: 20_000,
    staleTime: 15_000,
  })
  const selfHealthQ = useQuery({
    queryKey: ['task-mode-rail', 'self-health'],
    queryFn: fetchSelfHealth,
    refetchInterval: 20_000,
    staleTime: 15_000,
  })

  const launchEnvLamps = useMemo((): LaunchEnvLamps => {
    return {
      // Local / control-plane seat — closest DEV proxy without a release-gate tier.
      dev: selfHealthToLamp(selfHealthQ.data?.overall),
      stg: combineGates(platformStgQ.data?.result, tradeStgQ.data?.result),
      prod: combineGates(platformProdQ.data?.result, tradeProdQ.data?.result),
    }
  }, [
    selfHealthQ.data?.overall,
    platformStgQ.data?.result,
    tradeStgQ.data?.result,
    platformProdQ.data?.result,
    tradeProdQ.data?.result,
  ])

  const signals = useMemo((): Partial<Record<TaskModeId, ViewSignalLevel>> => {
    const out: Partial<Record<TaskModeId, ViewSignalLevel>> = {}
    const daily = resolveDailyOpsSignal(operateQueueOpen, fleetCritical)
    if (daily != null) out['daily-ops'] = daily
    // Launch uses env-lamp stack instead of a single attention dot.
    return out
  }, [operateQueueOpen, fleetCritical])

  const launchEnvSummary = useMemo(() => buildLaunchEnvSummary(launchEnvLamps), [launchEnvLamps])

  const decks: { key: string; label: string; modes: TaskModeDef[] }[] = [
    { key: 'home', label: 'Overview', modes: allModes.filter(m => m.loopArchetype === 'system') },
    { key: 'focused', label: 'Focused lenses', modes: allModes.filter(m => m.loopArchetype !== 'system') },
  ].filter(d => d.modes.length > 0)

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
          : 'flex w-full flex-row flex-nowrap items-center gap-1 px-1.5 py-1',
      )}
      role="toolbar"
      aria-label="Task mode views"
      data-active-archetype={mode.loopArchetype}
      data-collapsed={collapsed ? 'true' : undefined}
    >
      {decks.map(({ key, label, modes }, groupIndex) => (
        <div key={key} className="contents">
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
              collapsed
                ? 'flex-col gap-0.5'
                : key === 'focused'
                  ? 'min-w-0 flex-1 flex-row justify-evenly gap-0.5'
                  : 'shrink-0 flex-row',
            )}
            role="group"
            aria-label={label}
          >
            {modes.map(m => (
              <ModeGlyph
                key={m.id}
                mode={m}
                active={m.id === modeId}
                collapsed={collapsed}
                onPick={pick}
                signal={m.id === modeId ? null : (signals[m.id] ?? null)}
                envLamps={m.id === 'mission-launch' ? launchEnvLamps : null}
                envSummary={m.id === 'mission-launch' ? launchEnvSummary : null}
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
  envLamps,
  envSummary,
}: {
  mode: TaskModeDef
  active: boolean
  collapsed: boolean
  onPick: (id: TaskModeId) => void
  signal: ViewSignalLevel | null
  /** Launch-only: always-on DEV / STG / PROD lamps. */
  envLamps?: LaunchEnvLamps | null
  envSummary?: string | null
}) {
  const visual = taskModeVisual(mode.id)
  const Icon = visual.icon
  const archetypeBadge = ARCHETYPE_TOOLTIP[mode.loopArchetype]
  const showEnvStack = envLamps != null
  const ariaExtra =
    envSummary != null && envSummary !== ''
      ? ` — ${envSummary}`
      : signal != null
        ? ` (${signal === 'error' ? 'attention required' : 'needs review'})`
        : ''

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
            showEnvStack && 'task-mode-icon-rail__btn--env',
          )}
          aria-label={`${mode.label}${ariaExtra}`}
          aria-pressed={active}
          onClick={() => onPick(mode.id)}
        >
          <Icon className="task-mode-icon-rail__icon" aria-hidden />
          {showEnvStack ? (
            <span className="task-mode-icon-rail__env-strip" aria-hidden>
              {(
                [
                  ['D', 'DEV', envLamps.dev],
                  ['S', 'STG', envLamps.stg],
                  ['P', 'PROD', envLamps.prod],
                ] as const
              ).map(([key, title, level]) => (
                <span
                  key={key}
                  className={cn('task-mode-icon-rail__env-cell', envCellClass(level))}
                  title={`${title}: ${lampWord(level)}`}
                >
                  <span className="task-mode-icon-rail__env-key">{key}</span>
                </span>
              ))}
            </span>
          ) : (
            signal != null && (
              <span
                className={cn(
                  'pointer-events-none absolute right-0.5 top-0.5 size-1.5 rounded-full',
                  signal === 'error' ? 'bg-destructive' : 'bg-warning',
                )}
                aria-hidden
              />
            )
          )}
        </button>
      </TooltipTrigger>
      <TooltipContent side={collapsed ? 'right' : 'bottom'} className="max-w-[16rem]">
        <div className="flex flex-wrap items-center gap-1.5">
          <p className="m-0 text-[var(--text-dense-label)] font-semibold">{mode.label}</p>
          {mode.loopArchetype !== 'system' && (
            <DenseTag variant={archetypeBadge.variant} className="text-[9px]">
              {archetypeBadge.label}
            </DenseTag>
          )}
          {signal != null && !showEnvStack && (
            <DenseTag variant={signal === 'error' ? 'danger' : 'warning'} className="text-[9px]">
              {signal === 'error' ? 'Attention' : 'Review'}
            </DenseTag>
          )}
        </div>
        {showEnvStack && envLamps != null ? (
          <div className="mt-1 flex flex-col gap-0.5 font-mono-tabular text-[var(--text-dense-caption)] text-muted-foreground">
            <span>DEV · {lampWord(envLamps.dev).toUpperCase()}</span>
            <span>STG · {lampWord(envLamps.stg).toUpperCase()}</span>
            <span>PROD · {lampWord(envLamps.prod).toUpperCase()}</span>
            <span className="mt-0.5 text-[9px] opacity-80">
              STG/PROD = Platform∩Trade gates · DEV = self-health
            </span>
          </div>
        ) : (
          <p className="m-0 mt-0.5 text-[var(--text-dense-caption)] text-muted-foreground">
            {mode.description}
          </p>
        )}
      </TooltipContent>
    </Tooltip>
  )
}
