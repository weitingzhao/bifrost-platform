/**
 * Briefing Reconciliation catalog — Agent session context single-source discipline.
 *
 * Authoritative source for Ops Console → Agent → Doctrine → Briefing Reconciliation.
 * Governs how Agent Briefing packs are projected from spine + catalogs + live probes,
 * and how drift is detected before agents act on stale context.
 *
 * Implementation: console/src/lib/briefing/buildBriefingPack.ts
 * Drift scanners: agent/drift/scan_layer{1-4}.py
 * Progress spine: config/ops-context.yaml (GET /api/v1/context)
 */

import type { OpsContextResponse } from '@/api/types'

export const BRIEFING_RECONCILIATION_VERSION = '2026-07-01'
export const BRIEFING_RECONCILIATION_SOURCE =
  'console/src/lib/architecture/briefingReconciliationCatalog.ts'

export const RECONCILIATION_STATEMENT =
  'Agent Briefing is a projection of ops-context.yaml spine progress merged with ' +
  'read-only domain catalogs (wave labels, verify steps, IB constraints) and live ' +
  'platform-api probes. Progress has one writer (spine via platform-api sign-off); ' +
  'catalogs hold NO progress fields. The generator must reconcile all derived views ' +
  'before emit: blocker-level mismatches HARD-BLOCK pack generation (emit findings only), ' +
  'warning-level mismatches stamp a BRIEFING_STALE banner but still ship — never silently ' +
  'ship contradictory task content.'

export type DesignDecisionRow = {
  id: string
  topic: string
  decision: string
}

/** Owner-ratified top-level design decisions — authoritative for implementation. */
export const DESIGN_DECISIONS: DesignDecisionRow[] = [
  {
    id: 'D-A',
    topic: 'Delivered-but-unsigned transient state',
    decision:
      'spine stream gains a numeric ready_for_signoff field (e.g. done:3 + ready_for_signoff:1). ' +
      'Catalogs remove the delivery field entirely — spine is the single progress source.',
  },
  {
    id: 'D-B',
    topic: 'Reconcile gate behavior on hit',
    decision:
      'blocker severity HARD-BLOCKS pack generation (output findings only); warning severity ' +
      'ships the pack with a BRIEFING_STALE banner at top.',
  },
  {
    id: 'D-C',
    topic: 'Wave ↔ spine index contract',
    decision:
      'Each catalog wave explicitly declares spineIndex (its position in spine done count) ' +
      'instead of relying on implicit array order.',
  },
  {
    id: 'D-D',
    topic: 'Headline semantic equality',
    decision:
      'focus.headline MUST contain the active wave id substring (e.g. "W3"); absence => stale ' +
      '(warning). Avoids free-text headline drifting from next_task.',
  },
]

export type GateBehaviorRow = {
  severity: 'blocker' | 'warning'
  onHit: string
}

/** D-B ratified — maps reconcile severity to generator action. */
export const GATE_BEHAVIOR: GateBehaviorRow[] = [
  { severity: 'blocker', onHit: 'HARD-BLOCK: refuse pack, output BRIEFING_STALE + findings only' },
  { severity: 'warning', onHit: 'SOFT: ship pack with BRIEFING_STALE banner at top' },
]

export type SourceOfTruthRow = {
  layer: string
  source: string
  role: string
  mayHoldProgress: boolean
  authority: string
}

/** Three input layers fed into briefing generator (GitOps: desired + spec + actual). */
export const SOURCE_OF_TRUTH_LAYERS: SourceOfTruthRow[] = [
  {
    layer: 'Progress (desired state)',
    source: 'config/ops-context.yaml → GET /api/v1/context',
    role: 'stream.done/total/status, ready_for_signoff (D-A), next_task, milestones, focus.blocker',
    mayHoldProgress: true,
    authority: 'Owner sign-off + platform-api atomic patch; drift L4 after approval',
  },
  {
    layer: 'Domain spec (structure only)',
    source: 'console/src/lib/architecture/*Catalog.ts',
    role: 'Wave labels + spineIndex (D-C), verify text, IB constraints, gap analysis, migration phases',
    mayHoldProgress: false,
    authority: 'Catalog TS in bifrost-platform; NO delivery/signed/done fields (D-A)',
  },
  {
    layer: 'Runtime (actual state)',
    source: 'GET /api/v1/matrix, /cluster, /health',
    role: 'Probe reachability, failing pods, Layer A/B observability',
    mayHoldProgress: false,
    authority: 'Live fetch at briefing generation time',
  },
]

export type ProjectionRuleRow = {
  briefingView: string
  derivesFrom: string
  rule: string
  antiPattern: string
}

/** Pure projection rules — queue, headline, appendix must not diverge. */
export const PROJECTION_RULES: ProjectionRuleRow[] = [
  {
    briefingView: 'Lane queue item status (e.g. W0–W2 done)',
    derivesFrom: 'spine stream.done + wave.spineIndex (D-C) + stream.ready_for_signoff (D-A)',
    rule: 'spineIndex < done → done; spineIndex in [done, done+ready) → ready_for_signoff; spineIndex === done+ready && in_progress → next',
    antiPattern: 'catalog delivery field overriding spine (REMOVED per D-A)',
  },
  {
    briefingView: 'Trade K8s-native appendix spine next marker',
    derivesFrom: 'Shared projectWaveStatus(spineIndex, stream) — same fn as queue',
    rule: '*(spine next)* on wave where spineIndex === done+ready only',
    antiPattern: 'appendix uses stream.done only while queue applies a different rule',
  },
  {
    briefingView: 'Migrate track Next line',
    derivesFrom: 'active in_progress stream.next_task for selected lane stream',
    rule: 'Use stream matching lane (trade-k8s-native), not first in_progress globally',
    antiPattern: 'first in_progress stream wins for all lanes',
  },
  {
    briefingView: 'focus.headline / spine_focus live field',
    derivesFrom: 'active stream.next_task; must contain active wave id substring (D-D)',
    rule: 'Auto-derived on sign-off; headline missing active wave id (e.g. "W3") => stale (warning)',
    antiPattern: 'headline still says W0 awaiting sign-off when done=3',
  },
  {
    briefingView: 'Domain appendix text (verify, blocked_by)',
    derivesFrom: 'Domain catalog TS (tradeK8sNativeCatalog, dataLayerCatalog, …)',
    rule: 'Merge spec strings only; progress counts from spine in same appendix header',
    antiPattern: 'appendix Spine progress: 0/12 while stream.done=3',
  },
  {
    briefingView: 'UI progress snapshot notes',
    derivesFrom: 'Console feature ship checklist only',
    rule: 'CONSOLE_UI_PROGRESS notes describe UI panels — not migrate wave sign-off',
    antiPattern: 'uiProgressSnapshot.ts acts as parallel progress authority',
  },
]

export type ReconcileFindingKind =
  | 'progress_mismatch'
  | 'headline_stale'
  | 'queue_appendix_diverge'
  | 'catalog_version'
  | 'catalog_spine_parity'
  | 'catalog_milestone_refs'
  | 'stream_next_task'

export type ReconcileGateRule = {
  id: string
  kind: ReconcileFindingKind
  condition: string
  severity: 'blocker' | 'warning'
  emit: string
}

/** Conditions that stamp ⚠️ BRIEFING_STALE at pack top (ArgoCD OutOfSync analogue). */
export const RECONCILE_GATE_RULES: ReconcileGateRule[] = [
  {
    id: 'gate-queue-vs-spine-done',
    kind: 'progress_mismatch',
    condition: 'Any wave marked done in queue while index >= stream.done (unless sign-off overlay)',
    severity: 'blocker',
    emit: 'BRIEFING_STALE — queue/spine done mismatch',
  },
  {
    id: 'gate-appendix-vs-queue',
    kind: 'queue_appendix_diverge',
    condition: 'Same wave id shows different status in lane queue vs domain appendix',
    severity: 'blocker',
    emit: 'BRIEFING_STALE — appendix/queue diverge',
  },
  {
    id: 'gate-headline-vs-next-task',
    kind: 'headline_stale',
    condition: 'focus.headline does not contain active wave id substring (D-D, e.g. "W3")',
    severity: 'warning',
    emit: 'BRIEFING_STALE — headline stale',
  },
  {
    id: 'gate-catalog-version',
    kind: 'catalog_version',
    condition: 'meta.catalog_version ≠ environments-catalog CATALOG_VERSION',
    severity: 'warning',
    emit: 'BRIEFING_STALE — catalog_version drift (CI: check_spine_catalog.sh)',
  },
  {
    id: 'gate-catalog-spine-parity',
    kind: 'catalog_spine_parity',
    condition:
      'Spine-bound catalog rows (deployMainline seq 4/5/7) embed progress prose — Constitution requires spineMilestoneId + Projection only',
    severity: 'warning',
    emit: 'CATALOG_DRIFT — catalog embeds live progress on spine-bound row (use Projection)',
  },
  {
    id: 'gate-catalog-milestone-refs',
    kind: 'catalog_milestone_refs',
    condition:
      'Architecture catalog milestone id (Delivery / Vision / Deploy Mainline) missing from spine milestones',
    severity: 'warning',
    emit: 'CATALOG_DRIFT — unknown milestone ref in catalog',
  },
  {
    id: 'gate-migrate-next-vs-lane',
    kind: 'stream_next_task',
    condition:
      'Selected lane stream is primary in_progress migrate stream AND lane next ≠ spine stream.next_task (runtime only)',
    severity: 'warning',
    emit: 'BRIEFING_STALE — track/lane next mismatch for active stream',
  },
]

export type SignalAxisRow = {
  axis: string
  analogue: string
  measures: string
  independentOf: string
}

/** Sync vs Health — do not conflate briefing truth with cluster health. */
export const SIGNAL_AXES: SignalAxisRow[] = [
  {
    axis: 'SYNC',
    analogue: 'ArgoCD Synced / OutOfSync',
    measures: 'Briefing views faithfully project spine + catalogs (generator reconcile)',
    independentOf: 'Whether prod matrix is green',
  },
  {
    axis: 'HEALTH',
    analogue: 'ArgoCD Healthy / Degraded',
    measures: 'Live matrix/cluster matches operational reality',
    independentOf: 'Whether briefing sections agree with each other',
  },
]

export type WritePathRow = {
  actor: string
  path: string
  mayWriteProgress: boolean
  audit: string
}

export const WRITE_PATHS: WritePathRow[] = [
  {
    actor: 'Owner UI sign-off',
    path: 'Console → Wave verify gate → ConfirmDialog → Mark delivered / Sign off → platform-api PATCH ops-context.yaml',
    mayWriteProgress: true,
    audit: 'platform-api actuation log',
  },
  {
    actor: 'Drift Agent L4 (approved)',
    path: 'DriftProposalPanel approve → agent/drift fix → git push → platform reload',
    mayWriteProgress: true,
    audit: 'remediation job + git commit',
  },
  {
    actor: 'Agent session (Cursor)',
    path: 'Read briefing only; Source Audit + Contradiction Report on first reply',
    mayWriteProgress: false,
    audit: 'session transcript',
  },
  {
    actor: 'Manual ops-context edit',
    path: 'Owner strategic edit (focus, milestones) — must update next_task + done atomically',
    mayWriteProgress: true,
    audit: 'git history',
  },
]

export type DriftLayerMapRow = {
  layer: string
  scanner: string
  scope: string
  coversBriefingReconcile: string
  targetExtension: string
}

/** Maps existing nightly drift stack to reconciliation catalog obligations. */
export const DRIFT_LAYER_MAP: DriftLayerMapRow[] = [
  {
    layer: 'L1 Deterministic',
    scanner: 'agent/drift/scan_layer1.py',
    scope: 'Catalog path refs, port literals',
    coversBriefingReconcile: 'Code/catalog file integrity',
    targetExtension: 'None required for content drift',
  },
  {
    layer: 'L2 API probe',
    scanner: 'agent/drift/scan_layer2.py',
    scope: 'platform-api L0 routes reachable',
    coversBriefingReconcile: 'HEALTH axis — live status section',
    targetExtension: 'Optional: GET /context shape for tracks.migrate.streams',
  },
  {
    layer: 'L3 Semantic',
    scanner: 'agent/drift/scan_layer3.py',
    scope: 'catalog_version, vision spine ids, trade-k8s reconcile gates, deployMainline ↔ spine parity',
    coversBriefingReconcile: 'Full SYNC parity — nightly scan = Console SYNC banner + CATALOG_DRIFT',
    targetExtension:
      'Covered: spineIndex contiguity, queue/appendix projection parity, queue-vs-done, headline-vs-next, migrate-next-vs-lane, catalog-spine-parity',
  },
  {
    layer: 'L4 Remediation',
    scanner: 'agent/drift/scan_layer4.py + DriftProposalPanel',
    scope: 'Owner-approved auto-fix PR',
    coversBriefingReconcile: 'Incremental spine/catalog fixes',
    targetExtension: 'Never rewrite entire briefing generator in one PR',
  },
]

export type BriefingSyncLoopStepSpec = {
  id: string
  label: string
  agentTaskId?: string
  scanner?: string
  description: string
}

/** Owner-visible automation loop — detect → propose → approve → fix (S10-B). */
export const BRIEFING_SYNC_LOOP_STEPS: BriefingSyncLoopStepSpec[] = [
  {
    id: 'runtime-sync',
    label: 'Runtime SYNC',
    description:
      'Console reconcile gate (reconcileBriefing.ts) — same rules as pack generation and lane SYNC banner',
  },
  {
    id: 'nightly-scan',
    label: 'Nightly scan L1–L3',
    agentTaskId: 'drift-brief',
    scanner: 'scan_layer1.py · scan_layer2.py · scan_layer3.py',
    description: 'Scheduled Drift · Brief — catalog integrity, API probes, briefing reconcile parity',
  },
  {
    id: 'l4-proposal',
    label: 'L4 proposal',
    scanner: 'scan_layer4.py → POST /api/v1/agent/drift-proposals',
    description: 'Packages Layer 1–3 failures into an Owner-reviewable proposal',
  },
  {
    id: 'owner-approval',
    label: 'Owner approval',
    description: 'DriftProposalPanel — fixes never run unattended (WRITE_PATHS governance)',
  },
  {
    id: 'drift-fix',
    label: 'Drift · Fix',
    agentTaskId: 'drift-autofix',
    description: 'Remediation runner patches catalog/YAML on agent/drift-* branch after approval',
  },
]

export type AntiPatternRow = {
  pattern: string
  why: string
  fix: string
  status?: 'resolved'
  resolvedIn?: string
}

export const ANTI_PATTERNS: AntiPatternRow[] = [
  {
    pattern: 'Dual progress: catalog delivery + spine done',
    why: 'Queue can show signed while spine says 0/12',
    fix: 'Remove delivery from catalog; sign-off increments spine only',
  },
  {
    pattern: 'Hand-written focus.headline',
    why: 'Drifts from stream.next_task within days',
    fix: 'Derive headline from active lane stream on sign-off',
  },
  {
    pattern: 'uiProgressSnapshot as wave progress',
    why: 'Fourth manual progress source in briefing pack',
    fix: 'UI notes only; wave progress in spine appendix header only',
  },
  {
    pattern: 'Silent contradictory pack',
    why: 'Agent trusts briefing; wrong task scope',
    fix: 'RECONCILE_GATE_RULES → BRIEFING_STALE banner + Source Audit protocol',
  },
  {
    pattern: 'deployMainlineCatalog hardcoded IN_PROGRESS vs spine SIGNED',
    why: 'Catalog showed active cutover while spine recorded historical sign-off',
    fix: 'Governance P6: MAINLINE_PHASE_DEFINITIONS use spineMilestoneId; resolveMainlinePhases(context) for live status',
    status: 'resolved',
    resolvedIn: 'Governance Phase 6',
  },
]

export const DATA_FLOW_MERMAID = `flowchart TB
  subgraph SoT["① Progress SoT"]
    SPINE["ops-context.yaml / GET /api/v1/context"]
  end
  subgraph SPEC["② Domain spec (read-only)"]
    CAT["*Catalog.ts — labels, verify, constraints"]
  end
  subgraph LIVE["③ Runtime"]
    PROBE["matrix / cluster / health"]
  end
  GEN["briefing generator render()"]
  GATE{"reconcile gate"}
  SPINE --> GEN
  CAT --> GEN
  PROBE --> GEN
  GEN --> GATE
  GATE -->|SYNCED| PACK["Agent Session Briefing"]
  GATE -->|STALE| STALE["⚠️ BRIEFING_STALE + findings"]
  STALE --> PACK`

export const CROSS_REFERENCES = {
  agentProtocol: 'console/src/lib/architecture/agentProtocolCatalog.ts — CONTEXT_PACK_LAYERS[0]',
  blueprint: 'console/src/lib/architecture/blueprintCatalog.ts — AI_PLATFORM_CAPABILITIES.Maintenance',
  alignmentPack: 'console/src/lib/briefing/buildBriefingAlignmentPack.ts — audit checklist',
  generator: 'console/src/lib/briefing/buildBriefingPack.ts',
  laneQueue: 'console/src/lib/briefing/workLanes.ts — buildTradeK8sNativeQueue',
  ciCheck: 'scripts/ci/check_spine_catalog.sh',
  driftBrief: 'agentTaskCatalog drift-brief — nightly Layer 1–3',
  driftAutofix: 'agentTaskCatalog drift-autofix — L4 after Owner approve',
} as const

/** Build LLM-optimized text for Briefing Reconciliation page and Agent packs. */
export function buildBriefingReconciliationLlmPack(ctx?: OpsContextResponse): string {
  const lines: string[] = [
    '# Bifrost Ops — Briefing Reconciliation (Agent context single-source discipline)',
    `# Source: ${BRIEFING_RECONCILIATION_SOURCE} v${BRIEFING_RECONCILIATION_VERSION}`,
    '',
    '## Statement',
    RECONCILIATION_STATEMENT,
    '',
    '## Design decisions (Owner-ratified)',
    ...DESIGN_DECISIONS.map(d => `- **${d.id}** ${d.topic}: ${d.decision}`),
    '',
    '## Gate behavior',
    ...GATE_BEHAVIOR.map(g => `- **${g.severity}**: ${g.onHit}`),
    '',
    '## Data flow (ideal)',
    '```mermaid',
    DATA_FLOW_MERMAID,
    '```',
    '',
    '## Source of truth layers',
    ...SOURCE_OF_TRUTH_LAYERS.map(
      r =>
        `- **${r.layer}** [${r.source}]: ${r.role} | progress=${r.mayHoldProgress ? 'yes' : 'no'} | ${r.authority}`,
    ),
    '',
    '## Projection rules',
    ...PROJECTION_RULES.map(
      r =>
        `- **${r.briefingView}** ← ${r.derivesFrom}\n  - rule: ${r.rule}\n  - anti: ${r.antiPattern}`,
    ),
    '',
    '## Reconcile gate (BRIEFING_STALE)',
    ...RECONCILE_GATE_RULES.map(
      r => `- [${r.severity}] ${r.id}: ${r.condition} → ${r.emit}`,
    ),
    '',
    '## Signal axes',
    ...SIGNAL_AXES.map(
      a => `- **${a.axis}** (${a.analogue}): ${a.measures} | independent of: ${a.independentOf}`,
    ),
    '',
    '## Write paths',
    ...WRITE_PATHS.map(
      w => `- ${w.actor}: ${w.path} | writes progress=${w.mayWriteProgress} | audit: ${w.audit}`,
    ),
    '',
    '## Drift layer map',
    ...DRIFT_LAYER_MAP.map(
      d =>
        `- **${d.layer}** (\`${d.scanner}\`): ${d.scope} | briefing: ${d.coversBriefingReconcile} | extend: ${d.targetExtension}`,
    ),
    '',
    '## Anti-patterns',
    ...ANTI_PATTERNS.map(a => `- **${a.pattern}**: ${a.why} → fix: ${a.fix}`),
    '',
    '## Cross-references',
    ...Object.entries(CROSS_REFERENCES).map(([k, v]) => `- ${k}: \`${v}\``),
  ]

  if (ctx != null) {
    const tk = ctx.tracks?.migrate?.streams.find(s => s.id === 'trade-k8s-native')
    lines.push(
      '',
      '## Live spine snapshot (trade-k8s-native)',
      tk != null
        ? `- done=${tk.done}/${tk.total} status=${tk.status} next_task=${tk.next_task ?? '(none)'}`
        : '- stream not loaded',
      `- focus.headline: ${ctx.focus.headline}`,
    )
  }

  return lines.join('\n')
}
