import type {
  BuildTrack,
  MatrixResponse,
  MigrateTrack,
  OpsContextResponse,
  OpsContextTracks,
  Reachability,
  TrackTask,
} from '@/api/types'
import { hasProdFailures, prodFailingTargetIds } from '@/lib/control-room/matrixSummary'

export type TrackId = 'build' | 'migrate' | 'operate'

export interface TrackProgress {
  done: number
  total: number
  percent: number
}

export interface OperateIssue {
  kind: 'matrix_fail' | 'cluster_failing' | 'promote_blocked' | 'gate_missing'
  label: string
}

export interface TrackSummary {
  id: TrackId
  label: string
  agentMode: 'Ops' | 'Product' | 'Promote'
  progress: TrackProgress | null
  currentPhase: string | null
  nextStep: string | null
  issues: OperateIssue[]
  subtitle: string
}

export function computeBuildSummary(build?: BuildTrack): Omit<TrackSummary, 'id' | 'agentMode'> {
  if (build == null) {
    return { label: 'Build', progress: null, currentPhase: null, nextStep: null, issues: [], subtitle: 'No track data' }
  }

  const done = build.tasks.filter(t => t.status === 'done').length
  const total = build.tasks.length
  const percent = total > 0 ? Math.round((done / total) * 100) : 0

  const nextTask = build.tasks.find(t => t.status === 'next') ?? build.tasks.find(t => t.status === 'in_progress')

  return {
    label: build.label,
    progress: { done, total, percent },
    currentPhase: build.current_phase,
    nextStep: nextTask?.label ?? null,
    issues: [],
    subtitle: `Phase ${build.current_phase} · ${done}/${total} tasks`,
  }
}

export function computeMigrateSummary(migrate?: MigrateTrack): Omit<TrackSummary, 'id' | 'agentMode'> {
  if (migrate == null) {
    return { label: 'Migrate', progress: null, currentPhase: null, nextStep: null, issues: [], subtitle: 'No track data' }
  }

  let totalDone = 0
  let totalAll = 0
  for (const s of migrate.streams) {
    totalDone += s.done
    totalAll += s.total
  }
  const percent = totalAll > 0 ? Math.round((totalDone / totalAll) * 100) : 0

  const activeStream = migrate.streams.find(s => s.status === 'in_progress')
  const nextStep = activeStream?.next_task ?? null

  const closedCount = migrate.streams.filter(s => s.status === 'closed').length
  const subtitle = `${closedCount}/${migrate.streams.length} streams closed · ${totalDone}/${totalAll} items`

  return {
    label: migrate.label,
    progress: { done: totalDone, total: totalAll, percent },
    currentPhase: null,
    nextStep,
    issues: [],
    subtitle,
  }
}

export function computeOperateSummary(
  context: OpsContextResponse | undefined,
  matrices: MatrixResponse[],
  clusterFailingPods?: number,
  clusterReachability?: Reachability,
): Omit<TrackSummary, 'id' | 'agentMode'> {
  const issues: OperateIssue[] = []

  if (matrices.length > 0 && hasProdFailures(matrices)) {
    const fails = prodFailingTargetIds(matrices)
    issues.push({
      kind: 'matrix_fail',
      label: `Prod matrix: ${fails.length} target${fails.length > 1 ? 's' : ''} failing (${fails.slice(0, 3).join(', ')}${fails.length > 3 ? '...' : ''})`,
    })
  }

  if (clusterFailingPods != null && clusterFailingPods > 0) {
    issues.push({
      kind: 'cluster_failing',
      label: `Cluster: ${clusterFailingPods} failing pod${clusterFailingPods > 1 ? 's' : ''}`,
    })
  }

  if (clusterReachability === 'fail') {
    issues.push({
      kind: 'cluster_failing',
      label: 'Cluster: unreachable',
    })
  }

  if (context != null) {
    const cutover = context.milestones.find(m => m.id === '2c-b-prod-cutover')
    if (cutover?.status === 'BLOCKED_ON') {
      issues.push({
        kind: 'promote_blocked',
        label: `Promote blocked: ${cutover.blocker ?? 'decision'}`,
      })
    }

    const gate = context.promotion.last_gate
    if (gate.result == null || gate.result === '') {
      issues.push({
        kind: 'gate_missing',
        label: 'Release gate: not recorded',
      })
    }
  }

  const subtitle = issues.length > 0
    ? `${issues.length} issue${issues.length > 1 ? 's' : ''} need attention`
    : 'All clear — no issues detected'

  return {
    label: 'Day-to-day operations',
    progress: null,
    currentPhase: null,
    nextStep: issues.length > 0 ? issues[0].label : null,
    issues,
    subtitle,
  }
}

export function computeAllTracks(
  context: OpsContextResponse | undefined,
  matrices: MatrixResponse[],
  clusterFailingPods?: number,
  clusterReachability?: Reachability,
): TrackSummary[] {
  const tracks: OpsContextTracks | undefined = context?.tracks

  const build: TrackSummary = {
    id: 'build',
    agentMode: 'Ops',
    ...computeBuildSummary(tracks?.build),
  }

  const migrate: TrackSummary = {
    id: 'migrate',
    agentMode: 'Ops',
    ...computeMigrateSummary(tracks?.migrate),
  }

  const operate: TrackSummary = {
    id: 'operate',
    agentMode: 'Ops',
    ...computeOperateSummary(context, matrices, clusterFailingPods, clusterReachability),
  }

  return [build, migrate, operate]
}

export function trackById(tracks: TrackSummary[], id: TrackId): TrackSummary {
  return tracks.find(t => t.id === id) ?? tracks[0]
}

export function findNextTask(tasks: TrackTask[]): TrackTask | undefined {
  return tasks.find(t => t.status === 'next') ?? tasks.find(t => t.status === 'in_progress')
}
