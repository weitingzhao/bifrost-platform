import type {
  BuildTrack,
  ClusterSummary,
  MatrixResponse,
  MigrateStream,
  OpsContextResponse,
} from '@/api/types'
import {
  hasProdFailures,
  prodFailingTargetIds,
} from '@/lib/control-room/matrixSummary'
import type { WorkIntent } from '@/lib/briefing/workIntents'
import type { TrackId } from '@/lib/briefing/workTracks'

export type BuildLaneId = 'console-api' | 'cluster-infra' | 'mcp-gitops'
export type MigrateLaneId = 'compose-k3s' | 'legacy-retire' | 'trade-stack'
export type OperateLaneId = 'governance' | 'troubleshoot' | 'release'
export type LaneId = BuildLaneId | MigrateLaneId | OperateLaneId

export type QueueItemStatus =
  | 'done'
  | 'in_progress'
  | 'next'
  | 'pending'
  | 'blocked'
  | 'issue'
  | 'closed'

export interface QueueItem {
  id: string
  label: string
  status: QueueItemStatus
  note?: string
}

export interface WorkLane {
  id: LaneId
  track: TrackId
  label: string
  shortLabel: string
  description: string
  agentMode: 'Ops' | 'Product' | 'Promote'
  workIntent: WorkIntent
}

const BUILD_LANES: WorkLane[] = [
  {
    id: 'console-api',
    track: 'build',
    label: 'Console & platform-api',
    shortLabel: 'Console',
    description: 'Ops Console pages, Briefing/Control Room, auth, audit, actuation routes.',
    agentMode: 'Ops',
    workIntent: 'feature',
  },
  {
    id: 'cluster-infra',
    track: 'build',
    label: 'Cluster & K3s tooling',
    shortLabel: 'Cluster',
    description: 'Node lifecycle, workload actuation, metrics-server, observability stack UI.',
    agentMode: 'Ops',
    workIntent: 'cluster',
  },
  {
    id: 'mcp-gitops',
    track: 'build',
    label: 'GitOps & MCP (P3–P5)',
    shortLabel: 'GitOps',
    description: 'Argo/Tekton execution, stack install wizard, MCP actuation Tools.',
    agentMode: 'Ops',
    workIntent: 'feature',
  },
]

const MIGRATE_LANES: WorkLane[] = [
  {
    id: 'compose-k3s',
    track: 'migrate',
    label: 'Compose → K3s deployment',
    shortLabel: 'K3s',
    description: 'Move trade stack from Docker Compose to K3s bifrost-prod namespace.',
    agentMode: 'Ops',
    workIntent: 'ops',
  },
  {
    id: 'legacy-retire',
    track: 'migrate',
    label: 'Legacy retirement (Phase 3)',
    shortLabel: 'Retire',
    description: 'UI experience alignment (Design System polish with Legacy side-by-side, Owner sign-off required) → then shut down bifrost-trader-engine and compose prod after K3s is stable.',
    agentMode: 'Ops',
    workIntent: 'ops',
  },
  {
    id: 'trade-stack',
    track: 'migrate',
    label: 'Trade stack verification',
    shortLabel: 'Verify',
    description: 'Backend repos + frontend pages — reference closed streams for regression.',
    agentMode: 'Product',
    workIntent: 'frontend',
  },
]

const OPERATE_LANES: WorkLane[] = [
  {
    id: 'governance',
    track: 'operate',
    label: 'Governance & spine',
    shortLabel: 'Gov',
    description: 'Matrix probes, spine milestones, infra YAML, actuation guardrails.',
    agentMode: 'Ops',
    workIntent: 'ops',
  },
  {
    id: 'troubleshoot',
    track: 'operate',
    label: 'Troubleshooting',
    shortLabel: 'Debug',
    description: 'Failing probes, cluster reachability, workload errors, connectivity.',
    agentMode: 'Ops',
    workIntent: 'debug',
  },
  {
    id: 'release',
    track: 'operate',
    label: 'Release & promote',
    shortLabel: 'Release',
    description: 'Flywheel readiness, cutover blockers, release gate, prod matrix sign-off.',
    agentMode: 'Promote',
    workIntent: 'release',
  },
]

const ALL_LANES: WorkLane[] = [...BUILD_LANES, ...MIGRATE_LANES, ...OPERATE_LANES]

const BUILD_TASK_LANE: Record<string, BuildLaneId> = {
  'p1-auth-audit': 'console-api',
  'p1-workload-actuation': 'cluster-infra',
  'p1-pod-logs': 'cluster-infra',
  'p1-session-loop': 'console-api',
  'p1-track-model': 'console-api',
  'p2-node-lifecycle': 'cluster-infra',
  'p2-cluster-wizard': 'cluster-infra',
  'p3-gitops-execution': 'mcp-gitops',
  'p4-stack-install': 'mcp-gitops',
  'p5-mcp-tools': 'mcp-gitops',
}

const MIGRATE_STREAM_LANE: Record<string, MigrateLaneId> = {
  'trade-backend': 'trade-stack',
  'trade-frontend': 'trade-stack',
  'compose-to-k3s': 'compose-k3s',
  'legacy-retirement': 'legacy-retire',
}

export function lanesForTrack(track: TrackId): WorkLane[] {
  switch (track) {
    case 'build':
      return BUILD_LANES
    case 'migrate':
      return MIGRATE_LANES
    case 'operate':
      return OPERATE_LANES
  }
}

export function laneById(id: LaneId): WorkLane {
  return ALL_LANES.find(l => l.id === id) ?? ALL_LANES[0]
}

export function defaultLaneForTrack(track: TrackId): LaneId {
  const lanes = lanesForTrack(track)
  return lanes[0]?.id ?? 'console-api'
}

function mapTaskStatus(status: string): QueueItemStatus {
  if (status === 'done') return 'done'
  if (status === 'in_progress') return 'in_progress'
  if (status === 'next') return 'next'
  if (status === 'blocked') return 'blocked'
  return 'pending'
}

function mapStreamStatus(status: string): QueueItemStatus {
  if (status === 'closed') return 'closed'
  if (status === 'in_progress') return 'in_progress'
  if (status === 'not_started') return 'pending'
  return 'pending'
}

function buildQueueFromBuildTasks(build: BuildTrack | undefined, laneId: BuildLaneId): QueueItem[] {
  if (build == null) return []
  return build.tasks
    .filter(t => BUILD_TASK_LANE[t.id] === laneId)
    .map(t => ({
      id: t.id,
      label: t.label,
      status: mapTaskStatus(t.status),
    }))
}

function streamToQueueItem(stream: MigrateStream): QueueItem {
  const status = mapStreamStatus(stream.status)
  const progress = `${stream.done}/${stream.total}`
  let label = stream.label
  if (status !== 'closed') {
    label = `${stream.label} (${progress})`
  }
  const note = stream.next_task ?? stream.note
  return {
    id: stream.id,
    label,
    status,
    note: note ?? undefined,
  }
}

function buildQueueFromMigrateStreams(
  migrate: { streams: MigrateStream[] } | undefined,
  laneId: MigrateLaneId,
): QueueItem[] {
  if (migrate == null) return []
  return migrate.streams
    .filter(s => MIGRATE_STREAM_LANE[s.id] === laneId)
    .map(streamToQueueItem)
}

function buildGovernanceQueue(context: OpsContextResponse | undefined): QueueItem[] {
  if (context == null) return []
  const items: QueueItem[] = []

  for (const m of context.milestones) {
    if (m.status === 'IN_PROGRESS' || m.status === 'BLOCKED_ON') {
      items.push({
        id: `milestone-${m.id}`,
        label: m.label ?? m.id,
        status: m.status === 'BLOCKED_ON' ? 'blocked' : 'in_progress',
        note: m.blocker ?? undefined,
      })
    }
  }

  if (context.focus.blocker) {
    items.push({
      id: 'focus-blocker',
      label: `Spine blocker: ${context.focus.blocker}`,
      status: 'blocked',
    })
  }

  return items
}

function buildTroubleshootQueue(
  matrices: MatrixResponse[],
  clusterSummary: ClusterSummary | undefined,
): QueueItem[] {
  const items: QueueItem[] = []

  if (hasProdFailures(matrices)) {
    for (const id of prodFailingTargetIds(matrices)) {
      items.push({
        id: `matrix-${id}`,
        label: `Prod probe failing: ${id}`,
        status: 'issue',
      })
    }
  }

  const devMatrix = matrices.find(m => m.environment === 'dev')
  if (devMatrix != null) {
    for (const t of devMatrix.targets) {
      if (t.reachability === 'fail') {
        items.push({
          id: `dev-matrix-${t.id}`,
          label: `Dev probe failing: ${t.id}`,
          status: 'issue',
        })
      }
    }
  }

  if (clusterSummary?.reachability === 'fail') {
    items.push({
      id: 'cluster-reach',
      label: 'Cluster unreachable',
      status: 'issue',
      note: clusterSummary.detail,
    })
  }

  if (clusterSummary != null && clusterSummary.failing_pods > 0) {
    items.push({
      id: 'cluster-pods',
      label: `${clusterSummary.failing_pods} failing pod(s)`,
      status: 'issue',
    })
  }

  if (items.length === 0) {
    items.push({
      id: 'all-clear',
      label: 'No active issues — matrix and cluster healthy',
      status: 'done',
    })
  }

  return items
}

function buildReleaseQueue(context: OpsContextResponse | undefined, matrices: MatrixResponse[]): QueueItem[] {
  const items: QueueItem[] = []

  const cutover = context?.milestones.find(m => m.id === '2c-b-prod-cutover')
  if (cutover != null) {
    items.push({
      id: 'prod-cutover',
      label: cutover.label ?? cutover.id,
      status: cutover.status === 'BLOCKED_ON' ? 'blocked' : mapStreamStatus(cutover.status.toLowerCase()),
      note: cutover.blocker ?? undefined,
    })
  }

  const gate = context?.promotion.last_gate
  if (gate != null) {
    const gateOk = gate.result != null && gate.result !== ''
    items.push({
      id: 'release-gate',
      label: 'Release gate recorded',
      status: gateOk ? 'done' : 'pending',
      note: gateOk ? `Result: ${gate.result}` : gate.log_path,
    })
  }

  if (hasProdFailures(matrices)) {
    items.push({
      id: 'prod-matrix-signoff',
      label: 'Prod matrix sign-off',
      status: 'blocked',
      note: `${prodFailingTargetIds(matrices).length} failing target(s)`,
    })
  } else if (matrices.some(m => m.environment === 'prod')) {
    items.push({
      id: 'prod-matrix-signoff',
      label: 'Prod matrix sign-off',
      status: 'done',
    })
  }

  const legacy = context?.milestones.find(m => m.id === 'legacy-retirement')
  if (legacy != null) {
    items.push({
      id: 'legacy-retirement',
      label: legacy.label ?? legacy.id,
      status: legacy.status === 'NOT_STARTED' ? 'pending' : 'in_progress',
    })
  }

  return items
}

export function buildQueueForLane(
  laneId: LaneId,
  context: OpsContextResponse | undefined,
  matrices: MatrixResponse[],
  clusterSummary: ClusterSummary | undefined,
): QueueItem[] {
  const lane = laneById(laneId)
  const tracks = context?.tracks

  switch (lane.track) {
    case 'build':
      return buildQueueFromBuildTasks(tracks?.build, laneId as BuildLaneId)
    case 'migrate':
      return buildQueueFromMigrateStreams(tracks?.migrate, laneId as MigrateLaneId)
    case 'operate':
      switch (laneId as OperateLaneId) {
        case 'governance':
          return buildGovernanceQueue(context)
        case 'troubleshoot':
          return buildTroubleshootQueue(matrices, clusterSummary)
        case 'release':
          return buildReleaseQueue(context, matrices)
      }
  }
  return []
}

export function queueProgress(items: QueueItem[]): { done: number; total: number; percent: number } | null {
  if (items.length === 0) return null
  const countable = items.filter(i => i.status !== 'issue')
  if (countable.length === 0) return null
  const done = countable.filter(i => i.status === 'done' || i.status === 'closed').length
  const total = countable.length
  return { done, total, percent: Math.round((done / total) * 100) }
}
