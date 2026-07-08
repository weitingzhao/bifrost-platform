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

function resolveRocketLaunchPhase(phaseId: string, input: TaskPhaseStatusInput): TaskPhaseStatus {
  const supply = input.supplyChain
  const deploy = runStepStatus(input.platformStgRun)
  const gate = gateStepStatus(input.platformStgGate)

  switch (phaseId) {
    case 'supply-chain': {
      if (supply == null) return 'unknown'
      const cms = supply.dockerfile_configmaps ?? []
      const ready = cms.length > 0 && cms.every(cm => cm.present)
      const mirrorsOk = supply.mirror_credentials_configured
      return ready && mirrorsOk ? 'done' : 'active'
    }
    case 'deliver-platform-stg':
      if (deploy.status === 'done') return 'done'
      if (deploy.status === 'active') return 'active'
      if (deploy.status === 'error') return 'blocked'
      return 'planned'
    case 'platform-stg-gate':
      if (gate.status === 'done') return 'done'
      if (deploy.status !== 'done') return 'blocked'
      return gate.status === 'active' ? 'active' : 'planned'
    case 'deliver-platform-prod':
      if (input.platformProdGate?.result === 'pass') return 'done'
      if (gate.status !== 'done') return 'blocked'
      return 'active'
    case 'platform-prod-gate':
      return input.platformProdGate?.result === 'pass' ? 'done' : 'planned'
    default:
      return 'unknown'
  }
}

function resolveSatelliteDeployPhase(phaseId: string, input: TaskPhaseStatusInput): TaskPhaseStatus {
  const spinePhase = input.stgReleasePhases?.find(p => p.id === phaseId)
  if (spinePhase != null) {
    switch (spinePhase.status) {
      case 'done':
        return 'done'
      case 'active':
        return 'active'
      case 'blocked':
        return 'blocked'
      default:
        return 'planned'
    }
  }

  const tradeDeploy = runStepStatus(input.tradeStgRun)
  const tradeGate = gateStepStatus(input.tradeStgGate)
  switch (phaseId) {
    case 'deliver-stg':
      if (tradeDeploy.status === 'done') return 'done'
      if (tradeDeploy.status === 'active') return 'active'
      return 'planned'
    case 'verify-stg':
      if (input.tradeStgSmokeOk === true) return 'done'
      if (tradeDeploy.status === 'done') return 'active'
      return 'planned'
    case 'stg-gate':
      return tradeGate.status === 'done' ? 'done' : tradeDeploy.status === 'done' ? 'active' : 'planned'
    default:
      return 'unknown'
  }
}

function resolveDevBuildPhase(phaseId: string, input: TaskPhaseStatusInput): TaskPhaseStatus {
  const program = input.programDetail
  switch (phaseId) {
    case 'briefing':
      return 'done'
    case 'implement': {
      if (input.devAgentPhaseDone?.('implement') === true) return 'done'
      return program?.active === true ? 'active' : 'planned'
    }
    case 'pre-push':
      return input.devAgentPhaseDone?.('pre-push') === true ? 'done' : 'planned'
    case 'deliver-stg': {
      const run = input.tradeStgRun ?? input.platformStgRun
      if (run != null && isPipelineRunSucceeded(run)) return 'done'
      return run != null ? 'active' : 'planned'
    }
    case 'sign-off': {
      if (program == null) return 'unknown'
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
    case 'rocket-launch':
      return resolveRocketLaunchPhase(phaseId, input)
    case 'satellite-deploy':
      return resolveSatelliteDeployPhase(phaseId, input)
    case 'rocket-build':
    case 'satellite-build':
      return resolveDevBuildPhase(phaseId, input)
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

  if (status === 'planned' && priorPhasesDone(phase, statusOf)) {
    const activeId = firstIncompletePhase(phases, statusOf)
    if (activeId === phaseId) status = 'active'
  }

  if (status === 'active' && !priorPhasesDone(phase, statusOf)) {
    status = 'blocked'
  }

  return status
}

/** Resolve all phases for a mode — returns map phaseId → status. */
export function resolveAllTaskPhaseStatuses(
  modeId: TaskModeId,
  input: TaskPhaseStatusInput,
): Record<string, TaskPhaseStatus> {
  const mode = taskModeById(modeId)
  const out: Record<string, TaskPhaseStatus> = {}
  for (const p of mode.phases ?? []) {
    out[p.id] = resolveTaskPhaseStatus(modeId, p.id, input)
  }
  return out
}
