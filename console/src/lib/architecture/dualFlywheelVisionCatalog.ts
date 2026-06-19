/**
 * Dual Flywheel Vision — Ultimate North Star for Bifrost Trade + Ops Platform.
 *
 * Authoritative source for Ops Console → Architecture → Dual Flywheel Vision.
 * This catalog defines the convergence target: Trade (Flywheel A) and Ops Platform
 * (Flywheel B) merge into a unified AI-native development, operations, and business
 * intelligence experience powered by K3s, Cursor SDK, and MCP protocol.
 *
 * Single source of truth — do not duplicate elsewhere.
 */

export const VISION_VERSION = '2026-06-19'
export const VISION_SOURCE = 'console/src/lib/architecture/dualFlywheelVisionCatalog.ts'

// ---------------------------------------------------------------------------
// Core thesis
// ---------------------------------------------------------------------------

export const VISION_STATEMENT =
  'Mac Pro is the eyes and mouth (observe + decide); K3s is the body (run everything); ' +
  'AI Agents are the hands and brain (execute + analyze). The Owner decides — Agents execute — ' +
  'from writing code to releasing to production to intraday monitoring to strategy advice, ' +
  'all through one Cursor window.'

export const FLYWHEEL_CONVERGENCE =
  'Trade (Flywheel A) and Ops Platform (Flywheel B) accelerate each other: every Trade feature shipped ' +
  'improves Ops observability; every Ops capability unlocked accelerates Trade development. ' +
  'The three-layer Agent model is the manifestation of this convergence — ' +
  'one AI surface serves coding, operations, and business intelligence simultaneously.'

// ---------------------------------------------------------------------------
// Decoupling principle — platform is reusable, not married to Trade
// ---------------------------------------------------------------------------

export const DECOUPLING_STATEMENT =
  'The two flywheels accelerate each other but maintain a hard boundary. ' +
  'Bifrost Ops Platform is a general-purpose AI-native development and operations platform — ' +
  'it knows how to build, deploy, observe, and repair complex software. ' +
  'Bifrost Trade is one specific commercial application that runs on this platform. ' +
  'The platform must never contain Trade-specific business logic (strategy, Greeks, IB protocols). ' +
  'Tomorrow the same Ops Platform can be installed fresh and used to develop, deploy, and operate ' +
  'a completely different commercial application — with the same Dev Agent, Ops Agent, and infrastructure.'

export type DecouplingRule = {
  boundary: string
  platform: string
  business: string
}

export const DECOUPLING_RULES: DecouplingRule[] = [
  {
    boundary: 'Domain knowledge',
    platform: 'Generic: K8s, CI/CD, Redis, PG, health probes, release gates',
    business: 'Trade-specific: Greeks, IB protocol, SEPA, FSM, Daemon strategy',
  },
  {
    boundary: 'MCP servers',
    platform: 'mcp-k8s, mcp-redis, mcp-pg, mcp-argocd, mcp-tekton, mcp-prometheus',
    business: 'mcp-trade-api (Business Agent only — pluggable, not baked in)',
  },
  {
    boundary: 'Agent layers',
    platform: 'Dev Agent + Ops Agent (fully generic, any app)',
    business: 'Business Agent (domain-specific; swappable per application)',
  },
  {
    boundary: 'Config & spine',
    platform: 'ops-context.yaml, environments.yaml, clusters.yaml (app-agnostic)',
    business: 'Trade config (config.yaml: ib, strategy, massive — never in platform repo)',
  },
  {
    boundary: 'Repositories',
    platform: 'bifrost-platform (control plane) + bifrost-trade-infra (deployment recipes)',
    business: 'bifrost-trade-{core,socket,worker,api,frontend} (all business logic)',
  },
  {
    boundary: 'Forbidden cross-boundary',
    platform: 'Platform never imports bifrost_core, never reads ib:operator:cmd, never knows about straddles',
    business: 'Trade repos never modify platform-api routes or Ops Console architecture pages',
  },
]

export const REUSABILITY_STATEMENT =
  'Validation test: clone bifrost-platform onto a fresh K3s cluster, point it at a different Git org ' +
  'with a different application (e.g. a SaaS product), and the Dev Agent + Ops Agent should work out of the box — ' +
  'only the Business Agent layer needs to be replaced with the new domain\'s MCP and read APIs.'

// ---------------------------------------------------------------------------
// Three-layer Agent architecture
// ---------------------------------------------------------------------------

export type AgentLayerRow = {
  layer: number
  name: string
  scope: string
  examples: string[]
  cursorRole: string
  k8sRole: string
  forbidden: string
}

export const AGENT_LAYERS: AgentLayerRow[] = [
  {
    layer: 1,
    name: 'Dev Agent',
    scope: 'Code → Test → Build → Deploy → Verify',
    examples: [
      'Auto lint + type-check on save',
      'Push → Tekton CI → STG deliver → HTTP 200 verify',
      '"promote to prod" → deliver-prod → verify → report',
      'PR review + Dense UI governance check',
    ],
    cursorRole: 'IDE Agent (chat) + SDK (CI webhook trigger)',
    k8sRole: 'Tekton Pipeline execution; ArgoCD sync; Registry push',
    forbidden: 'Skip CI gates; deploy without STG green',
  },
  {
    layer: 2,
    name: 'Ops Agent',
    scope: 'Observe → Diagnose → Act → Audit',
    examples: [
      'AlertManager webhook → read logs/metrics → root cause summary',
      'L1: rollout restart, scale, Celery purge (auto + audit)',
      'L2: ArgoCD rollback, resource limit change (Owner confirm)',
      'L3: PVC resize, RBAC change → open PR → Owner approve → ArgoCD apply',
    ],
    cursorRole: 'SDK (webhook-triggered) + MCP K8s/Redis/PG tools',
    k8sRole: 'Prometheus/Loki read; kubectl via MCP; ArgoCD API',
    forbidden: 'daemon_control write; ib:operator:cmd; R-DV3 bypass',
  },
  {
    layer: 3,
    name: 'Business Agent',
    scope: 'Market Intelligence → Strategy Advice → Risk Monitoring',
    examples: [
      'Pre-market: SEPA candidates + Greeks + IV analysis → recommendation',
      'Intraday: Daemon FSM anomaly → diagnosis + "no action needed" or "TWS down"',
      'Post-market: PnL summary + trade review + hedge effectiveness',
      'Gate tuning suggestion: "IV percentile warrants tighter delta band" → L3 PR',
    ],
    cursorRole: 'IDE chat (ad-hoc Q&A) + SDK (scheduled daily brief)',
    k8sRole: 'Read Trade API (9 domains) + Redis live quotes + PG positions',
    forbidden: 'Write ib:operator:cmd; modify daemon_control; auto-place orders',
  },
]

// ---------------------------------------------------------------------------
// Unified experience model
// ---------------------------------------------------------------------------

export type ExperienceRow = {
  userSays: string
  agentDoes: string
  layer: string
  touchesK8s: boolean
}

export const EXPERIENCE_EXAMPLES: ExperienceRow[] = [
  {
    userSays: '"Fix the Greeks table sort on Portfolio page"',
    agentDoes: 'Edit code → lint → push → STG deliver → verify HTTP 200 → report',
    layer: 'Dev',
    touchesK8s: true,
  },
  {
    userSays: '"What happened to prod daemon?"',
    agentDoes: 'Read K8s logs + Redis health hash → diagnose → suggest action',
    layer: 'Ops',
    touchesK8s: true,
  },
  {
    userSays: '"Promote to prod"',
    agentDoes: 'deliver-prod → rollout → verify 9 API + FE → "Prod green"',
    layer: 'Dev',
    touchesK8s: true,
  },
  {
    userSays: '"NVDA current IV vs 30d mean?"',
    agentDoes: 'Read Market API + Research API → compute → present analysis',
    layer: 'Business',
    touchesK8s: false,
  },
  {
    userSays: '"Scale Celery workers to 3"',
    agentDoes: 'kubectl scale → verify → audit log → report',
    layer: 'Ops',
    touchesK8s: true,
  },
  {
    userSays: '"Is today\'s PnL abnormal?"',
    agentDoes: 'Read Trading API → compare historical baseline → explain',
    layer: 'Business',
    touchesK8s: false,
  },
]

// ---------------------------------------------------------------------------
// Dev environment topology (Mac thin + K3s thick)
// ---------------------------------------------------------------------------

export type DevTopoRow = {
  component: string
  location: string
  reason: string
}

export const DEV_TOPOLOGY: DevTopoRow[] = [
  { component: 'Cursor IDE + Git', location: 'Mac Pro', reason: 'File system, HMR, debugger — irreplaceable local' },
  { component: 'Frontend Vite (HMR)', location: 'Mac Pro', reason: 'Sub-second hot-reload requires localhost' },
  { component: 'Active API (the one being edited)', location: 'Mac Pro', reason: 'uvicorn --reload <2s inner loop' },
  { component: 'Remaining 8 APIs', location: 'K3s dev namespace', reason: 'Not being edited; no need for local resources' },
  { component: 'Socket (IB/Massive)', location: 'K3s', reason: 'LAN proximity to TWS; always-on edge services' },
  { component: 'Daemon', location: 'K3s', reason: 'Runs against K3s Redis/PG; debug mode via remote attach' },
  { component: 'Celery workers', location: 'K3s', reason: 'Distributed by nature; no local benefit' },
  { component: 'Redis (live + queue)', location: 'K3s data NS', reason: 'Stateful; HA; shared by all services' },
  { component: 'PostgreSQL', location: 'K3s data NS', reason: 'Stateful; CloudNativePG; WAL backup' },
]

// ---------------------------------------------------------------------------
// Redis ideal topology (per environment, 2 instances)
// ---------------------------------------------------------------------------

export type RedisRoleRow = {
  role: string
  instance: string
  keys: string
  sla: string
}

export const REDIS_ROLES: RedisRoleRow[] = [
  {
    role: 'R1 — Realtime quote bus',
    instance: 'redis-live',
    keys: 'ib:ingester:tick:*, ib:ingester:channel, massive:stream, quote:*',
    sla: 'ms latency; noeviction; AOF everysec',
  },
  {
    role: 'R2 — Trade command RPC',
    instance: 'redis-live',
    keys: 'ib:operator:cmd (Stream+CG), ib:operator:result:{id}',
    sla: 'ms latency; must not lose commands; AOF',
  },
  {
    role: 'R3 — Account event stream',
    instance: 'redis-live',
    keys: 'ib:account:stream:v1, ib:account:{id}',
    sla: '100ms; staging before PG persistence',
  },
  {
    role: 'R4 — Health & observability',
    instance: 'redis-live',
    keys: 'bifrost:health:*, bifrost:ops:worker_presence:*',
    sla: 'Seconds; Ops lamp accuracy',
  },
  {
    role: 'R5 — Console logs',
    instance: 'redis-live',
    keys: 'bifrost:console:ws_* (MAXLEN ~10000)',
    sla: 'Seconds; loss of a few lines acceptable',
  },
  {
    role: 'R6 — Celery tasks',
    instance: 'redis-queue',
    keys: 'Celery broker queues + result backend',
    sla: 'Seconds–minutes; retryable; isolated from live path',
  },
]

export const REDIS_TOPOLOGY_STATEMENT =
  'Each environment (stg, prod) runs two HA Redis instances: redis-live (R1–R5, noeviction, Sentinel) ' +
  'and redis-queue (R6, Celery, allkeys-lru). Dev uses a single local Redis or K3s dev-namespace instance. ' +
  'Environment isolation is absolute: Prod Redis never shared with STG or Dev.'

// ---------------------------------------------------------------------------
// MCP bridge layer
// ---------------------------------------------------------------------------

export type McpBridgeRow = {
  server: string
  provides: string
  agentLayers: string
}

export const MCP_BRIDGES: McpBridgeRow[] = [
  { server: 'mcp-server-kubernetes', provides: 'Pod/Node/Namespace CRUD, logs, events, rollout', agentLayers: 'Ops, Dev' },
  { server: 'mcp-server-redis', provides: 'Health keys read, Stream inspect, pub/sub status', agentLayers: 'Ops, Business' },
  { server: 'mcp-server-postgres', provides: 'Positions, PnL, daemon state, strategy config (read)', agentLayers: 'Business, Ops' },
  { server: 'mcp-server-argocd', provides: 'App sync status, sync trigger, rollback', agentLayers: 'Dev, Ops' },
  { server: 'mcp-server-tekton', provides: 'Pipeline run, logs, status', agentLayers: 'Dev' },
  { server: 'mcp-server-prometheus', provides: 'Metrics query, alert status', agentLayers: 'Ops' },
  { server: 'mcp-trade-api (future)', provides: 'Trade domain APIs — quotes, portfolio, SEPA, strategy', agentLayers: 'Business' },
]

// ---------------------------------------------------------------------------
// Model allocation
// ---------------------------------------------------------------------------

export type ModelAllocRow = {
  task: string
  model: string
  reason: string
}

export const MODEL_ALLOCATION: ModelAllocRow[] = [
  { task: 'Code generation / refactoring', model: 'Claude Opus / Sonnet', reason: 'Deep context, multi-file reasoning' },
  { task: 'Quick Ops diagnosis', model: 'Claude Sonnet', reason: 'Fast, sufficient for log/metric interpretation' },
  { task: 'Strategy analysis / Greeks reasoning', model: 'Claude Opus', reason: 'Complex quantitative reasoning' },
  { task: 'Log summary / pattern matching', model: 'Ollama local (7B–32B)', reason: 'Privacy; strategy data never leaves LAN' },
  { task: 'Security review', model: 'Claude Opus', reason: 'Thoroughness on sensitive code paths' },
  { task: 'Daily trade brief (scheduled)', model: 'Ollama or Sonnet via SDK', reason: 'Routine; cost-efficient; local option preserves privacy' },
]

// ---------------------------------------------------------------------------
// Convergence milestones
// ---------------------------------------------------------------------------

export type VisionMilestone = {
  id: string
  title: string
  flywheels: string
  deliverables: string[]
  unlocks: string
}

export const VISION_MILESTONES: VisionMilestone[] = [
  {
    id: 'V1',
    title: 'Dev inner-loop on K3s',
    flywheels: 'A + B',
    deliverables: [
      'bifrost-dev namespace with remaining APIs + Redis + PG',
      'Frontend VITE_API_* points to K3s dev Ingress',
      'Mac Pro = IDE + Vite + 1 active API only',
    ],
    unlocks: 'Mac Pro fully liberated from Docker full-stack',
  },
  {
    id: 'V2',
    title: 'Dev Agent closed-loop',
    flywheels: 'A',
    deliverables: [
      'Cursor SDK CI hook: push → Tekton → STG deliver → verify → report',
      'Agent auto-runs lint/build/check:legacy-css before push',
      'promote command: Agent executes deliver-prod with safety gates',
    ],
    unlocks: 'Zero-manual-step code-to-production pipeline',
  },
  {
    id: 'V3',
    title: 'Ops Agent L1/L2',
    flywheels: 'B',
    deliverables: [
      'MCP K8s + Redis + PG servers deployed and connected to Cursor',
      'AlertManager webhook → Cursor SDK → Agent diagnosis + L1 auto-fix',
      'Ops Console displays Agent action audit trail',
    ],
    unlocks: 'Agent handles routine ops; Owner only reviews L2+ confirmations',
  },
  {
    id: 'V4',
    title: 'Business Agent read-only',
    flywheels: 'A + B',
    deliverables: [
      'mcp-trade-api: Agent reads 9 Trade API domains',
      'Scheduled daily brief (pre-market, post-market) via SDK',
      'Ad-hoc Trade Q&A in Cursor chat ("current IV?", "PnL today?")',
    ],
    unlocks: 'AI as trading assistant — observe + advise (never execute)',
  },
  {
    id: 'V5',
    title: 'Full convergence',
    flywheels: 'A + B merged',
    deliverables: [
      'Single Cursor window: code + deploy + ops + trade intelligence',
      'Ollama local inference for sensitive strategy analysis',
      'Trade insights feed back into platform improvements (closed loop)',
      'Agent suggests Gate parameter changes → L3 PR → Owner approve',
    ],
    unlocks: 'Owner decides; Agents execute everything else. Dual flywheel self-accelerating.',
  },
]

// ---------------------------------------------------------------------------
// Absolute boundaries (inherited + extended)
// ---------------------------------------------------------------------------

export type VisionBoundary = {
  rule: string
  detail: string
  enforced: string
}

export const VISION_BOUNDARIES: VisionBoundary[] = [
  { rule: 'R-DV3', detail: 'One auto-trade Daemon per IB account; Dev/Prod separate client_id', enforced: 'Config + NetworkPolicy' },
  { rule: 'Trade write path', detail: 'Only Daemon → ib:operator:cmd; Agents never write this Stream', enforced: 'RBAC + MCP deny-list' },
  { rule: 'TWS isolation', detail: 'Win11 dedicated; never K3s-scheduled; Socket connects via IB_HOST LAN', enforced: 'Physical separation' },
  { rule: 'Business Agent read-only', detail: 'Agent reads Trade APIs/Redis/PG; cannot modify positions or orders', enforced: 'MCP server read-only mode' },
  { rule: 'L3 requires Owner PR approval', detail: 'Structural K8s changes, Gate params, scaling policy → PR → review → merge', enforced: 'ArgoCD + branch protection' },
  { rule: 'Secrets on LAN', detail: 'Strategy code, API keys, trade data never leave LAN; Ollama for sensitive analysis', enforced: 'NetworkPolicy egress deny + local model' },
]

// ---------------------------------------------------------------------------
// LLM pack builder
// ---------------------------------------------------------------------------

export function buildDualFlywheelVisionLlmPack(): string {
  const lines: string[] = [
    '# Bifrost — Dual Flywheel Vision (Ultimate North Star)',
    `# Source: ${VISION_SOURCE} v${VISION_VERSION}`,
    '',
    '## Vision statement',
    VISION_STATEMENT,
    '',
    '## Flywheel convergence thesis',
    FLYWHEEL_CONVERGENCE,
    '',
    '## Decoupling principle (platform ≠ business)',
    DECOUPLING_STATEMENT,
    ...DECOUPLING_RULES.map(r =>
      `- **${r.boundary}**: Platform=[${r.platform}] | Business=[${r.business}]`),
    REUSABILITY_STATEMENT,
    '',
    '## Three-layer Agent architecture',
    ...AGENT_LAYERS.flatMap(l => [
      `### Layer ${l.layer}: ${l.name}`,
      `Scope: ${l.scope}`,
      `Cursor role: ${l.cursorRole}`,
      `K8s role: ${l.k8sRole}`,
      `Forbidden: ${l.forbidden}`,
      ...l.examples.map(e => `- ${e}`),
    ]),
    '',
    '## Unified experience (one Cursor window)',
    ...EXPERIENCE_EXAMPLES.map(e =>
      `- [${e.layer}] "${e.userSays}" → ${e.agentDoes}`),
    '',
    '## Dev topology (Mac thin + K3s thick)',
    ...DEV_TOPOLOGY.map(d => `- **${d.component}** @ ${d.location}: ${d.reason}`),
    '',
    '## Redis ideal topology',
    REDIS_TOPOLOGY_STATEMENT,
    ...REDIS_ROLES.map(r => `- **${r.role}** [${r.instance}]: ${r.keys} — SLA: ${r.sla}`),
    '',
    '## MCP bridge layer',
    ...MCP_BRIDGES.map(m => `- **${m.server}**: ${m.provides} → layers: ${m.agentLayers}`),
    '',
    '## Model allocation',
    ...MODEL_ALLOCATION.map(m => `- ${m.task}: **${m.model}** — ${m.reason}`),
    '',
    '## Convergence milestones (V1–V5)',
    ...VISION_MILESTONES.flatMap(m => [
      `### ${m.id}: ${m.title} [${m.flywheels}]`,
      ...m.deliverables.map(d => `- ${d}`),
      `→ Unlocks: ${m.unlocks}`,
    ]),
    '',
    '## Absolute boundaries',
    ...VISION_BOUNDARIES.map(b => `- **${b.rule}**: ${b.detail} — enforced by: ${b.enforced}`),
  ]
  return lines.join('\n')
}
