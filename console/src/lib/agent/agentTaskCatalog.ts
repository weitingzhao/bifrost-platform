/**
 * Remediation runner task catalog — Console display + hydration.
 * Flight Director trust matrix (Go) + this catalog load `config/agent-tasks.yaml`
 * via GET /api/v1/agent-tasks. Core ids/scopes/labels/tiers come from the API;
 * entryPoint / trigger / description / aliases / parentId stay as UI overlays here.
 *
 * NAMING CONVENTION — `<Domain> · <Action>`
 * ------------------------------------------
 * Two controlled vocabularies: the Domain sets the hierarchy (same domain →
 * related tasks group together), the Action sets the progression (read → write,
 * escalation runs deeper). The trigger (nightly / scheduled) is expressed by
 * `tier`, NOT the name.
 *
 *   Domain (subject the task acts on, establishes hierarchy):
 *     Operator  — human-driven open session, any domain
 *     Platform  — the Ops Platform control plane itself (bifrost-platform)
 *     Trade     — the managed trade stack (bifrost-trade-*)   [reserved]
 *     Cluster   — K3s infrastructure (pods / nodes)
 *     Drift     — governance / config drift
 *     Health    — system health verification
 *
 *   Action (ordered by depth / risk, establishes progression):
 *     Session   — open interactive assistant (Operator only)
 *     Brief     — read-only narrative summary
 *     Check     — read-only verification (pass / fail)
 *     Remediate — runtime corrective action (restart / delete / scale), no code
 *     Fix       — patch source code / config, commit & push
 *     Release   — ship through STG → PROD
 *     Release Fix — escalation child of Release (diagnose + patch + retry)
 *
 * Field rules:
 * - `scope`  = backend contract id. Lower-case kebab-case. Existing scopes are
 *              kept as-is for backward compatibility; map any legacy /
 *              non-conforming strings through `aliases` instead of renaming.
 * - `label`  = `Domain · Action`, Title Case, domain segment first.
 * - `aliases`= historical / non-conforming scope strings that must still resolve
 *              to this entry's label (e.g. persisted jobs from before a fix).
 *
 * When adding a task: edit config/agent-tasks.yaml (SSOT), then add UI overlay
 * fields here if needed — do not hard-code labels elsewhere.
 */

import type { AgentTaskApi } from '@/api/agentTasks'

export type AgentTaskTier = 'manual' | 'automated' | 'escalation'

export type AgentTaskDomain =
  | 'Operator'
  | 'Platform'
  | 'Trade'
  | 'Cluster'
  | 'Drift'
  | 'Health'

export type AgentTaskEntry = {
  id: string
  scope: string
  label: string
  domain: AgentTaskDomain
  action: string
  tier: AgentTaskTier
  /** Where the operator starts or observes this task */
  entryPoint: string
  trigger: string
  /** Parent catalog id when tier === escalation */
  parentId?: string
  /** Legacy / non-conforming scope strings that still resolve to this label */
  aliases?: string[]
  description: string
}

/** Single execution runtime — every capability scope runs through this process. */
export const AGENT_RUNTIME = {
  id: 'remediation-runner',
  label: 'Remediation runner',
  host: 'Mac Mini (remote agent host)',
  port: 8781,
  sdk: 'Cursor SDK',
  description:
    'One Node process executes every capability below. Jobs persist as JSON on runner disk and in platform-api JobStore.',
} as const

export type AgentTaskRelationKind = 'escalation' | 'approval' | 'on-failure'

export type AgentTaskRelation = {
  fromId: string
  toId: string
  kind: AgentTaskRelationKind
  label: string
}

/** Directed edges between capabilities (escalation, approval, failure handoff). */
export const AGENT_TASK_RELATIONS: AgentTaskRelation[] = [
  {
    fromId: 'release',
    toId: 'release-fix',
    kind: 'escalation',
    label: 'Release phase fails (code/config)',
  },
  {
    fromId: 'trade-deploy',
    toId: 'trade-release-fix',
    kind: 'escalation',
    label: 'Trade deliver phase fails (code/GitOps)',
  },
  {
    fromId: 'deliver-stg-recover',
    toId: 'trade-release-fix',
    kind: 'escalation',
    label: 'Pipeline failure needs repo/manifest fix',
  },
  {
    fromId: 'deliver-stg-recover',
    toId: 'gitops-config-repair',
    kind: 'on-failure',
    label: 'GitOps ComparisonError or gitops-sync fail',
  },
  {
    fromId: 'stale-pipeline-triage',
    toId: 'deliver-stg-recover',
    kind: 'on-failure',
    label: 'Classified as stale pipeline fail',
  },
  {
    fromId: 'trade-deploy',
    toId: 'satellite-bus-ingest-triage',
    kind: 'on-failure',
    label: 'Bus Status ingest rows look inactive but STG topology is policy-off/managed',
  },
  {
    fromId: 'drift-brief',
    toId: 'drift-autofix',
    kind: 'approval',
    label: 'Owner approves Layer-4 proposal',
  },
  {
    fromId: 'nightly-health',
    toId: 'cluster-auto',
    kind: 'on-failure',
    label: 'Checker reports open issues',
  },
  {
    fromId: 'daily-ops-checklist-run',
    toId: 'cluster-auto',
    kind: 'on-failure',
    label: 'Checklist infra/data items fail after retry',
  },
  {
    fromId: 'daily-ops-checklist-run',
    toId: 'deliver-stg-recover',
    kind: 'on-failure',
    label: 'Checklist release-readiness items fail after retry',
  },
]

type DisplayOverlay = {
  entryPoint: string
  trigger: string
  description: string
  parentId?: string
  aliases?: string[]
  /** Override domain/action when API omits them. */
  domain?: AgentTaskDomain
  action?: string
}

/** UI-only fields keyed by task id — not part of Flight Director YAML SSOT. */
const AGENT_TASK_DISPLAY: Record<string, DisplayOverlay> = {
  'satellite-bus-ingest-triage': {
    entryPoint: 'Task CC (Satellite Deploy) · Agent Triage · or Satellite → Bus Status',
    trigger: 'Operator investigates misleading ingest inactive rows from Task CC or Bus Status',
    description:
      'Cross-check bus-deep ingest vs monitor.socket vs ib-gateway plugin; classify policy-off/managed-ok/false-alarm; safe L1 restart only (no daemon scale, D10).',
  },
  'operator-plane-remediate': {
    entryPoint: 'Engineer → Operator Plane (L-1) · AI Fix',
    trigger: 'Operator clicks AI Fix when bridge/deploy probes fail',
    description:
      'Diagnose Git Bridge, agent deploy, and MCP bridge errors on L-1; guide operator through Mac Pro/Mini host fixes via manual steps.',
  },
  ops: {
    entryPoint: 'Agent Desk → Ops scope',
    trigger: 'Operator sends a prompt',
    description:
      'General SRE assistant — cluster health, spine, kubectl read, safe actuation via platform-api.',
  },
  release: {
    entryPoint: 'Agent Desk → Release scope · Platform release quick prompt',
    trigger: 'Operator starts a STG → PROD release of the Ops Platform',
    description:
      'Release the Ops Platform (bifrost-deliver-platform): commit & push via Git Bridge — **must include bifrost-ui on main** (Kaniko sibling COPY), STG/PROD pipelines + gates, Phase F console CSS smoke; Phase G install-ib-gateway when plugin repo changed.',
  },
  'release-fix': {
    parentId: 'release',
    entryPoint: 'Spawned inside a Release task · visible in Recent tasks',
    trigger: 'Release Agent escalates after code/config failure (operator approves)',
    description: 'Diagnose failure, patch code/manifest, commit & push so Release can retry the failed phase.',
  },
  'trade-deploy': {
    entryPoint: 'Control Room → Launch Pad · Agent Deploy · Agent Desk trade-deploy scope',
    trigger: 'Operator starts STG → PROD Trade stack deliver (bifrost-deliver-stg / bifrost-deliver-prod)',
    description:
      'Deliver Trade stack: mirror sync + Dockerfile CMs → Kaniko build → rollout bifrost-stg/prod → STG smoke + release gates. Does NOT enable live trading (D10).',
  },
  'deliver-stg-recover': {
    entryPoint: 'Task CC Agent Fix (signal dispatch) · Control Room · Cluster Triage · Deliver-stg Fix',
    trigger: 'Last bifrost-deliver-stg failed (especially stale-fail: pipeline red + STG smoke green)',
    description:
      'L1: get_delivery_run_logs → identify failing Tekton task → fix rollout/GitOps → delete_pipeline_run for terminal leftovers → re-run deliver-stg. Distinct from K8s node outages.',
  },
  'trade-release-fix': {
    parentId: 'trade-deploy',
    entryPoint: 'Spawned from trade-deploy or deliver-stg-recover · Recent tasks',
    trigger: 'Trade deliver failure requires bifrost-trade-infra / trade-* repo patch',
    description: 'Diagnose and patch trade GitOps/code; commit & push so deliver-stg can retry.',
  },
  'gitops-config-repair': {
    entryPoint: 'Cluster triage · Agent Fix · deliver-stg-recover escalation',
    trigger: 'Argo ComparisonError, missing programs/config path, gitops-sync pipeline failure',
    description: 'Restore GitOps manifests, mirror sync, Argo sync, re-run deliver pipeline.',
  },
  'defect-pattern-remediate': {
    entryPoint: 'Defects → Recurring Patterns · Fix',
    trigger: 'Operator fixes a high-recurrence Defects pattern via routed playbook',
    description:
      'Route pattern to deliver-stg-recover, cluster remediate, or release-fix — reduce agent-desk ad-hoc.',
  },
  'cluster-auto': {
    entryPoint: 'Rocket → Cluster → Auto-remediate',
    trigger: 'Operator clicks Auto-remediate with open cluster issues',
    description:
      'Diagnose reported pod/node issues and apply safe remediation (restart, delete debug pods, etc.).',
  },
  'drift-autofix': {
    entryPoint: 'Agent Briefing → approve drift proposal',
    trigger: 'Owner approves a nightly drift Layer-4 proposal',
    description:
      'Edit bifrost-platform catalog/YAML/scanners per briefingReconciliationCatalog WRITE_PATHS; branch agent/drift-YYYYMMDD, commit, push.',
  },
  'drift-brief': {
    entryPoint: 'Agent Briefing · nightly_drift.sh',
    trigger: 'Scheduled nightly scan (primary runner)',
    description:
      'Read-only Layer 1–3 drift summary — report only, no fixes. L3 extends per briefingReconciliationCatalog DRIFT_LAYER_MAP.',
  },
  'nightly-health': {
    entryPoint: 'Skills & Schedules · launchd health job',
    trigger: 'Scheduled verification pass',
    aliases: ['Nightly scheduled health verification'],
    description: 'Confirm cluster healthy when checker reports zero issues; no destructive actions.',
  },
  'stale-pipeline-triage': {
    entryPoint: 'Skills & Schedules · optional pre-health job',
    trigger: 'Scheduled L0 classification: pipeline fail vs runtime smoke',
    description: 'Read-only: classify stale-fail vs real outage; no actuation.',
  },
  'daily-ops-checklist-run': {
    entryPoint:
      'Daily Ops → Checklist · AI Check · scripts/agent/daily_ops_checklist.sh · launchd / market-open',
    trigger: 'TCC AI Check or scheduled Daily Ops Checklist probe (18 items)',
    description:
      'L0 prober (AI Check): verify_mission_snapshot + bridge/gitops/smoke/pipelines → report_checklist_signals. Not Operator Plane Fix. Auto-dispatch gated by fixCapability (D10 skip IB).',
  },
  'platform-self-health-recover': {
    entryPoint: 'Cluster Failure triage · Control self-health row',
    trigger: 'Control plane self-health probes failing (console/API routes)',
    description: 'Restart platform-prod workloads; verify console/API NodePort reachability.',
  },
  'registry-pull-recover': {
    entryPoint: 'Cluster Failure triage · ImagePull rows',
    trigger: 'ImagePullBackOff / ErrImagePull from registry.cicd',
    description: 'Diagnose registry reachability and image tags; safe rollout restart after fix.',
  },
  'post-fix-verification': {
    entryPoint: 'Remediation runner · verifying phase (automatic after every job)',
    trigger: 'Agent job completes — runner calls verify_mission_snapshot',
    description:
      'Fresh matrix reprobe + verify_payload; post_fix_verification.passed must be true before declaring remediation success.',
  },
  'hermes-first-task': {
    entryPoint: 'Control Room · Copy first-task prompt · Nous Hermes dashboard',
    trigger: 'Owner onboarding — first L0 read-only Mission health pass via Hermes + platform MCP',
    description:
      'Call get_hermes_readiness, then run hermes-mission-health-l0 prompt (verify_mission_snapshot + matrix). No actuation.',
  },
}

let catalog: AgentTaskEntry[] = []
let scopeLabelIndex: Record<string, string> = {}

function rebuildScopeIndex() {
  const index: Record<string, string> = {}
  for (const entry of catalog) {
    index[entry.scope] = entry.label
    for (const alias of entry.aliases ?? []) index[alias] = entry.label
  }
  scopeLabelIndex = index
}

function asTier(t: string): AgentTaskTier {
  if (t === 'automated' || t === 'escalation' || t === 'manual') return t
  return 'manual'
}

function asDomain(d: string | undefined, fallback: AgentTaskDomain = 'Platform'): AgentTaskDomain {
  const allowed: AgentTaskDomain[] = ['Operator', 'Platform', 'Trade', 'Cluster', 'Drift', 'Health']
  if (d != null && (allowed as string[]).includes(d)) return d as AgentTaskDomain
  return fallback
}

export function mapAgentTaskApiToEntry(api: AgentTaskApi): AgentTaskEntry {
  const overlay = AGENT_TASK_DISPLAY[api.id]
  return {
    id: api.id,
    scope: api.scope,
    label: api.label,
    domain: asDomain(api.domain ?? overlay?.domain),
    action: api.action ?? overlay?.action ?? '',
    tier: asTier(api.tier),
    entryPoint: overlay?.entryPoint ?? '—',
    trigger: overlay?.trigger ?? '—',
    description: overlay?.description ?? '',
    parentId: overlay?.parentId,
    aliases: overlay?.aliases,
  }
}

/** Replace in-memory catalog (hydrated from GET /api/v1/agent-tasks). */
export function setAgentTaskCatalog(tasks: AgentTaskEntry[]): void {
  catalog = [...tasks]
  rebuildScopeIndex()
}

export function allAgentTasks(): AgentTaskEntry[] {
  return catalog
}

/**
 * Resolve any scope string to its display label. Catalog scopes and their
 * historical aliases map directly; unknown scopes are prettified to Title Case
 * so a missing catalog entry degrades gracefully instead of showing raw ids.
 */
export function scopeToLabel(scope?: string | null): string {
  if (scope == null || scope.trim() === '') return 'Agent session'
  const hit = scopeLabelIndex[scope]
  if (hit != null) return hit
  return scope
    .replace(/[_-]+/g, ' ')
    .trim()
    .replace(/\b\w/g, c => c.toUpperCase())
}

export const AGENT_TASK_DOCTRINE_LINKS = [
  { label: 'Agent Protocol', tab: 'agent-protocol' as const, hint: 'Modes, escalation rules, forbidden actions' },
  { label: 'MCP Contract', tab: 'mcp-contract' as const, hint: 'platform-api MCP tools (read/actuate)' },
]

export function agentTaskTierLabel(tier: AgentTaskTier): string {
  if (tier === 'manual') return 'Manual'
  if (tier === 'automated') return 'Scheduled'
  return 'Escalation'
}

export function manualAgentTasks(): AgentTaskEntry[] {
  return catalog.filter(t => t.tier === 'manual')
}

export function escalationChildren(parentId: string): AgentTaskEntry[] {
  return catalog.filter(t => t.parentId === parentId)
}

const DOMAIN_ORDER: AgentTaskDomain[] = ['Operator', 'Platform', 'Cluster', 'Health', 'Drift', 'Trade']

export function agentTasksByDomain(): { domain: AgentTaskDomain; tasks: AgentTaskEntry[] }[] {
  const groups = new Map<AgentTaskDomain, AgentTaskEntry[]>()
  for (const task of catalog) {
    const list = groups.get(task.domain) ?? []
    list.push(task)
    groups.set(task.domain, list)
  }
  return DOMAIN_ORDER.filter(d => groups.has(d)).map(domain => ({
    domain,
    tasks: groups.get(domain)!,
  }))
}

export function catalogTaskById(id: string): AgentTaskEntry | undefined {
  return catalog.find(t => t.id === id)
}

export function agentTaskRelationKindLabel(kind: AgentTaskRelationKind): string {
  if (kind === 'escalation') return 'Escalation'
  if (kind === 'approval') return 'Approval chain'
  return 'On failure'
}

export function agentSystemSummary() {
  const tiers = { manual: 0, automated: 0, escalation: 0 }
  for (const t of catalog) tiers[t.tier] += 1
  return {
    runtimeCount: 1,
    capabilityCount: catalog.length,
    domainCount: new Set(catalog.map(t => t.domain)).size,
    relationCount: AGENT_TASK_RELATIONS.length,
    manualCount: tiers.manual,
    scheduledCount: tiers.automated,
    escalationCount: tiers.escalation,
  }
}
