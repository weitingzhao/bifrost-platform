/**
 * Bifrost Ops — environments & architecture catalog.
 *
 * Static catalog for LLM packs and Runtime Map registry (probe targets, scope, hardware).
 * Milestones and decisions: authoritative via config/ops-context.yaml + GET /api/v1/context.
 *
 * Live environment status: Control Room + Runtime Map. Governance UI: Blueprint, Standards, Agent Protocol.
 */

export const CATALOG_VERSION = '2026-06-29-ai-compute-strategy'

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

export type HardwareRow = {
  id: string
  host: string
  roleCompose: string
  roleK3s: string
}

export type PlatformPhase = {
  id: string
  label: string
  sequence: string
  deliverables: string
}

export const SCOPE_ROWS: ScopeRow[] = [
  {
    tag: 'PLATFORM',
    component: 'Bifrost Ops Platform (control plane)',
    technology: 'Go API :8780 · React Ops Console :5180 · Dense UI tokens',
    notes:
      'Control plane — Go API :8780, React Ops Console :5180, MCP tools. Probes matrix/topology/spine; no Monitor POST /control/* or ib:operator:cmd.',
  },
  {
    tag: 'TRADE-FE',
    component: 'bifrost-trade-frontend',
    technology: 'React 18 · Vite · TanStack Query · shadcn/ui',
    notes: 'Trade monitoring SPA :5173. Separate from Platform Console. Phase 1: New FE + Legacy API equivalence.',
  },
  {
    tag: 'TRADE-API',
    component: 'bifrost-trade-api (path domains; massive retired)',
    technology: 'FastAPI · ports 8765–8773 via nginx /api/{domain}/',
    notes:
      'monitor · docs · ops · trading · strategy · portfolio · market · research (massive REST retired → Market Data Plugin). Platform probes health endpoints only.',
  },
  {
    tag: 'WORKER',
    component: 'bifrost-trade-worker',
    technology: 'Python · daemon (GsTrading FSM) · account-sync',
    notes: 'Daemon reads Redis quotes/account; orders via ib_operator RPC. Does not connect to TWS directly.',
  },
  {
    tag: 'SOCKET',
    component: 'bifrost-trade-socket (RETIRED / reference-only)',
    technology: 'Python · legacy IB ingestor / account / operator (reference)',
    notes:
      'Wave 14G-F: production IB edge is Platform IB Gateway Plugin → redis-ib (IBGP). Do not start trade-socket for Inner Loop or K3s. Repo kept as reference until Phase 3 archive. Compose default path still pending Phase 1 profile cutover.',
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
    technology: 'CloudNativePG bifrost-postgres @ data NS · bifrost_dev / bifrost_stg / bifrost_prod',
    notes:
      'D2-prime: CNPG is truth. Legacy bare .80 / options_db decommissioned 2026-06-29 (box → ubt-k3s-06 @ .79). LAN DBeaver: NodePort 30432.',
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
    technology: 'Mac minis run the operator plane out of band and peer-watchdog; .50 relays alerts · Git Bridge (Mac Pro, Dev-only)',
    notes:
      'The engineer on the ground — recovers Ops Platform (rocket) + Trade (payload) from OUTSIDE K8s; never an in-cluster Pod. Mutual watchdog + platform-api failover. See cicdBootstrapCatalog.ts L-1 · k3sBootstrapCatalog.ts · Flywheel Vision.',
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
    id: 'ubt-k3s-06',
    host: '192.168.10.79',
    roleCompose: '— (former bare PG .80 decommissioned 2026-06-29)',
    roleK3s: 'Outside the cluster since 2026-10-17 · Agent host (agentd for PROD and STG)',
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
    roleCompose: 'Out-of-band operator-plane · mutual watch of .52 · alert relay on .50',
    roleK3s: 'UTM Agent ops-vm-ubt-01 · frontend dev (Ready) · operator-plane stays OUTSIDE cluster',
  },
  {
    id: 'mac-mini-2',
    host: '192.168.10.52 (macOS · Agent)',
    roleCompose: 'Out-of-band operator-plane · mutual watch of .50',
    roleK3s: 'UTM Agent ops-vm-ubt-02 · CI · external watchdog (Ready) · operator-plane stays OUTSIDE cluster',
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

/** Spine section from GET /api/v1/context (authoritative milestones). */
