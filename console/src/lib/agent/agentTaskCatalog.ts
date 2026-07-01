/**
 * Remediation runner task catalog — scopes the Agent can run.
 * Single source of truth for every task's display label (Agent Desk
 * "Task capabilities" panel, Recent tasks timeline, init brief, cluster panel).
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
 * When adding a task: pick the Domain + Action from the vocabularies above,
 * give it a kebab-case scope, and add it here — do not hard-code labels elsewhere.
 */

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
]

export const AGENT_TASK_CATALOG: AgentTaskEntry[] = [
  {
    id: 'ops',
    scope: 'agent-desk',
    label: 'Ops · Session',
    domain: 'Operator',
    action: 'Session',
    tier: 'manual',
    entryPoint: 'Agent Desk → Ops scope',
    trigger: 'Operator sends a prompt',
    description: 'General SRE assistant — cluster health, spine, kubectl read, safe actuation via platform-api.',
  },
  {
    id: 'release',
    scope: 'release',
    label: 'Platform · Release',
    domain: 'Platform',
    action: 'Release',
    tier: 'manual',
    entryPoint: 'Agent Desk → Release scope · Platform release quick prompt',
    trigger: 'Operator starts a STG → PROD release of the Ops Platform',
    description: 'Release the Ops Platform itself (bifrost-deliver-platform): commit & push via Git Bridge, STG/PROD pipelines + gates, release report.',
  },
  {
    id: 'release-fix',
    scope: 'release-fix',
    label: 'Platform · Release Fix',
    domain: 'Platform',
    action: 'Release Fix',
    tier: 'escalation',
    parentId: 'release',
    entryPoint: 'Spawned inside a Release task · visible in Recent tasks',
    trigger: 'Release Agent escalates after code/config failure (operator approves)',
    description: 'Diagnose failure, patch code/manifest, commit & push so Release can retry the failed phase.',
  },
  {
    id: 'cluster-auto',
    scope: 'cluster_issues_full_auto',
    label: 'Cluster · Remediate',
    domain: 'Cluster',
    action: 'Remediate',
    tier: 'manual',
    entryPoint: 'Operate → Cluster → Auto-remediate',
    trigger: 'Operator clicks Auto-remediate with open cluster issues',
    description: 'Diagnose reported pod/node issues and apply safe remediation (restart, delete debug pods, etc.).',
  },
  {
    id: 'drift-autofix',
    scope: 'nightly-drift-autofix',
    label: 'Drift · Fix',
    domain: 'Drift',
    action: 'Fix',
    tier: 'manual',
    entryPoint: 'Agent Briefing → approve drift proposal',
    trigger: 'Owner approves a nightly drift Layer-4 proposal',
    description: 'Edit bifrost-platform catalog/YAML/scanners per briefingReconciliationCatalog WRITE_PATHS; branch agent/drift-YYYYMMDD, commit, push.',
  },
  {
    id: 'drift-brief',
    scope: 'nightly-drift-briefing',
    label: 'Drift · Brief',
    domain: 'Drift',
    action: 'Brief',
    tier: 'automated',
    entryPoint: 'Agent Briefing · nightly_drift.sh',
    trigger: 'Scheduled nightly scan (primary runner)',
    description: 'Read-only Layer 1–3 drift summary — report only, no fixes. L3 extends per briefingReconciliationCatalog DRIFT_LAYER_MAP.',
  },
  {
    id: 'nightly-health',
    scope: 'nightly-health-check',
    label: 'Health · Check',
    domain: 'Health',
    action: 'Check',
    tier: 'automated',
    entryPoint: 'Skills & Schedules · launchd health job',
    trigger: 'Scheduled verification pass',
    aliases: ['Nightly scheduled health verification'],
    description: 'Confirm cluster healthy when checker reports zero issues; no destructive actions.',
  },
  {
    id: 'post-fix-verification',
    scope: 'post-fix-verification',
    label: 'Health · Post-fix',
    domain: 'Health',
    action: 'Check',
    tier: 'manual',
    entryPoint: 'Remediation runner · verifying phase (automatic after every job)',
    trigger: 'Agent job completes — runner calls verify_mission_snapshot',
    description:
      'Fresh matrix reprobe + verify_payload; post_fix_verification.passed must be true before declaring remediation success.',
  },
]

/** scope (and legacy aliases) → display label. Built once from the catalog. */
const SCOPE_LABEL_INDEX: Record<string, string> = (() => {
  const index: Record<string, string> = {}
  for (const entry of AGENT_TASK_CATALOG) {
    index[entry.scope] = entry.label
    for (const alias of entry.aliases ?? []) index[alias] = entry.label
  }
  return index
})()

/**
 * Resolve any scope string to its display label. Catalog scopes and their
 * historical aliases map directly; unknown scopes are prettified to Title Case
 * so a missing catalog entry degrades gracefully instead of showing raw ids.
 */
export function scopeToLabel(scope?: string | null): string {
  if (scope == null || scope.trim() === '') return 'Agent session'
  const hit = SCOPE_LABEL_INDEX[scope]
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
  return AGENT_TASK_CATALOG.filter(t => t.tier === 'manual')
}

export function escalationChildren(parentId: string): AgentTaskEntry[] {
  return AGENT_TASK_CATALOG.filter(t => t.parentId === parentId)
}

const DOMAIN_ORDER: AgentTaskDomain[] = ['Operator', 'Platform', 'Cluster', 'Health', 'Drift', 'Trade']

export function agentTasksByDomain(): { domain: AgentTaskDomain; tasks: AgentTaskEntry[] }[] {
  const groups = new Map<AgentTaskDomain, AgentTaskEntry[]>()
  for (const task of AGENT_TASK_CATALOG) {
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
  return AGENT_TASK_CATALOG.find(t => t.id === id)
}

export function agentTaskRelationKindLabel(kind: AgentTaskRelationKind): string {
  if (kind === 'escalation') return 'Escalation'
  if (kind === 'approval') return 'Approval chain'
  return 'On failure'
}

export function agentSystemSummary() {
  const tiers = { manual: 0, automated: 0, escalation: 0 }
  for (const t of AGENT_TASK_CATALOG) tiers[t.tier] += 1
  return {
    runtimeCount: 1,
    capabilityCount: AGENT_TASK_CATALOG.length,
    domainCount: new Set(AGENT_TASK_CATALOG.map(t => t.domain)).size,
    relationCount: AGENT_TASK_RELATIONS.length,
    manualCount: tiers.manual,
    scheduledCount: tiers.automated,
    escalationCount: tiers.escalation,
  }
}
