/**
 * Remediation runner task catalog — scopes the Agent can run.
 * Authoritative for Agent Desk "Task capabilities" panel.
 */

export type AgentTaskTier = 'manual' | 'automated' | 'escalation'

export type AgentTaskEntry = {
  id: string
  scope: string
  label: string
  tier: AgentTaskTier
  /** Where the operator starts or observes this task */
  entryPoint: string
  trigger: string
  /** Parent scope id when tier === escalation */
  parentId?: string
  description: string
}

export const AGENT_TASK_CATALOG: AgentTaskEntry[] = [
  {
    id: 'ops',
    scope: 'agent-desk',
    label: 'Ops',
    tier: 'manual',
    entryPoint: 'Agent Desk → Ops scope',
    trigger: 'Operator sends a prompt',
    description: 'General SRE assistant — cluster health, spine, kubectl read, safe actuation via platform-api.',
  },
  {
    id: 'release',
    scope: 'release',
    label: 'Release',
    tier: 'manual',
    entryPoint: 'Agent Desk → Release scope · Platform release quick prompt',
    trigger: 'Operator starts a STG → PROD release',
    description: 'Commit & push via Git Bridge, deploy pipelines, STG/PROD gates, release report.',
  },
  {
    id: 'release-fix',
    scope: 'release-fix',
    label: 'Release Fix',
    tier: 'escalation',
    parentId: 'release',
    entryPoint: 'Spawned inside a Release task · visible in Recent tasks',
    trigger: 'Release Agent escalates after code/config failure (operator approves)',
    description: 'Diagnose failure, patch code/manifest, commit & push so Release can retry the failed phase.',
  },
  {
    id: 'cluster-auto',
    scope: 'cluster_issues_full_auto',
    label: 'Cluster auto-remediate',
    tier: 'manual',
    entryPoint: 'Operate → Cluster → Auto-remediate',
    trigger: 'Operator clicks Auto-remediate with open cluster issues',
    description: 'Diagnose reported pod/node issues and apply safe remediation (restart, delete debug pods, etc.).',
  },
  {
    id: 'drift-autofix',
    scope: 'nightly-drift-autofix',
    label: 'Drift auto-fix',
    tier: 'manual',
    entryPoint: 'Agent Briefing → approve drift proposal',
    trigger: 'Owner approves a nightly drift Layer-4 proposal',
    description: 'Edit bifrost-platform catalog/YAML/scanners; branch agent/drift-YYYYMMDD, commit, push.',
  },
  {
    id: 'drift-brief',
    scope: 'nightly-drift-briefing',
    label: 'Nightly drift brief',
    tier: 'automated',
    entryPoint: 'Agent Briefing · nightly_drift.sh',
    trigger: 'Scheduled nightly scan (primary runner)',
    description: 'Read-only Layer 1–3 drift summary — report only, no fixes.',
  },
  {
    id: 'nightly-health',
    scope: 'nightly-health-check',
    label: 'Nightly health check',
    tier: 'automated',
    entryPoint: 'Skills & Schedules · launchd health job',
    trigger: 'Scheduled verification pass',
    description: 'Confirm cluster healthy when checker reports zero issues; no destructive actions.',
  },
]

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
