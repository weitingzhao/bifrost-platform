/**
 * Daily Ops Checklist — fleet-standard resolution.
 *
 * Pure logic + shared types for matching live Fleet Board standards against
 * the static DAILY_OPS_CHECKLIST catalog. No JSX here — consumed by both
 * DailyOpsOperatorPlanPanel (orchestrator) and operator-plan/ChecklistSection.
 */
import { ChecklistDispatchActionDto } from '@/api/checklist'
import type {
  ChecklistFailoverItemInput,
} from '@/lib/control-room/checklistCursorFailoverPrompt'
import {
  DAILY_OPS_CHECKLIST,
  matchStandardToChecklistItem,
  type ChecklistItem,
  type DailyOpsChecklistStep,
} from '@/lib/control-room/dailyOpsChecklistCatalog'
import type {
  FleetCellSignal,
  FleetEnvColumn,
  FleetRole,
  FleetSnapshot,
  FleetStandard,
} from '@/lib/control-room/fleetSnapshot'

export const ENV_ORDER: FleetEnvColumn[] = ['dev', 'stg', 'prod']
export const ENV_LABEL: Record<FleetEnvColumn, string> = {
  dev: 'DEV',
  stg: 'STG',
  prod: 'PROD',
}

export type MatchedStandard = FleetStandard & {
  cellRole: FleetRole
  cellEnv: FleetEnvColumn | null
  cellKey: string
}

export type EnvRollup = {
  env: FleetEnvColumn
  signal: FleetCellSignal
  failing: string[]
}

export type ResolvedItem = {
  checklistItem: ChecklistItem
  matchedStandards: MatchedStandard[]
  overallSignal: FleetCellSignal
  envRollups: EnvRollup[]
}

export type ResolvedStep = {
  step: DailyOpsChecklistStep
  items: ResolvedItem[]
  stepSignal: FleetCellSignal
  blockedByUpstream: boolean
  envScopeLabel: string
}

/** Row Fix / Item Fix callback shape — shared between the panel and ChecklistSection. */
export type ChecklistItemFixHandler = (args: {
  itemId: string
  fixScope: string
  label: string
  prompt: string
}) => void

export function worstSignal(signals: FleetCellSignal[]): FleetCellSignal {
  if (signals.length === 0) return 'unknown'
  if (signals.includes('fail')) return 'fail'
  if (signals.includes('degraded')) return 'degraded'
  if (signals.includes('unavailable')) return 'unavailable'
  if (signals.includes('unknown')) return 'unknown'
  return 'ok'
}

export function lampValue(signal: FleetCellSignal): 'ok' | 'fail' | 'degraded' | 'unknown' {
  if (signal === 'ok') return 'ok'
  if (signal === 'fail') return 'fail'
  if (signal === 'degraded') return 'degraded'
  return 'unknown'
}

export function envLampTitle(rollup: EnvRollup): string {
  const env = ENV_LABEL[rollup.env]
  if (rollup.signal === 'ok') return `${env}: ok`
  if (rollup.signal === 'fail') {
    return rollup.failing.length > 0
      ? `${env}: fail — ${rollup.failing.join(', ')}`
      : `${env}: fail`
  }
  if (rollup.signal === 'degraded') {
    return rollup.failing.length > 0
      ? `${env}: degraded — ${rollup.failing.join(', ')}`
      : `${env}: degraded`
  }
  if (rollup.signal === 'unavailable') {
    return `${env}: unavailable — probe path missing or not applicable`
  }
  return `${env}: unknown — no matching probe / not scored yet`
}

export function deployEnvsFromMapping(
  mapping: DailyOpsChecklistStep['fleetMapping'],
): FleetEnvColumn[] {
  const set = new Set<FleetEnvColumn>()
  for (const m of mapping) {
    if (m.env !== 'span') set.add(m.env)
  }
  return ENV_ORDER.filter(e => set.has(e))
}

export function formatEnvScopeLabel(mapping: DailyOpsChecklistStep['fleetMapping']): string {
  const deploy = deployEnvsFromMapping(mapping)
  if (deploy.length > 0) return deploy.map(e => ENV_LABEL[e]).join(' · ')
  return 'ALL'
}

export function buildEnvRollups(
  matched: MatchedStandard[],
  mapping: DailyOpsChecklistStep['fleetMapping'],
): EnvRollup[] {
  const envs = deployEnvsFromMapping(mapping)
  if (envs.length === 0) return []

  return envs.map(env => {
    const inEnv = matched.filter(s => s.cellEnv === env)
    if (inEnv.length === 0) {
      return { env, signal: 'unknown' as const, failing: [] }
    }
    const required = inEnv.filter(s => s.required !== false)
    const scored = required.length > 0 ? required : inEnv
    const signal = worstSignal(scored.map(s => s.signal))
    const failing = scored.filter(s => s.signal !== 'ok').map(s => s.label || s.id)
    return { env, signal, failing }
  })
}

/** Live unhealthy envs only — unknown/unavailable are not "failing". */
export function unhealthyEnvs(envRollups: EnvRollup[]): EnvRollup[] {
  return envRollups.filter(e => e.signal === 'fail' || e.signal === 'degraded')
}

export function resolveChecklist(fleet: FleetSnapshot): ResolvedStep[] {
  const allStandards: MatchedStandard[] = fleet.cells.flatMap(c =>
    c.standards.map(s => ({
      ...s,
      cellRole: c.role,
      cellEnv: c.env,
      cellKey: c.key,
    })),
  )

  let upstreamBlocked = false

  return DAILY_OPS_CHECKLIST.map(step => {
    const currentBlocked = upstreamBlocked

    const resolvedItems: ResolvedItem[] = step.items.map(item => {
      const matched = allStandards.filter(s => {
        const hit = matchStandardToChecklistItem(s.id, s.group, {
          role: s.cellRole,
          env: s.cellEnv,
        })
        return hit?.item.id === item.id && hit.step.id === step.id
      })

      const envRollups = buildEnvRollups(matched, step.fleetMapping)
      const scored =
        matched.filter(s => s.required !== false).length > 0
          ? matched.filter(s => s.required !== false)
          : matched

      // Prefer env-column worst when step is multi-deploy — keeps Status and DEV/STG/PROD aligned
      const overallSignal =
        matched.length === 0
          ? ('unknown' as const)
          : envRollups.length > 0
            ? worstSignal(envRollups.map(e => e.signal))
            : worstSignal(scored.map(s => s.signal))

      return {
        checklistItem: item,
        matchedStandards: matched,
        overallSignal,
        envRollups,
      }
    })

    const stepSignal = worstSignal(resolvedItems.map(ri => ri.overallSignal))

    if (step.blocksDownstream && stepSignal === 'fail') {
      const criticalItems = resolvedItems.filter(ri => ri.checklistItem.critical)
      if (criticalItems.length > 0 && criticalItems.every(ri => ri.overallSignal === 'fail')) {
        upstreamBlocked = true
      }
    }

    return {
      step,
      items: resolvedItems,
      stepSignal,
      blockedByUpstream: currentBlocked,
      envScopeLabel: formatEnvScopeLabel(step.fleetMapping),
    }
  })
}

export function toFailoverInput(
  step: DailyOpsChecklistStep,
  resolved: ResolvedItem,
  agentSignal?: string,
  dispatch?: ChecklistDispatchActionDto,
): ChecklistFailoverItemInput {
  return {
    stepOrder: step.order,
    stepLabel: step.label,
    item: resolved.checklistItem,
    overallSignal: resolved.overallSignal,
    matchedStandards: resolved.matchedStandards.map(s => ({
      id: s.id,
      label: s.label,
      signal: s.signal,
      detail: s.reason,
      source: s.source,
      cellRole: s.cellRole,
      cellEnv: s.cellEnv,
    })),
    agentSignal,
    dispatchGate: dispatch?.gate,
    dispatchDetail: dispatch?.detail,
  }
}
