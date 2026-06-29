/**
 * Platform Roadmap — Compose → K3s execution plan.
 *
 * Authoritative source for Ops Console → Architecture → Platform Roadmap.
 * Migrated from bifrost-trade-infra/docs/PLATFORM_ROADMAP.md (2026-06-15).
 */

export const ROADMAP_VERSION = '2026-06-27'
export const ROADMAP_SOURCE = 'console/src/lib/architecture/roadmapCatalog.ts'

export const ROADMAP_STATUS =
  'In progress — 2C-A CLOSED, 2C-B / K3s phased; k3s-phase1 bootstrap CLOSED @ .73'

export type RoadmapHardwareRow = {
  device: string
  current: string
  nearTerm: string
  k3sTarget: string
}

export const HARDWARE_MAPPING: RoadmapHardwareRow[] = [
  {
    device: 'Win11 ×2',
    current: 'Host / Secondary TWS dedicated hosts',
    nearTerm: 'Outside cluster — TWS/Gateway only; IB_HOST → LAN IP',
    k3sTarget: 'Same — never run K3s workloads on TWS hosts',
  },
  {
    device: 'Linux mini-pc-a',
    current: 'Legacy Prod .70',
    nearTerm: 'New Docker Prod (2C-B primary host)',
    k3sTarget: 'K3s Server ① · API / Redis / Ingress / Gitea / ArgoCD',
  },
  {
    device: 'Linux mini-pc-b',
    current: 'PG dedicated .80',
    nearTerm: 'PostgreSQL bifrost_prod / bifrost_dev',
    k3sTarget: 'K3s Server ② · CloudNativePG Primary',
  },
  {
    device: 'Linux mini-pc-c',
    current: 'Planned / idle until Legacy retires',
    nearTerm: 'Staging or idle',
    k3sTarget: 'K3s Server ③ · monitoring / Tekton / AIOps',
  },
  {
    device: 'Mac Mini ×2',
    current: 'M4 idle',
    nearTerm: '#1 Dev stack · #2 CI / staging',
    k3sTarget: 'UTM Ubuntu K3s Agent · ops-vm-ubt-01/.54 · ops-vm-ubt-02/.56 (P5b Done)',
  },
  {
    device: 'MacBook',
    current: 'Primary dev machine',
    nearTerm: 'Cursor · prod-preflight-local · sign-off',
    k3sTarget: 'kubectl · Claude + mcp-k8s',
  },
  {
    device: '4090 server',
    current: 'K3s P5a — gpu-server @ 192.168.10.60 (WOL eno1)',
    nearTerm: 'Data warehouse + Ollama trial + Tekton heavy CI',
    k3sTarget: 'Agent · node-role=warehouse · workload=gpu · bifrost.io/workload-pool=compute',
  },
  {
    device: 'Network',
    current: 'Router + switch + VLAN + 3×UPS',
    nearTerm: 'Trading VLAN isolated from Dev/AI',
    k3sTarget: 'Traefik Ingress LAN-only',
  },
]

export const HARDWARE_NOTES = [
  'K3s doc lists three Mini PCs; current reality is two Linux Minis live + mini-pc-c as second batch (K3s §9 stage 4).',
  'TWS runs on Win11 (not Mac Mini) — socket connects via IB_HOST on LAN; same principle as ARCHITECTURE §2.',
  'The two Mac Minis (.50 primary / .52 standby) double as the L-1 Out-of-Band Operator Plane: AI Remediation Runners + launchd mutual watchdog, living OUTSIDE K8s so they can recover the cluster. Fate-isolated by design — see Architecture → K3s Bootstrap (L-1) and Flywheel Vision.',
]

export type MilestoneBaselineRow = {
  milestone: string
  status: string
  meaning: string
}

export const SOFTWARE_BASELINE: MilestoneBaselineRow[] = [
  { milestone: 'Phase 2B', status: 'CLOSED', meaning: 'New frontend + new API (Dev 8765–8773)' },
  { milestone: '2C-A', status: 'CLOSED', meaning: 'Mac localhost compose + Session 0–9' },
  { milestone: '2C-A.1', status: 'Owner verified', meaning: 'Ops docker executor · Socket/Celery control plane' },
  { milestone: '2C-B', status: 'Planning', meaning: 'New Docker Prod on Linux · Legacy retire' },
  { milestone: 'K3s', status: 'Partial', meaning: 'Bootstrap @ .73 CLOSED; full migration not blocking app code' },
]

export const PHASE_OVERVIEW =
  'Phase A (now): 2C-B Docker Prod → Legacy retire → Mac Mini Dev+CI. Phase B (3–9mo): K3s bootstrap + GitOps + Compose→Helm. Phase C (9–18mo): observability + AI ops platform + downstream page refactor / replay AI.'

export type StepRow = { step: string; action: string }

export const PHASE_A_2CB_STEPS: StepRow[] = [
  { step: 'A1.1', action: '.env: POSTGRES_* → .80/bifrost_prod; REDIS_* → .70; IB_HOST → Win11 Host IP' },
  { step: 'A1.2', action: 'BIFROST_BUILD_LOCAL=0 or Linux monorepo build; make prod-preflight + prod-health' },
  { step: 'A1.3', action: 'Maintenance window: stop Legacy engine + multi-port APIs; R-DV3 single New daemon' },
  { step: 'A1.4', action: 'Owner 2C-B sign-off → bifrost-trade-infra/docs/PHASE2C_SIGNOFF_MASTER.md' },
]

export type MacMiniRoleRow = { machine: string; service: string; connection: string }

export const PHASE_A_MAC_MINI: MacMiniRoleRow[] = [
  {
    machine: 'Mac Mini #1 (.50)',
    service: 'docker-compose.dev.yml all-day Dev stack + Remediation Runner PRIMARY (nightly-drift)',
    connection: 'bifrost_dev @ .80; Dev client_id → Win11; peer watchdog → .52',
  },
  {
    machine: 'Mac Mini #2 (.52)',
    service: 'Git runner + make prod-health release gate + Remediation Runner STANDBY (failover)',
    connection: 'Auto verify before tag; optional Uptime Kuma; peer watchdog → .50',
  },
]

export const PHASE_A_GPU_RULES = [
  'Install Ollama + one 7B–32B model for dev/ops Q&A trial only.',
  'Forbidden: attach Prod Redis; forbidden: ib:operator:cmd.',
  'Reserve for Phase C Research RAG.',
]

export type DeliverableRow = { artifact: string; description: string }

export const PHASE_A_DELIVERABLES: DeliverableRow[] = [
  { artifact: 'scripts/release_gate.sh', description: 'Aggregate prod-health + optional smoke URL' },
  { artifact: 'CI workflow (Mac Mini #2)', description: 'PR: pytest / npm build / check-legacy-css' },
]

export const PHASE_A_EXIT =
  'Legacy retired; .70 runs New compose only; Dev/CI separated from Prod; Win11 TWS-only.'

export const PHASE_B_PREREQ =
  'Phase A stable ≥2 weeks; bifrost-trade-* images reproducibly buildable.'

export const PHASE_B_BOOTSTRAP = [
  'mini-pc-a: Ubuntu 24.04 + K3s Server (single-node validation)',
  'mini-pc-b: join Server; label node-role=postgres',
  'gpu-server: Agent + workload=gpu',
  'Mac Mini: UTM Ubuntu VM → Agent (P5b Done — ops-vm-ubt-01/.54, ops-vm-ubt-02/.56)',
]

export const PHASE_B_DATA_MIGRATION = [
  'CloudNativePG bifrost-postgres on mini-pc-b (see K3s Architecture catalog)',
  'pg_dump/pg_restore from bare .80; apps connect bifrost-postgres-rw.data.svc',
  'After stable: bare .80 PG becomes standby or retires',
  'Redis: Bitnami Helm in data NS, or short-term .70 host Redis (single-variable)',
]

export const PHASE_B_APP_ORDER =
  'data (PG, Redis) → bifrost NS: socket → worker (daemon, celery) → api (×9) → frontend → Traefik Ingress replaces nginx compose'

export type CicdComponentRow = { component: string; location: string; role: string }

export const PHASE_B_CICD: CicdComponentRow[] = [
  { component: 'Gitea', location: 'cicd @ mini-pc-a', role: 'Internal Git; strategy code stays on LAN' },
  { component: 'Tekton', location: 'cicd + gpu-server / Mac Agent', role: 'Build images; 4090 for heavy tests' },
  { component: 'ArgoCD', location: 'cicd', role: 'GitOps; argocd app sync replaces compose up' },
  { component: 'Registry', location: 'cicd', role: 'Internal images; BIFROST_*_REF → digest' },
]

export const PHASE_B_REPO_LAYOUT = [
  'bifrost-trade-infra/k8s/base/ — Kustomize: api, worker, socket, frontend',
  'bifrost-trade-infra/k8s/overlays/dev|prod/',
  'bifrost-trade-infra/argocd/apps/',
  'bifrost-trade-infra/tekton/',
]

export const PHASE_B_EXIT = 'Prod on K3s; Compose Dev-only; Gitea+ArgoCD manage releases.'

export type PhaseCCapabilityRow = { capability: string; implementation: string }

export const PHASE_C_OPS_PLATFORM: PhaseCCapabilityRow[] = [
  { capability: 'Self-discovery', implementation: 'K8s/Compose inventory + health + Git/image versions → MCP & docs' },
  { capability: 'Self-maintenance', implementation: 'Tekton build/test; ArgoCD GitOps; release_gate; Mac Mini CI sentinel' },
  { capability: 'Self-healing', implementation: 'Ops API L0–L2; bifrost-ops-mcp; Ollama @ gpu-server; no LLM direct orders' },
  { capability: 'Observability', implementation: 'kube-prometheus-stack + Loki @ monitoring (after mini-pc-c)' },
  { capability: 'Inference shell', implementation: 'Open-WebUI / Cursor MCP reads Grafana, Loki, K8s events' },
]

export const PHASE_C_DOWNSTREAM = [
  'Tekton: tag triggers release_gate + image push + ArgoCD sync',
  'Cursor/Claude: MIGRATION_TRACKING + Goal + migration-protocol; MCP reads health signals',
  'Mac Mini Dev → K3s bifrost-dev namespace; Dense UI pages gated by CI before staging',
]

export type OptionalHardwareRow = { item: string; trigger: string }

export const OPTIONAL_HARDWARE: OptionalHardwareRow[] = [
  { item: 'NAS', trigger: 'PG backup needs off-site; MinIO insufficient' },
  { item: 'Dedicated Redis host', trigger: 'Celery + live quotes saturate mini-pc-a' },
  { item: '10Gb switch', trigger: 'Cross-node PG replication bottleneck' },
]

export const OWNER_CHECKLIST: string[] = [
  '2C-B Prod host = mini-pc-a (.70)?',
  'PG short-term stays bare .80; K3s CNPG in Phase B?',
  'Mac Mini #1 = Dev compose, #2 = CI?',
  'K3s agents: Mac Mini UTM Done (P5b); gpu-server (P5a) and mini-pc-a/b Server join still pending?',
  'Platform priority: 2C-B + release_gate before K3s single-node?',
  'Replay AI: pgvector vs Qdrant? (see Goal §10)',
]

export type RelatedDocRow = { topic: string; authority: string }

export const RELATED_DOCS: RelatedDocRow[] = [
  { topic: 'AI-native ops north star', authority: 'Ops Console → Architecture → Blueprint § AI Native Platform (blueprintCatalog.ts)' },
  { topic: 'K3s target topology', authority: 'Ops Console → Architecture → K3s Architecture (k3sArchitectureCatalog.ts)' },
  { topic: 'K3s bootstrap runbook', authority: 'Ops Console → Architecture → K3s Bootstrap (k3sBootstrapCatalog.ts)' },
  { topic: 'Deploy mainline', authority: 'Ops Console → Operate → Deploy Mainline (deployMainlineCatalog.ts)' },
  { topic: '2C-B runbook / sign-off', authority: 'bifrost-trade-infra/docs/PHASE2C_SIGNOFF_MASTER.md' },
  { topic: 'Code migration progress', authority: 'bifrost-trade-infra/docs/MIGRATION_TRACKING.md' },
  { topic: 'Docker build / rebuild', authority: 'bifrost-trade-infra/docs/DOCKER_BUILD.md' },
]

export function buildRoadmapLlmPack(): string {
  const lines: string[] = [
    '# Bifrost Ops — Platform Roadmap (Compose → K3s)',
    `# Source: ${ROADMAP_SOURCE} v${ROADMAP_VERSION}`,
    `Status: ${ROADMAP_STATUS}`,
    '',
    '## §1 Hardware mapping',
    ...HARDWARE_MAPPING.map(
      r => `- **${r.device}**: now=${r.current}; near=${r.nearTerm}; k3s=${r.k3sTarget}`,
    ),
    ...HARDWARE_NOTES.map(n => `- Note: ${n}`),
    '',
    '## §2 Software baseline',
    ...SOFTWARE_BASELINE.map(r => `- **${r.milestone}** [${r.status}]: ${r.meaning}`),
    '',
    '## §3 Phase overview',
    PHASE_OVERVIEW,
    '',
    '## §4 Phase A — 2C-B + resource activation (current priority)',
    '### A1 2C-B cutover',
    ...PHASE_A_2CB_STEPS.map(s => `- ${s.step}: ${s.action}`),
    '### A2 Mac Mini roles',
    ...PHASE_A_MAC_MINI.map(r => `- ${r.machine}: ${r.service} (${r.connection})`),
    '### A3 4090 trial rules',
    ...PHASE_A_GPU_RULES.map(r => `- ${r}`),
    '### A4 Compose-era deliverables',
    ...PHASE_A_DELIVERABLES.map(r => `- ${r.artifact}: ${r.description}`),
    `Exit: ${PHASE_A_EXIT}`,
    '',
    '## §5 Phase B — K3s + GitOps',
    `Prereq: ${PHASE_B_PREREQ}`,
    'Bootstrap:',
    ...PHASE_B_BOOTSTRAP.map(s => `- ${s}`),
    'Data migration:',
    ...PHASE_B_DATA_MIGRATION.map(s => `- ${s}`),
    `App order: ${PHASE_B_APP_ORDER}`,
    ...PHASE_B_CICD.map(r => `- **${r.component}** @ ${r.location}: ${r.role}`),
    'Repo layout:',
    ...PHASE_B_REPO_LAYOUT.map(s => `- ${s}`),
    `Exit: ${PHASE_B_EXIT}`,
    '',
    '## §6 Phase C — AI-native ops + downstream',
    ...PHASE_C_OPS_PLATFORM.map(r => `- **${r.capability}**: ${r.implementation}`),
    ...PHASE_C_DOWNSTREAM.map(s => `- ${s}`),
    '',
    '## §7 Optional hardware (12–18 months)',
    ...OPTIONAL_HARDWARE.map(r => `- ${r.item}: when ${r.trigger}`),
    '',
    '## §8 Owner checklist',
    ...OWNER_CHECKLIST.map(q => `- [ ] ${q}`),
    '',
    '## Related authorities',
    ...RELATED_DOCS.map(r => `- ${r.topic}: ${r.authority}`),
  ]
  return lines.join('\n')
}
