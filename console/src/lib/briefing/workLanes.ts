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
import { visionGovernanceQueueItems } from '@/lib/architecture/visionSpineMap'
import {
  TRADE_K8S_NATIVE_MIGRATE_STREAM_ID,
  TRADE_K8S_NATIVE_WAVES,
} from '@/lib/architecture/tradeK8sNativeCatalog'

export type BuildLaneId = 'console-api' | 'cluster-infra' | 'mcp-gitops' | 'cicd-delivery'
export type MigrateLaneId = 'compose-k3s' | 'trade-k8s-native' | 'data-layer-k3s' | 'legacy-retire' | 'trade-stack'
export type AutomateLaneId = 'platform-gitops' | 'agent-infra' | 'drift-remediation' | 'agent-services'
export type InfraLaneId = 'network-server' | 'network-wifi' | 'ai-network'
export type OperateLaneId = 'governance' | 'troubleshoot' | 'release' | 'business-advisory'
export type LaneId = BuildLaneId | MigrateLaneId | AutomateLaneId | InfraLaneId | OperateLaneId

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
  /** Milestone progress: done / total (from spine stream). */
  progress?: { done: number; total: number }
  /** Prerequisites from spine stream (human-readable conditions). */
  prerequisites?: string[]
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
    description: 'Argo/Tekton execution, MCP server implementation (per MCP Contract), Agent SDK integration, Vision milestones V1–V5.',
    agentMode: 'Ops',
    workIntent: 'feature',
  },
  {
    id: 'cicd-delivery',
    track: 'build',
    label: 'CI/CD bootstrap (P6)',
    shortLabel: 'CI/CD',
    description: 'L0/L1/L2 bootstrap model — CI gates (Tekton Trigger), deliver-prod pipeline, platform prod overlay, self-health probe, gate→spine closure, escape hatch runbook.',
    agentMode: 'Ops',
    workIntent: 'feature',
  },
]

const MIGRATE_LANES: WorkLane[] = [
  {
    id: 'compose-k3s',
    track: 'migrate',
    label: 'Compose → K3s lift-and-shift',
    shortLabel: 'K3s',
    description: 'STG v2 + prod overlay on K3s (CLOSED). Native refactor → trade-k8s-native lane.',
    agentMode: 'Ops',
    workIntent: 'ops',
  },
  {
    id: 'trade-k8s-native',
    track: 'migrate',
    label: 'Trade K8s-native + IB Edge',
    shortLabel: 'K8s native',
    description:
      'Ideal K8s runtime: Traefik Ingress, Ops kubernetes executor, IB Lease active-standby, 3 gateways/env, dev mock. Authority: tradeK8sNativeCatalog.ts.',
    agentMode: 'Ops',
    workIntent: 'cluster',
  },
  {
    id: 'data-layer-k3s',
    track: 'migrate',
    label: 'Data layer (PG + Redis)',
    shortLabel: 'Data',
    description:
      'Lift stateful services to data NS: CloudNativePG (R-DV1 DBs), redis-live/queue per env, NAS backups. Spine stream data-layer-k3s.',
    agentMode: 'Ops',
    workIntent: 'cluster',
  },
  {
    id: 'legacy-retire',
    track: 'migrate',
    label: 'Legacy retirement (Phase 3)',
    shortLabel: 'Retire',
    description: 'SIGNED 2026-06-29 (decision D8): UI side-by-side gate dropped (Legacy already stopped; Phase 2B 9/9 domains business-equivalent). Legacy runtime stopped, bifrost-trader-engine NAS-archived read-only. UI polish continues as ordinary Design System work.',
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

const AUTOMATE_LANES: WorkLane[] = [
  {
    id: 'platform-gitops',
    track: 'automate',
    label: 'Platform GitOps & containerization',
    shortLabel: 'GitOps',
    description: 'Gitea mirror for bifrost-platform, Dockerfile (api + runner), K8s overlay, Argo CD Application, Tekton deliver-platform pipeline. Enables Mac Pro Cursor dev → Gitea → K3s auto-deploy.',
    agentMode: 'Ops',
    workIntent: 'automate',
  },
  {
    id: 'agent-infra',
    track: 'automate',
    label: 'Agent host & Gateway',
    shortLabel: 'Infra',
    description: 'Hermes Gateway on Mac Mini, CTRL NODE Bridge, Staleguard/ctxharness tooling, iMessage/Telegram channel.',
    agentMode: 'Ops',
    workIntent: 'automate',
  },
  {
    id: 'drift-remediation',
    track: 'automate',
    label: 'Drift detection & remediation',
    shortLabel: 'Drift',
    description: 'Nightly catalog drift scan, deterministic + LLM comparison, auto-fix PR, Owner morning briefing. Retrospective Agent: cross-job pattern analysis → identify systemic platform defects → self-evolving fix proposals.',
    agentMode: 'Ops',
    workIntent: 'automate',
  },
  {
    id: 'agent-services',
    track: 'automate',
    label: 'Agent services (MCP + Trade)',
    shortLabel: 'Services',
    description: 'Agent Desk in Console, Hermes MCP bridge, Trade advisory notifications.',
    agentMode: 'Ops',
    workIntent: 'automate',
  },
]

const INFRA_LANES: WorkLane[] = [
  {
    id: 'network-server',
    track: 'infra',
    label: 'Server LAN upgrade (router + switch)',
    shortLabel: 'Server LAN',
    description: 'UCG Max + USW-Pro-Max-24 deployment: VLAN 10/20/50, 2.5G uplinks, K3s node validation, kube-vip VIP.',
    agentMode: 'Ops',
    workIntent: 'ops',
  },
  {
    id: 'network-wifi',
    track: 'infra',
    label: 'WiFi upgrade (U7 Pro + U6 Mesh)',
    shortLabel: 'WiFi',
    description: 'Replace Eero with UniFi APs: per-floor rollout, MoCA backhaul, VLAN SSIDs (Bifrost/Home), coverage benchmark.',
    agentMode: 'Ops',
    workIntent: 'ops',
  },
  {
    id: 'ai-network',
    track: 'infra',
    label: 'AI network autonomy',
    shortLabel: 'AI Net',
    description: 'UniFi MCP Server (REST API read/write), Ops Console Network dashboard, anomaly auto-response, predictive maintenance, self-healing network.',
    agentMode: 'Ops',
    workIntent: 'automate',
  },
]

const OPERATE_LANES: WorkLane[] = [
  {
    id: 'governance',
    track: 'operate',
    label: 'Governance & spine',
    shortLabel: 'Gov',
    description: 'Matrix probes, spine milestones, infra YAML, actuation guardrails, Vision/Blueprint/MCP Contract alignment.',
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
  {
    id: 'business-advisory',
    track: 'operate',
    label: 'Trade analysis & advisory',
    shortLabel: 'Biz',
    description: 'Read-only portfolio/market/strategy analysis; Greeks monitoring, SEPA research, risk advisory via Trade APIs.',
    agentMode: 'Ops',
    workIntent: 'business',
  },
]

const ALL_LANES: WorkLane[] = [...BUILD_LANES, ...MIGRATE_LANES, ...AUTOMATE_LANES, ...INFRA_LANES, ...OPERATE_LANES]

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
  'p6-bootstrap-model': 'cicd-delivery',
  'p6-ci-gate-trade': 'cicd-delivery',
  'p6-ci-gate-platform': 'cicd-delivery',
  'p6-deliver-prod': 'cicd-delivery',
  'p6-platform-prod': 'cluster-infra',
  'p6-self-health': 'console-api',
  'p6-gate-spine-closure': 'console-api',
  'p6-escape-hatch': 'cluster-infra',
}

const MIGRATE_STREAM_LANE: Record<string, MigrateLaneId> = {
  'trade-backend': 'trade-stack',
  'trade-frontend': 'trade-stack',
  'compose-to-k3s': 'compose-k3s',
  'trade-k8s-native': 'trade-k8s-native',
  'data-layer-k3s': 'data-layer-k3s',
  'vision-v1-dev': 'compose-k3s',
  'vision-s3-briefing': 'compose-k3s',
  'vision-v2-dev-agent': 'compose-k3s',
  'vision-v3-ops-agent': 'compose-k3s',
  'vision-v4-business-agent': 'compose-k3s',
  'vision-v5-convergence': 'compose-k3s',
  'legacy-retirement': 'legacy-retire',
}

const AUTOMATE_STREAM_LANE: Record<string, AutomateLaneId> = {
  'platform-gitops': 'platform-gitops',
  'agent-infra-bootstrap': 'agent-infra',
  'nightly-drift-scan': 'drift-remediation',
  'release-agent-task': 'agent-services',
  'retrospective-agent': 'drift-remediation',
  'agent-mcp-integration': 'agent-services',
  'agent-trade-advisory': 'agent-services',
  'mission-auto-remediation': 'agent-services',
  'trade-k8s-migration': 'agent-services',
  'mutual-watchdog': 'agent-infra',
  'agent-release-discipline': 'agent-services',
  'hermes-gateway-integration': 'agent-infra',
  'flight-director-governance': 'agent-services',
}

const INFRA_STREAM_LANE: Record<string, InfraLaneId> = {
  'network-upgrade-core': 'network-server',
  'network-upgrade-wifi': 'network-wifi',
  'unifi-mcp-server': 'ai-network',
  'ai-home-network': 'ai-network',
}

export function lanesForTrack(track: TrackId): WorkLane[] {
  switch (track) {
    case 'build':
      return BUILD_LANES
    case 'migrate':
      return MIGRATE_LANES
    case 'automate':
      return AUTOMATE_LANES
    case 'infra':
      return INFRA_LANES
    case 'operate':
      return OPERATE_LANES
  }
}

export function laneById(id: LaneId): WorkLane {
  return ALL_LANES.find(l => l.id === id) ?? ALL_LANES[0]
}

export function defaultLaneForTrack(
  track: TrackId,
  context?: OpsContextResponse,
  matrices?: MatrixResponse[],
  clusterSummary?: ClusterSummary,
): LaneId {
  const lanes = lanesForTrack(track)
  const fallback = lanes[0]?.id ?? 'console-api'

  if (track === 'build' && context?.tracks?.build != null) {
    const activeTask =
      context.tracks.build.tasks.find(t => t.status === 'in_progress') ??
      context.tracks.build.tasks.find(t => t.status === 'next')
    const laneId = activeTask != null ? BUILD_TASK_LANE[activeTask.id] : undefined
    if (laneId != null) return laneId
  }

  if (track === 'migrate' && context?.tracks?.migrate != null) {
    const activeStream = context.tracks.migrate.streams.find(s => s.status === 'in_progress')
    const laneId = activeStream != null ? MIGRATE_STREAM_LANE[activeStream.id] : undefined
    if (laneId != null) return laneId
  }

  if (track === 'automate' && context?.tracks?.automate != null) {
    const activeStream = context.tracks.automate.streams.find(s => s.status === 'in_progress')
    const laneId = activeStream != null ? AUTOMATE_STREAM_LANE[activeStream.id] : undefined
    if (laneId != null) return laneId
  }

  if (track === 'infra' && context?.tracks?.infra != null) {
    const activeStream = context.tracks.infra.streams.find(s => s.status === 'in_progress')
    const laneId = activeStream != null ? INFRA_STREAM_LANE[activeStream.id] : undefined
    if (laneId != null) return laneId
  }

  if (track === 'operate') {
    const troubleshoot = buildTroubleshootQueue(matrices ?? [], clusterSummary)
    if (troubleshoot.some(i => i.status === 'issue')) return 'troubleshoot'
    if (context?.focus.blocker) return 'release'
    return 'governance'
  }

  return fallback
}

function mapTaskStatus(status: string): QueueItemStatus {
  if (status === 'done') return 'done'
  if (status === 'in_progress') return 'in_progress'
  if (status === 'next') return 'next'
  if (status === 'blocked') return 'blocked'
  return 'pending'
}

function mapStreamStatus(status: string): QueueItemStatus {
  const normalized = status.toLowerCase()
  if (normalized === 'closed' || normalized === 'signed') return 'closed'
  if (normalized === 'in_progress') return 'in_progress'
  if (normalized === 'blocked_on') return 'blocked'
  if (normalized === 'not_started') return 'pending'
  return 'pending'
}

function mapMilestoneStatus(status: string): QueueItemStatus {
  if (status === 'BLOCKED_ON') return 'blocked'
  if (status === 'IN_PROGRESS') return 'in_progress'
  if (status === 'CLOSED' || status === 'SIGNED') return 'closed'
  if (status === 'NOT_STARTED') return 'pending'
  return mapStreamStatus(status)
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
  const progressStr = `${stream.done}/${stream.total}`
  let label = stream.label
  if (status !== 'closed') {
    label = `${stream.label} (${progressStr})`
  }
  const note = stream.next_task ?? stream.note
  return {
    id: stream.id,
    label,
    status,
    note: note ?? undefined,
    progress: { done: stream.done, total: stream.total },
    prerequisites: stream.prerequisites,
  }
}

function buildTradeK8sNativeQueue(context: OpsContextResponse | undefined): QueueItem[] {
  const stream = context?.tracks?.migrate?.streams.find(s => s.id === TRADE_K8S_NATIVE_MIGRATE_STREAM_ID)
  const doneCount = stream?.done ?? 0

  return TRADE_K8S_NATIVE_WAVES.map((wave, index) => {
    let status: QueueItemStatus = 'pending'
    if (index < doneCount) status = 'done'
    else if (index === doneCount && stream?.status === 'in_progress') status = 'next'
    else if (index === doneCount && stream?.status === 'closed') status = 'done'

    // Delivered-but-unsigned waves surface as in_progress with an explicit sign-off note,
    // so the Owner can confirm via UI before the spine `done` count advances.
    let note = wave.blockedBy != null ? `blocked_by: ${wave.blockedBy}` : `verify: ${wave.verify}`
    if (wave.delivery === 'signed') {
      status = 'done'
    } else if (wave.delivery === 'ready_for_signoff' && status !== 'done') {
      status = 'in_progress'
      note = `✅ DELIVERED — awaiting Owner sign-off · verify: ${wave.verify}`
    }

    return {
      id: wave.id,
      label: `${wave.wave} — ${wave.label}`,
      status,
      note,
      progress: stream != null ? { done: stream.done, total: stream.total } : undefined,
    }
  })
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

  for (const v of visionGovernanceQueueItems(context)) {
    items.push({
      id: v.id,
      label: v.label,
      status: v.status === 'done' ? 'closed' : v.status,
      note: v.note,
    })
  }

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
      status: mapMilestoneStatus(cutover.status),
      note: cutover.blocker ?? context?.focus.blocker ?? undefined,
    })
  }

  if (context?.focus.blocker) {
    items.push({
      id: 'spine-blocker',
      label: `Spine blocker: ${context.focus.blocker}`,
      status: 'blocked',
      note: context.focus.headline,
    })
  }

  const gate = context?.promotion.last_gate
  if (gate != null) {
    const gateResult = gate.result?.trim() ?? ''
    const gateStatus: QueueItemStatus =
      gateResult === ''
        ? 'pending'
        : gateResult.toLowerCase() === 'fail'
          ? 'issue'
          : 'done'
    items.push({
      id: 'release-gate',
      label: 'Release gate recorded',
      status: gateStatus,
      note: gateResult !== '' ? `Result: ${gate.result}` : gate.log_path,
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
    const legacyStatus =
      legacy.status === 'SIGNED' || legacy.status === 'CLOSED'
        ? 'done'
        : legacy.status === 'NOT_STARTED'
          ? 'pending'
          : 'in_progress'
    items.push({
      id: 'legacy-retirement',
      label: legacy.label ?? legacy.id,
      status: legacyStatus,
    })
  }

  return items
}

function buildBusinessAdvisoryQueue(): QueueItem[] {
  return [
    { id: 'biz-positions', label: 'Portfolio & positions analysis', status: 'pending', note: 'Greeks, P&L attribution, risk exposure' },
    { id: 'biz-market', label: 'Market data & IV analysis', status: 'pending', note: 'Quote stream, IV cone, term structure' },
    { id: 'biz-sepa', label: 'SEPA research pipeline', status: 'pending', note: 'Screener results, phase scoring, opportunities' },
    { id: 'biz-strategy', label: 'Strategy & gate review', status: 'pending', note: 'Active instances, gate parameters, structure templates' },
  ]
}

function buildQueueFromAutomateStreams(
  automate: { streams: MigrateStream[] } | undefined,
  laneId: AutomateLaneId,
): QueueItem[] {
  if (automate == null) return []
  return automate.streams
    .filter(s => AUTOMATE_STREAM_LANE[s.id] === laneId)
    .map(streamToQueueItem)
}

function buildQueueFromInfraStreams(
  infra: { streams: MigrateStream[] } | undefined,
  laneId: InfraLaneId,
): QueueItem[] {
  if (infra == null) return []
  return infra.streams
    .filter(s => INFRA_STREAM_LANE[s.id] === laneId)
    .map(streamToQueueItem)
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
      if (laneId === 'trade-k8s-native') {
        return buildTradeK8sNativeQueue(context)
      }
      return buildQueueFromMigrateStreams(tracks?.migrate, laneId as MigrateLaneId)
    case 'automate':
      return buildQueueFromAutomateStreams(tracks?.automate, laneId as AutomateLaneId)
    case 'infra':
      return buildQueueFromInfraStreams(tracks?.infra, laneId as InfraLaneId)
    case 'operate':
      switch (laneId as OperateLaneId) {
        case 'governance':
          return buildGovernanceQueue(context)
        case 'troubleshoot':
          return buildTroubleshootQueue(matrices, clusterSummary)
        case 'release':
          return buildReleaseQueue(context, matrices)
        case 'business-advisory':
          return buildBusinessAdvisoryQueue()
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
