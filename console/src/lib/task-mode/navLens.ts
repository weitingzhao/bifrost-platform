import { LayoutGrid, ListTodo } from 'lucide-react'
import type { ShellNavGroup, ShellNavItem } from '@bifrost/ui'
import { getAllNavItems } from '@bifrost/ui'
import type {
  DeliveryPipelineRunView,
  OpsContextResponse,
  ReleaseGateResponse,
  SupplyChainResponse,
} from '@/api/types'
import type { ProgramDetailResponse } from '@/api/programsTypes'
import type { DeliveryReleasePhase } from '@/lib/architecture/deliveryMainlineCatalog'
import type { MissionSnapshot } from '@/lib/control-room/missionSignals'
import { gateStepStatus, runStepStatus } from '@/components/delivery/ReleaseStepCommandCenter'
import { isPipelineRunSucceeded } from '@/lib/delivery/pipelineRunAskPack'
import { taskModeById } from './taskModeCatalog'
import type { TaskModeId, TaskPhaseDef, TaskPhaseStatus } from './types'

const TASK_CC_NAV_ITEM: ShellNavItem = {
  id: 'task-cc',
  label: 'Task Control Center',
  icon: ListTodo,
  shortLabel: 'T',
}

export type TaskPhaseStatusInput = {
  context?: OpsContextResponse
  snapshot?: MissionSnapshot
  supplyChain?: SupplyChainResponse
  stgReleasePhases?: DeliveryReleasePhase[]
  operateQueueOpenCount?: number
  programDetail?: ProgramDetailResponse
  platformStgRun?: DeliveryPipelineRunView
  platformStgGate?: ReleaseGateResponse
  platformProdGate?: ReleaseGateResponse
  tradeStgRun?: DeliveryPipelineRunView
  tradeStgGate?: ReleaseGateResponse
  tradeStgSmokeOk?: boolean
  /** True after user clicked "Open scoped Briefing" (localStorage; D-A). */
  briefingOpened?: boolean
  /** Dev Agent API phase completion — maps Task Mode phase ids (implement / pre-push). */
  devAgentPhaseDone?: (phaseId: string) => boolean
}

function filterGroupItems(group: ShellNavGroup, allowed: Set<string>): ShellNavGroup | null {
  const subGroups = group.subGroups?.map(sg => ({
    ...sg,
    items: sg.items.filter(item => allowed.has(item.id)),
  })).filter(sg => sg.items.length > 0)

  const items = group.items?.filter(item => allowed.has(item.id))

  const hasSub = subGroups != null && subGroups.length > 0
  const hasFlat = items != null && items.length > 0
  if (!hasSub && !hasFlat) return null

  return {
    ...group,
    subGroups: hasSub ? subGroups : undefined,
    items: hasFlat ? items : undefined,
  }
}

function injectTaskCc(groups: ShellNavGroup[]): ShellNavGroup[] {
  if (groups.length === 0) return groups
  const missionIdx = groups.findIndex(g => g.label === 'Mission Control')
  const idx = missionIdx >= 0 ? missionIdx : 0
  const target = groups[idx]

  if (target.subGroups != null && target.subGroups.length > 0) {
    const first = target.subGroups[0]
    const already = first.items.some(i => i.id === 'task-cc')
    const nextSubGroups = [...target.subGroups]
    nextSubGroups[0] = {
      ...first,
      items: already ? first.items : [TASK_CC_NAV_ITEM, ...first.items],
    }
    return groups.map((g, i) => (i === idx ? { ...target, subGroups: nextSubGroups } : g))
  }

  const flatItems = target.items ?? []
  if (flatItems.some(i => i.id === 'task-cc')) return groups
  return groups.map((g, i) =>
    i === idx ? { ...target, items: [TASK_CC_NAV_ITEM, ...flatItems] } : g,
  )
}

function buildMoreDomainsGroup(fullGroups: ShellNavGroup[], allowed: Set<string>): ShellNavGroup | null {
  const subGroups = fullGroups
    .map(g => {
      const items = getAllNavItems(g).filter(item => !allowed.has(item.id))
      if (items.length === 0) return null
      return { label: g.label, items }
    })
    .filter((sg): sg is NonNullable<typeof sg> => sg != null)

  if (subGroups.length === 0) return null

  return {
    label: 'More domains',
    icon: LayoutGrid,
    defaultOpen: false,
    dividerBefore: true,
    subGroups,
  }
}

/** Filter full CONSOLE_NAV_GROUPS to the lens for a task mode. System mode returns full groups. */
export function buildTaskNavGroups(modeId: TaskModeId, fullGroups: ShellNavGroup[]): ShellNavGroup[] {
  const mode = taskModeById(modeId)
  if (mode.loopArchetype === 'system' || mode.navLens.includeTabs == null) {
    return fullGroups
  }

  const allowed = new Set(mode.navLens.includeTabs)
  if (mode.navLens.showTaskControlCenter) {
    allowed.add('task-cc')
  }

  const filtered = fullGroups
    .map(g => filterGroupItems(g, allowed))
    .filter((g): g is ShellNavGroup => g != null)

  const focused = mode.navLens.showTaskControlCenter ? injectTaskCc(filtered) : filtered
  const more = buildMoreDomainsGroup(fullGroups, allowed)
  return more != null ? [...focused, more] : focused
}

export function allNavTabIds(groups: ShellNavGroup[]): string[] {
  const ids: string[] = []
  for (const g of groups) {
    for (const item of getAllNavItems(g)) {
      ids.push(item.id)
    }
  }
  return ids
}

function priorPhasesDone(
  phase: TaskPhaseDef,
  statusOf: (id: string) => TaskPhaseStatus,
): boolean {
  if (phase.dependsOn == null || phase.dependsOn.length === 0) return true
  return phase.dependsOn.every(depId => statusOf(depId) === 'done')
}

function firstIncompletePhase(phases: TaskPhaseDef[], statusOf: (id: string) => TaskPhaseStatus): string | null {
  for (const p of phases) {
    const st = statusOf(p.id)
    if (st !== 'done') return p.id
  }
  return null
}

function resolveDailyOpsPhase(phaseId: string, input: TaskPhaseStatusInput): TaskPhaseStatus {
  const snap = input.snapshot
  switch (phaseId) {
    case 'scan-signals':
      if (snap == null) return 'unknown'
      return snap.missionOverall === 'ok'
        ? 'done'
        : snap.missionOverall === 'fail'
          ? 'blocked'
          : 'active'
    case 'triage-defects':
      if (snap == null) return 'unknown'
      return snap.missionOverall === 'ok' ? 'done' : 'active'
    case 'operate-queue': {
      const open = input.operateQueueOpenCount ?? 0
      if (open === 0) return 'done'
      return open > 0 ? 'active' : 'planned'
    }
    case 'verify-mission':
      if (snap == null) return 'unknown'
      return snap.missionOverall === 'ok' ? 'done' : 'planned'
    default:
      return 'unknown'
  }
}

function combineDualStatus(
  a: TaskPhaseStatus,
  b: TaskPhaseStatus,
): TaskPhaseStatus {
  if (a === 'blocked' || b === 'blocked') return 'blocked'
  if (a === 'unknown' || b === 'unknown') return 'unknown'
  if (a === 'done' && b === 'done') return 'done'
  if (a === 'active' || b === 'active') return 'active'
  if (a === 'planned' || b === 'planned') return 'planned'
  return 'unknown'
}

/** Unified platform + trade mission launch phases. */
function resolveMissionLaunchPhase(phaseId: string, input: TaskPhaseStatusInput): TaskPhaseStatus {
  const supply = input.supplyChain
  const platformDeploy = runStepStatus(input.platformStgRun)
  const platformGate = gateStepStatus(input.platformStgGate)
  const tradeDeploy = runStepStatus(input.tradeStgRun)
  const tradeGate = gateStepStatus(input.tradeStgGate)

  switch (phaseId) {
    case 'supply-chain': {
      if (supply == null) return 'unknown'
      const cms = supply.dockerfile_configmaps ?? []
      const ready = cms.length > 0 && cms.every(cm => cm.present)
      const mirrorsOk = supply.mirror_credentials_configured
      return ready && mirrorsOk ? 'done' : 'active'
    }
    case 'deploy-stg': {
      // Both must succeed; if only one finished, stay active (other still pending).
      const platform = platformDeploy.status === 'error' ? 'blocked' as const
        : platformDeploy.status === 'done' ? 'done' as const
          : platformDeploy.status === 'active' ? 'active' as const
            : 'planned' as const
      const trade = tradeDeploy.status === 'error' ? 'blocked' as const
        : tradeDeploy.status === 'done' ? 'done' as const
          : tradeDeploy.status === 'active' ? 'active' as const
            : 'planned' as const
      return combineDualStatus(platform, trade)
    }
    case 'stg-gate': {
      if (platformDeploy.status !== 'done' || tradeDeploy.status !== 'done') {
        return platformDeploy.status === 'done' || tradeDeploy.status === 'done' ? 'blocked' : 'planned'
      }
      const platform = platformGate.status === 'done' ? 'done' as const
        : platformGate.status === 'active' ? 'active' as const
          : 'planned' as const
      const trade = tradeGate.status === 'done' ? 'done' as const
        : tradeGate.status === 'active' ? 'active' as const
          : 'planned' as const
      return combineDualStatus(platform, trade)
    }
    case 'deploy-prod': {
      if (platformGate.status !== 'done' || tradeGate.status !== 'done') return 'blocked'
      if (input.platformProdGate?.result === 'pass') return 'done'
      return 'active'
    }
    case 'prod-gate': {
      const platformProdOk = input.platformProdGate?.result === 'pass'
      const missionOk = input.snapshot?.missionOverall === 'ok'
      if (platformProdOk && missionOk) return 'done'
      if (platformProdOk || missionOk) return 'active'
      return 'planned'
    }
    default:
      return 'unknown'
  }
}

/** Ground Build deliver-stg: Board/program progress — not Tekton pipeline runs (F3). */
function resolveGroundDeliverStg(input: TaskPhaseStatusInput): TaskPhaseStatus {
  const program = input.programDetail
  if (program == null) return 'planned'
  const signed = program.program.phases_signed ?? program.program.signed ?? 0
  const phasesDone = program.program.phases_done ?? 0
  if (signed > 0 || phasesDone > 0 || program.program.complete === true) return 'done'
  if (program.active === true || program.program.active === true) return 'active'
  return 'planned'
}

function resolveDevBuildPhase(
  modeId: TaskModeId,
  phaseId: string,
  input: TaskPhaseStatusInput,
): TaskPhaseStatus {
  const program = input.programDetail
  switch (phaseId) {
    case 'briefing':
      return input.briefingOpened === true ? 'done' : 'planned'
    case 'implement': {
      if (input.devAgentPhaseDone?.('implement') === true) return 'done'
      return program?.active === true ? 'active' : 'planned'
    }
    case 'pre-push':
      return input.devAgentPhaseDone?.('pre-push') === true ? 'done' : 'planned'
    case 'deliver-stg': {
      if (modeId === 'ground-build') return resolveGroundDeliverStg(input)
      const run = input.tradeStgRun ?? input.platformStgRun
      if (run != null && isPipelineRunSucceeded(run)) return 'done'
      return run != null ? 'active' : 'planned'
    }
    case 'sign-off': {
      if (program == null) return 'planned'
      const signed = program.program.phases_signed ?? program.program.signed ?? 0
      const total = program.program.phase_count
      if (program.program.complete === true || signed >= total) return 'done'
      if (signed > 0) return 'active'
      return 'planned'
    }
    default:
      return 'unknown'
  }
}

function rawPhaseStatus(
  modeId: TaskModeId,
  phaseId: string,
  input: TaskPhaseStatusInput,
): TaskPhaseStatus {
  switch (modeId) {
    case 'daily-ops':
      return resolveDailyOpsPhase(phaseId, input)
    case 'mission-launch':
      return resolveMissionLaunchPhase(phaseId, input)
    case 'rocket-build':
    case 'satellite-build':
    case 'engineer-build':
    case 'ground-build':
    case 'plugin-build':
      return resolveDevBuildPhase(modeId, phaseId, input)
    default:
      return 'unknown'
  }
}

/** Resolve a single phase status with dependency-aware active promotion. */
export function resolveTaskPhaseStatus(
  modeId: TaskModeId,
  phaseId: string,
  input: TaskPhaseStatusInput,
): TaskPhaseStatus {
  const mode = taskModeById(modeId)
  const phases = mode.phases ?? []
  const phase = phases.find(p => p.id === phaseId)
  if (phase == null) return 'unknown'

  const statusOf = (id: string) => rawPhaseStatus(modeId, id, input)
  let status = statusOf(phaseId)

  const priorsDone = priorPhasesDone(phase, statusOf)

  if (status === 'planned' && priorsDone) {
    const activeId = firstIncompletePhase(phases, statusOf)
    if (activeId === phaseId) status = 'active'
  }

  if (!priorsDone) {
    if (status === 'active') status = 'blocked'
    if (status === 'done') status = 'planned'
  }

  return status
}

/** Resolve all phases for a mode — returns map phaseId → status.
 *  Phases are resolved in order so that sequential clamping
 *  (a later phase cannot be "done" if a prior phase is not "done")
 *  uses resolved values of dependencies, not raw values.
 */
export function resolveAllTaskPhaseStatuses(
  modeId: TaskModeId,
  input: TaskPhaseStatusInput,
): Record<string, TaskPhaseStatus> {
  const mode = taskModeById(modeId)
  const phases = mode.phases ?? []
  const out: Record<string, TaskPhaseStatus> = {}
  const rawOf = (id: string) => rawPhaseStatus(modeId, id, input)

  for (const phase of phases) {
    let status = rawOf(phase.id)

    const resolvedOf = (id: string) => out[id] ?? rawOf(id)
    const priorsDone = phase.dependsOn == null || phase.dependsOn.length === 0
      || phase.dependsOn.every(depId => resolvedOf(depId) === 'done')

    if (status === 'planned' && priorsDone) {
      const activeId = firstIncompletePhase(phases, rawOf)
      if (activeId === phase.id) status = 'active'
    }

    if (!priorsDone) {
      if (status === 'active') status = 'blocked'
      if (status === 'done') status = 'planned'
    }

    out[phase.id] = status
  }
  return out
}
