/**
 * Blueprint catalog — North Star, system architecture, design principles.
 *
 * Authoritative source for Ops Console → Architecture → Blueprint.
 * Do not duplicate in docs/ — see docs/STAGING.md.
 */

import type { OpsContextResponse } from '@/api/types'

export const BLUEPRINT_VERSION = '2026-06-15'
export const BLUEPRINT_SOURCE = 'console/src/lib/architecture/blueprintCatalog.ts'

export const NORTH_STAR_STATEMENT =
  'All routine environment, cluster, release, and ops actions go through Bifrost Ops Console and platform-api; infra scripts run only as API executors. The Owner\'s only out-of-band action is restarting the Ops Platform itself.'

export const NORTH_STAR_STRATEGY = 'C — Hybrid control plane'
export const NORTH_STAR_DECISION = 'D6'

export type OwnerException = {
  allowed: string
  forbidden: string
}

export const OWNER_EXCEPTIONS: OwnerException[] = [
  {
    allowed: 'Start / restart bifrost-platform (make start, upgrade control plane)',
    forbidden: 'kubectl, ssh, make k3s-*, manual cluster changes, manual release_gate.sh',
  },
  {
    allowed: 'First-time install Go/Node, clone repo (cold start)',
    forbidden: 'Daily probes, releases, node join, Pod restart, Argo sync',
  },
  {
    allowed: 'Edit ops-context.yaml / Goal (Owner strategic changes)',
    forbidden: 'Bypass platform-api to invoke Trade write paths',
  },
]

export type StrategyCLayer = {
  layer: string
  responsibility: string
}

export const STRATEGY_C_LAYERS: StrategyCLayer[] = [
  {
    layer: 'Ops Console',
    responsibility: 'Unified navigation, confirmation flows, audit display, Copy-for-LLM / Agent packs',
  },
  {
    layer: 'platform-api',
    responsibility: 'Auth L0/L1/L2, job queue, audit log, no arbitrary shell',
  },
  {
    layer: 'Mature components',
    responsibility: 'Argo CD, Tekton, Headlamp/Rancher — wrapped via API or deep-linked, not replacing control plane',
  },
  {
    layer: 'Infra scripts',
    responsibility: 'install-server.sh, fetch-kubeconfig.sh, etc. — executor implementation only, operators do not run manually',
  },
]

export type DesignPrinciple = {
  id: number
  title: string
  description: string
}

export const DESIGN_PRINCIPLES: DesignPrinciple[] = [
  { id: 1, title: 'Single pane', description: 'Interaction entry is only Ops Console + GET/POST /api/v1/* (future MCP same contract).' },
  { id: 2, title: 'Scripts are implementation', description: 'Shell/Makefile in repo are API call implementation details, not operations manual.' },
  { id: 3, title: 'Graduated actuation', description: 'L0 diagnose → L1 safe retry → L2 Owner confirm; all leave audit trail.' },
  { id: 4, title: 'LLM-ready context', description: 'Every operation produces structured action/target/status/detail, feedable into spine and Agent packs.' },
  { id: 5, title: 'Forbidden unchanged', description: 'daemon_control write, ib:operator:cmd, R-DV3 auto-order bypass — never exposed to platform AI.' },
]

export type ConsoleViewRow = {
  view: string
  plane: string
  purpose: string
}

export const CONSOLE_VIEWS: ConsoleViewRow[] = [
  { view: 'Agent Briefing', plane: 'Governance', purpose: 'New-session entry — work-intent picker, UI progress, live snapshot, full LLM briefing pack' },
  { view: 'Control Room', plane: 'Governance', purpose: 'Dual flywheel bays, program milestone spine, Agent focus dock' },
  { view: 'Delivery', plane: 'PLAN + LIVE', purpose: 'CI/CD dual track — near-term Mac runner vs target GitOps; coupling gate summary' },
  { view: 'Runtime Map', plane: 'LIVE + PLAN', purpose: 'Unified runtime — hardware topology + SCOPE stack + matrix probes + gap analysis' },
  { view: 'Cluster', plane: 'LIVE', purpose: 'K3s L0 probe — nodes, namespaces, workloads via platform-api + local kubeconfig' },
  { view: 'Pulse', plane: 'LIVE + focus', purpose: 'Table dashboard — matrix summary + spine headline + cluster KPI' },
  { view: 'Milestones', plane: 'TRACK + PLAN', purpose: 'Milestones, decisions D1–Dn, north star, roadmap (ops-context spine)' },
  { view: 'Promote', plane: 'Coupling', purpose: 'Read-only release readiness (flywheel A + B)' },
  { view: 'Architecture', plane: 'PLAN static', purpose: 'Blueprint, standards, agent protocol, environments — Copy Prompt for LLM' },
  { view: 'Tools', plane: 'B', purpose: 'Server console (SSH/WebSocket)' },
]

export type AuthorizationLevel = {
  level: string
  behavior: string
}

export const BLUEPRINT_AUTHORIZATION_LEVELS: AuthorizationLevel[] = [
  { level: 'L0', behavior: 'Read-only probes (matrix, topology, cluster, logs)' },
  { level: 'L1', behavior: 'Safe actuation via platform-api (rollout restart, scale, sync — north star P1)' },
  { level: 'L2', behavior: 'Owner-confirmed changes (node join, stack install, Argo rollback — north star P2+)' },
  { level: 'forbidden', behavior: 'daemon_control write · ib:operator:cmd · R-DV3 auto-trade bypass' },
]

export type ApiEndpointRow = {
  method: string
  path: string
  description: string
}

export const PLATFORM_API_ENDPOINTS: ApiEndpointRow[] = [
  { method: 'GET', path: '/health', description: 'Platform API health' },
  { method: 'GET', path: '/api/v1/context', description: 'Ops spine (milestones, decisions, focus)' },
  { method: 'GET', path: '/api/v1/matrix', description: 'Connectivity matrix' },
  { method: 'GET', path: '/api/v1/topology', description: 'Network topology + live status' },
  { method: 'GET', path: '/api/v1/cluster', description: 'K3s cluster summary' },
  { method: 'GET', path: '/api/v1/cluster/nodes', description: 'Node list' },
  { method: 'GET', path: '/api/v1/cluster/namespaces', description: 'Namespaces (?watch=bifrost)' },
  { method: 'GET', path: '/api/v1/cluster/workloads?ns=', description: 'Pods in namespace' },
  { method: 'GET', path: '/api/v1/cluster/metrics', description: 'Cluster CPU/Mem, top pods' },
  { method: 'GET', path: '/api/v1/cluster/observability', description: 'Layer B probe status' },
  { method: 'POST', path: '/api/v1/cluster/sync-kubeconfig', description: 'Run fetch-kubeconfig.sh' },
]

export type ConfigFileRow = {
  file: string
  role: string
}

export const CONFIG_FILES: ConfigFileRow[] = [
  { file: 'config/environments.yaml', role: 'Dev/prod probe targets' },
  { file: 'config/ops-context.yaml', role: 'Spine — milestones, decisions, focus' },
  { file: 'config/topology.yaml', role: 'Hardware graph' },
  { file: 'config/clusters.yaml', role: 'K3s cluster registry + Bifrost namespaces' },
]

export type SuccessCriterion = {
  area: string
  criterion: string
}

export const SUCCESS_CRITERIA: SuccessCriterion[] = [
  { area: 'Cluster', criterion: 'Node join/drain, namespace, workload restart/scale/logs — UI/API only' },
  { area: 'Delivery', criterion: 'Tekton run, Argo sync/rollback — UI/API only' },
  { area: 'Promote', criterion: 'release_gate trigger and results — UI/API only' },
  { area: 'Runtime', criterion: 'Runtime Map / Pulse / FocusStrip — clickable to executable actions, not read-only' },
  { area: 'Spine', criterion: 'GET /api/v1/context + Program page always shows north star' },
  { area: 'MCP', criterion: 'MCP Tools and UI — same permissions, same audit (AI Agent self-interaction loop)' },
]

export type ActuationPhaseRow = {
  phase: string
  deliverables: string
  eliminates: string
}

export const ACTUATION_PHASES: ActuationPhaseRow[] = [
  { phase: 'P0 (current)', deliverables: 'Cluster L0 probes, Delivery dual track display', eliminates: 'Observation only' },
  { phase: 'P1', deliverables: 'Auth + audit + workload L1 + logs', eliminates: 'Daily kubectl' },
  { phase: 'P2', deliverables: 'Node lifecycle job + Cluster UI wizard', eliminates: 'install-server.sh, join, drain' },
  { phase: 'P3', deliverables: 'GitOps + CI execution (Argo/Tekton API)', eliminates: 'Argo UI, tkn CLI' },
  { phase: 'P4', deliverables: 'Platform stack install wizard', eliminates: 'Manual Helm install' },
  { phase: 'P5', deliverables: 'MCP actuation Tools', eliminates: 'Agent direct shell' },
]

/** Build LLM-optimized text for the Blueprint page. */
export function buildBlueprintLlmPack(spine?: OpsContextResponse): string {
  const lines: string[] = [
    '# Bifrost Ops — Blueprint (Architecture & North Star)',
    `# Source: ${BLUEPRINT_SOURCE} v${BLUEPRINT_VERSION}`,
    '',
    '## North Star',
    `Strategy: ${NORTH_STAR_STRATEGY} (decision ${NORTH_STAR_DECISION})`,
    NORTH_STAR_STATEMENT,
    '',
    '### Owner exceptions',
    ...OWNER_EXCEPTIONS.map(e => `- Allowed: ${e.allowed}\n  Forbidden: ${e.forbidden}`),
    '',
    '### Strategy C layers',
    ...STRATEGY_C_LAYERS.map(l => `- **${l.layer}**: ${l.responsibility}`),
    '',
    '## Design principles',
    ...DESIGN_PRINCIPLES.map(p => `${p.id}. **${p.title}** — ${p.description}`),
    '',
    '## Console views',
    ...CONSOLE_VIEWS.map(v => `- **${v.view}** [${v.plane}]: ${v.purpose}`),
    '',
    '## Authorization levels',
    ...BLUEPRINT_AUTHORIZATION_LEVELS.map(a => `- **${a.level}**: ${a.behavior}`),
    '',
    '## Platform API endpoints',
    ...PLATFORM_API_ENDPOINTS.map(e => `- ${e.method} ${e.path} — ${e.description}`),
    '',
    '## Configuration files',
    ...CONFIG_FILES.map(c => `- **${c.file}** — ${c.role}`),
    '',
    '## Success criteria (north star completion)',
    ...SUCCESS_CRITERIA.map(s => `- [${s.area}] ${s.criterion}`),
    '',
    '## Actuation phases (P0–P5)',
    ...ACTUATION_PHASES.map(p => `- **${p.phase}**: ${p.deliverables} → eliminates: ${p.eliminates}`),
  ]

  if (spine != null) {
    lines.push(
      '',
      '## Live spine snapshot',
      `- phase: ${spine.deployment.phase}`,
      `- active_track: ${spine.deployment.active_track}`,
      `- focus: ${spine.focus.headline}`,
    )
  }

  return lines.join('\n')
}
