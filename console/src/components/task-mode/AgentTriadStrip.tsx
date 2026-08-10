import { useQuery } from '@tanstack/react-query'
import { StatusLamp, cn, type Reachability } from '@bifrost/ui'
import { BrainCircuit, Gauge, Hammer } from 'lucide-react'
import { fetchHermesInsights, fetchHermesReadiness } from '@/api/hermes'
import { isBriefingOpened } from '@/lib/task-mode/briefingOpenedFlag'
import { resolveActivePhaseId, resolveAllTaskPhaseStatuses } from '@/lib/task-mode/navLens'
import { taskModeById } from '@/lib/task-mode/taskModeCatalog'
import type { TaskModeId } from '@/lib/task-mode/types'
import { useTaskMode } from '@/lib/task-mode/useTaskMode'
import { usePatrolSnapshot } from '@/hooks/usePatrolSnapshot'
import { formatPatrolRelativeTime, latestPatrolRun, patrolPosture } from '@/lib/patrol/patrolStatus'

export type AgentTriadStripProps = {
  onModeChange: (landingTab: string, modeId: TaskModeId) => void
  operateQueueOpen?: number
  recentRemediationFail?: boolean
}

type TriadCell = {
  id: 'build' | 'ops' | 'analysis'
  modeId: TaskModeId
  label: string
  lamp: Reachability
  summary: string
  Icon: typeof Hammer
}

function buildCell(currentModeId: TaskModeId): TriadCell {
  const build = taskModeById('build')
  const statuses = resolveAllTaskPhaseStatuses('build', {
    briefingOpened: isBriefingOpened('build'),
  })
  const activeId = resolveActivePhaseId(statuses, build.phases?.map(p => p.id))
  const phase = build.phases?.find(p => p.id === activeId)
  const inBuild = currentModeId === 'build'
  const blocked = phase != null && statuses[phase.id] === 'blocked'
  const summary =
    inBuild && phase != null ? `Phase ${phase.seq}: ${phase.title}` : 'Idle'
  return {
    id: 'build',
    modeId: 'build',
    label: 'Build',
    lamp: blocked ? 'fail' : inBuild ? 'ok' : 'unknown',
    summary,
    Icon: Hammer,
  }
}

function opsCell(
  operateQueueOpen: number,
  recentRemediationFail: boolean,
  patrolSummary: string,
  patrolLamp: Reachability,
): TriadCell {
  let lamp: Reachability = patrolLamp
  let summary =
    operateQueueOpen > 0
      ? `${operateQueueOpen} open · ${patrolSummary}`
      : `Queue clear · ${patrolSummary}`
  if (recentRemediationFail) {
    lamp = 'fail'
    summary =
      operateQueueOpen > 0
        ? `${operateQueueOpen} open · recent fail`
        : `Recent fail · ${patrolSummary}`
  } else if (operateQueueOpen > 0) {
    lamp = lamp === 'fail' ? 'fail' : 'degraded'
  } else if (lamp === 'unknown') {
    lamp = 'ok'
  }
  return {
    id: 'ops',
    modeId: 'ops',
    label: 'Ops',
    lamp,
    summary,
    Icon: Gauge,
  }
}

function analysisCell(
  hermesReachable: boolean | null,
  lastInsight: string | null,
): TriadCell {
  let lamp: Reachability = 'unknown'
  if (hermesReachable === false) lamp = 'degraded'
  else if (hermesReachable === true) lamp = 'ok'
  const summary =
    hermesReachable === false
      ? 'Hermes unreachable'
      : lastInsight != null && lastInsight !== ''
        ? lastInsight
        : 'No insights yet'
  return {
    id: 'analysis',
    modeId: 'analysis',
    label: 'Analysis',
    lamp,
    summary,
    Icon: BrainCircuit,
  }
}

/**
 * Three-up desk switcher — Build / Ops / Analysis without a new page chrome row.
 */
export function AgentTriadStrip({
  onModeChange,
  operateQueueOpen = 0,
  recentRemediationFail = false,
}: AgentTriadStripProps) {
  const { modeId, setModeId } = useTaskMode()
  const patrol = usePatrolSnapshot()
  const posture = patrolPosture(patrol.skills, patrol.runs)
  const latest = latestPatrolRun(patrol.runs)
  const when =
    latest != null
      ? formatPatrolRelativeTime(latest.finished_at ?? latest.started_at)
      : 'no runs'
  const readinessQ = useQuery({
    queryKey: ['hermes', 'readiness'],
    queryFn: fetchHermesReadiness,
    refetchInterval: 30_000,
    retry: false,
  })
  const insightsQ = useQuery({
    queryKey: ['hermes', 'insights', 1],
    queryFn: () => fetchHermesInsights(1),
    refetchInterval: 30_000,
    retry: false,
  })
  const hermesReachable =
    readinessQ.isError
      ? false
      : readinessQ.data == null
        ? null
        : readinessQ.data.nous_hermes.gateway_running !== false &&
          !['fail', 'error', 'down', 'unreachable'].includes(
            (readinessQ.data.nous_hermes.status ?? '').toLowerCase(),
          )
  const lastInsight = insightsQ.data?.items[0]
  const lastInsightLine =
    lastInsight == null
      ? null
      : lastInsight.summary?.trim() ||
        [lastInsight.symbol, lastInsight.type, lastInsight.verdict].filter(s => s !== '').join(' · ')

  const cells: TriadCell[] = [
    buildCell(modeId),
    opsCell(operateQueueOpen, recentRemediationFail, `${posture.label} · ${when}`, posture.lamp),
    analysisCell(hermesReachable, lastInsightLine),
  ]

  const pick = (next: TaskModeId) => {
    setModeId(next)
    onModeChange(taskModeById(next).landingTab, next)
  }

  return (
    <div
      className="grid grid-cols-1 gap-2 sm:grid-cols-3"
      role="group"
      aria-label="Three desks"
    >
      {cells.map(cell => {
        const active = modeId === cell.modeId
        const Icon = cell.Icon
        return (
          <button
            key={cell.id}
            type="button"
            data-agent-triad={cell.id}
            aria-pressed={active}
            aria-label={`${cell.label}: ${cell.summary}`}
            className={cn(
              'flex min-w-0 items-start gap-2 rounded-md border px-2.5 py-2 text-left',
              'hover:bg-secondary/80',
              active
                ? 'border-[color-mix(in_oklab,var(--task-mode-accent)_55%,var(--border))] bg-[color-mix(in_oklab,var(--task-mode-accent)_10%,var(--card))]'
                : 'border-border bg-secondary/40',
            )}
            onClick={() => pick(cell.modeId)}
          >
            <StatusLamp value={cell.lamp} kind="reach" />
            <span className="min-w-0 flex-1">
              <span className="flex items-center gap-1 text-[var(--text-dense-label)] font-semibold">
                <Icon className="size-3 shrink-0 text-muted-foreground" aria-hidden />
                {cell.label}
              </span>
              <span className="mt-0.5 block truncate text-[var(--text-dense-caption)] text-muted-foreground">
                {cell.summary}
              </span>
            </span>
          </button>
        )
      })}
    </div>
  )
}
