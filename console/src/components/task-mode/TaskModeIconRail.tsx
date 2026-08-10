import { useMemo } from 'react'
import { useQuery } from '@tanstack/react-query'
import { cn, DenseTag, Tooltip, TooltipContent, TooltipTrigger } from '@bifrost/ui'
import { fetchHermesReadiness } from '@/api/hermes'
import {
  FLEET_COLUMNS,
  fleetEnvColumnPosture,
  viewerEnvToColumn,
  type FleetCellSignal,
  type FleetEnvColumn,
  type FleetEnvColumnPosture,
  type FleetSnapshot,
  type FleetViewerEnv,
} from '@/lib/control-room/fleetSnapshot'
import { taskModesForSwitcher } from '@/lib/task-mode/taskModeCatalog'
import { taskModeVisual } from '@/lib/task-mode/taskModeVisual'
import type { LoopArchetype, TaskModeDef, TaskModeId } from '@/lib/task-mode/types'
import { useTaskMode } from '@/lib/task-mode/useTaskMode'

/**
 * Flat single-row rail: System | [Build · Ops · Analysis] via hairline divider.
 * Ops (expanded) shows DEV/STG/PROD posture strip; collapsed keeps icon + badge.
 */

const ARCHETYPE_TOOLTIP: Record<
  LoopArchetype,
  { label: string; variant: 'neutral' | 'warning' | 'info' }
> = {
  system: { label: 'System', variant: 'neutral' },
  ops: { label: 'Ops', variant: 'warning' },
  dev: { label: 'Build', variant: 'info' },
  analysis: { label: 'Analysis', variant: 'info' },
}

const ENV_KEY: Record<FleetEnvColumn, string> = {
  dev: 'D',
  stg: 'S',
  prod: 'P',
}

const ENV_LABEL: Record<FleetEnvColumn, string> = {
  dev: 'DEV',
  stg: 'STG',
  prod: 'PROD',
}

export type ViewSignalLevel = 'warn' | 'error'

type TaskModeIconRailProps = {
  collapsed?: boolean
  onModeChange?: (landingTab: string, modeId: TaskModeId) => void
  /** Ops — open operate-queue count (from parent; avoids duplicate heavy polling). */
  operateQueueOpen?: number
  /** Ops — fleet NO-GO / mission fail. */
  fleetCritical?: boolean
  /** Live Fleet snapshot — Ops env strip (Rocket+Satellite per column). */
  fleet?: FleetSnapshot | null
  /** Current viewer seat; underlines matching D/S/P cell. */
  viewerEnv?: FleetViewerEnv
  viewerEnvLoading?: boolean
}

function resolveOpsSignal(
  operateQueueOpen: number,
  fleetCritical: boolean,
): ViewSignalLevel | null {
  if (fleetCritical) return 'error'
  if (operateQueueOpen > 0) return 'warn'
  return null
}

function envCellClass(signal: FleetCellSignal): string {
  switch (signal) {
    case 'ok':
      return 'task-mode-icon-rail__env-cell--ok'
    case 'fail':
      return 'task-mode-icon-rail__env-cell--fail'
    case 'degraded':
      return 'task-mode-icon-rail__env-cell--degraded'
    default:
      return 'task-mode-icon-rail__env-cell--unknown'
  }
}

export function TaskModeIconRail({
  collapsed = false,
  onModeChange,
  operateQueueOpen = 0,
  fleetCritical = false,
  fleet = null,
  viewerEnv = 'dev',
  viewerEnvLoading = false,
}: TaskModeIconRailProps) {
  const { modeId, setModeId, mode } = useTaskMode()
  const allModes = taskModesForSwitcher()
  const hermesQ = useQuery({
    queryKey: ['task-mode-rail', 'hermes-readiness'],
    queryFn: fetchHermesReadiness,
    refetchInterval: 30_000,
    staleTime: 20_000,
    retry: false,
  })

  const envPosture = useMemo((): FleetEnvColumnPosture | null => {
    if (fleet == null) return null
    return fleetEnvColumnPosture(fleet)
  }, [fleet])

  const seatColumn = viewerEnvLoading ? null : viewerEnvToColumn(viewerEnv)

  const signals = useMemo((): Partial<Record<TaskModeId, ViewSignalLevel>> => {
    const out: Partial<Record<TaskModeId, ViewSignalLevel>> = {}
    const ops = resolveOpsSignal(operateQueueOpen, fleetCritical)
    if (ops != null) out.ops = ops
    const nous = hermesQ.data?.nous_hermes
    const hermesDown =
      hermesQ.isError ||
      nous?.gateway_running === false ||
      ['fail', 'error', 'down', 'unreachable'].includes((nous?.status ?? '').toLowerCase())
    if (hermesDown) out.analysis = 'warn'
    return out
  }, [operateQueueOpen, fleetCritical, hermesQ.isError, hermesQ.data?.nous_hermes])

  const decks: { key: string; label: string; modes: TaskModeDef[] }[] = [
    { key: 'home', label: 'Overview', modes: allModes.filter(m => m.loopArchetype === 'system') },
    { key: 'focused', label: 'Focused lenses', modes: allModes.filter(m => m.loopArchetype !== 'system') },
  ].filter(d => d.modes.length > 0)

  const pick = (next: TaskModeId) => {
    setModeId(next)
    const landing = allModes.find(m => m.id === next)?.landingTab ?? 'control-room'
    onModeChange?.(landing, next)
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
                envPosture={m.id === 'ops' ? envPosture : null}
                seatColumn={m.id === 'ops' ? seatColumn : null}
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
  envPosture,
  seatColumn,
}: {
  mode: TaskModeDef
  active: boolean
  collapsed: boolean
  onPick: (id: TaskModeId) => void
  signal: ViewSignalLevel | null
  envPosture: FleetEnvColumnPosture | null
  seatColumn: FleetEnvColumn | null
}) {
  const visual = taskModeVisual(mode.id)
  const Icon = visual.icon
  const archetypeBadge = ARCHETYPE_TOOLTIP[mode.loopArchetype]
  const showEnvStrip = mode.id === 'ops' && !collapsed && envPosture != null
  const ariaExtra =
    signal != null
      ? ` (${signal === 'error' ? 'attention required' : 'needs review'})`
      : ''
  const envAria = showEnvStrip
    ? ` · ${FLEET_COLUMNS.map(e => `${ENV_LABEL[e]} ${envPosture[e]}`).join(', ')}`
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
            showEnvStrip && 'task-mode-icon-rail__btn--env',
          )}
          aria-label={`${mode.label}${ariaExtra}${envAria}`}
          aria-pressed={active}
          onClick={() => onPick(mode.id)}
        >
          {!showEnvStrip && <Icon className="task-mode-icon-rail__icon" aria-hidden />}
          {showEnvStrip && (
            <span className="task-mode-icon-rail__env-strip" aria-hidden>
              {FLEET_COLUMNS.map(env => (
                <span
                  key={env}
                  className={cn(
                    'task-mode-icon-rail__env-cell',
                    envCellClass(envPosture[env]),
                    seatColumn === env && 'task-mode-icon-rail__env-cell--seat',
                  )}
                  data-env={env}
                  data-signal={envPosture[env]}
                >
                  <span className="task-mode-icon-rail__env-key">{ENV_KEY[env]}</span>
                </span>
              ))}
            </span>
          )}
          {!showEnvStrip && signal != null && (
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
      <TooltipContent side={collapsed ? 'right' : 'bottom'} className="max-w-[16rem]">
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
        {showEnvStrip && (
          <ul className="m-0 mt-1.5 list-none space-y-0.5 p-0 text-[var(--text-dense-caption)] text-muted-foreground">
            {FLEET_COLUMNS.map(env => (
              <li key={env} className="flex items-center gap-1.5">
                <span
                  className={cn(
                    'task-mode-icon-rail__env-cell',
                    envCellClass(envPosture[env]),
                    seatColumn === env && 'task-mode-icon-rail__env-cell--seat',
                  )}
                >
                  <span className="task-mode-icon-rail__env-key">{ENV_KEY[env]}</span>
                </span>
                <span>
                  {ENV_LABEL[env]} · {envPosture[env]}
                  {seatColumn === env ? ' · seat' : ''}
                </span>
              </li>
            ))}
          </ul>
        )}
      </TooltipContent>
    </Tooltip>
  )
}
