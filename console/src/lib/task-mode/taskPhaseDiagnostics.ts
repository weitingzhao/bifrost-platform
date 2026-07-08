import type { MissionSnapshot } from '@/lib/control-room/missionSignals'
import { buildDiagnosticPrompt, missionStatus } from '@/lib/control-room/missionSignals'
import type { TaskPhaseStatusInput } from '@/lib/task-mode/navLens'
import { resolveTaskPhaseStatus } from '@/lib/task-mode/navLens'
import { taskModeById } from '@/lib/task-mode/taskModeCatalog'
import type { TaskModeId, TaskPhaseDef, TaskPhaseStatus } from '@/lib/task-mode/types'

export type TaskPhaseFixAction = {
  label: string
  tabId?: string
  kind: 'navigate' | 'agent-fix'
}

export type TaskPhaseHint = {
  reason: string
  rootCauses: string[]
  fixActions: TaskPhaseFixAction[]
}

function missionIssueLines(snap: MissionSnapshot | undefined): string[] {
  if (snap == null) return ['Mission snapshot still probing — wait for matrix + cluster probes.']
  const lines: string[] = []
  const rocket: Array<[string, MissionSnapshot['infra']]> = [
    ['Infra', snap.infra],
    ['Release', snap.release],
    ['Control', snap.control],
    ['Agent', snap.agent],
  ]
  for (const [name, state] of rocket) {
    if (state.signal !== 'ok') lines.push(`${name}: ${state.detail} (${state.signal})`)
  }
  if (snap.tradeDev.signal !== 'ok') {
    lines.push(`Trade dev: ${snap.tradeDev.detail} (${snap.tradeDev.signal})`)
  }
  if (snap.tradeProd.signal !== 'ok') {
    lines.push(`Trade prod: ${snap.tradeProd.detail} (${snap.tradeProd.signal})`)
  }
  if (lines.length === 0) {
    lines.push(`Mission overall ${missionStatus(snap.missionOverall)} — review Control Room signals.`)
  }
  return lines
}

function dependencyBlockReason(
  modeId: TaskModeId,
  phase: TaskPhaseDef,
  input: TaskPhaseStatusInput,
): string | null {
  if (phase.dependsOn == null || phase.dependsOn.length === 0) return null
  const mode = taskModeById(modeId)
  for (const depId of phase.dependsOn) {
    const dep = mode.phases?.find(p => p.id === depId)
    const depStatus = resolveTaskPhaseStatus(modeId, depId, input)
    if (depStatus !== 'done') {
      return `Waiting for "${dep?.title ?? depId}" (${depStatus}) before this step can run.`
    }
  }
  return null
}

function dailyOpsFixActions(phaseId: string, rootCauses: string[]): TaskPhaseFixAction[] {
  const actions: TaskPhaseFixAction[] = [
    { label: 'Open Control Room', tabId: 'control-room', kind: 'navigate' },
  ]
  if (phaseId === 'scan-signals' || phaseId === 'verify-mission') {
    actions.push({ label: 'Agent Fix', kind: 'agent-fix' })
    actions.push({ label: 'Runtime Map', tabId: 'runtime-map', kind: 'navigate' })
    actions.push({ label: 'Cluster', tabId: 'cluster', kind: 'navigate' })
  }
  if (phaseId === 'triage-defects') {
    actions.push({ label: 'Open Defects', tabId: 'defects', kind: 'navigate' })
  }
  if (phaseId === 'operate-queue') {
    actions.push({ label: 'Control Room queue', tabId: 'control-room', kind: 'navigate' })
  }
  if (rootCauses.some(c => c.toLowerCase().includes('trade prod'))) {
    actions.push({ label: 'Satellite Bus', tabId: 'satellite-bus', kind: 'navigate' })
  }
  if (rootCauses.some(c => c.toLowerCase().includes('release'))) {
    actions.push({ label: 'Platform Release', tabId: 'platform-release', kind: 'navigate' })
  }
  return actions
}

export function explainTaskPhase(
  modeId: TaskModeId,
  phase: TaskPhaseDef,
  status: TaskPhaseStatus,
  input: TaskPhaseStatusInput,
): TaskPhaseHint | null {
  if (status === 'done' || status === 'unknown') return null

  const snap = input.snapshot
  const depReason = dependencyBlockReason(modeId, phase, input)

  if (modeId === 'daily-ops') {
    const rootCauses = missionIssueLines(snap)
    const missionLine =
      snap != null
        ? `Mission ${missionStatus(snap.missionOverall)} — rocket ${missionStatus(snap.rocketOverall)}, payload ${missionStatus(snap.payloadOverall)}.`
        : 'Mission snapshot probing.'

    switch (phase.id) {
      case 'scan-signals': {
        if (status === 'blocked') {
          return {
            reason: `Scan blocked: mission signals CRITICAL. ${missionLine}`,
            rootCauses,
            fixActions: dailyOpsFixActions(phase.id, rootCauses),
          }
        }
        if (status === 'active') {
          return {
            reason: `Mission not NOMINAL yet. ${missionLine}`,
            rootCauses,
            fixActions: dailyOpsFixActions(phase.id, rootCauses),
          }
        }
        break
      }
      case 'triage-defects':
      case 'operate-queue':
      case 'verify-mission': {
        if (status === 'blocked' && depReason != null) {
          return {
            reason: depReason,
            rootCauses: phase.id === 'verify-mission' ? rootCauses : [depReason],
            fixActions: dailyOpsFixActions('scan-signals', rootCauses),
          }
        }
        if (phase.id === 'operate-queue' && status === 'active') {
          const open = input.operateQueueOpenCount ?? 0
          return {
            reason: `${open} open operate-queue item${open === 1 ? '' : 's'} need triage or closure in Control Room.`,
            rootCauses: [`Operate queue: ${open} open`],
            fixActions: dailyOpsFixActions(phase.id, []),
          }
        }
        if (phase.id === 'triage-defects' && status === 'active') {
          return {
            reason: 'Mission CAUTION/CRITICAL — triage open defects before closing the queue.',
            rootCauses,
            fixActions: dailyOpsFixActions(phase.id, rootCauses),
          }
        }
        if (phase.id === 'verify-mission' && status === 'planned') {
          return {
            reason: 'Complete prior steps, then re-probe mission snapshot.',
            rootCauses: snap?.missionOverall === 'ok' ? [] : rootCauses,
            fixActions: dailyOpsFixActions(phase.id, rootCauses),
          }
        }
        break
      }
    }
  }

  if (status === 'blocked' && depReason != null) {
    return {
      reason: depReason,
      rootCauses: [depReason],
      fixActions:
        phase.navigateTab != null
          ? [{ label: 'Open related page', tabId: phase.navigateTab, kind: 'navigate' }]
          : [],
    }
  }

  return null
}

export function buildDailyOpsMissionFixPrompt(snap: MissionSnapshot | undefined): string | null {
  if (snap == null) return null
  return buildDiagnosticPrompt(snap)
}

export function buildPhaseHints(
  modeId: TaskModeId,
  phases: TaskPhaseDef[],
  statuses: Record<string, TaskPhaseStatus>,
  input: TaskPhaseStatusInput,
): Record<string, TaskPhaseHint> {
  const out: Record<string, TaskPhaseHint> = {}
  for (const phase of phases) {
    const status = statuses[phase.id] ?? 'unknown'
    const hint = explainTaskPhase(modeId, phase, status, input)
    if (hint != null) out[phase.id] = hint
  }
  return out
}
