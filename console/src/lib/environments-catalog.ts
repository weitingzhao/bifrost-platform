/**
 * Bifrost Ops — environments & architecture catalog.
 *
 * Static catalog for Environments UI and "Copy for LLM" context.
 * Milestones and decisions: authoritative via config/ops-context.yaml + GET /api/v1/context.
 *
 * Authoritative architecture UI: Ops Console → Architecture (Blueprint, Standards, Agent Protocol, Environments).
 */

import type { DenseTagVariant } from '@bifrost/ui'
import type { OpsContextResponse } from '@/api/types'

export const CATALOG_VERSION = '2026-06-27-agent-operator-plane'
export const CATALOG_SOURCE = 'console/src/lib/environments-catalog.ts'

/** Scope row — one logical component in the Bifrost stack. */
export type ScopeRow = {
  tag: string
  component: string
  technology: string
  notes: string
}

/** End-to-end flow row — how a path moves through environments. */
export type FlowRowStatus = 'live' | 'planned' | 'blocked' | 'tbd'

export type FlowRow = {
  path: string
  stage: 'Development' | 'Staging' | 'Production'
  trigger: string
  runtime: string
  dataStore: string
  status: FlowRowStatus
}

export function flowStatusVariant(status: FlowRowStatus): DenseTagVariant {
  if (status === 'live') return 'success'
  if (status === 'blocked') return 'danger'
  if (status === 'tbd') return 'neutral'
  return 'neutral'
}

export type HardwareRow = {
  id: string
  host: string
  roleCompose: string
  roleK3s: string
}

export type PlatformPhase = {
  id: string
  label: string
  timeframe: string
  deliverables: string
}

export const PLATFORM_PORTS = {
  platformApi: 8780,
  platformConsole: 5180,
  tradeFrontend: 5173,
  infraDocs: 8050,
  platformDocs: 8060,
} as const

/** Registered trade environments (mirrors config/environments.yaml + K3s data layer). */
export const TRADE_ENVIRONMENTS = [
  {
    id: 'dev',
    label: 'Development',
    nginx: 'http://192.168.10.73:30882',
    postgres: 'bifrost-postgres-rw.data.svc:5432 (bifrost_dev @ CNPG)',
    redis: 'redis-dev.data.svc:6379',
    host: 'K3s bifrost-dev ns (Vision V1)',
  },
  {
    id: 'stg',
    label: 'Staging',
    nginx: 'http://192.168.10.73:30880',
    postgres: 'bifrost-postgres-rw.data.svc:5432 (bifrost_stg @ CNPG)',
    redis: 'redis-live-stg.data.svc / redis-queue-stg.data.svc:6379',
    host: 'K3s bifrost-stg ns',
  },
  {
    id: 'prod',
    label: 'Production',
    nginx: 'http://192.168.10.70:30881',
    postgres: 'bifrost-postgres-rw.data.svc:5432 (bifrost_prod @ CNPG)',
    redis: 'redis-live-prod.data.svc:6379',
    host: 'K3s bifrost-prod ns',
  },
] as const

export const SCOPE_ROWS: ScopeRow[] = [
  {
    tag: 'PLATFORM',
    component: 'Bifrost Ops Platform (control plane)',
    technology: 'Go API :8780 · React Ops Console :5180 · Dense UI tokens',
    notes:
      'Phase 0 L0 read-only: Control Room live strip, connectivity matrix, topology, Program spine, Ops auth probe. No daemon_control or ib:operator:cmd. Future: agent/, mcp/.',
  },
  {
    tag: 'TRADE-FE',
    component: 'bifrost-trade-frontend',
    technology: 'React 18 · Vite · TanStack Query · shadcn/ui',
    notes: 'Trade monitoring SPA :5173. Separate from Platform Console. Phase 1: New FE + Legacy API equivalence.',
  },
  {
    tag: 'TRADE-API',
    component: 'bifrost-trade-api (9 domains)',
    technology: 'FastAPI · ports 8765–8773 via nginx /api/{domain}/',
    notes:
      'monitor · massive · docs · ops · trading · strategy · portfolio · market · research. Platform probes health endpoints only.',
  },
  {
    tag: 'WORKER',
    component: 'bifrost-trade-worker',
    technology: 'Python · daemon (GsTrading FSM) · Celery · Flower :5555',
    notes: 'Daemon reads Redis quotes/account; orders via ib_operator RPC. Does not connect to TWS directly.',
  },
  {
    tag: 'SOCKET',
    component: 'bifrost-trade-socket',
    technology: 'Python · IB ingestor / account agent / operator · Massive WS',
    notes: 'Only layer that talks to TWS (IB_HOST → Win11 LAN). Writes Redis streams/hashes; operator listens ib:operator:cmd.',
  },
  {
    tag: 'CORE',
    component: 'bifrost-trade-core',
    technology: 'Python package bifrost_core — config, persistence, portfolio, monitor',
    notes: 'Shared library pip-installed by api/worker/socket. Version via BIFROST_CORE_REF tag in Docker builds.',
  },
  {
    tag: 'INFRA',
    component: 'bifrost-trade-infra',
    technology: 'Docker Compose · nginx · Makefile · config/*.yaml',
    notes:
      'Prod: docker-compose.yml on mini-pc-a. Dev: docker-compose.dev.yml. make prod-health · prod-preflight · release_gate (planned).',
  },
  {
    tag: 'PG',
    component: 'PostgreSQL',
    technology: 'Bare metal mini-pc-b :5432 · bifrost_dev / bifrost_prod',
    notes: 'Owner D2: keep .80 bare PG until CloudNativePG on K3s (mini-pc-b Primary + standby on mini-pc-a).',
  },
  {
    tag: 'REDIS',
    component: 'Redis',
    technology: 'Prod on mini-pc-a :6379 · Dev local or co-located',
    notes: 'Streams: ib:ingester:tick:* · RPC: ib:operator:cmd · Health keys bifrost:health:ws_*',
  },
  {
    tag: 'TWS',
    component: 'Interactive Brokers TWS / Gateway',
    technology: 'Win11 ×2 (Host + Secondary) — cluster-external forever',
    notes: 'R-DV3: Dev/Prod distinct client_id; one auto-trading Engine per IB account. D4: auto-order switch deferred.',
  },
  {
    tag: 'GITHUB',
    component: 'Source control & CI (target)',
    technology: 'Git · Gitea on K3s (internal) · Mac Mini #2 runner (near-term)',
    notes: 'Near-term: pytest · npm run build · check:legacy-css on PR. Target: Tekton Pipeline in K3s cicd namespace.',
  },
  {
    tag: 'K3S',
    component: 'K3s cluster (target)',
    technology: 'K3s HA · Traefik Ingress · CloudNativePG · local-path PVC',
    notes:
      'Phase B GitOps: Gitea · ArgoCD · Tekton. Namespaces: data · cicd · monitoring · ai · bifrost. deployment_phase in topology.yaml.',
  },
  {
    tag: 'OBSERVE',
    component: 'Observability (target)',
    technology: 'Prometheus · Loki · Grafana · AlertManager · AIOps webhook',
    notes: 'Phase C on mini-pc-c. External watchdog cron on Mac Mini #2 (monitor the monitors).',
  },
  {
    tag: 'AI',
    component: 'Inference & MCP (target)',
    technology: 'Ollama on gpu-server (4090) · mcp-server-kubernetes · bifrost-ops-mcp',
    notes: 'L0/L1 ops only; research RAG CronJob read-only. Forbidden: trade write paths for platform AI.',
  },
  {
    tag: 'AGENT',
    component: 'Out-of-Band Operator Plane (L-1)',
    technology: 'Dual Mac Mini Remediation Runners (primary .50 + standby .52) · launchd peer watchdog · Git Bridge (Mac Pro, Dev-only)',
    notes:
      'The engineer on the ground — recovers Ops Platform (rocket) + Trade (payload) from OUTSIDE K8s; never an in-cluster Pod. Mutual watchdog + platform-api failover. See K3s Bootstrap L-1 + Flywheel Vision.',
  },
]

export const FLOW_ROWS: FlowRow[] = [
  {
    path: 'Application (trade stack)',
    stage: 'Development',
    trigger: 'Local / Mac Mini #1: make dev · feature branches',
    runtime: 'docker-compose.dev.yml · nginx localhost · hot reload mounts',
    dataStore: 'bifrost_dev @ PG .80 · local Redis',
    status: 'live',
  },
  {
    path: 'Application (trade stack)',
    stage: 'Staging',
    trigger: 'Mac Mini #2 CI or K3s bifrost-stg (planned) · merge to main',
    runtime: 'Same images as prod · optional Mac Mini #2 or mini-pc-c',
    dataStore: 'bifrost_dev or isolated stg DB (TBD)',
    status: 'tbd',
  },
  {
    path: 'Application (trade stack)',
    stage: 'Production',
    trigger: '2C-B compose on .70 · future: git tag → Tekton → ArgoCD sync',
    runtime: 'docker-compose.yml (now) → K3s bifrost namespace (target)',
    dataStore: 'bifrost_prod @ PG .80 · Redis .70',
    status: 'blocked',
  },
  {
    path: 'Platform (control plane)',
    stage: 'Development',
    trigger: 'make start / ./scripts/run_platform.py on MacBook',
    runtime: 'Go platform-api :8780 · Vite console :5180',
    dataStore: 'config/environments.yaml · config/topology.yaml (no DB)',
    status: 'live',
  },
  {
    path: 'Platform (control plane)',
    stage: 'Production',
    trigger: 'Same binary; probes prod nginx/PG/Redis via LAN',
    runtime: 'MacBook or Mac Mini #2 watchdog · L0 matrix refresh 30s',
    dataStore: 'Optional BIFROST_PROD_OPS_TOKEN for capabilities probe',
    status: 'live',
  },
  {
    path: 'Release gate',
    stage: 'Production',
    trigger: 'Tag / maintenance window · scripts/release_gate.sh (planned)',
    runtime: 'make prod-health 12/12 · Platform GET /api/v1/matrix?env=prod',
    dataStore: 'Sign-off: PHASE2C_SIGNOFF_MASTER.md · Deploy Mainline (deployMainlineCatalog.ts)',
    status: 'planned',
  },
  {
    path: 'IB edge (socket)',
    stage: 'Production',
    trigger: 'Socket containers on prod host · IB_HOST env',
    runtime: 'bifrost-trade-socket → Win11 TWS LAN',
    dataStore: 'Redis on .70 · no direct daemon→TWS',
    status: 'live',
  },
  {
    path: 'Frontend migration',
    stage: 'Development',
    trigger: 'Page-by-page migrate · npm run lint && build && check:legacy-css',
    runtime: 'New frontend :5173 vs Legacy UI on same Legacy API (Phase 1 rule)',
    dataStore: 'MIGRATION_TRACKING.md progress',
    status: 'live',
  },
]

export const HARDWARE_ROWS: HardwareRow[] = [
  {
    id: 'mini-pc-a',
    host: '192.168.10.70',
    roleCompose: 'Prod compose · Redis · nginx',
    roleK3s: 'Server ① · API · Redis · Ingress · Gitea · ArgoCD',
  },
  {
    id: 'mini-pc-b',
    host: '192.168.10.80',
    roleCompose: 'PostgreSQL dedicated',
    roleK3s: 'Server ② · CNPG Primary · pgvector',
  },
  {
    id: 'mini-pc-c',
    host: '192.168.10.73 (ubt-k3s-01)',
    roleCompose: 'K3s bootstrap server (first node)',
    roleK3s: 'Live K3s bootstrap @ .73 · monitoring · Tekton (planned)',
  },
  {
    id: 'mac-mini-1',
    host: '192.168.10.50 (macOS · Agent)',
    roleCompose: '24/7 Dev stack · Remediation Runner PRIMARY (L-1) · peer watchdog → .52',
    roleK3s: 'UTM Agent ops-vm-ubt-01 · frontend dev (Ready) · runner stays OUTSIDE cluster',
  },
  {
    id: 'mac-mini-2',
    host: '192.168.10.52 (macOS · Agent)',
    roleCompose: 'Git runner · prod-health gate · Remediation Runner STANDBY (L-1) · peer watchdog → .50',
    roleK3s: 'UTM Agent ops-vm-ubt-02 · CI · external watchdog (Ready) · runner stays OUTSIDE cluster',
  },
  {
    id: 'gpu-server',
    host: '192.168.10.60',
    roleCompose: 'Data warehouse · Ollama dev trial — no Prod Redis',
    roleK3s: 'Agent compute · warehouse · GPU/AI · Tekton heavy (WOL eno1)',
  },
  {
    id: 'ubt-k3s-04',
    host: '192.168.10.75',
    roleCompose: '—',
    roleK3s: 'Agent · CNPG primary · data-primary pool · NFS client',
  },
  {
    id: 'ubt-k3s-05',
    host: '192.168.10.77',
    roleCompose: '—',
    roleK3s: 'Agent · general runtime pool · stg/dev/CI offload · NFS client',
  },
  {
    id: 'win11-host',
    host: '(LAN TBD)',
    roleCompose: 'TWS Host account',
    roleK3s: 'Never in cluster',
  },
  {
    id: 'win11-secondary',
    host: '(LAN TBD)',
    roleCompose: 'TWS Secondary account',
    roleK3s: 'Never in cluster',
  },
  {
    id: 'macbook',
    host: '127.0.0.1',
    roleCompose: 'Cursor dev · platform console · local prod smoke',
    roleK3s: 'kubectl · MCP client',
  },
]

export const PLATFORM_PHASES: PlatformPhase[] = [
  {
    id: 'A',
    label: 'Gate (now ~3mo)',
    timeframe: 'Compose prod + Mac CI',
    deliverables: '2C-B Docker Prod · release_gate.sh · Mac Mini #2 CI · MkDocs Goal',
  },
  {
    id: 'B',
    label: 'GitOps (3–9mo)',
    timeframe: 'K3s + CNPG',
    deliverables: 'Gitea · Tekton · ArgoCD · k8s/base/ · FE staging on K3s',
  },
  {
    id: 'C',
    label: 'Closed loop (9–18mo)',
    timeframe: 'Observe + AI ops',
    deliverables: 'Prometheus/Loki/Grafana · bifrost-ops-mcp · AlertManager · research RAG',
  },
]

export const AUTHORIZATION_LEVELS = [
  { level: 'L0', behavior: 'Read-only probes (matrix, topology, cluster, logs)' },
  {
    level: 'L1',
    behavior: 'Safe actuation via platform-api (rollout restart, scale, sync — north star P1)',
  },
  {
    level: 'L2',
    behavior: 'Owner-confirmed changes (node join, stack install, Argo rollback — north star P2+)',
  },
  { level: 'forbidden', behavior: 'daemon_control write · ib:operator:cmd · R-DV3 auto-trade bypass' },
]

/** Spine section from GET /api/v1/context (authoritative milestones). */
export function formatSpineContextSection(ctx: OpsContextResponse): string {
  const lines: string[] = [
    '## Ops context spine (authoritative)',
    `Spine version: ${ctx.meta.version} · catalog_version: ${ctx.meta.catalog_version}`,
    '',
  ]
  if (ctx.north_star != null) {
    lines.push(
      '### North star (ultimate goal)',
      `- id: ${ctx.north_star.id}`,
      `- strategy: ${ctx.north_star.strategy}`,
      `- statement: ${ctx.north_star.statement.trim()}`,
      `- owner_exception: ${ctx.north_star.owner_exception}`,
      `- authority: ${ctx.north_star.authority}`,
      ...ctx.north_star.principles.map(p => `- principle: ${p}`),
      '',
    )
  }
  lines.push(
    '### Focus',
    `- headline: ${ctx.focus.headline}`,
    `- flywheel_primary: ${ctx.focus.flywheel_primary}`,
    ctx.focus.blocker != null && ctx.focus.blocker !== ''
      ? `- blocker: ${ctx.focus.blocker}`
      : '- blocker: (none)',
    '',
    '### Deployment',
    `- phase: ${ctx.deployment.phase}`,
    `- active_track: ${ctx.deployment.active_track}`,
    '',
    '### Milestones',
    ...ctx.milestones.map(
      m =>
        `- **${m.id}** ${m.label ?? ''}: ${m.status}${m.blocker != null ? ` (blocker: ${m.blocker})` : ''}${m.signed_at != null ? ` signed ${m.signed_at}` : ''}`,
    ),
    '',
    '### Owner decisions',
    ...ctx.decisions.map(
      d => `- **${d.id}** (${d.status}${d.signed_at != null ? ` ${d.signed_at}` : ''}): ${d.conclusion}`,
    ),
    '',
    '### Promotion / staging',
    `- last_gate: ${ctx.promotion.last_gate.result ?? 'not recorded'}`,
    `- staging: ${ctx.environments_extended.staging?.status ?? 'unknown'}`,
    '',
  )
  return lines.join('\n')
}

/** Static catalog without milestone narrative (spine is authoritative). */
export function buildStaticCatalogContext(): string {
  const lines: string[] = [
    '# Bifrost Ops — Environments & Architecture Context',
    `# Catalog version: ${CATALOG_VERSION} · source: ${CATALOG_SOURCE}`,
    '',
    '## One-line goal',
    'AI-native environment governance control plane (Bifrost Ops) over Bifrost Trade data plane.',
    'North star: all routine ops via Ops Console/API only (Strategy C hybrid); Owner exception = restart Ops Platform.',
    'Authority: console/src/lib/architecture/blueprintCatalog.ts · spine north_star + decision D6.',
    '',
    '## Ports',
    ...Object.entries(PLATFORM_PORTS).map(([k, v]) => `- ${k}: ${v}`),
    '',
    '## Trade environments',
    ...TRADE_ENVIRONMENTS.flatMap(e => [
      `### ${e.label} (${e.id})`,
      `- nginx: ${e.nginx}`,
      `- postgres: ${e.postgres}`,
      `- redis: ${e.redis}`,
      `- host: ${e.host}`,
      '',
    ]),
    '## SCOPE (components)',
    ...SCOPE_ROWS.map(r => `- **${r.tag}** ${r.component}: ${r.technology}. ${r.notes}`),
    '',
    '## End-to-end flows',
    ...FLOW_ROWS.map(
      r =>
        `- **${r.path}** · ${r.stage} [${r.status}]: trigger=${r.trigger}; runtime=${r.runtime}; data=${r.dataStore}`,
    ),
    '',
    '## Hardware (planned / partial)',
    ...HARDWARE_ROWS.map(h => `- **${h.id}** ${h.host}: compose=[${h.roleCompose}] k3s=[${h.roleK3s}]`),
    '',
    '## Platform roadmap phases',
    ...PLATFORM_PHASES.map(p => `- **Phase ${p.id}** ${p.label} (${p.timeframe}): ${p.deliverables}`),
    '',
    '## Authorization (platform)',
    ...AUTHORIZATION_LEVELS.map(a => `- **${a.level}**: ${a.behavior}`),
    '',
    '## Key repos',
    '- bifrost-platform — Bifrost Ops control plane (this console)',
    '- bifrost-trade-infra — compose, nginx, Goal, migration sign-off docs',
    '- Architecture → Platform Roadmap · K3s Architecture (roadmapCatalog.ts · k3sArchitectureCatalog.ts)',
    '- bifrost-trade-{api,worker,socket,frontend,core} — data plane',
    '- bifrost-trader-engine — READ-ONLY reference (do not edit)',
    '',
    '## Architecture (Ops Console)',
    '- Blueprint — North Star, system architecture, design principles',
    '- Standards — Trade probe contract, cluster actuation phases',
    '- Agent Protocol — Product / Ops / Promote modes',
    '- Environments — hardware, flows, platform phases (this catalog)',
    '',
    '## Agent discipline',
    '- Probe, do not duplicate trade health endpoints',
    '- Never expose forbidden write paths to platform MCP/AI',
    '- Frontend Phase 1: do not migrate API until FE business-equivalent to Legacy',
    '- Agent modes: Ops Console → Architecture → Agent Protocol',
  ]
  return lines.join('\n')
}

/** Plain-text block optimized for LLM / Agent context paste. */
export function buildEnvironmentsLlmContext(spine?: OpsContextResponse): string {
  const staticPart = buildStaticCatalogContext()
  if (spine == null) {
    return `${staticPart}\n\n## Ops context spine\n(spine not loaded — open Ops Console or call GET /api/v1/context)\n`
  }
  return `${staticPart}\n\n${formatSpineContextSection(spine)}`
}
