import type { MatrixResponse, OpsContextResponse } from '@/api/types'
import {
  buildEnvironmentsLlmContext,
  formatSpineContextSection,
} from '@/lib/environments-catalog'
import {
  evaluatePromoteStatus,
  prodFailingTargetIds,
  summarizeMatrix,
  getProdMatrix,
} from '@/lib/control-room/matrixSummary'

export type AgentMode = 'Product' | 'Ops' | 'Promote'

const PHASE1_DISCIPLINE = [
  'Frontend Phase 1: New Frontend + Legacy API only — do not migrate bifrost-trade-api yet.',
  'One page / one variable per task; compare with Legacy Frontend on same API.',
  'Never edit bifrost-trader-engine/ (read-only reference).',
].join('\n')

function modeLine(mode: AgentMode): string {
  return `Mode: ${mode}`
}

export function buildProductPack(ctx?: OpsContextResponse): string {
  const lines: string[] = [modeLine('Product'), '', '## Discipline', PHASE1_DISCIPLINE, '']
  if (ctx != null) {
    lines.push('## Focus (spine)', `- headline: ${ctx.focus.headline}`)
    lines.push(`- flywheel_primary: ${ctx.focus.flywheel_primary}`)
    if (ctx.focus.blocker) lines.push(`- blocker: ${ctx.focus.blocker}`)
    lines.push('')
    lines.push('## Deployment')
    lines.push(`- phase: ${ctx.deployment.phase}`)
    lines.push(`- active_track: ${ctx.deployment.active_track}`)
    lines.push('')
    lines.push('(No live matrix in Product pack — use Ops mode for connectivity.)')
  } else {
    lines.push('(Spine not loaded — open Control Room or GET /api/v1/context)')
  }
  return lines.join('\n')
}

export function buildOpsPack(ctx: OpsContextResponse, matrices: MatrixResponse[]): string {
  const spine = formatSpineContextSection(ctx)
  const matrixLines: string[] = ['## Matrix summary']
  for (const m of matrices) {
    const s = summarizeMatrix(m)
    matrixLines.push(
      `- **${m.environment}** (${m.label}): ok=${s.ok} fail=${s.fail} degraded=${s.degraded} worst=${s.worstReach}`,
    )
  }
  const northStarDiscipline =
    ctx.north_star != null
      ? [
          '## North star discipline',
          'All routine ops via Ops Console/API only (Strategy C hybrid).',
          `Owner exception: ${ctx.north_star.owner_exception}`,
          'Do not instruct manual ssh/kubectl/Makefile for operations available or planned in platform-api.',
          '',
        ].join('\n')
      : ''
  return [
    modeLine('Ops'),
    '',
    spine,
    northStarDiscipline,
    matrixLines.join('\n'),
    '',
    '## Deployment',
    `- phase: ${ctx.deployment.phase}`,
    `- active_track: ${ctx.deployment.active_track}`,
    '',
    PHASE1_DISCIPLINE,
  ].join('\n')
}

const FLYWHEEL_A_CHECKS = [
  'npm run lint',
  'npm run build',
  'npm run check:legacy-css',
  'Page-by-page Legacy equivalence (Phase 1)',
] as const

const FLYWHEEL_B_CHECKS = [
  'make prod-health (12/12)',
  'scripts/release_gate.sh (when available)',
  'Platform GET /api/v1/matrix?env=prod',
  'Owner sign-off chain',
] as const

export function buildPromotePack(ctx: OpsContextResponse, matrices: MatrixResponse[]): string {
  const ops = buildOpsPack(ctx, matrices)
  const status = evaluatePromoteStatus(ctx, matrices)
  const fails = prodFailingTargetIds(matrices)
  const prod = getProdMatrix(matrices)

  const checklist = [
    '## Promote checklist',
    '### Flywheel A',
    ...FLYWHEEL_A_CHECKS.map(c => `- ${c}`),
    '### Flywheel B',
    ...FLYWHEEL_B_CHECKS.map(c => `- ${c}`),
    '',
    `## Promote status: ${status.ready ? 'READY (narrative)' : 'BLOCKED'}`,
    ...status.reasons.map(r => `- ${r}`),
    status.cutoverBlocker != null ? `- cutover blocker: ${status.cutoverBlocker}` : '',
    fails.length > 0 ? `- prod failing targets: ${fails.join(', ')}` : '',
    prod != null ? `- prod generated_at: ${prod.generated_at}` : '',
    `- last_gate: ${ctx.promotion.last_gate.result ?? 'not recorded'}`,
  ]
    .filter(Boolean)
    .join('\n')

  return [modeLine('Promote'), '', ops, '', checklist].join('\n')
}

export function buildScopedMilestonePack(
  ctx: OpsContextResponse,
  milestoneId: string,
  matrices: MatrixResponse[],
): string {
  const m = ctx.milestones.find(ms => ms.id === milestoneId)
  const decision = ctx.decisions.find(d => `decision:${d.id}` === milestoneId)
  const base = buildOpsPack(ctx, matrices)
  const scoped = m != null
    ? `## Scoped milestone: ${m.id}\n- label: ${m.label ?? ''}\n- status: ${m.status}\n- blocker: ${m.blocker ?? '(none)'}\n- authority: ${m.authority ?? ''}`
    : decision != null
      ? `## Scoped decision: ${decision.id}\n- topic: ${decision.topic ?? ''}\n- conclusion: ${decision.conclusion}`
      : `## Scoped: ${milestoneId}`
  return [modeLine('Ops'), '', scoped, '', base].join('\n')
}

export function buildFullCatalogPack(ctx?: OpsContextResponse): string {
  return buildEnvironmentsLlmContext(ctx)
}

export type SuggestedMode = AgentMode

export function suggestAgentMode(
  ctx: OpsContextResponse | undefined,
  selectionMilestoneId?: string | null,
  selectionBayId?: string | null,
): SuggestedMode {
  if (selectionBayId === 'bay_promote_gate' || selectionMilestoneId === '2c-b-prod-cutover') {
    return 'Promote'
  }
  if (!ctx) return 'Product'
  if (ctx.focus.blocker != null && ctx.focus.blocker !== '') return 'Ops'
  if (ctx.focus.flywheel_primary === 'B') return 'Ops'
  return 'Product'
}

export const STARTER_PROMPTS: Record<AgentMode, string> = {
  Product:
    'Mode: Product. Task: migrate one frontend page with Dense UI; Legacy API only. No infra or API backend changes.',
  Ops: 'Mode: Ops. Task: read spine + prod matrix; list blockers. No trade-frontend page edits.',
  Promote:
    'Mode: Promote. Task: assess release readiness from spine + matrix; do not skip D1 or gate blockers.',
}

function selectionSection(
  selection?: { kind: 'bay' | 'milestone'; id: string } | null,
): string {
  if (selection == null) return '## UI selection\n(none — using global focus)'
  return `## UI selection\n- kind: ${selection.kind}\n- id: ${selection.id}`
}

function nextStepQuestion(
  mode: AgentMode,
  ctx: OpsContextResponse,
  selection?: { kind: 'bay' | 'milestone'; id: string } | null,
): string {
  if (selection?.kind === 'milestone' && selection.id.startsWith('decision:')) {
    const id = selection.id.replace(/^decision:/, '')
    return `Given decision ${id} and the spine below, what is the smallest next action to unblock 2c-b-prod-cutover without violating D1?`
  }
  if (selection?.kind === 'bay' && selection.id === 'bay_apis') {
    return 'Prod Trade APIs are failing in matrix. List failing target ids, likely root cause, and a read-only verification plan (no writes).'
  }
  if (mode === 'Promote') {
    return 'From spine + matrix, list all promote blockers in priority order and propose one single-variable next step for Owner.'
  }
  if (ctx.focus.blocker != null && ctx.focus.blocker !== '') {
    return `Focus is blocked on ${ctx.focus.blocker}. What should we do on active track ${ctx.deployment.active_track} while cutover remains blocked?`
  }
  if (ctx.deployment.active_track === 'k3s_phase1') {
    return 'K3s phase 1 is IN_PROGRESS while compose prod cutover is blocked. Propose the parallel work plan and what must NOT change on flywheel B until D1 is resolved.'
  }
  if (ctx.deployment.active_track === 'ops_ui_actuation') {
    return 'k3s-phase1 is closed. Propose the next ops-ui-actuation slice (P1 workload smoke, agent join, or P2 node lifecycle) without Prod cutover or Layer B ad-hoc install.'
  }
  return STARTER_PROMPTS[mode]
}

/** One-click pack for Control Room → paste into Cursor / Agent chat. */
export function buildSessionPack(
  ctx: OpsContextResponse | undefined,
  matrices: MatrixResponse[],
  selection?: { kind: 'bay' | 'milestone'; id: string } | null,
): string {
  const mode = suggestAgentMode(
    ctx,
    selection?.kind === 'milestone' ? selection.id : null,
    selection?.kind === 'bay' ? selection.id : null,
  )

  const core =
    mode === 'Product'
      ? buildProductPack(ctx)
      : mode === 'Ops' && ctx != null
        ? selection?.kind === 'milestone'
          ? buildScopedMilestonePack(ctx, selection.id, matrices)
          : buildOpsPack(ctx, matrices)
        : ctx != null
          ? buildPromotePack(ctx, matrices)
          : buildProductPack(ctx)

  const question =
    ctx != null ? nextStepQuestion(mode, ctx, selection) : STARTER_PROMPTS[mode]

  return [
    core,
    '',
    selectionSection(selection),
    '',
    '## Suggested Agent question',
    question,
    '',
    '## Session discipline',
    '- Reply in Chinese for dialogue; English for UI strings and code identifiers.',
    '- One repo / one variable per task unless Owner expands scope.',
  ].join('\n')
}

export function packForMode(
  mode: AgentMode,
  ctx: OpsContextResponse | undefined,
  matrices: MatrixResponse[],
  selection?: { kind: 'bay' | 'milestone'; id: string } | null,
): string {
  if (mode === 'Product') return buildProductPack(ctx)
  if (!ctx) return buildProductPack(ctx)
  if (mode === 'Ops' && selection?.kind === 'milestone') {
    return buildScopedMilestonePack(ctx, selection.id, matrices)
  }
  if (mode === 'Ops') return buildOpsPack(ctx, matrices)
  return buildPromotePack(ctx, matrices)
}
