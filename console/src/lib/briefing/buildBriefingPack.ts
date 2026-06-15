import type { MatrixResponse, OpsContextResponse } from '@/api/types'
import {
  buildOpsPack,
  buildProductPack,
  buildPromotePack,
  STARTER_PROMPTS,
} from '@/lib/control-room/agentContextPacks'
import {
  evaluatePromoteStatus,
  prodFailingTargetIds,
} from '@/lib/control-room/matrixSummary'
import { formatBriefingLiveStatus, type BriefingSnapshotInput } from '@/lib/briefing/briefingSnapshot'
import { formatDeltaForPack, type SessionDelta } from '@/lib/briefing/sessionDiff'
import { formatUiProgressSection } from '@/lib/briefing/uiProgressSnapshot'
import { type WorkIntent, workIntentById } from '@/lib/briefing/workIntents'
import type { TrackId, TrackSummary } from '@/lib/briefing/workTracks'
import type { LaneId, QueueItem } from '@/lib/briefing/workLanes'
import { laneById } from '@/lib/briefing/workLanes'

export interface BriefingInputs extends BriefingSnapshotInput {
  intent: WorkIntent
  sessionDelta?: SessionDelta | null
  trackSummaries?: TrackSummary[]
  selectedTrack?: TrackId
  selectedLane?: LaneId
  laneQueue?: QueueItem[]
}

function intentTaskSection(intent: WorkIntent, ctx?: OpsContextResponse): string {
  const opt = workIntentById(intent)
  const lines = [
    '## Your task for this session',
    '',
    `Work intent: **${opt.label}** (${opt.id})`,
    `Suggested Agent mode: ${opt.agentMode}`,
    '',
    opt.description,
    '',
  ]
  if (ctx?.focus.blocker) {
    lines.push(`Current spine blocker: ${ctx.focus.blocker}`, '')
  }

  const readFirst: Record<WorkIntent, string[]> = {
    ops: [
      'Ops Console → Architecture → Blueprint (console/src/lib/architecture/blueprintCatalog.ts)',
      'Ops Console → Architecture → Standards (standardsCatalog.ts)',
      'GET /api/v1/context',
      'bifrost-trade-infra/docs/MIGRATION_TRACKING.md (trade stack only)',
    ],
    feature: [
      'Ops Console → Architecture → Blueprint + Standards',
      'api/internal/server/server.go — registered /api/v1/* routes',
      'config/ops-context.yaml — milestone ops-ui-actuation',
    ],
    debug: [
      'Ops Console → Runtime Map (failing matrix targets)',
      'Ops Console → Cluster (kubeconfig, failing pods)',
      'Ops Console → Architecture → Standards — probe contract',
      'context.probe_hints in GET /api/v1/context',
    ],
    release: [
      'Ops Console → Architecture → Blueprint — North Star',
      'bifrost-trade-infra/docs/LOCAL_PROD_FINAL_SIGNOFF.md',
      'bifrost-trade-infra/docs/PHASE2C_SIGNOFF_MASTER.md',
      'decision D1 in ops-context spine',
    ],
    cluster: [
      'Ops Console → Architecture → Standards — cluster actuation + observability layers',
      'api/internal/cluster — implementation',
      'bifrost-platform/config/clusters.yaml',
    ],
    frontend: [
      'bifrost-trade-frontend/CLAUDE.md + docs/DENSE_UI.md',
      '.cursor/rules/migration-protocol.mdc — Phase 1: New FE + Legacy API',
      'bifrost-trade-infra/docs/MIGRATION_TRACKING.md',
      'Never edit bifrost-trader-engine/ (read-only reference)',
    ],
  }

  lines.push('### Read first')
  for (const doc of readFirst[intent]) lines.push(`- ${doc}`)

  lines.push('', '### Do not (unless Owner expands scope)')
  const avoid: Record<WorkIntent, string[]> = {
    ops: ['Edit bifrost-trade-frontend pages', 'Migrate bifrost-trade-api (Phase 1)'],
    feature: ['Mix trade-frontend + infra in one task', 'Skip audit/auth for write routes'],
    debug: ['Apply prod actuation without operator token', 'Restart trading daemon via platform'],
    release: ['Skip D1 or release_gate blockers', 'Mix API migration + FE in one change'],
    cluster: ['Raw kubectl as operator runbook — use platform-api', 'Install kube-prometheus via ad-hoc shell'],
    frontend: ['Change compose/prod cutover', 'Migrate bifrost-trade-api backends'],
  }
  for (const rule of avoid[intent]) lines.push(`- ${rule}`)

  return lines.join('\n')
}

function suggestedOpening(intent: WorkIntent, ctx?: OpsContextResponse, matrices?: MatrixResponse[]): string {
  const opt = workIntentById(intent)
  const fails = matrices != null ? prodFailingTargetIds(matrices) : []

  switch (intent) {
    case 'ops':
      return ctx?.focus.blocker
        ? `Mode: Ops. Work intent: operations. Spine blocker is ${ctx.focus.blocker}. List the smallest read-only verification steps on active track ${ctx.deployment.active_track}, then propose one single-variable next action. No trade-frontend edits.`
        : `Mode: Ops. Work intent: operations. Read spine + prod/dev matrix. Summarize platform governance state and recommend the next ops-ui-actuation milestone step. No trade-frontend edits.`
    case 'feature':
      return `Mode: Ops. Work intent: feature extension. Scope to bifrost-platform unless Owner named trade repos. Check milestone ops-ui-actuation and Architecture → Standards (actuation phases). Propose minimal API+Console diff for one capability.`
    case 'debug':
      return fails.length > 0
        ? `Mode: Ops. Work intent: troubleshooting. Prod failing targets: ${fails.join(', ')}. Diagnose root cause with read-only probes first; list evidence from matrix/cluster/spine before suggesting fixes.`
        : `Mode: Ops. Work intent: troubleshooting. Use live status below. Identify failing or degraded probes, hypothesize root cause, propose read-only verification then minimal fix.`
    case 'release':
      return `Mode: Promote. Work intent: release. Assess flywheel A/B readiness from spine + matrix. List all blockers (especially D1). Do not recommend cutover until blockers are explicit.`
    case 'cluster':
      return `Mode: Ops. Work intent: cluster/K3s. Review Cluster page Layer A (metrics-server) vs Layer B (observability stack). Propose next step for k3s-phase1 milestone without skipping kubeconfig guardrails.`
    case 'frontend':
      return STARTER_PROMPTS.Product +
        ' Work intent: trade frontend migration. One page / one variable; Legacy API only.'
    default:
      return `Mode: ${opt.agentMode}. Work intent: ${intent}.`
  }
}

function intentCorePack(intent: WorkIntent, ctx?: OpsContextResponse, matrices: MatrixResponse[] = []): string {
  const opt = workIntentById(intent)
  if (!ctx) return buildProductPack(ctx)

  if (opt.agentMode === 'Product' || intent === 'frontend') return buildProductPack(ctx)
  if (opt.agentMode === 'Promote' || intent === 'release') return buildPromotePack(ctx, matrices)

  const ops = buildOpsPack(ctx, matrices)
  if (intent === 'debug') {
    const status = evaluatePromoteStatus(ctx, matrices)
    const fails = prodFailingTargetIds(matrices)
    return [
      ops,
      '',
      '## Debug appendix',
      fails.length > 0 ? `- prod_failing_targets: ${fails.join(', ')}` : '- prod_failing_targets: (none)',
      ...status.reasons.map(r => `- promote_note: ${r}`),
      ctx.probe_hints.length > 0
        ? ctx.probe_hints.map(h => `- hint [${h.target_id}]: ${h.hint}`).join('\n')
        : '',
    ]
      .filter(Boolean)
      .join('\n')
  }
  if (intent === 'cluster' && matrices.length >= 0) {
    return [
      ops,
      '',
      '## Cluster appendix',
      'Layer A: metrics-server + GET /cluster/metrics (CPU/mem, top pods).',
      'Layer B: GET /cluster/observability — Prometheus/Grafana/Loki/Alertmanager in monitoring NS.',
      'P1 actuation: ensure namespaces, rollout restart, scale, delete pod (operator token).',
      'Reference: Ops Console → Architecture → Standards (cluster actuation + observability layers).',
    ].join('\n')
  }
  return ops
}

function formatTrackSection(tracks: TrackSummary[], selected: TrackId): string {
  const lines = ['## Work tracks (progress from spine)']
  const active = tracks.find(t => t.id === selected) ?? tracks[0]
  lines.push(`Active track: **${active.id}** — ${active.label}`)
  lines.push('')

  for (const t of tracks) {
    const marker = t.id === selected ? '> ' : '  '
    const progressStr = t.progress != null
      ? ` [${t.progress.done}/${t.progress.total}, ${t.progress.percent}%]`
      : ''
    lines.push(`${marker}**${t.id}**${progressStr}: ${t.subtitle}`)
    if (t.nextStep) lines.push(`${marker}  Next: ${t.nextStep}`)
    if (t.issues.length > 0) {
      for (const issue of t.issues) {
        lines.push(`${marker}  Issue: ${issue.label}`)
      }
    }
  }
  return lines.join('\n')
}

function formatLaneQueueSection(laneId: LaneId, queue: QueueItem[]): string {
  const lane = laneById(laneId)
  const lines = [`## Active lane queue — ${lane.label} (${lane.id})`, '', lane.description, '']
  if (queue.length === 0) {
    lines.push('(empty queue)')
    return lines.join('\n')
  }
  for (const item of queue) {
    const note = item.note ? ` — ${item.note}` : ''
    lines.push(`- [${item.status}] ${item.label}${note}`)
  }
  return lines.join('\n')
}

/** Full briefing for a new Cursor Agent session — paste as first message or context block. */
export function buildBriefingPack(input: BriefingInputs): string {
  const now = new Date().toISOString()
  const opt = workIntentById(input.intent)
  const opening = suggestedOpening(input.intent, input.context, input.matrices)
  const track = input.selectedTrack ?? 'build'
  const lane = input.selectedLane ?? 'console-api'
  const laneMeta = laneById(lane)

  const deltaSection = input.sessionDelta != null ? formatDeltaForPack(input.sessionDelta) : null
  const trackSection = input.trackSummaries != null && input.trackSummaries.length > 0
    ? formatTrackSection(input.trackSummaries, track)
    : null
  const queueSection = input.laneQueue != null
    ? formatLaneQueueSection(lane, input.laneQueue)
    : null

  return [
    '# Bifrost Ops Platform — Agent Session Briefing',
    `Generated: ${now}`,
    `Work track: ${track} · Lane: ${laneMeta.label} (${lane}) · Intent: ${opt.label} (${input.intent})`,
    '',
    ...(trackSection != null ? [trackSection, ''] : []),
    ...(queueSection != null ? [queueSection, ''] : []),
    intentTaskSection(input.intent, input.context),
    '',
    ...(deltaSection != null ? [deltaSection, ''] : []),
    formatBriefingLiveStatus(input),
    '',
    formatUiProgressSection(),
    '',
    '## Authoritative context (spine + matrix)',
    intentCorePack(input.intent, input.context, input.matrices),
    '',
    '## Suggested opening message (paste to Agent)',
    opening,
    '',
    '## Session discipline',
    '- Reply in Chinese for dialogue; English for UI strings and code identifiers.',
    '- One repo / one variable per task unless Owner expands scope.',
    '- bifrost-trader-engine/ is read-only reference — never edit.',
    '- Phase 1 trade stack: New Frontend + Legacy API only — do not migrate bifrost-trade-api yet.',
    '',
    '## Related Console views',
    '- Control Room — governance + scoped packs',
    '- Runtime Map — hardware + failing probes',
    '- Cluster — K3s workloads + Layer A/B observability',
    '- Catalog → Copy for LLM — full static catalog appendix if needed',
  ].join('\n')
}
