import { useMemo } from 'react'
import { useQuery } from '@tanstack/react-query'
import { cn, StatusLamp, type Reachability } from '@bifrost/ui'
import { fetchRemediationJobs } from '@/api/remediation'
import { fetchPipelineRuns, fetchSupplyChain } from '@/api/delivery'
import { fetchReleaseGate, fetchStgSmoke } from '@/api/promote'
import { useFleetSnapshot } from '@/hooks/useFleetSnapshot'
import { useOperateQueue } from '@/hooks/useOperateQueue'
import { usePatrolSnapshot } from '@/hooks/usePatrolSnapshot'
import { useDailyOpsChecklistCoverage } from '@/hooks/useDailyOpsChecklistCoverage'
import { DELIVER_PLATFORM_PIPELINE } from '@/lib/delivery/deliverPlatformPhases'
import { DELIVER_STG_PIPELINE } from '@/lib/delivery/deliverStgPhases'
import {
  gateStepStatus,
  pickDeployPipelineRun,
  runStepStatus,
} from '@/lib/delivery/releaseStepTypes'
import { resolveCellGate } from '@/lib/control-room/fleetSnapshot'
import { patrolPosture, patrolRunLamp } from '@/lib/patrol/patrolStatus'
import {
  buildOpsDeskFocusChip,
  OPS_DESK_FOCUS_CATEGORIES,
  type OpsDeskFocus,
  type OpsDeskFocusChip,
  worseReachability,
} from '@/lib/task-mode/opsDeskFocus'

const AUTOPILOT_SKILL_ID = 'ops-autopilot'

function stepLamp(status: 'done' | 'active' | 'pending' | 'error'): Reachability {
  if (status === 'done') return 'ok'
  if (status === 'error') return 'fail'
  if (status === 'active') return 'degraded'
  return 'unknown'
}

function useOpsDeskFocusChips(): OpsDeskFocusChip[] {
  const { fleet, snapshot, isLoading: fleetLoading } = useFleetSnapshot()
  const queueQ = useOperateQueue()
  const patrol = usePatrolSnapshot()
  const coverage = useDailyOpsChecklistCoverage(fleet)
  const jobsQ = useQuery({
    queryKey: ['remediation', 'jobs', 'ops-desk-focus'],
    queryFn: () => fetchRemediationJobs({ limit: 5 }),
    refetchInterval: 15_000,
  })
  const platformRunsQ = useQuery({
    queryKey: ['task-cc', 'platform-runs-focus'],
    queryFn: () => fetchPipelineRuns(DELIVER_PLATFORM_PIPELINE),
    refetchInterval: 20_000,
  })
  const platformGateQ = useQuery({
    queryKey: ['task-cc', 'platform-stg-gate-focus'],
    queryFn: () => fetchReleaseGate('platform-stg'),
    refetchInterval: 20_000,
  })
  const tradeRunsQ = useQuery({
    queryKey: ['task-cc', 'trade-runs-focus'],
    queryFn: () => fetchPipelineRuns(DELIVER_STG_PIPELINE),
    refetchInterval: 20_000,
  })
  const tradeGateQ = useQuery({
    queryKey: ['task-cc', 'trade-gate-focus'],
    queryFn: () => fetchReleaseGate('stg'),
    refetchInterval: 20_000,
  })
  const smokeQ = useQuery({
    queryKey: ['task-cc', 'stg-smoke-focus'],
    queryFn: fetchStgSmoke,
    refetchInterval: 20_000,
  })
  const supplyQ = useQuery({
    queryKey: ['task-cc', 'supply-chain-focus'],
    queryFn: fetchSupplyChain,
    refetchInterval: 20_000,
  })

  return useMemo(() => {
    const open = queueQ.data?.open.length ?? 0
    const patrolLamp = patrol.isLoading ? 'unknown' : patrolPosture(patrol.skills, patrol.runs).lamp
    const autopilotLatest = (patrol.runs ?? []).find(r => r.skill_id === AUTOPILOT_SKILL_ID)
    const autopilotLamp = patrolRunLamp(autopilotLatest?.result)
    const recentFail = (jobsQ.data?.jobs ?? []).some(j => j.status === 'failed')
    let agentLamp: Reachability = open > 0 ? 'degraded' : 'ok'
    agentLamp = worseReachability(agentLamp, patrolLamp)
    agentLamp = worseReachability(agentLamp, autopilotLamp)
    if (recentFail) agentLamp = worseReachability(agentLamp, 'degraded')
    const agentSummary = [
      `${open} queue`,
      patrol.isLoading ? 'Patrol…' : patrolPosture(patrol.skills, patrol.runs).label,
      autopilotLatest == null ? 'Autopilot idle' : `AP ${autopilotLatest.result}`,
    ].join(' · ')

    const platformGate = gateStepStatus(platformGateQ.data)
    const platformRun = pickDeployPipelineRun(platformRunsQ.data?.runs, {
      gatePassed: platformGateQ.data?.result === 'pass',
    })
    const platformDeploy = runStepStatus(platformRun)
    const smokeOk = smokeQ.data?.reachability === 'ok'
    const tradeGate = gateStepStatus(tradeGateQ.data)
    const tradeRun = pickDeployPipelineRun(tradeRunsQ.data?.runs, {
      gatePassed: tradeGateQ.data?.result === 'pass',
      smokeOk,
    })
    const tradeDeploy = runStepStatus(tradeRun)
    const cms = supplyQ.data?.dockerfile_configmaps ?? []
    const cmsPresent = cms.filter(c => c.present).length
    let releaseLamp: Reachability = worseReachability(
      stepLamp(platformDeploy.status),
      stepLamp(platformGate.status),
    )
    releaseLamp = worseReachability(releaseLamp, stepLamp(tradeDeploy.status))
    releaseLamp = worseReachability(releaseLamp, stepLamp(tradeGate.status))
    if (smokeQ.data != null) {
      releaseLamp = worseReachability(releaseLamp, smokeOk ? 'ok' : 'fail')
    }
    if (cms.length > 0 && cmsPresent < cms.length) {
      releaseLamp = worseReachability(releaseLamp, 'degraded')
    }
    const releaseSummary = [
      `Rocket ${platformGate.label}`,
      `Trade smoke ${smokeQ.isLoading ? '…' : smokeOk ? 'ok' : 'fail'}`,
      cms.length > 0 ? `CM ${cmsPresent}/${cms.length}` : null,
    ]
      .filter(Boolean)
      .join(' · ')

    const fleetOk = snapshot.missionOverall === 'ok' || fleet.fleetClear
    let envLamp: Reachability = fleetLoading
      ? 'unknown'
      : snapshot.missionOverall === 'fail'
        ? 'fail'
        : fleetOk
          ? 'ok'
          : 'degraded'
    const noGo = fleet.cells.filter(c => resolveCellGate(c) === 'NO-GO').length
    const boardHit = coverage?.boardMatchedCount ?? 0
    const boardTotal = coverage?.boardTotalCount ?? 0
    if (noGo > 0) envLamp = worseReachability(envLamp, 'fail')
    const coverageLabel =
      boardTotal > 0 ? `Coverage ${boardHit}/${boardTotal}` : 'Coverage —'
    const envSummary = fleetLoading
      ? 'Loading fleet…'
      : fleetOk
        ? `Fleet clear · ${coverageLabel}`
        : `NO-GO ${noGo} · ${coverageLabel}`

    return OPS_DESK_FOCUS_CATEGORIES.map(id => {
      if (id === 'agent') return buildOpsDeskFocusChip(id, agentLamp, agentSummary)
      if (id === 'release') return buildOpsDeskFocusChip(id, releaseLamp, releaseSummary)
      return buildOpsDeskFocusChip(id, envLamp, envSummary)
    })
  }, [
    queueQ.data?.open.length,
    patrol.isLoading,
    patrol.skills,
    patrol.runs,
    jobsQ.data?.jobs,
    platformGateQ.data,
    platformRunsQ.data?.runs,
    tradeGateQ.data,
    tradeRunsQ.data?.runs,
    smokeQ.data,
    smokeQ.isLoading,
    supplyQ.data?.dockerfile_configmaps,
    fleet,
    fleetLoading,
    snapshot.missionOverall,
    coverage,
  ])
}

/**
 * Summary chips under TCC Verdict — click to filter Body to one focus category.
 * Default All; click again / All restores. Non-green chips get slight emphasis.
 */
export function OpsDeskFocusSummary({
  focus,
  onChange,
}: {
  focus: OpsDeskFocus
  onChange: (next: OpsDeskFocus) => void
}) {
  const chips = useOpsDeskFocusChips()

  const select = (id: OpsDeskFocus) => {
    onChange(focus === id ? 'all' : id)
  }

  return (
    <div
      className="flex flex-col gap-1.5 rounded-lg border border-border bg-secondary/40 px-3 py-2"
      data-ops-desk-focus-summary
      role="group"
      aria-label="Ops desk focus"
    >
      <div className="flex flex-wrap items-center gap-x-2 gap-y-1">
        <span className="text-[var(--text-dense-caption)] font-semibold uppercase tracking-wide text-muted-foreground">
          Focus
        </span>
        <button
          type="button"
          aria-pressed={focus === 'all'}
          className={cn(
            'rounded border px-2 py-0.5 text-[var(--text-dense-meta)] font-medium transition-colors',
            focus === 'all'
              ? 'border-primary/50 bg-primary/10 text-foreground'
              : 'border-border/60 bg-card text-muted-foreground hover:border-primary/30',
          )}
          onClick={() => onChange('all')}
        >
          All
        </button>
        <span className="text-[var(--text-dense-caption)] text-muted-foreground">
          Agent · Release · Environment — filter Body sections
        </span>
      </div>
      <div className="grid grid-cols-1 gap-1.5 sm:grid-cols-3">
        {chips.map(chip => {
          const selected = focus === chip.id
          return (
            <button
              key={chip.id}
              type="button"
              aria-pressed={selected}
              title={chip.summary}
              onClick={() => select(chip.id)}
              className={cn(
                'rounded border px-2.5 py-1.5 text-left transition-colors',
                selected
                  ? 'border-primary/50 bg-primary/10'
                  : chip.attention
                    ? 'border-warning/40 bg-card hover:border-primary/40'
                    : 'border-border/60 bg-card hover:border-primary/30',
              )}
            >
              <div className="flex items-center gap-1.5">
                <StatusLamp value={chip.lamp} kind="reach" />
                <span className="min-w-0 flex-1 truncate text-[var(--text-dense-meta)] font-semibold">
                  {chip.label}
                </span>
              </div>
              <p className="m-0 mt-0.5 truncate text-[var(--text-dense-caption)] text-muted-foreground">
                {chip.summary}
              </p>
            </button>
          )
        })}
      </div>
    </div>
  )
}
