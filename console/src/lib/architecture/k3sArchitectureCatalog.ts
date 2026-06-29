/**
 * K3s platform target architecture.
 *
 * Authoritative source for Ops Console → Architecture → K3s Architecture.
 * Migrated from bifrost-trade-infra/docs/K3S_PLATFORM_ARCHITECTURE.md (2026-06-15).
 */

export const K3S_ARCH_VERSION = '2026-06-15-p5b'
export const K3S_ARCH_SOURCE = 'console/src/lib/architecture/k3sArchitectureCatalog.ts'

export const K3S_ARCH_STATUS =
  'Bootstrap @ 192.168.10.73 CLOSED (k3s-phase1); P5b Mac UTM agents 2/2 Ready (3/3 nodes); full Compose→K3s migration blocked on D1'

export const BACKGROUND_COMPOSE =
  'Legacy on dedicated Linux; new stack via docker-compose.yml. Port conflicts (Redis 6379, PG 5432, APIs 8765–8773) when sharing one host.'

export const BACKGROUND_K3S_GOALS = [
  'Unified K3s cluster — ClusterIP + Ingress routing, no host port conflicts',
  'GitOps — ArgoCD deploys all workloads; no manual compose up for prod',
  'AI-native — agents inspect cluster state, run routine ops, propose changes via PR',
]

export type K3sNodeRow = {
  name: string
  cpu: string
  ram: string
  os: string
  batch: string
  role: string
}

export const HARDWARE_NODES: K3sNodeRow[] = [
  { name: 'mini-pc-a', cpu: 'Ryzen 7 7735HS', ram: '24GB', os: 'Linux', batch: 'First', role: 'K3s Server ① · general services' },
  { name: 'mini-pc-b', cpu: 'Ryzen 7 7735HS', ram: '32GB', os: 'Linux', batch: 'First', role: 'K3s Server ② · database dedicated' },
  { name: 'gpu-server', cpu: 'Ryzen 9 9500S', ram: '128GB', os: 'Linux', batch: 'First', role: 'K3s Agent · data warehouse · compute · GPU @ 192.168.10.60' },
  { name: 'mac-mini-1', cpu: 'Apple M4', ram: '16GB', os: 'macOS + UTM', batch: 'First', role: 'Host for ops-vm-ubt-01 Agent · dev/CI' },
  { name: 'mac-mini-2', cpu: 'Apple M4', ram: '16GB', os: 'macOS + UTM', batch: 'First', role: 'Host for ops-vm-ubt-02 Agent · monitor/CI' },
  { name: 'mini-pc-c', cpu: 'Ryzen 7 7735HS', ram: '32GB', os: 'Linux', batch: 'Second', role: 'K3s Server ③ · monitoring/CI (Legacy retired ✓; 3-Server HA join pending)' },
  { name: 'ubt-k3s-04', cpu: 'Ryzen 7', ram: '32GB', os: 'Linux', batch: 'Third', role: 'K3s Agent · CNPG primary · data-primary @ .75' },
  { name: 'ubt-k3s-05', cpu: 'Ryzen 7', ram: '22GB', os: 'Linux', batch: 'Third', role: 'K3s Agent · general pool · stg/CI offload @ .77' },
]

export const HARDWARE_NOTE =
  'Three Mini PCs form odd Server quorum (2/3 etcd). Mac Minis host UTM Ubuntu VMs (ops-vm-ubt-01/.54, ops-vm-ubt-02/.56) as K3s Agents — joined P5b 2026-06-15.'

export const CLUSTER_TOPOLOGY_ASCII = `
┌──────────────────────────── K3s HA Control Plane ──────────────────────────────┐
│  mini-pc-a (24GB)     mini-pc-b (32GB)     mini-pc-c (32GB) [second batch]     │
│  Server ①             Server ②             Server ③                             │
│  API·Redis·Gitea      PG Primary·pgvector   Prometheus·Loki·Grafana            │
│  ArgoCD·Traefik       Standby→mini-pc-a     Tekton runners                     │
└─────────────────────────────────────────────────────────────────────────────────┘
  gpu-server (4090·128GB @ .60)        mac-mini-1 / mac-mini-2 (M4·16GB ×2)
  Agent warehouse·compute·gpu          Agent (UTM Ubuntu VM · .54 / .56)
  WOL eno1 · MinIO/OLAP·Ollama         frontend·light CI·kubectl client
`.trim()

export type PgPrincipleRow = { layer: string; content: string; note: string }

export const PG_PRINCIPLES: PgPrincipleRow[] = [
  { layer: 'Storage', content: 'local-path PVC', note: 'mini-pc-b local NVMe for max IO' },
  { layer: 'Scheduling', content: 'nodeAffinity → mini-pc-b', note: 'Primary always on DB node' },
  { layer: 'HA', content: 'Streaming replication', note: 'Standby on mini-pc-a; auto failover' },
  { layer: 'Backup', content: 'WAL archive → MinIO', note: 'PITR capable' },
  { layer: 'Operator', content: 'CloudNativePG', note: 'Declarative YAML; operator manages lifecycle' },
]

export const PG_DATA_PATH =
  'Host: /var/lib/rancher/k3s/storage/pvc-*/PGDATA/ · Pod mount: /var/lib/postgresql/data'

export const PG_CONFIG_SNIPPET = `
apiVersion: postgresql.cnpg.io/v1
kind: Cluster
metadata:
  name: bifrost-postgres
  namespace: data
spec:
  instances: 2
  storage: { size: 500Gi, storageClass: local-path }
  affinity: nodeAffinity node-role=postgres (mini-pc-b)
  parameters: shared_buffers=8GB, max_connections=200
  backup: barmanObjectStore → s3://postgres-backup @ minio.data.svc
`.trim()

export type CiCdCompareRow = { dimension: string; selfHosted: string; github: string }

export const CICD_COMPARE: CiCdCompareRow[] = [
  { dimension: 'Strategy code security', selfHosted: 'Never leaves LAN', github: 'Uploaded to third party' },
  { dimension: 'Secrets', selfHosted: 'K8s Secrets internal', github: 'Third-party hosted' },
  { dimension: 'CI speed', selfHosted: 'RTX 4090 local, no queue', github: 'Shared runners, queue at peak' },
  { dimension: 'Integration tests', selfHosted: 'Direct PG/Redis on LAN', github: 'Tunnel or mocks' },
  { dimension: 'GPU tests', selfHosted: '4090 local', github: 'GPU runners expensive' },
  { dimension: 'Offline', selfHosted: 'Works offline', github: 'Requires internet' },
]

export const CICD_CONCLUSION = 'Trading core assets never pass third parties — self-hosted Gitea+ArgoCD+Tekton.'

export const GITOPS_FLOW =
  'Developer/Agent → git push Gitea → webhook → Tekton (build/test→registry) + ArgoCD (sync to K3s) → rolling update'

export const CICD_DEPLOYMENT =
  'Gitea, ArgoCD, Tekton in cicd namespace on stable Mini PC nodes. Emergency: scripts/emergency-deploy.sh + kubectl apply if API reachable.'

export const AI_LAYERS_ASCII = `
Layer 4 Automation: AlertManager → webhook → AI analysis → auto-fix or human alert
Layer 3 Execution: K8s API · ArgoCD API · Gitea API · Bifrost business APIs
Layer 2 Inference: Ollama (4090) · mcp-server-kubernetes · Open-WebUI
Layer 1 Observation: Prometheus · Loki · Grafana · K8s Events
`.trim()

export const MCP_K8S_CAPABILITIES = [
  'list_pods(namespace)',
  'get_pod_logs(pod, namespace, tail_lines)',
  'describe_node(node)',
  'apply_manifest(yaml_content)',
  'kubectl_exec(pod, command)',
  'get_events(namespace)',
]

export type AiPermissionRow = { level: string; ops: string; execution: string }

export const AI_PERMISSION_LEVELS: AiPermissionRow[] = [
  { level: '1 Read', ops: 'get/describe/logs/metrics', execution: 'AI runs directly' },
  { level: '2 Routine', ops: 'rollout restart · scale · delete pod · argocd sync', execution: 'AI runs + audit log' },
  { level: '3 Structural', ops: 'StatefulSet · PVC · RBAC · Ingress · Namespace', execution: 'AI opens Gitea PR → Owner approve → ArgoCD' },
]

export const EXTERNAL_SENTINEL =
  'Mac Mini cron (outside K3s): if API or Grafana unreachable >3min → push notify (monitor the monitors).'

export type NamespaceRow = { namespace: string; services: string; nodeBinding: string }

export const NAMESPACE_ALLOCATION: NamespaceRow[] = [
  { namespace: 'data', services: 'PostgreSQL · Redis (mini-pc) · Warehouse OLAP · MinIO (4090)', nodeBinding: 'mini-pc-b/a · gpu-server @ .60 (node-role=warehouse)' },
  { namespace: 'cicd', services: 'Gitea · ArgoCD · Tekton · Registry', nodeBinding: 'mini-pc-a' },
  { namespace: 'monitoring', services: 'Prometheus · Loki · Grafana · AlertManager', nodeBinding: 'mini-pc-c (second batch)' },
  { namespace: 'ai', services: 'Ollama · Open-WebUI · AIOps webhook', nodeBinding: 'gpu-server @ 192.168.10.60 / mini-pc-c' },
  { namespace: 'bifrost-stg', services: 'Trade stack STG — 9 APIs · worker · socket · frontend', nodeBinding: 'amd64 pool · ubt-k3s-01/02/04/05' },
  { namespace: 'bifrost-dev', services: 'Trade stack Dev', nodeBinding: 'amd64 pool' },
  { namespace: 'bifrost-prod', services: 'Trade stack Prod', nodeBinding: 'amd64 pool · prod-pool taint' },
  { namespace: 'bifrost-platform-stg', services: 'Ops Platform — platform-api · platform-console', nodeBinding: 'amd64 pool' },
]

export type ComposeMapRow = { compose: string; k8s: string }

export const COMPOSE_TO_K8S: ComposeMapRow[] = [
  { compose: 'services: stateless', k8s: 'Deployment + ClusterIP Service' },
  { compose: 'services: stateful (PG/Redis)', k8s: 'StatefulSet / CloudNativePG / Bitnami Helm' },
  { compose: 'ports: host mapping', k8s: 'Ingress (Traefik) + ClusterIP' },
  { compose: 'volumes:', k8s: 'PersistentVolumeClaim' },
  { compose: 'depends_on:', k8s: 'readinessProbe + initContainer' },
  { compose: '.env', k8s: 'ConfigMap + Secret' },
  { compose: 'networks:', k8s: 'Service DNS (<svc>.<ns>.svc.cluster.local)' },
  { compose: 'docker compose up', k8s: 'argocd app sync or kubectl apply -k' },
]

export type ImplementationPhase = {
  id: string
  title: string
  items: string[]
}

export const IMPLEMENTATION_PHASES: ImplementationPhase[] = [
  {
    id: 'stage-0',
    title: 'Stage 0 (current) — code migration continues',
    items: ['Docker Compose remains production path', 'K3s work does not block worker/api/frontend migration'],
  },
  {
    id: 'stage-1',
    title: 'Stage 1 — K3s foundation',
    items: [
      'mini-pc-a Ubuntu 24.04 + K3s Server (single-node validation)',
      'gpu-server K3s Agent @ 192.168.10.60 · node-role=warehouse · workload=gpu · WOL eno1',
      'DONE: Mac Mini ×2 UTM Ubuntu → Agent nodes P5b (ops-vm-ubt-01/.54, ops-vm-ubt-02/.56)',
      'mini-pc-b Ubuntu + K3s Server join (HA etcd pending mini-pc-c)',
      'CloudNativePG Operator + PostgreSQL StatefulSet + PVC verify',
      'DONE: bootstrap @ .73 · metrics-server Layer A · 6 Bifrost NS · platform-api L0',
    ],
  },
  {
    id: 'stage-2',
    title: 'Stage 2 — CI/CD platform',
    items: [
      'Deploy Gitea (migrate repos)',
      'Deploy ArgoCD (GitOps for compose services)',
      'Tekton + internal Registry',
      'Configure mcp-server-kubernetes for Claude Code',
    ],
  },
  {
    id: 'stage-3',
    title: 'Stage 3 — observability + AI',
    items: [
      'kube-prometheus-stack',
      'Loki log aggregation',
      'Ollama on gpu-server',
      'Open-WebUI wired to Prometheus/Loki/K8s',
      'AIOps webhook (AlertManager → AI → action)',
    ],
  },
  {
    id: 'stage-4',
    title: 'Stage 4 — mini-pc-c join → 3-Server HA (Legacy retired ✓ 2026-06-29)',
    items: [
      'Legacy migration verified stable ✓ — Phase 3 retirement SIGNED (decision D8)',
      'mini-pc-c joins → full 3-Server HA (pending — k3s_ha phase)',
      'Monitoring stack moves to mini-pc-c (pending)',
      'Legacy Linux server retired ✓ — host freed / spare',
    ],
  },
]

export type StatusCheckpointRow = {
  target: string
  planned: string
  actual: string
  notes: string
}

export const STATUS_CHECKPOINTS: StatusCheckpointRow[] = [
  {
    target: 'K3s Server first node',
    planned: 'mini-pc-a / bootstrap @ .73',
    actual: 'Done',
    notes: 'ubt-k3s-01 Ready; 6 Bifrost NS; platform-api L0 ok',
  },
  {
    target: 'metrics-server (Layer A)',
    planned: 'kube-system',
    actual: 'Done',
    notes: 'GET /cluster/metrics available; Console ensure route',
  },
  {
    target: 'Platform cluster probe',
    planned: 'Layer A + B probe',
    actual: 'Done',
    notes: 'Layer B not_installed (Planned); no kube-prometheus yet',
  },
  {
    target: 'K3s Agent join scripts',
    planned: 'gpu-server / Mac Mini',
    actual: 'Mac agents Done',
    notes: 'P5b: ops-vm-ubt-01/.54 + ops-vm-ubt-02/.56 Ready; P5a gpu-server @ .60 — make k3s-join-gpu-server',
  },
  {
    target: 'K3s HA (3 Server)',
    planned: 'mini-pc-a/b/c',
    actual: '—',
    notes: 'install-server-join.sh ready; pending D1 path',
  },
  {
    target: 'PostgreSQL on K3s',
    planned: 'CloudNativePG + local-path',
    actual: '—',
    notes: 'Not started (D2: bare .80 transition)',
  },
  {
    target: 'PG streaming replication',
    planned: 'Primary(b) + Standby(a)',
    actual: '—',
    notes: 'Not started',
  },
  {
    target: 'Gitea',
    planned: 'cicd namespace',
    actual: '—',
    notes: 'Not started',
  },
  {
    target: 'ArgoCD GitOps',
    planned: 'cicd namespace',
    actual: '—',
    notes: 'Not started',
  },
  {
    target: 'Ollama on 4090',
    planned: 'ai namespace',
    actual: '—',
    notes: 'Not started',
  },
  {
    target: 'mcp-server-kubernetes',
    planned: 'Mac Mini local',
    actual: '—',
    notes: 'Not started',
  },
  {
    target: 'Prometheus + Grafana',
    planned: 'monitoring namespace',
    actual: '—',
    notes: 'Not started (Layer B Planned)',
  },
  {
    target: 'Loki',
    planned: 'monitoring namespace',
    actual: '—',
    notes: 'Not started',
  },
  {
    target: 'AIOps webhook',
    planned: 'ai namespace',
    actual: '—',
    notes: 'Not started',
  },
  {
    target: 'External sentinel watchdog',
    planned: 'Mac Mini cron',
    actual: '—',
    notes: 'Not started',
  },
  {
    target: 'Compose → K3s full migration',
    planned: 'bifrost namespace',
    actual: '—',
    notes: 'After code migration; prod cutover blocked on D1',
  },
  {
    target: 'k3s-phase1 sign-off',
    planned: 'K3S_BOOTSTRAP slices 1–6',
    actual: 'Signed 2026-06-14',
    notes: 'Spine k3s-phase1 CLOSED; active_track → ops_ui_actuation',
  },
]

export const RELATED_AUTHORITIES = [
  'Execution order + hardware map: Ops Console → Architecture → Platform Roadmap (roadmapCatalog.ts)',
  'Bootstrap runbook: Ops Console → Architecture → K3s Bootstrap (k3sBootstrapCatalog.ts)',
  'North star: Ops Console → Architecture → Blueprint § AI Native Platform (blueprintCatalog.ts)',
  'Spine: config/ops-context.yaml · GET /api/v1/context',
]

export function buildK3sArchitectureLlmPack(): string {
  const lines: string[] = [
    '# Bifrost Ops — K3s Platform Architecture',
    `# Source: ${K3S_ARCH_SOURCE} v${K3S_ARCH_VERSION}`,
    `Status: ${K3S_ARCH_STATUS}`,
    '',
    '## Background',
    `Compose problem: ${BACKGROUND_COMPOSE}`,
    ...BACKGROUND_K3S_GOALS.map(g => `- ${g}`),
    '',
    '## Hardware nodes',
    ...HARDWARE_NODES.map(
      n => `- **${n.name}** (${n.cpu}, ${n.ram}, ${n.os}, ${n.batch}): ${n.role}`,
    ),
    HARDWARE_NOTE,
    '',
    '## Cluster topology',
    CLUSTER_TOPOLOGY_ASCII,
    '',
    '## PostgreSQL (CloudNativePG target)',
    ...PG_PRINCIPLES.map(r => `- **${r.layer}**: ${r.content} — ${r.note}`),
    `Paths: ${PG_DATA_PATH}`,
    PG_CONFIG_SNIPPET,
    '',
    '## CI/CD — self-hosted vs GitHub',
    ...CICD_COMPARE.map(r => `- ${r.dimension}: self-hosted=${r.selfHosted}; GitHub=${r.github}`),
    CICD_CONCLUSION,
    `GitOps flow: ${GITOPS_FLOW}`,
    CICD_DEPLOYMENT,
    '',
    '## AI-native ops',
    AI_LAYERS_ASCII,
    'MCP k8s:',
    ...MCP_K8S_CAPABILITIES.map(c => `- ${c}`),
    ...AI_PERMISSION_LEVELS.map(r => `- **${r.level}**: ${r.ops} → ${r.execution}`),
    EXTERNAL_SENTINEL,
    '',
    '## Namespace allocation',
    ...NAMESPACE_ALLOCATION.map(r => `- **${r.namespace}**: ${r.services} @ ${r.nodeBinding}`),
    '',
    '## Compose → K8s mapping',
    ...COMPOSE_TO_K8S.map(r => `- ${r.compose} → ${r.k8s}`),
    '',
    '## Implementation phases',
    ...IMPLEMENTATION_PHASES.flatMap(p => [
      `### ${p.title}`,
      ...p.items.map(i => `- ${i}`),
    ]),
    '',
    '## Status checkpoints (update after each architecture change)',
    ...STATUS_CHECKPOINTS.map(
      r => `- **${r.target}**: planned=${r.planned}; actual=${r.actual}; ${r.notes}`,
    ),
    '',
    '## Related authorities',
    ...RELATED_AUTHORITIES.map(a => `- ${a}`),
  ]
  return lines.join('\n')
}
