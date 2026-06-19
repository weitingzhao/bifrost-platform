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
import {
  agentDialogueLanguageById,
  DEFAULT_AGENT_DIALOGUE_LANGUAGE,
  type AgentDialogueLanguage,
} from '@/lib/briefing/agentDialogueLanguage'

export interface BriefingInputs extends BriefingSnapshotInput {
  intent: WorkIntent
  sessionDelta?: SessionDelta | null
  trackSummaries?: TrackSummary[]
  selectedTrack?: TrackId
  selectedLane?: LaneId
  laneQueue?: QueueItem[]
  agentDialogueLanguage?: AgentDialogueLanguage
}

function intentTaskSection(intent: WorkIntent, ctx?: OpsContextResponse): string {
  const opt = workIntentById(intent)
  const lines = [
    '## Your task for this session',
    '',
    `Work intent: **${opt.label}** (${opt.id})`,
    `Agent layer: **${opt.agentLayer} Agent** · Mode: ${opt.agentMode}`,
    '',
    opt.description,
    '',
  ]
  if (ctx?.focus.blocker) {
    lines.push(`Current spine blocker: ${ctx.focus.blocker}`, '')
  }

  const readFirst: Record<WorkIntent, string[]> = {
    ops: [
      'Ops Console → Architecture → Vision (dualFlywheelVisionCatalog.ts)',
      'Ops Console → Architecture → Blueprint (blueprintCatalog.ts)',
      'Ops Console → Architecture → Standards + MCP Contract',
      'GET /api/v1/context',
      'bifrost-trade-infra/docs/MIGRATION_TRACKING.md (trade stack only)',
    ],
    feature: [
      'Ops Console → Architecture → Blueprint + Standards + MCP Contract',
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
      'Ops Console → Operate → Deploy Mainline (deployMainlineCatalog.ts)',
      'bifrost-trade-infra/docs/PHASE2C_SIGNOFF_MASTER.md',
      'decision D1 in ops-context spine',
    ],
    cluster: [
      'Ops Console → Architecture → K3s → Data Layer (dataLayerCatalog.ts)',
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
    business: [
      'Ops Console → Architecture → Vision § Agent Layers (Business Agent)',
      'Ops Console → Architecture → Standards → MCP Contract (permission model + deny-list)',
      'bifrost-trade-api/CLAUDE.md — 9 API domains (read endpoints)',
      'bifrost-trade-frontend — existing pages for context on data presentation',
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
    business: ['Any write operation (orders, config, strategy changes)', 'Direct IB/Redis access — use Trade API read endpoints only', 'Recommend trades without Owner confirmation'],
  }
  for (const rule of avoid[intent]) lines.push(`- ${rule}`)

  return lines.join('\n')
}

function formatAgentDialogueSection(language: AgentDialogueLanguage): string {
  const lang = agentDialogueLanguageById(language)
  const dialogueRule =
    language === 'zh'
      ? 'Use **Chinese** for all dialogue with the Owner (chat, explanations, summaries).'
      : 'Use **English** for all dialogue with the Owner (chat, explanations, summaries).'

  return [
    '## Agent dialogue language',
    '',
    `Owner selected: **${lang.agentLabel}**`,
    dialogueRule,
    '- UI strings, code identifiers, and commit messages stay **English** unless Owner says otherwise.',
  ].join('\n')
}

function formatFirstResponseProtocol(
  language: AgentDialogueLanguage,
  track: TrackId,
  lane: LaneId,
  intent: WorkIntent,
): string {
  const lang = agentDialogueLanguageById(language)
  const laneMeta = laneById(lane)
  const intentMeta = workIntentById(intent)

  const confirmStep =
    language === 'zh'
      ? 'Summarize your understanding of this briefing in **Chinese**: active track/lane, work intent, queue priorities, spine blockers, and scope constraints. Ask the Owner to confirm or correct before any implementation.'
      : 'Summarize your understanding of this briefing in **English**: active track/lane, work intent, queue priorities, spine blockers, and scope constraints. Ask the Owner to confirm or correct before any implementation.'

  const taskListStep =
    language === 'zh'
      ? 'Based on the briefing, active lane queue, spine, and matrix below, propose a **numbered task list** (3–7 concrete items). Each item: one-sentence scope + primary repo/files. Mark your recommended default with *(recommended)*.'
      : 'Based on the briefing, active lane queue, spine, and matrix below, propose a **numbered task list** (3–7 concrete items). Each item: one-sentence scope + primary repo/files. Mark your recommended default with *(recommended)*.'

  const waitStep =
    language === 'zh'
      ? '**Do not start implementation** until the Owner confirms your understanding and selects task(s) (or adjusts the list).'
      : '**Do not start implementation** until the Owner confirms your understanding and selects task(s) (or adjusts the list).'

  return [
    '## Required first response (before any work)',
    '',
    `Dialogue language for this session: **${lang.agentLabel}**`,
    `Context scope: track **${track}** · lane **${laneMeta.label}** (${lane}) · intent **${intentMeta.label}** (${intent})`,
    '',
    'Your **first reply** in this new chat MUST include:',
    '',
    '1. **Confirm understanding** — ' + confirmStep,
    '2. **Propose task list** — ' + taskListStep,
    '3. **Source audit** — ' + formatSourceAuditInstruction(language),
    '4. **Wait for selection** — ' + waitStep,
    '',
    'Only after Owner confirmation and task selection should you read the read-first list and begin work.',
  ].join('\n')
}

function formatSourceAuditInstruction(language: AgentDialogueLanguage): string {
  if (language === 'zh') {
    return [
      'After reading this briefing and the read-first references, produce a **Source Audit** table and a **Contradiction Report**.',
      '',
      '   **Source Audit table** — For each key fact you relied on to form your understanding above, state its provenance:',
      '',
      '   | # | Key fact (brief) | Source | Discovery method |',
      '   |---|---|---|---|',
      '   | 1 | e.g. "k3s-phase1 CLOSED" | Briefing § Milestones | Direct extraction |',
      '   | 2 | e.g. "metrics-server installed" | `config/clusters.yaml` | Read tool (secondary search) |',
      '   | 3 | e.g. "Layer B not detected" | platform-api `/cluster/observability` | MCP / HTTP probe |',
      '',
      '   Discovery method values: `Direct extraction` (from this briefing text), `Read tool` (opened a file based on briefing clues), `Grep/Search` (searched codebase), `MCP call`, `Web search`, `Inference` (deduced from multiple sources).',
      '',
      '   **Contradiction Report** — List any discrepancy between what this briefing states and what you found via secondary search:',
      '',
      '   | Briefing states | Actual finding (source) | Severity |',
      '   |---|---|---|',
      '   | "..." | "..." (file/API) | low / medium / high |',
      '',
      '   If no contradictions found, explicitly state: "No contradictions detected between briefing and secondary sources."',
      '',
      '   This audit helps the Owner assess briefing freshness and identify generator drift.',
    ].join('\n')
  }
  return [
    'After reading this briefing and the read-first references, produce a **Source Audit** table and a **Contradiction Report**.',
    '',
    '   **Source Audit table** — For each key fact you relied on to form your understanding above, state its provenance:',
    '',
    '   | # | Key fact (brief) | Source | Discovery method |',
    '   |---|---|---|---|',
    '   | 1 | e.g. "k3s-phase1 CLOSED" | Briefing § Milestones | Direct extraction |',
    '   | 2 | e.g. "metrics-server installed" | `config/clusters.yaml` | Read tool (secondary search) |',
    '   | 3 | e.g. "Layer B not detected" | platform-api `/cluster/observability` | MCP / HTTP probe |',
    '',
    '   Discovery method values: `Direct extraction` (from this briefing text), `Read tool` (opened a file based on briefing clues), `Grep/Search` (searched codebase), `MCP call`, `Web search`, `Inference` (deduced from multiple sources).',
    '',
    '   **Contradiction Report** — List any discrepancy between what this briefing states and what you found via secondary search:',
    '',
    '   | Briefing states | Actual finding (source) | Severity |',
    '   |---|---|---|',
    '   | "..." | "..." (file/API) | low / medium / high |',
    '',
    '   If no contradictions found, explicitly state: "No contradictions detected between briefing and secondary sources."',
    '',
    '   This audit helps the Owner assess briefing freshness and identify generator drift.',
  ].join('\n')
}

function suggestedOpening(
  intent: WorkIntent,
  ctx?: OpsContextResponse,
  matrices?: MatrixResponse[],
  language: AgentDialogueLanguage = DEFAULT_AGENT_DIALOGUE_LANGUAGE,
): string {
  const opt = workIntentById(intent)
  const fails = matrices != null ? prodFailingTargetIds(matrices) : []

  let base: string
  switch (intent) {
    case 'ops':
      base = ctx?.focus.blocker
        ? `Mode: Ops. Work intent: operations. Spine blocker is ${ctx.focus.blocker}. List the smallest read-only verification steps on active track ${ctx.deployment.active_track}, then propose one single-variable next action. No trade-frontend edits.`
        : `Mode: Ops. Work intent: operations. Read spine + prod/dev matrix. Summarize platform governance state and recommend the next ops-ui-actuation milestone step. No trade-frontend edits.`
      break
    case 'feature':
      base = `Mode: Ops. Work intent: feature extension. Scope to bifrost-platform unless Owner named trade repos. Check milestone ops-ui-actuation and Architecture → Standards (actuation phases). Propose minimal API+Console diff for one capability.`
      break
    case 'debug':
      base = fails.length > 0
        ? `Mode: Ops. Work intent: troubleshooting. Prod failing targets: ${fails.join(', ')}. Diagnose root cause with read-only probes first; list evidence from matrix/cluster/spine before suggesting fixes.`
        : `Mode: Ops. Work intent: troubleshooting. Use live status below. Identify failing or degraded probes, hypothesize root cause, propose read-only verification then minimal fix.`
      break
    case 'release':
      base = `Mode: Promote. Work intent: release. Assess flywheel A/B readiness from spine + matrix. List all blockers (especially D1). Do not recommend cutover until blockers are explicit.`
      break
    case 'cluster':
      base = `Mode: Ops. Work intent: cluster/K3s. Review Cluster page Layer A (metrics-server) vs Layer B (observability stack). Propose next step for k3s-phase1 milestone without skipping kubeconfig guardrails.`
      break
    case 'frontend':
      base =
        STARTER_PROMPTS.Product +
        ' Work intent: trade frontend migration. One page / one variable; Legacy API only.'
      break
    case 'business':
      base = `Mode: Ops (Business Agent layer). Work intent: trade analysis. Read-only access to Trade API domains (positions, Greeks, SEPA, market). Provide advisory analysis; no write operations or order placement. Respect MCP Contract deny-list.`
      break
    default:
      base = `Mode: ${opt.agentMode}. Work intent: ${intent}.`
  }

  const lang = agentDialogueLanguageById(language)
  const firstReplyHint = ` First reply ONLY in ${lang.agentLabel}: (1) your understanding of this briefing for Owner confirmation, (2) a numbered task list for Owner to pick from, (3) a Source Audit table + Contradiction Report — no implementation yet.`

  return base + firstReplyHint
}

function intentCorePack(intent: WorkIntent, ctx?: OpsContextResponse, matrices: MatrixResponse[] = []): string {
  const opt = workIntentById(intent)
  if (!ctx) return buildProductPack(ctx)

  if (opt.agentMode === 'Product' || intent === 'frontend') return buildProductPack(ctx)
  if (opt.agentMode === 'Promote' || intent === 'release') return buildPromotePack(ctx, matrices)

  if (intent === 'business') {
    return [
      buildOpsPack(ctx, matrices),
      '',
      '## Business Agent appendix',
      'Layer: Business Agent (read-only advisory)',
      'Access: Trade API read endpoints only (portfolio, market, research, strategy, trading)',
      'Forbidden: order placement, config writes, daemon control, IB operator commands',
      'Reference: Ops Console → Architecture → Standards → MCP Contract (deny-list)',
    ].join('\n')
  }

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
  const language = input.agentDialogueLanguage ?? DEFAULT_AGENT_DIALOGUE_LANGUAGE
  const langMeta = agentDialogueLanguageById(language)
  const opening = suggestedOpening(input.intent, input.context, input.matrices, language)
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

  const dialogueRule =
    language === 'zh'
      ? 'Reply in **Chinese** for dialogue with the Owner.'
      : 'Reply in **English** for dialogue with the Owner.'

  return [
    '# Bifrost Ops Platform — Agent Session Briefing',
    `Generated: ${now}`,
    `Work track: ${track} · Lane: ${laneMeta.label} (${lane}) · Intent: ${opt.label} (${input.intent})`,
    `Agent layer: ${opt.agentLayer} Agent · Mode: ${opt.agentMode}`,
    `Agent dialogue language: ${langMeta.agentLabel}`,
    '',
    formatAgentDialogueSection(language),
    '',
    formatFirstResponseProtocol(language, track, lane, input.intent),
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
    `- ${dialogueRule} English for UI strings and code identifiers.`,
    '- First reply: confirm briefing understanding + propose task list + Source Audit (provenance table + contradiction report) — wait for Owner selection before implementing.',
    '- One repo / one variable per task unless Owner expands scope.',
    '- bifrost-trader-engine/ is read-only reference — never edit.',
    '- Phase 1 trade stack: New Frontend + Legacy API only — do not migrate bifrost-trade-api yet.',
    '',
    '## Related Console views',
    '- Observe → Diagnosis: Control Room → Runtime Map (business topology + matrix, L0)',
    '- Observe → Scheduling: Placement (K8s node-pool / policy gap, L0)',
    '- Operate → Cluster ops: Cluster (L0 read + L1 actuation)',
    '- Observe → Session & audit: Agent Briefing · Audit (actuation history)',
    '- Architecture → Vision / Blueprint / Data Layer / MCP Contract — governance catalogs',
    '- Architecture Copy All for LLM — full static catalog appendix if needed',
  ].join('\n')
}
