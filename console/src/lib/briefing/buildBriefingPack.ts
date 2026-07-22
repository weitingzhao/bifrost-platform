import type { MatrixResponse } from '@/api/matrixTypes'
import type { OpsContextResponse } from '@/api/opsContextTypes'
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
import { formatVisionBriefingSection } from '@/lib/architecture/visionSpineMap'
import { formatDataLayerBriefingAppendix } from '@/lib/architecture/dataLayerCatalog'
import { formatTradeK8sNativeBriefingAppendix } from '@/lib/architecture/tradeK8sNativeCatalog'
import {
  formatReconcileFindings,
  hasBlockingFindings,
  buildReconcileBriefingOptions,
  reconcileBriefing,
} from '@/lib/briefing/reconcileBriefing'
import type { BriefingPackSize } from '@/lib/briefing/briefingUrlState'
import type { TaskModeBriefingContext } from '@/lib/task-mode/TaskModeContext'
import {
  formatQueueStageSummary,
  splitQueueByCompletion,
} from '@/lib/briefing/queueDisplay'
import { formatEmptyLaneInitSection } from '@/lib/briefing/laneInitPack'
import {
  briefingScopeById,
  trackTypeById,
  type BriefingScopeId,
} from '@/lib/briefing/briefingViewTabs'

export interface BriefingInputs extends BriefingSnapshotInput {
  intent: WorkIntent
  sessionDelta?: SessionDelta | null
  trackSummaries?: TrackSummary[]
  selectedTrack?: TrackId
  selectedLane?: LaneId
  /** Layer-1 scope (Rocket / Satellite / …). Falls back to lane.componentLine. */
  selectedScope?: BriefingScopeId
  laneQueue?: QueueItem[]
  agentDialogueLanguage?: AgentDialogueLanguage
  packSize?: BriefingPackSize
  taskModeContext?: TaskModeBriefingContext
  /** Archive headers — required for session↔progress validation. */
  sessionId?: string
  programId?: string
  phaseId?: string
}

function formatTaskModeContextSection(ctx: TaskModeBriefingContext): string {
  const lines = [
    '## Task mode context',
    '',
    `Task mode: **${ctx.modeLabel}** (\`${ctx.modeId}\`)`,
    `Loop: **${ctx.loopArchetype}**`,
  ]
  if (ctx.programId != null) {
    lines.push(`Linked program: \`${ctx.programId}\` (Delivery Board sign-off)`)
  }
  lines.push('', 'Follow the active task mode phase playbook in Task Control Center before expanding scope.')
  return lines.join('\n')
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
      'Ops Console → Governance → Vision (dualFlywheelVisionCatalog.ts)',
      'Ops Console → Governance → Blueprint (blueprintCatalog.ts)',
      'Ops Console → Governance → Standards + MCP Contract',
      'GET /api/v1/context',
      'bifrost-trade-infra/docs/MIGRATION_TRACKING.md (trade stack only)',
    ],
    feature: [
      'Ops Console → Governance → Blueprint + Standards + MCP Contract',
      'api/internal/server/server.go — registered /api/v1/* routes',
      'config/ops-context.yaml — milestone ops-ui-actuation',
    ],
    debug: [
      'Ops Console → Mission Control → Control Room → Runtime Map sheet (failing matrix targets)',
      'Ops Console → Rocket → Cluster (kubeconfig, failing pods)',
      'Ops Console → Governance → Standards — probe contract',
      'context.probe_hints in GET /api/v1/context',
    ],
    release: [
      'Ops Console → Governance → Blueprint — North Star',
      'Ops Console → Satellite → Deploy Satellite (deployMainlineCatalog.ts)',
      'bifrost-trade-infra/docs/PHASE2C_SIGNOFF_MASTER.md',
      'decision D1 in ops-context spine',
    ],
    cluster: [
      'dataLayerCatalog.ts (catalog-only; live: Cluster + Briefing data-layer-k3s)',
      'Ops Console → Governance → tradeK8sNativeCatalog.ts — IB Edge + K8s-native waves W0–W11',
      'config/ops-context.yaml — tracks.migrate.streams trade-k8s-native + data-layer-k3s',
      'bifrost-trade-infra/k8s/ (base + overlays/dev|stg|prod)',
      'Ops Console → Governance → Standards — cluster actuation + observability layers',
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
      'Ops Console → Governance → Vision § Agent Layers (Business Agent)',
      'Ops Console → Governance → MCP Contract (permission model + deny-list)',
      'bifrost-trade-api/CLAUDE.md — 9 API domains (read endpoints)',
      'bifrost-trade-frontend — existing pages for context on data presentation',
    ],
    automate: [
      'config/ops-context.yaml — tracks.automate (streams + milestone autonomous-agent-v1)',
      'Ops Console → Satellite → Deploy Satellite (deliveryMainlineCatalog.ts) — existing CI/CD pipeline reference',
      'config/clusters.yaml — gitops + stack addons (Gitea, Tekton, Argo CD)',
      'k8s/cicd/ in bifrost-trade-infra — Tekton pipelines, Gitea mirror-sync, Argo Applications',
      'Ops Console → Governance → Agent Protocol (agentProtocolCatalog.ts)',
      'Hermes Agent docs: hermes-agent.nousresearch.com/docs (Gateway, cron, skills, MCP Server)',
      'Staleguard / ctxharness — deterministic drift detection CLI tools',
      'console/src/lib/briefing/uiProgressSnapshot.ts — static catalog example for drift targets',
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
    cluster: [
      'Raw kubectl as operator runbook — use platform-api',
      'Install kube-prometheus via ad-hoc shell',
      'Put PG primary PVC on nfs-hot (use local-path on postgres node only)',
      'Prod PG cutover from .80 without stg validation or Owner maintenance window',
    ],
    frontend: ['Change compose/prod cutover', 'Migrate bifrost-trade-api backends'],
    business: ['Any write operation (orders, config, strategy changes)', 'Direct IB/Redis access — use Trade API read endpoints only', 'Recommend trades without Owner confirmation'],
    automate: ['Edit Ops Console pages or platform-api (separate feature intent)', 'Start Hermes auto-fix before Owner confirms accuracy threshold', 'Expose Agent services to LAN without auth planning'],
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
  return [
    '## Required first response (before any work)',
    '',
    `Dialogue language for this session: **${lang.agentLabel}**`,
    `Context scope: track **${track}** · lane **${laneMeta.label}** (${lane}) · intent **${intentMeta.label}** (${intent})`,
    '',
    formatSlashBriefingFirstReplyTemplate(language, {
      laneLabel: laneMeta.label,
      laneId: lane,
      laneDescription: laneMeta.description,
      compact: false,
    }),
  ].join('\n')
}

function formatCompactFirstResponseProtocol(
  language: AgentDialogueLanguage,
  scopeLabel: string,
  trackTypeLabel: string,
  lane: LaneId,
  intent: WorkIntent,
  queueStage: string,
): string {
  const lang = agentDialogueLanguageById(language)
  const laneMeta = laneById(lane)
  const intentMeta = workIntentById(intent)
  return [
    '## Required first response (compact pack)',
    '',
    `Dialogue language: **${lang.agentLabel}**`,
    `Session anchor: **${scopeLabel}** · **${trackTypeLabel}** · **${laneMeta.label}** (${lane}) · intent **${intentMeta.label}**`,
    `Queue stage: ${queueStage}`,
    '',
    formatSlashBriefingFirstReplyTemplate(language, {
      laneLabel: laneMeta.label,
      laneId: lane,
      laneDescription: laneMeta.description,
      compact: true,
      queueStage,
    }),
  ].join('\n')
}

/**
 * Owner-ratified `/briefing` first-reply contract (2026-07-22).
 * Five sections: echo Session → understanding → sources → Plan vs Exec → next directions.
 */
function formatSlashBriefingFirstReplyTemplate(
  language: AgentDialogueLanguage,
  opts: {
    laneLabel: string
    laneId: string
    laneDescription: string
    compact: boolean
    queueStage?: string
  },
): string {
  if (language === 'zh') {
    const queueHint =
      opts.queueStage != null && opts.queueStage !== ''
        ? `当前 Queue stage 提示：\`${opts.queueStage}\`。`
        : '结合本 pack 的 Queue stage / Lane Init Mode / Delivery session 绑定判断。'
    return [
      '当 Owner 输入 `/briefing`（或本 pack 作为新 chat 首条上下文）时，你的**第一条回复**必须按下列 **1–5 节**组织，**用中文**与 Owner 对话。在 Owner 确认方向之前**不要开始实现**。',
      '',
      '### 1. 原始 Session Title 与 Content',
      '原样列出 Owner 的 Session（不要改写）：',
      `- **Title**: ${opts.laneLabel}`,
      `- **Content**: ${opts.laneDescription}`,
      `- **Lane id**: \`${opts.laneId}\``,
      '若 pack 绑定了 `session_id` / `program_id` / `phase_id`，一并列出。若缺失，明确写「未绑定 Delivery Session」。',
      '',
      '### 2. 基于项目现状的理解',
      '用你自己的话说明：这段 Title/Content 在当前 Bifrost 项目里意味着什么（裁决 / 分析 / 实现 / 运维），以及明确**不在范围内**的事项（例如 D10 冻结下的 live trading）。',
      '',
      '### 3. 为什么这么理解（资料与源）',
      '用表格列出依据，并区分两类源：',
      '- **系统提供（事实）**：spine / matrix / MCP / `lanes.yaml` / 代码与 verify 证据',
      '- **方向性指导**：Agent Protocol、migration / workspace 规则、Governance catalog、本会话 Owner 共识',
      opts.compact
        ? '_Compact：每类至少 1–2 行即可；发现矛盾时再展开。_'
        : '每条关键事实一行；若 briefing 与二次探查矛盾，另附简短 Contradiction 说明。',
      '',
      '### 4. 当前 Session 状态',
      '明确二选一（或过渡态），并给证据：',
      '- **Plan / 发现**：Backlog READY、空队列、无 session 绑定、尚无验收/verify',
      '- **已计划、执行中**：Doing、有 queue 项、有 session_id + phase progress',
      queueHint,
      '',
      '### 5. 接下来的任务方向（可讨论可执行）',
      '基于 1–4 给出下一步方向（编号 3–7 条为宜），并**显式邀请 Owner**：',
      '- 改变方向 / 收窄范围，或',
      '- 确认后按你列的任务直接执行',
      '在 Owner 回复确认或调整之前，不要开始改代码。',
    ].join('\n')
  }

  const queueHint =
    opts.queueStage != null && opts.queueStage !== ''
      ? `Queue stage hint: \`${opts.queueStage}\`.`
      : 'Infer from Queue stage / Lane Init Mode / Delivery session binding in this pack.'

  return [
    'When the Owner types `/briefing` (or this pack is the first message in a new chat), your **first reply** MUST use the **five sections below**. Reply in the Owner dialogue language. **Do not implement** until the Owner confirms direction.',
    '',
    '### 1. Original Session Title and Content',
    'Echo the Owner Session verbatim (do not rewrite):',
    `- **Title**: ${opts.laneLabel}`,
    `- **Content**: ${opts.laneDescription}`,
    `- **Lane id**: \`${opts.laneId}\``,
    'Also list `session_id` / `program_id` / `phase_id` when bound; if missing, state **No Delivery Session bound**.',
    '',
    '### 2. Understanding in project context',
    'In your own words: what this Title/Content means in the current Bifrost project (decision / analysis / implementation / ops), and what is **explicitly out of scope** (e.g. live trading under D10 freeze).',
    '',
    '### 3. Why you understand it that way (sources)',
    'Table of evidence, split into two kinds:',
    '- **System facts**: spine / matrix / MCP / `lanes.yaml` / code / verify evidence',
    '- **Directional guidance**: Agent Protocol, migration/workspace rules, Governance catalogs, Owner consensus in this chat',
    opts.compact
      ? '_Compact: 1–2 rows per kind is enough; expand only if contradictions appear._'
      : 'One row per material fact; add a short Contradiction note if briefing disagrees with secondary probes.',
    '',
    '### 4. Current Session status',
    'State clearly (with evidence):',
    '- **Plan / discovery**: Backlog READY, empty queue, no session binding, no acceptance/verify yet',
    '- **Planned and in execution**: Doing, queue items present, session_id + phase progress',
    queueHint,
    '',
    '### 5. Next task directions (discuss or execute)',
    'Based on §1–4, propose next directions (about 3–7 numbered items) and **explicitly invite the Owner** to either:',
    '- change / narrow direction, or',
    '- confirm and execute the listed tasks',
    'Do not start code changes until the Owner replies.',
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
      base = `Mode: Ops. Work intent: feature extension. Scope to bifrost-platform unless Owner named trade repos. Check milestone ops-ui-actuation and Governance → Standards (actuation phases). Propose minimal API+Console diff for one capability.`
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
      base = `Mode: Ops. Work intent: cluster/K3s data layer. Follow spine stream data-layer-k3s and DATA_LAYER_MIGRATION_PHASES in dataLayerCatalog.ts — stg cutover before prod PG. Review Cluster Layer A/B; label ubt-k3s-02 postgres-role for CNPG Primary.`
      break
    case 'frontend':
      base =
        STARTER_PROMPTS.Product +
        ' Work intent: trade frontend migration. One page / one variable; Legacy API only.'
      break
    case 'business':
      base = `Mode: Ops (Business Agent layer). Work intent: trade analysis. Read-only access to Trade API domains (positions, Greeks, SEPA, market). Provide advisory analysis; no write operations or order placement. Respect MCP Contract deny-list.`
      break
    case 'automate':
      base = `Mode: Ops. Work intent: autonomous agent infrastructure. Focus on Hermes Gateway setup, nightly drift scan, CTRL NODE Bridge, and self-improving skills on Mac Mini. Reference spine tracks.automate streams and milestone autonomous-agent-v1.`
      break
    default:
      base = `Mode: ${opt.agentMode}. Work intent: ${intent}.`
  }

  const lang = agentDialogueLanguageById(language)
  const firstReplyHint = ` First reply ONLY in ${lang.agentLabel} using the five-section /briefing template: (1) echo Session Title+Content, (2) project understanding, (3) sources (system facts vs guidance), (4) Plan vs Exec status, (5) next directions + invite Owner to discuss or execute — no implementation yet.`

  return base + firstReplyHint
}

function shouldIncludeDataLayerAppendix(intent: WorkIntent, lane: LaneId): boolean {
  return intent === 'cluster' || lane === 'data-layer-k3s'
}

function shouldIncludeTradeK8sNativeAppendix(intent: WorkIntent, lane: LaneId): boolean {
  return lane === 'trade-k8s-native' || lane === 'compose-k3s' || intent === 'cluster'
}

function intentCorePack(
  intent: WorkIntent,
  ctx?: OpsContextResponse,
  matrices: MatrixResponse[] = [],
  lane: LaneId = 'console-api',
  packSize: BriefingPackSize = 'full',
): string {
  const opt = workIntentById(intent)
  const compact = packSize === 'compact'
  if (!ctx) return buildProductPack(ctx)

  if (opt.agentMode === 'Product' || intent === 'frontend') return buildProductPack(ctx)
  if (opt.agentMode === 'Promote' || intent === 'release') return buildPromotePack(ctx, matrices)

  if (intent === 'business') {
    return [
      buildOpsPack(ctx, matrices, { compact }),
      '',
      '## Business Agent appendix',
      'Layer: Business Agent (read-only advisory)',
      'Access: Trade API read endpoints only (portfolio, market, research, strategy, trading)',
      'Forbidden: order placement, config writes, daemon control, IB operator commands',
      'Reference: Ops Console → Governance → MCP Contract (deny-list)',
    ].join('\n')
  }

  if (intent === 'automate') {
    const automate = ctx.tracks?.automate
    const allStreams = automate?.streams ?? []
    const streams = compact
      ? allStreams.filter(s => s.status !== 'closed')
      : allStreams
    const closedCount = allStreams.length - streams.length
    const streamLines = streams.length > 0
      ? streams.map(s =>
          `- [${s.status}] ${s.label} (${s.done}/${s.total})${s.next_task ? ` — next: ${s.next_task}` : ''}`
        )
      : ['(no active automate streams)']
    if (compact && closedCount > 0) {
      streamLines.unshift(`- (${closedCount} closed streams omitted)`)
    }
    return [
      buildOpsPack(ctx, matrices, { compact }),
      '',
      '## Automate appendix',
      'Milestone: autonomous-agent-v1 (nightly drift + morning briefing)',
      '',
      '### Platform GitOps (foundation)',
      '- Git flow: Mac Pro → GitHub → Gitea mirror → K3s Tekton/Argo + Mac Mini git pull',
      '- Containerization: platform-api (Go) + remediation-runner (Node) + console (static)',
      '- K3s target: platform NS, Argo CD Application, Tekton deliver-platform pipeline',
      '- Agent Host (Mac Mini): git clone from Gitea LAN, `git pull` for updates (not rsync)',
      '',
      '### Agent Stack',
      '- Hermes Gateway + Staleguard/ctxharness + Cursor CLI',
      '- Host: Mac Mini #1 (192.168.10.50, always-on)',
      '',
      '### Streams',
      ...streamLines,
      '',
      '### Key principles',
      '- Platform-gitops stream unblocks all other automate streams (code availability on Mini)',
      '- Layer 1 (deterministic): broken paths, stale versions, missing exports — ctxharness/Staleguard',
      '- Layer 2 (API probe): compare catalog text vs live spine/matrix API responses',
      '- Layer 3 (LLM): semantic drift — description says X but live data says Y',
      '- Auto-fix requires Owner approval threshold before merging (configurable confidence %)',
      '- Containerized platform in K3s removes Mac Pro as runtime dependency for Ops services',
    ].join('\n')
  }

  const ops = buildOpsPack(ctx, matrices, { compact })
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
    const sections = [
      ops,
      '',
      '## Cluster appendix',
      'Layer A: metrics-server + GET /cluster/metrics (CPU/mem, top pods).',
      'Layer B: GET /cluster/observability — Prometheus/Grafana/Loki/Alertmanager in monitoring NS.',
      'P1 actuation: ensure namespaces, rollout restart, scale, delete pod (operator token).',
      'Reference: Ops Console → Governance → Standards (cluster actuation + observability layers).',
    ]
    if (shouldIncludeDataLayerAppendix(intent, lane)) {
      sections.push('', formatDataLayerBriefingAppendix(ctx))
    }
    if (shouldIncludeTradeK8sNativeAppendix(intent, lane)) {
      sections.push('', formatTradeK8sNativeBriefingAppendix(ctx))
    }
    return sections.join('\n')
  }
  const appendixSections: string[] = [ops]
  if (shouldIncludeDataLayerAppendix(intent, lane)) {
    appendixSections.push('', formatDataLayerBriefingAppendix(ctx))
  }
  if (shouldIncludeTradeK8sNativeAppendix(intent, lane)) {
    appendixSections.push('', formatTradeK8sNativeBriefingAppendix(ctx))
  }
  if (appendixSections.length > 1) {
    return appendixSections.join('\n')
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

function formatLaneQueueSection(
  laneId: LaneId,
  active: QueueItem[],
  completedCount: number,
  packSize: BriefingPackSize,
): string {
  const lane = laneById(laneId)
  const lines = [`## Active lane queue — ${lane.label} (${lane.id})`, '', lane.description, '']
  if (active.length === 0) {
    if (completedCount > 0) {
      lines.push(
        `(no active items — ${completedCount} completed hidden in compact; see Console Task Queue or Full pack)`,
      )
    } else {
      lines.push('(no active items — see completed in Console if needed)')
    }
  } else {
    for (const item of active) {
      const note = item.note ? ` — ${item.note}` : ''
      lines.push(`- [${item.status}] ${item.label}${note}`)
    }
  }
  if (packSize === 'compact' && completedCount > 0) {
    lines.push(
      '',
      `_Compact summary: completed ${completedCount} (omitted) — expand Full pack or Console for history._`,
    )
  }
  return lines.join('\n')
}

/** Full briefing for a new Cursor Agent session — paste as first message or context block. */
export function buildBriefingPack(input: BriefingInputs): string {
  const now = new Date().toISOString()
  const packSize: BriefingPackSize = input.packSize ?? 'compact'
  const opt = workIntentById(input.intent)
  const language = input.agentDialogueLanguage ?? DEFAULT_AGENT_DIALOGUE_LANGUAGE
  const langMeta = agentDialogueLanguageById(language)
  const opening = suggestedOpening(input.intent, input.context, input.matrices, language)
  const track = input.selectedTrack ?? 'build'
  const lane = input.selectedLane ?? 'console-api'
  const laneMeta = laneById(lane)
  const scopeId: BriefingScopeId = input.selectedScope ?? laneMeta.componentLine
  const scopeMeta = briefingScopeById(scopeId)
  const trackTypeMeta = trackTypeById(laneMeta.trackType)
  const queueItems = input.laneQueue ?? []
  const queueSplit =
    input.laneQueue != null ? splitQueueByCompletion(input.laneQueue) : { active: [], completed: [] }
  const queueStage =
    input.laneQueue != null ? formatQueueStageSummary(input.laneQueue) : 'active 0/0 · top: (none active) · completed: 0'

  const queueActiveForPack =
    packSize === 'compact' ? queueSplit.active : input.laneQueue != null ? queueItems : null
  const completedCountForPack =
    packSize === 'compact' ? queueSplit.completed.length : 0

  const deltaSection = input.sessionDelta != null ? formatDeltaForPack(input.sessionDelta) : null
  const trackSection =
    packSize === 'full' && input.trackSummaries != null && input.trackSummaries.length > 0
      ? formatTrackSection(input.trackSummaries, track)
      : null
  const queueSection =
    input.laneQueue != null && input.laneQueue.length === 0
      ? formatEmptyLaneInitSection(lane)
      : queueActiveForPack != null
        ? formatLaneQueueSection(lane, queueActiveForPack, completedCountForPack, packSize)
        : null

  const dialogueRule =
    language === 'zh'
      ? 'Reply in **Chinese** for dialogue with the Owner.'
      : 'Reply in **English** for dialogue with the Owner.'

  const firstResponseProtocol =
    packSize === 'compact'
      ? formatCompactFirstResponseProtocol(
          language,
          scopeMeta.shortLabel,
          trackTypeMeta.label,
          lane,
          input.intent,
          queueStage,
        )
      : formatFirstResponseProtocol(language, track, lane, input.intent)

  // Reconcile gate (D-B): blocker findings hard-block the pack; warnings stamp a banner.
  const migrateTrack = input.trackSummaries?.find(t => t.id === 'migrate')
  const findings = reconcileBriefing(
    input.context,
    buildReconcileBriefingOptions({
      context: input.context,
      selectedLane: input.selectedLane,
      laneQueue: input.laneQueue,
      migrateTrackNext: migrateTrack?.nextStep ?? null,
    }),
  )
  const sessionID = input.sessionId?.trim() || '—'
  const programID = input.programId?.trim() || '—'
  const phaseID = input.phaseId?.trim() || '—'

  if (hasBlockingFindings(findings)) {
    return [
      '# Bifrost Ops Platform — Agent Session Briefing',
      `Generated: ${now}`,
      `session_id: ${sessionID}`,
      `program_id: ${programID}`,
      `phase_id: ${phaseID}`,
      '',
      formatReconcileFindings(findings),
      '',
      'Pack generation halted. Resolve the blockers above (spine ↔ catalog) and regenerate.',
      'Doctrine: Ops Console → Governance → Briefing Reconciliation.',
    ].join('\n')
  }
  const staleBanner = findings.length > 0 ? formatReconcileFindings(findings) : null

  const sections: string[] = [
    '# Bifrost Ops Platform — Agent Session Briefing',
    `Generated: ${now}`,
    `session_id: ${sessionID}`,
    `program_id: ${programID}`,
    `phase_id: ${phaseID}`,
    `Pack size: **${packSize}**`,
    ...(staleBanner != null ? ['', staleBanner, ''] : []),
    `Scope: **${scopeMeta.shortLabel}** (${scopeId}) · Track: **${trackTypeMeta.label}** (${laneMeta.trackType}) · Lane: **${laneMeta.label}** (${lane})`,
    `Intent: ${opt.label} (${input.intent}) · Spine track id: ${track}`,
    `Queue stage: ${queueStage}`,
    `Agent layer: ${opt.agentLayer} Agent · Mode: ${opt.agentMode}`,
    `Agent dialogue language: ${langMeta.agentLabel}`,
    '',
    '## Session binding',
    '',
    'Progress reports must use this `session_id` with matching `program_id` + `phase_id`.',
    'To advance to another phase: MCP `create_session` with the new `phase_id`, then `report_phase_progress` with that new session.',
    'Do not reuse this session_id for a different phase_id.',
    'When the phase defines `verify_cmd`, `status=done` requires `verify_passed=true` after you run verify locally.',
    '',
    formatAgentDialogueSection(language),
    '',
    firstResponseProtocol,
    '',
  ]

  if (input.taskModeContext != null) {
    sections.push(formatTaskModeContextSection(input.taskModeContext), '')
  }

  if (trackSection != null) {
    sections.push(trackSection, '')
  }

  if (queueSection != null) {
    sections.push(queueSection, '')
  }

  sections.push(intentTaskSection(input.intent, input.context), '')

  if (deltaSection != null) {
    sections.push(deltaSection, '')
  }

  sections.push(formatBriefingLiveStatus(input), '')

  if (packSize === 'full') {
    sections.push(
      formatUiProgressSection(),
      '',
      formatVisionBriefingSection(input.context),
      '',
    )
  }

  sections.push(
    '## Authoritative context (spine + matrix)',
    intentCorePack(input.intent, input.context, input.matrices, lane, packSize),
    '',
    '## Suggested opening message (paste to Agent)',
    opening,
    '',
    '## Session discipline',
    `- ${dialogueRule} English for UI strings and code identifiers.`,
    packSize === 'compact'
      ? '- Compact pack: five-section /briefing first reply (§1–5); implement only after Owner confirms direction.'
      : '- First reply: five-section /briefing template (Title+Content → understanding → sources → Plan/Exec → next directions) — wait for Owner confirmation before implementing.',
    '- One repo / one variable per task unless Owner expands scope.',
    '- bifrost-trader-engine/ is read-only reference — never edit.',
    '- Phase 1 trade stack: New Frontend + Legacy API only — do not migrate bifrost-trade-api yet.',
  )

  if (packSize === 'full') {
    sections.push(
      '',
      '## Related Console views',
      '- Mission Control: Control Room → Runtime Map sheet (business topology + matrix, L0)',
      '- Rocket: Placement (fleet facility constraints — node-pool / policy gap for CI + STG + shared infra, L0)',
      '- Rocket: Cluster (L0 read + L1 actuation)',
      '- Mission Control: Audit · Engineer → Agent Briefing (actuation history)',
      '- Governance catalogs: Vision, Blueprint, dataLayerCatalog.ts, cicdBootstrapCatalog.ts, MCP Contract',
      '- Governance Copy All for LLM — full static catalog appendix if needed',
    )
  }

  return sections.join('\n')
}
