import type { BuildTrack, MigrateStream, OpsContextResponse } from '@/api/opsContextTypes'
import type { ClusterSummary } from '@/api/clusterTypes'
import type { MatrixResponse } from '@/api/matrixTypes'
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
import {
  DATA_LAYER_MIGRATE_STREAM_ID,
  DATA_LAYER_MIGRATION_PHASES,
} from '@/lib/architecture/dataLayerCatalog'
import { projectWaveStatus } from '@/lib/briefing/waveProjection'

/** Well-known lane ids (documented; catalog is YAML-authoritative). */
export type BuildLaneId = 'console-api' | 'cluster-infra' | 'mcp-gitops' | 'cicd-delivery'
export type MigrateLaneId = 'compose-k3s' | 'trade-k8s-native' | 'data-layer-k3s' | 'legacy-retire' | 'trade-stack'
export type AutomateLaneId =
  | 'platform-gitops'
  | 'agent-infra'
  | 'drift-remediation'
  | 'agent-services'
  | 'agent-trade-advisory-parked'
  | 'flight-director-parked'
  | 'polygon-vendor'
  | 'market-data-expand'
export type InfraLaneId = 'network-server' | 'network-wifi' | 'ai-network'
export type OperateLaneId = 'governance' | 'troubleshoot' | 'release' | 'business-advisory'
export type FutureLaneId =
  | 'platform-health'
  | 'network-monitoring'
  | 'trade-features'
  | 'ib-vendor'
  | 'vendor-health'
/** Lane id — open string; entities live in config/lanes.yaml via API. */
export type LaneId = string

export type QueueItemStatus =
  | 'done'
  | 'in_progress'
  | 'ready_for_signoff'
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
  /** Spine migrate stream for wave SYNC actuation (trade-k8s-native lane). */
  migrateStreamId?: string
  /** Owner UI: mark delivered or sign-off (WRITE_PATHS — ArgoCD SYNC paradigm). */
  waveActuation?: 'deliver' | 'signoff'
}

/** Component line — first layer of the three-tier Briefing model. */
export type ComponentLineId = 'rocket' | 'satellite' | 'engineer' | 'ground' | 'operations' | 'subcontractor'

/** Work track type — second layer (what *kind* of work within a component line). */
export type WorkTrackType = 'build' | 'migrate' | 'maintain' | 'release'

export interface WorkLane {
  id: LaneId
  /** Spine data-source track (unchanged for queue building). */
  track: TrackId
  /** Three-tier layer 1: which component line this lane belongs to. */
  componentLine: ComponentLineId
  /** Three-tier layer 2: what type of work this lane represents. */
  trackType: WorkTrackType
  label: string
  shortLabel: string
  description: string
  agentMode: 'Ops' | 'Product' | 'Promote'
  workIntent: WorkIntent
}

let catalog: WorkLane[] = []

/** Replace in-memory catalog (hydrated from GET /api/v1/lanes). */
export function setLaneCatalog(lanes: WorkLane[]): void {
  catalog = [...lanes]
}

/** All Briefing work lanes (catalog) — for portfolio digests. */
export function allWorkLanes(): WorkLane[] {
  return catalog
}

const LANE_ID_RE = /^[a-z][a-z0-9-]{1,62}$/

export function isLaneId(id: string): id is LaneId {
  if (catalog.length > 0) return catalog.some(l => l.id === id)
  return LANE_ID_RE.test(id)
}

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
  // PARKED vision — Ready lanes agent-trade-advisory-parked / flight-director-parked;
  // do not project onto agent-services (avoids fake Planned/Doing mix).
  'mission-auto-remediation': 'agent-services',
  'trade-k8s-migration': 'agent-services',
  'mutual-watchdog': 'agent-infra',
  'agent-release-discipline': 'agent-services',
  'hermes-gateway-integration': 'agent-infra',
  'polygon-data-pipeline': 'polygon-vendor',
}

const INFRA_STREAM_LANE: Record<string, InfraLaneId> = {
  'network-upgrade-core': 'network-server',
  'network-upgrade-wifi': 'network-wifi',
  'unifi-mcp-server': 'ai-network',
  'ai-home-network': 'ai-network',
}

export function lanesForTrack(track: TrackId): WorkLane[] {
  return catalog.filter(l => l.track === track)
}

/** Get all lanes for a (componentLine, trackType) combination. */
export function lanesForLineTrack(
  line: ComponentLineId,
  tt: WorkTrackType,
): WorkLane[] {
  return catalog.filter(l => l.componentLine === line && l.trackType === tt)
}

/** Get all lanes for a track type across every component line (All scope). */
export function lanesForTrackType(tt: WorkTrackType): WorkLane[] {
  return catalog.filter(l => l.trackType === tt)
}

/** Get distinct WorkTrackTypes available under a component line, in display order. */
export function trackTypesForLine(line: ComponentLineId): WorkTrackType[] {
  const order: WorkTrackType[] = ['build', 'migrate', 'maintain', 'release']
  const available = new Set(catalog.filter(l => l.componentLine === line).map(l => l.trackType))
  return order.filter(tt => available.has(tt))
}

/** Track types that exist anywhere (for All scope). */
export function trackTypesAcrossAllLines(): WorkTrackType[] {
  const order: WorkTrackType[] = ['build', 'migrate', 'maintain', 'release']
  const available = new Set(catalog.map(l => l.trackType))
  return order.filter(tt => available.has(tt))
}

/** All component line IDs in display order. */
export const COMPONENT_LINE_IDS: ComponentLineId[] = [
  'rocket',
  'satellite',
  'engineer',
  'ground',
  'operations',
  'subcontractor',
]

function placeholderLane(id: LaneId): WorkLane {
  return {
    id,
    track: 'build',
    componentLine: 'rocket',
    trackType: 'build',
    label: id,
    shortLabel: id,
    description: 'Lane catalog loading…',
    agentMode: 'Ops',
    workIntent: 'feature',
  }
}

export function laneById(id: LaneId): WorkLane {
  return catalog.find(l => l.id === id) ?? catalog[0] ?? placeholderLane(id)
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

  return TRADE_K8S_NATIVE_WAVES.map(wave => {
    // Status projected from spine (D-A/D-C) — same projectWaveStatus as the briefing appendix.
    const projected =
      stream != null
        ? projectWaveStatus(wave.spineIndex, {
            done: stream.done,
            readyForSignoff: stream.ready_for_signoff ?? 0,
            streamStatus: stream.status,
          })
        : 'pending'

    let status: QueueItemStatus
    let note = wave.blockedBy != null ? `blocked_by: ${wave.blockedBy}` : `verify: ${wave.verify}`
    let waveActuation: QueueItem['waveActuation']
    switch (projected) {
      case 'done':
        status = 'done'
        break
      case 'ready_for_signoff':
        status = 'ready_for_signoff'
        note = `✅ DELIVERED — awaiting Owner sign-off · verify: ${wave.verify}`
        if (stream != null && wave.spineIndex === stream.done) {
          waveActuation = 'signoff'
        }
        break
      case 'next':
        status = 'next'
        waveActuation = 'deliver'
        break
      default:
        status = 'pending'
    }

    return {
      id: wave.id,
      label: `${wave.wave} — ${wave.label}`,
      status,
      note,
      progress: stream != null ? { done: stream.done, total: stream.total } : undefined,
      migrateStreamId: TRADE_K8S_NATIVE_MIGRATE_STREAM_ID,
      waveActuation,
    }
  })
}

function buildDataLayerK3sQueue(context: OpsContextResponse | undefined): QueueItem[] {
  const stream = context?.tracks?.migrate?.streams.find(s => s.id === DATA_LAYER_MIGRATE_STREAM_ID)

  return DATA_LAYER_MIGRATION_PHASES.map(phase => {
    const projected =
      stream != null
        ? projectWaveStatus(phase.spineIndex, {
            done: stream.done,
            readyForSignoff: stream.ready_for_signoff ?? 0,
            streamStatus: stream.status,
          })
        : 'pending'

    let status: QueueItemStatus
    let note = phase.blockedBy != null ? `blocked_by: ${phase.blockedBy}` : `verify: ${phase.verify}`
    let waveActuation: QueueItem['waveActuation']
    switch (projected) {
      case 'done':
        status = 'done'
        break
      case 'ready_for_signoff':
        status = 'ready_for_signoff'
        note = `✅ DELIVERED — awaiting Owner sign-off · verify: ${phase.verify}`
        if (stream != null && phase.spineIndex === stream.done) {
          waveActuation = 'signoff'
        }
        break
      case 'next':
        status = 'next'
        waveActuation = 'deliver'
        break
      default:
        status = 'pending'
    }

    return {
      id: phase.id,
      label: `${phase.displayCode} — ${phase.label}`,
      status,
      note,
      progress: stream != null ? { done: stream.done, total: stream.total } : undefined,
      migrateStreamId: DATA_LAYER_MIGRATE_STREAM_ID,
      waveActuation,
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


function buildQueueFromAutomateStreams(
  automate: { streams: MigrateStream[] } | undefined,
  laneId: AutomateLaneId,
): QueueItem[] {
  if (automate == null) return []
  return automate.streams
    .filter(s => AUTOMATE_STREAM_LANE[s.id] === laneId)
    .map(streamToQueueItem)
}

/**
 * Synthetic queue for Subcontractor · Automate lane market-data-expand.
 * Program market-data-expand: Layer 1+2 expansion (P0–P7 planned).
 */
function buildMarketDataExpandQueue(): QueueItem[] {
  return [
    {
      id: 'md-expand-p0',
      label: 'P0 — Analytics schema + Plugin API skeleton',
      status: 'done',
      note: 'Owner signed off — market_analytics DDL + Plugin API skeleton + K8s',
      progress: { done: 1, total: 1 },
    },
    {
      id: 'md-expand-p1',
      label: 'P1 — Raw ingest: Stock Snapshots + Trades & Quotes',
      status: 'done',
      note: 'verify_passed — stock_snapshot + stock_movers (D1=A: Trades/Quotes deferred to P5)',
      progress: { done: 1, total: 1 },
    },
    {
      id: 'md-expand-p2',
      label: 'P2 — Raw ingest: Option Daily OI full backfill',
      status: 'ready_for_signoff',
      note: 'verify_passed — snapshot→OI extract + backfill registry + oi-gap-heal CronJob (D4=B,D5=A,D6=B); awaiting Owner sign-off',
      progress: { done: 1, total: 1 },
    },
    {
      id: 'md-expand-p3',
      label: 'P3 — Analytics: Max Pain Daily CronJob',
      status: 'pending',
      note: 'Compute from market.option_open_interest → market_analytics.max_pain_daily',
    },
    {
      id: 'md-expand-p4',
      label: 'P4 — Analytics: ATM IV + PCR + IV Percentile',
      status: 'pending',
      note: 'Complete derived metrics suite; document black-box caveats',
    },
    {
      id: 'md-expand-p5',
      label: 'P5 — Plugin API: migrate Trade API research/massive/*',
      status: 'pending',
      note: 'Stand up Plugin REST API (port 8790); replace Trade API proxy routes',
    },
    {
      id: 'md-expand-p6',
      label: 'P6 — Ops Console: Subcontractors Plugin management UI',
      status: 'pending',
      note: 'Migrate Settings → Massive (67 files) → Ops Console Plugin page',
    },
    {
      id: 'md-expand-p7',
      label: 'P7 — Trade System cleanup: retire zombie tables',
      status: 'pending',
      note: 'DROP report_option_*; remove no-op Celery tasks; rewire frontend API',
    },
  ]
}

/**
 * Synthetic queue for Subcontractor · Automate lane ib-vendor.
 * Gateway plugin + Launch Plugin meta-program closed; lane Done (not empty Init).
 */
function buildIbVendorQueue(): QueueItem[] {
  return [
    {
      id: 'ib-gateway-plugin',
      label: 'IB Gateway Plugin',
      status: 'closed',
      note:
        'DONE — 7/7 gates signed · redis-ib + ib-gateway live (host+secondary) · D3 SIGNED · D10 BLOCKED',
      progress: { done: 7, total: 7 },
    },
    {
      id: 'launch-plugin-lane',
      label: 'Launch Plugin Lane',
      status: 'closed',
      note:
        'CLOSED-SUPERSEDED — Mission Launch dogfood meta-program; publish path remains Launch Plugin UI + make install/verify',
      progress: { done: 4, total: 4 },
    },
  ]
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

/**
 * Synthetic queue for Ground · Maintain lane network-monitoring.
 * Tracks network-monitoring-ops P0–P4; update statuses as phases complete in YAML.
 */
function buildNetworkMonitoringQueue(): QueueItem[] {
  // Program network-monitoring-ops completed — all phases closed (Done lifecycle).
  const phases: Array<{ id: string; label: string; note: string }> = [
    {
      id: 'network-monitoring-ops-P0',
      label: 'P0 Baseline contract',
      note: 'DONE — program + lane contract',
    },
    {
      id: 'network-monitoring-ops-P1',
      label: 'P1 Device health',
      note: 'DONE — Wave A health live',
    },
    {
      id: 'network-monitoring-ops-P2',
      label: 'P2 Bandwidth',
      note: 'DONE — Wave B bandwidth live',
    },
    {
      id: 'network-monitoring-ops-P3',
      label: 'P3 Rule anomaly',
      note: 'DONE — Wave C anomalies live',
    },
    {
      id: 'network-monitoring-ops-P4',
      label: 'P4 SLA + predictive-lite',
      note: 'DONE — Wave D SLA + ground.network; program completed · no_handoff',
    },
  ]
  return phases.map(p => ({
    id: p.id,
    label: p.label,
    status: 'closed' as const,
    note: p.note,
    progress: { done: 1, total: 1 },
  }))
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
      // Delivery program trade-celery-k8s-ideal (complete) — no spine build.tasks mapping.
      if (laneId === 'trade-system-celery') {
        return [
          {
            id: 'trade-celery-k8s-ideal',
            label: 'Trade Celery / Massive K8s Ideal',
            status: 'closed',
            note:
              '6/6 phases done · 5/5 required gates signed · Massive superseded by market-data-subcontractor; stocks_ib Celery retained',
            progress: { done: 6, total: 6 },
          },
        ]
      }
      return buildQueueFromBuildTasks(tracks?.build, laneId as BuildLaneId)
    case 'migrate':
      // Acceptance-test placeholder — migration already covered by trade-k8s-native +
      // trade-celery-k8s-ideal + market-data-subcontractor; stocks_ib worker live on K8s.
      if (laneId === 'qa-describe-first-4894') {
        return [
          {
            id: 'qa-trade-bars-pipeline',
            label: 'QA Trade bars pipeline',
            status: 'closed',
            note:
              'DONE by evidence: celery-worker-stocks-ib 1/1 DEV/STG/PROD; market.stock_daily populated; Polygon daily via plugin-market-data; migrate spine 27/27 closed',
            progress: { done: 1, total: 1 },
          },
        ]
      }
      if (laneId === 'trade-k8s-native') {
        return buildTradeK8sNativeQueue(context)
      }
      if (laneId === 'data-layer-k3s') {
        return buildDataLayerK3sQueue(context)
      }
      return buildQueueFromMigrateStreams(tracks?.migrate, laneId as MigrateLaneId)
    case 'automate':
      // PARKED vision lanes — empty queue ⇒ Ready (streams stay in spine for history).
      if (laneId === 'agent-trade-advisory-parked' || laneId === 'flight-director-parked') {
        return []
      }
      // IB Gateway Plugin + Launch Plugin meta closed — lane Done; ongoing IB maintain ≠ Init Build.
      if (laneId === 'ib-vendor') {
        return buildIbVendorQueue()
      }
      // Market Data Plugin expansion — P0–P7 planned phases (program market-data-expand).
      if (laneId === 'market-data-expand') {
        return buildMarketDataExpandQueue()
      }
      return buildQueueFromAutomateStreams(tracks?.automate, laneId as AutomateLaneId)
    case 'infra':
      // Network Monitoring Ops delivery complete — lane Done; ongoing ops via Network / Daily Ops.
      if (laneId === 'network-monitoring') {
        return buildNetworkMonitoringQueue()
      }
      // PARKED vision — empty queue ⇒ Ready (not Planned). Spine streams unifi-mcp-server /
      // ai-home-network stay in ops-context for history; do not project a fake 1/2 Planned queue.
      if (laneId === 'ai-network') {
        return []
      }
      return buildQueueFromInfraStreams(tracks?.infra, laneId as InfraLaneId)
    case 'operate':
      // Mission Signal delivery complete — lane archived; ongoing health is Daily Ops / Control Room.
      if (laneId === 'platform-health') {
        return [
          {
            id: 'mission-signal',
            label: 'Mission Signal',
            status: 'closed',
            note:
              'DONE — 7/7 gates signed · program completed · no_handoff. New signal work = patches, not this lane.',
            progress: { done: 7, total: 7 },
          },
        ]
      }
      // On-demand Business Agent template — empty queue ⇒ Ready (not fake Planned 0/4).
      // Vision V4 SIGNED; analysis topics live in pack/session, not a sticky delivery queue.
      if (laneId === 'business-advisory') {
        return []
      }
      switch (laneId as OperateLaneId) {
        case 'governance':
          return buildGovernanceQueue(context)
        case 'troubleshoot':
          return buildTroubleshootQueue(matrices, clusterSummary)
        case 'release':
          return buildReleaseQueue(context, matrices)
        case 'business-advisory':
          return [] // unreachable — handled above; keep for exhaustiveness
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
