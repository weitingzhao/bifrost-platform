/**
 * Standards catalog — Trade Contract + Cluster Actuation.
 *
 * Authoritative source for Ops Console → Architecture → Standards.
 * Single source of truth — do not duplicate elsewhere.
 */

export const STANDARDS_VERSION = '2026-06-15'
export const STANDARDS_SOURCE = 'console/src/lib/architecture/standardsCatalog.ts'

/* ── Trade stack read-only contract ── */

export type ProbeRow = {
  targetId: string
  path: string
  okCodes: string
}

export const HTTP_PROBES: ProbeRow[] = [
  { targetId: 'nginx-spa', path: '/', okCodes: '200' },
  { targetId: 'api-monitor', path: '/api/monitor/status', okCodes: '200, 503' },
  { targetId: 'api-massive', path: '/api/massive/research/massive/health', okCodes: '200, 503' },
  { targetId: 'api-docs', path: '/api/docs/research/docs/health', okCodes: '200, 503' },
  { targetId: 'api-ops', path: '/api/ops/health', okCodes: '200, 503' },
  { targetId: 'api-trading', path: '/api/trading/health', okCodes: '200, 503' },
  { targetId: 'api-strategy', path: '/api/strategy/health', okCodes: '200, 503' },
  { targetId: 'api-portfolio', path: '/api/portfolio/health', okCodes: '200, 503' },
  { targetId: 'api-market', path: '/api/market/health', okCodes: '200, 503' },
  { targetId: 'api-research', path: '/api/research/health', okCodes: '200, 503' },
]

export type AuthProbeRow = {
  targetId: string
  path: string
  token: string
}

export const AUTH_PROBES: AuthProbeRow[] = [
  {
    targetId: 'ops-capabilities',
    path: '/api/ops/ops/auth/capabilities',
    token: 'BIFROST_{DEV,PROD}_OPS_TOKEN optional',
  },
]

export type TcpProbeRow = {
  targetId: string
  addressSource: string
}

export const TCP_PROBES: TcpProbeRow[] = [
  { targetId: 'postgres', addressSource: 'environments.yaml postgres host:port' },
  { targetId: 'redis', addressSource: 'environments.yaml redis host:port' },
]

export type PolicyBlockedRow = {
  targetId: string
  reason: string
}

export const POLICY_BLOCKED: PolicyBlockedRow[] = [
  { targetId: 'ib-operator-rpc', reason: 'Trade write path — R-DV3' },
  { targetId: 'daemon-control-write', reason: 'Platform L0 does not invoke control writes' },
]

/* ── Cluster Actuation phases ── */

export type ActuationPhaseDetail = {
  phase: string
  nodes: string
  workloads: string
  gitops: string
  stack: string
  audit: string
}

export const ACTUATION_PHASE_MATRIX: ActuationPhaseDetail[] = [
  {
    phase: 'P1',
    nodes: 'Read-only health + Layer A KPIs',
    workloads: 'Ensure NS, restart/scale Deployment, delete Pod, tail logs',
    gitops: 'Documented only',
    stack: 'Documented only',
    audit: 'Bearer auth, in-memory/JSON audit, job stub',
  },
  {
    phase: 'P2',
    nodes: 'Join, drain/uncordon/cordon with confirmation',
    workloads: 'Safer presets, NS guardrails',
    gitops: 'Delivery deep links',
    stack: 'Preflight checks',
    audit: 'Durable jobs for long-running node ops',
  },
  {
    phase: 'P3',
    nodes: 'Readiness gates for promotion',
    workloads: 'Rollout status gates',
    gitops: 'Argo sync/rollback, Tekton run/logs',
    stack: 'GitOps app status',
    audit: 'Shared audit contract (UI, API, MCP)',
  },
  {
    phase: 'P4',
    nodes: 'Maintenance playbooks',
    workloads: 'Stack-wide restart presets',
    gitops: 'Release gate integration',
    stack: 'Install/upgrade add-ons via curated Helm/API',
    audit: 'Persistent store, retention, export, replay',
  },
]

export type ActuationApiRoute = {
  phase: string
  method: string
  route: string
  role: string
  purpose: string
}

export const ACTUATION_API_ROUTES: ActuationApiRoute[] = [
  { phase: 'P1', method: 'GET', route: '/api/v1/auth/capabilities', role: 'viewer', purpose: 'Report Bearer token role and capability' },
  { phase: 'P1', method: 'GET', route: '/api/v1/audit', role: 'viewer', purpose: 'Recent actuation records' },
  { phase: 'P1', method: 'GET', route: '/api/v1/jobs', role: 'viewer', purpose: 'Current simple job list' },
  { phase: 'P1', method: 'POST', route: '/api/v1/cluster/namespaces/ensure-bifrost', role: 'operator', purpose: 'Idempotently create Bifrost namespaces' },
  { phase: 'P1', method: 'POST', route: '/api/v1/cluster/workloads/rollout-restart', role: 'operator', purpose: 'Rollout restart Deployment' },
  { phase: 'P1', method: 'POST', route: '/api/v1/cluster/workloads/scale', role: 'operator', purpose: 'Scale Deployment' },
  { phase: 'P1', method: 'DELETE', route: '/api/v1/cluster/workloads/pods/{ns}/{name}', role: 'operator', purpose: 'Delete Pod' },
  { phase: 'P1', method: 'GET', route: '/api/v1/cluster/workloads/pods/{ns}/{name}/logs', role: 'viewer', purpose: 'Tail Pod logs' },
  { phase: 'P1', method: 'GET', route: '/api/v1/cluster/metrics', role: 'viewer', purpose: 'Cluster CPU/Mem + top pods' },
  { phase: 'P1', method: 'GET', route: '/api/v1/cluster/observability', role: 'viewer', purpose: 'Layer B probe' },
  { phase: 'P1', method: 'POST', route: '/api/v1/cluster/addons/metrics-server/ensure', role: 'admin', purpose: 'Install metrics-server (Layer A)' },
  { phase: 'P2', method: 'POST', route: '/api/v1/cluster/nodes/join', role: 'admin', purpose: 'K3s join job' },
  { phase: 'P2', method: 'POST', route: '/api/v1/cluster/nodes/{name}/cordon', role: 'operator', purpose: 'Prevent scheduling' },
  { phase: 'P2', method: 'POST', route: '/api/v1/cluster/nodes/{name}/drain', role: 'admin', purpose: 'Drain node (with confirmation)' },
  { phase: 'P2', method: 'POST', route: '/api/v1/cluster/nodes/{name}/uncordon', role: 'operator', purpose: 'Re-enable scheduling' },
  { phase: 'P3', method: 'GET', route: '/api/v1/gitops/apps', role: 'viewer', purpose: 'Argo CD health/sync' },
  { phase: 'P3', method: 'POST', route: '/api/v1/gitops/apps/{name}/sync', role: 'operator', purpose: 'Argo sync' },
  { phase: 'P3', method: 'POST', route: '/api/v1/gitops/apps/{name}/rollback', role: 'admin', purpose: 'Argo rollback' },
  { phase: 'P3', method: 'POST', route: '/api/v1/delivery/pipelines/{name}/runs', role: 'operator', purpose: 'Start Tekton pipeline' },
  { phase: 'P3', method: 'GET', route: '/api/v1/delivery/runs/{id}/logs', role: 'viewer', purpose: 'Pipeline logs' },
  { phase: 'P4', method: 'GET', route: '/api/v1/stack/addons', role: 'viewer', purpose: 'Add-on status' },
  { phase: 'P4', method: 'POST', route: '/api/v1/stack/addons/{name}/install', role: 'admin', purpose: 'Install add-on' },
  { phase: 'P4', method: 'POST', route: '/api/v1/stack/addons/{name}/upgrade', role: 'admin', purpose: 'Upgrade add-on' },
  { phase: 'P4', method: 'GET', route: '/api/v1/promote/release-gate?tier=stg|prod', role: 'viewer', purpose: 'STG or Prod release gate state' },
  { phase: 'P4', method: 'POST', route: '/api/v1/promote/release-gate?tier=stg|prod', role: 'admin', purpose: 'Run STG or Prod release gate' },
  { phase: 'P4', method: 'GET', route: '/api/v1/promote/tier-b', role: 'viewer', purpose: 'Tier B extended STG acceptance probes + sign-off state' },
  { phase: 'P4', method: 'POST', route: '/api/v1/promote/tier-b/signoff', role: 'admin', purpose: 'Record Tier B Owner sign-off' },
]

export type LayerDescription = {
  layer: string
  scope: string
  dataSource: string
  notes: string
}

export const OBSERVABILITY_LAYERS: LayerDescription[] = [
  {
    layer: 'A — KPIs',
    scope: 'Node capacity, CPU/Mem %, top pods',
    dataSource: 'Kubernetes API + metrics-server',
    notes: 'Available from P1; requires metrics-server install',
  },
  {
    layer: 'B — Probe',
    scope: 'Prometheus, Grafana, Loki, Alertmanager presence',
    dataSource: 'monitoring namespace workloads (substring match)',
    notes: 'Probe only — full stack install is P4',
  },
]

/** Build LLM-optimized text for the Standards page. */
export function buildStandardsLlmPack(): string {
  const lines: string[] = [
    '# Bifrost Ops — Standards (Trade Contract + Cluster Actuation)',
    `# Source: ${STANDARDS_SOURCE} v${STANDARDS_VERSION}`,
    '',
    '## Trade stack read-only contract',
    '',
    '### HTTP probes',
    ...HTTP_PROBES.map(p => `- ${p.targetId}: ${p.path} → ${p.okCodes}`),
    '',
    '### Auth probe',
    ...AUTH_PROBES.map(p => `- ${p.targetId}: ${p.path} (token: ${p.token})`),
    '',
    '### TCP probes',
    ...TCP_PROBES.map(p => `- ${p.targetId}: ${p.addressSource}`),
    '',
    '### Policy-blocked',
    ...POLICY_BLOCKED.map(p => `- ${p.targetId}: ${p.reason}`),
    '',
    '## Cluster actuation phases',
    ...ACTUATION_PHASE_MATRIX.map(p =>
      `- **${p.phase}**: Nodes: ${p.nodes} | Workloads: ${p.workloads} | GitOps: ${p.gitops} | Stack: ${p.stack} | Audit: ${p.audit}`),
    '',
    '## Actuation API routes',
    ...ACTUATION_API_ROUTES.map(r => `- [${r.phase}] ${r.method} ${r.route} (${r.role}) — ${r.purpose}`),
    '',
    '## Observability layers',
    ...OBSERVABILITY_LAYERS.map(l => `- **${l.layer}**: ${l.scope} — ${l.notes}`),
  ]
  return lines.join('\n')
}
