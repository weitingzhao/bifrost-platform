/**
 * System Domain catalog — Apollo seven-domain taxonomy (Console sidebar planes).
 *
 * Authoritative for Ops Console → Governance → Blueprint / Agent Protocol,
 * and for Defects / Audit / Agent scope projection onto a shared mental model.
 *
 * Distinct from AgentTaskDomain (Operator|Platform|Trade|…) which classifies
 * remediation *task subjects*, not Console system domains.
 */

import type { DenseTagVariant } from '@bifrost/ui'
import type { LucideIcon } from 'lucide-react'
import {
  BookOpen,
  Bot,
  Building2,
  Gauge,
  Handshake,
  Rocket,
  Satellite,
} from 'lucide-react'
import type { ConsoleNavPlane } from '@/lib/consoleNavConfig'

export const SYSTEM_DOMAIN_VERSION = '2026-07-20'
export const SYSTEM_DOMAIN_SOURCE = 'console/src/lib/architecture/systemDomainCatalog.ts'

/** Kebab ids — stable for filters, maps, and LLM packs. */
export type SystemDomainId =
  | 'mission-control'
  | 'rocket'
  | 'ground-systems'
  | 'satellite'
  | 'subcontractors'
  | 'engineer'
  | 'governance'

export type SystemDomainRow = {
  id: SystemDomainId
  /** Display label — equals ConsoleNavPlane / sidebar group label. */
  label: ConsoleNavPlane
  purpose: string
}

/**
 * Apollo seven domains — keep label strings identical to ConsoleNavPlane
 * in consoleNavConfig.ts. Six domains appear in the sidebar rail; Governance
 * is reached via the shell User menu (still a first-class plane for catalogs).
 */
export const SYSTEM_DOMAINS: SystemDomainRow[] = [
  {
    id: 'mission-control',
    label: 'Mission Control',
    purpose:
      'Cross-domain ops hub — Task CC (execute) → Control Room (posture) → Observability (health); Defects & Audit (collapsible records); topology drill-down sheet',
  },
  {
    id: 'rocket',
    label: 'Rocket',
    purpose: 'Ops Platform itself — K8s cluster (incl. Facility constraints), Rocket Health, Launch Rocket, platform CI/CD',
  },
  {
    id: 'ground-systems',
    label: 'Ground Systems',
    purpose: 'Infrastructure — network (UniFi); SSH via shell Operator Dock Console slot. K3s nodes → Rocket → Cluster',
  },
  {
    id: 'satellite',
    label: 'Satellite',
    purpose: 'Trade payload — bus, Satellite Health (Probes + Runtime), Deploy Satellite',
  },
  {
    id: 'subcontractors',
    label: 'Subcontractors',
    purpose:
      'External plugins — Plugin Gallery observes IB Gateway + Market Data (pipeline + optional readiness_rollup); publish via Launch Plugin',
  },
  {
    id: 'engineer',
    label: 'Engineer',
    purpose: 'AI Agent workspace — Desk, Briefing, Operator Plane, trust (fate-isolated)',
  },
  {
    id: 'governance',
    label: 'Governance',
    purpose: 'Reference library — Blueprint, Agent Protocol, standards, AI strategy',
  },
]

/** Outline DenseTag colors for Domain chips (distinct from Origin filled chips). */
export const SYSTEM_DOMAIN_VARIANT: Record<SystemDomainId, DenseTagVariant> = {
  'mission-control': 'warning',
  rocket: 'info',
  'ground-systems': 'success',
  satellite: 'category',
  subcontractors: 'neutral',
  engineer: 'info',
  governance: 'neutral',
}

/** Same Lucide icons as CONSOLE_NAV_GROUPS — Domain chips stay visually distinct from kind tags. */
export const SYSTEM_DOMAIN_ICON: Record<SystemDomainId, LucideIcon> = {
  'mission-control': Gauge,
  rocket: Rocket,
  'ground-systems': Building2,
  satellite: Satellite,
  subcontractors: Handshake,
  engineer: Bot,
  governance: BookOpen,
}

const DOMAIN_BY_ID = Object.fromEntries(SYSTEM_DOMAINS.map(d => [d.id, d])) as Record<
  SystemDomainId,
  SystemDomainRow
>

export function systemDomainLabel(id: SystemDomainId): string {
  return DOMAIN_BY_ID[id].label
}

/**
 * Explicit remediation scope → System Domain.
 * Keep in sync with agentScopes.ts / agentTaskCatalog display scopes.
 */
export const SCOPE_TO_SYSTEM_DOMAIN: Record<string, SystemDomainId> = {
  // Rocket — platform / cluster / CI
  release: 'rocket',
  'release-fix': 'rocket',
  'deliver-stg-recover': 'rocket',
  'gitops-config-repair': 'rocket',
  'cluster-auto': 'rocket',
  'cluster_issues_full_auto': 'rocket',
  'data-layer-backup': 'rocket',
  'data-layer-clone': 'rocket',
  'platform-self-health-recover': 'rocket',
  'registry-pull-recover': 'rocket',
  'platform-workload-recover': 'rocket',
  'cicd-domain-recover': 'rocket',
  'stale-pipeline-triage': 'rocket',

  // Satellite — trade payload
  'trade-deploy': 'satellite',
  'trade-release-fix': 'satellite',
  'satellite-bus-ingest-triage': 'satellite',

  // Subcontractors — plugins / vendors
  'ib-gateway': 'subcontractors',
  'ib-gateway-reconnect': 'subcontractors',
  'plugin-launch': 'subcontractors',
  'plugin-release': 'subcontractors',
  'market-data': 'subcontractors',
  'market-data-subcontractor': 'subcontractors',

  // Ground
  network: 'ground-systems',
  'network-firewall': 'ground-systems',
  'unifi-zone': 'ground-systems',

  // Engineer — agent / operator plane
  ops: 'engineer',
  'agent-desk': 'engineer',
  queue: 'engineer',
  'agent-capability': 'engineer',
  'operator-plane-remediate': 'engineer',
  'git-dirty-remediate': 'engineer',

  // Governance — doctrine / drift catalogs
  'drift-autofix': 'governance',
  'drift-brief': 'governance',

  // Mission Control — cross-domain ops loop / defects meta
  'daily-ops-checklist-run': 'mission-control',
  'nightly-health': 'mission-control',
  'defect-pattern-remediate': 'mission-control',
  'post-fix-verification': 'mission-control',
  'hermes-first-task': 'mission-control',
}

/** AgentTaskDomain (task subject) → System Domain fallback — not a substitute for SCOPE map. */
export const AGENT_TASK_DOMAIN_TO_SYSTEM: Record<string, SystemDomainId> = {
  Operator: 'engineer',
  Platform: 'rocket',
  Trade: 'satellite',
  Cluster: 'rocket',
  Drift: 'governance',
  Health: 'mission-control',
}

function normalizeScopeKey(scope: string): string {
  return scope.trim().toLowerCase().replace(/\s+/g, '-')
}

/**
 * Map a remediation scope (or pattern label fragment) to a System Domain.
 * Order: exact SCOPE map → substring heuristics → Mission Control default.
 */
export function scopeToDomain(scope: string): SystemDomainId {
  if (scope == null || scope.trim() === '') return 'mission-control'
  const raw = scope.trim()
  const key = normalizeScopeKey(raw)

  const exact = SCOPE_TO_SYSTEM_DOMAIN[key] ?? SCOPE_TO_SYSTEM_DOMAIN[raw]
  if (exact != null) return exact

  // Pattern labels: "scope → namespace"
  const beforeArrow = key.split('→')[0]?.trim() ?? key
  if (beforeArrow !== key) {
    const fromArrow = SCOPE_TO_SYSTEM_DOMAIN[beforeArrow]
    if (fromArrow != null) return fromArrow
  }

  if (
    key.includes('cluster') ||
    key.includes('tekton') ||
    key.includes('pipeline') ||
    key.includes('gitops') ||
    key.includes('argocd') ||
    key.includes('release') ||
    key.includes('cicd') ||
    key.includes('platform-self') ||
    key.includes('registry')
  ) {
    return 'rocket'
  }
  if (
    key.includes('trade') ||
    key.includes('satellite') ||
    key.includes('deliver-stg') ||
    key.includes('deliver-prod')
  ) {
    return 'satellite'
  }
  if (key.includes('ib') || key.includes('vendor') || key.includes('plugin')) {
    return 'subcontractors'
  }
  if (key.includes('network') || key.includes('unifi') || key.includes('vlan')) {
    return 'ground-systems'
  }
  if (key.includes('drift') || key.includes('briefing-reconcil')) {
    return 'governance'
  }
  if (
    key.includes('agent-desk') ||
    key.includes('operator-plane') ||
    key.includes('git-dirty') ||
    key === 'ops'
  ) {
    return 'engineer'
  }

  return 'mission-control'
}

/** Dominant scope from a retrospective pattern (jobs[].scope, else label). */
export function patternDominantScope(pattern: {
  label: string
  jobs?: { scope?: string }[]
}): string {
  const counts = new Map<string, number>()
  for (const j of pattern.jobs ?? []) {
    const s = (j.scope ?? '').trim()
    if (s === '') continue
    counts.set(s, (counts.get(s) ?? 0) + 1)
  }
  if (counts.size > 0) {
    return [...counts.entries()].sort((a, b) => b[1] - a[1])[0]![0]
  }
  const label = pattern.label ?? ''
  if (label.includes('→')) return label.split('→')[0]!.trim()
  return label.trim()
}

export function patternToDomain(pattern: {
  label: string
  jobs?: { scope?: string }[]
}): SystemDomainId {
  return scopeToDomain(patternDominantScope(pattern))
}

export function buildSystemDomainLlmPack(): string {
  const lines: string[] = [
    '## System Domains (Apollo — Console sidebar)',
    `# Source: ${SYSTEM_DOMAIN_SOURCE} v${SYSTEM_DOMAIN_VERSION}`,
    '',
    'Use these domains for Defects / Audit / Agent scope projection. Do not confuse with AgentTaskDomain (Operator|Platform|Trade|…).',
    '',
    ...SYSTEM_DOMAINS.map(d => `- **${d.label}** (\`${d.id}\`): ${d.purpose}`),
    '',
    '### Scope → Domain (selected)',
    ...Object.entries(SCOPE_TO_SYSTEM_DOMAIN)
      .sort(([a], [b]) => a.localeCompare(b))
      .map(([scope, id]) => `- \`${scope}\` → ${systemDomainLabel(id)}`),
  ]
  return lines.join('\n')
}
