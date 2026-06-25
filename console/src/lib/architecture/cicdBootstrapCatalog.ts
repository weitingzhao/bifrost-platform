/**
 * CI/CD Bootstrap Model — L0 / L1 / L2 layered self-hosting architecture.
 *
 * Authoritative source for Ops Console → Architecture → K3s → CI/CD Bootstrap.
 * Defines the bootstrap paradox resolution: the Ops Platform (control plane)
 * runs on the same K3s cluster it governs — layers separate concerns so a
 * failure at one level never orphans the ability to recover.
 *
 * Aligned with: blueprintCatalog.ts (D6, north star), deliveryMainlineCatalog.ts
 * (STG/prod release phases), workloadPlacementCatalog.ts (CI node placement).
 */

export const CICD_BOOTSTRAP_VERSION = '2026-06-25.1'
export const CICD_BOOTSTRAP_SOURCE = 'console/src/lib/architecture/cicdBootstrapCatalog.ts'

// ---------------------------------------------------------------------------
// Layer definitions
// ---------------------------------------------------------------------------

export type BootstrapLayerId = 'L0' | 'L1' | 'L2'

export type DeploymentStatus = 'deployed' | 'partial' | 'planned'

export type ComponentDef = {
  name: string
  status: DeploymentStatus
}

export type BootstrapLayerDef = {
  id: BootstrapLayerId
  label: string
  scope: string
  ownership: string
  cicdRule: string
  recoveryPath: string
  components: ComponentDef[]
}

export const BOOTSTRAP_LAYERS: BootstrapLayerDef[] = [
  {
    id: 'L0',
    label: 'Foundation (底座)',
    scope: 'Infrastructure that platform depends on — never self-managed by platform',
    ownership: 'Owner manual / scripts (带外)',
    cicdRule: 'No pipeline — Owner upgrades via kubectl / Helm / shell scripts; platform must not create circular dependency',
    recoveryPath: 'SSH + kubectl from any machine with kubeconfig',
    components: [
      { name: 'K3s cluster runtime', status: 'deployed' },
      { name: 'Argo CD (cicd namespace)', status: 'deployed' },
      { name: 'Tekton Pipelines controller + Triggers controller + Interceptors', status: 'deployed' },
      { name: 'Internal registry (registry.cicd.svc)', status: 'deployed' },
      { name: 'Gitea mirror service', status: 'deployed' },
      { name: 'kubeconfig secrets', status: 'deployed' },
      { name: 'CI node (amd64) availability', status: 'deployed' },
      { name: 'NodePort / LB networking', status: 'deployed' },
      { name: 'NFS provisioners (nfs-hot / nfs-cold)', status: 'deployed' },
    ],
  },
  {
    id: 'L1',
    label: 'Control Plane (控制面)',
    scope: 'Ops Platform itself — self-hosted with mandatory escape hatch',
    ownership: 'Self-hosted (deliver-platform + Argo selfHeal) + Owner escape',
    cicdRule: 'Automated build+deploy via deliver-platform; conservative rollout (Recreate or single-replica rolling); MUST preserve two escape routes',
    recoveryPath: '① Local "make start" (:8780/:5180) bypasses cluster entirely; ② "kubectl apply -k k8s/overlays/platform-stg" bypasses pipeline',
    components: [
      { name: 'platform-api Deployment (NodePort :30878)', status: 'deployed' },
      { name: 'platform-console Deployment (NodePort :30879)', status: 'deployed' },
      { name: 'bifrost-platform-config ConfigMap', status: 'deployed' },
      { name: 'bifrost-platform-kubeconfig Secret', status: 'deployed' },
      { name: 'Argo Application bifrost-platform-stg (selfHeal)', status: 'deployed' },
      { name: 'Argo Application bifrost-platform-prod (selfHeal + HA ×2)', status: 'deployed' },
      { name: 'Tekton pipeline bifrost-deliver-platform (STG)', status: 'deployed' },
      { name: 'Tekton pipeline bifrost-deliver-platform-prod (STG preflight + prod)', status: 'deployed' },
      { name: 'Tekton pipeline bifrost-ci-platform (CI gate: go test + type-check + spine)', status: 'deployed' },
    ],
  },
  {
    id: 'L2',
    label: 'Workload (数据面)',
    scope: 'Trade business stack — fully managed via platform, zero out-of-band operations',
    ownership: 'Pure GitOps via Ops Console + platform-api',
    cicdRule: 'Full automation: Tekton Trigger → test → Kaniko build → rollout → verify → Argo sync; progressive delivery (canary) in future',
    recoveryPath: 'Ops Console → Delivery → re-run deliver-stg/prod; Argo selfHeal auto-corrects drift',
    components: [
      { name: 'bifrost-trade-{stg,prod,dev} workloads (9 APIs + FE + worker + socket + daemon)', status: 'partial' },
      { name: 'Tekton pipelines bifrost-deliver-stg / bifrost-deliver-prod / bifrost-ci-python / bifrost-ci-frontend', status: 'deployed' },
      { name: 'EventListener bifrost-ci (Gitea push webhook → CI PipelineRun)', status: 'deployed' },
      { name: 'Argo Applications bifrost-stg / bifrost-prod', status: 'partial' },
      { name: 'Dockerfile ConfigMaps (api/fe/worker/socket)', status: 'deployed' },
      { name: 'STG/DEV/PROD smoke probes', status: 'partial' },
      { name: 'Release gate + Promote flow', status: 'planned' },
    ],
  },
]

// ---------------------------------------------------------------------------
// Bootstrap sequence (cold start → full stack)
// ---------------------------------------------------------------------------

export type StepStatus = 'verified' | 'partial' | 'planned'

export type BootstrapStep = {
  seq: number
  layer: BootstrapLayerId
  action: string
  prerequisite: string | null
  verify: string
  status: StepStatus
}

export const BOOTSTRAP_SEQUENCE: BootstrapStep[] = [
  {
    seq: 1,
    layer: 'L0',
    action: 'K3s cluster init + join agent nodes',
    prerequisite: null,
    verify: 'kubectl get nodes — all Ready',
    status: 'verified',
  },
  {
    seq: 2,
    layer: 'L0',
    action: 'Install Tekton Pipelines + Triggers + Argo CD + internal registry + Gitea',
    prerequisite: 'K3s cluster running',
    verify: 'Tekton pipelines + triggers controllers running; Argo argocd-server accessible; registry:5000 push test',
    status: 'verified',
  },
  {
    seq: 3,
    layer: 'L0',
    action: 'Create cicd namespace, RBAC (tekton-deliver SA), NFS provisioners',
    prerequisite: 'L0 infra installed',
    verify: 'kubectl get ns cicd; SA token mounts; PVC provisioning works',
    status: 'verified',
  },
  {
    seq: 4,
    layer: 'L1',
    action: 'Deploy platform-api + platform-console via kubectl apply -k (first time bootstrap)',
    prerequisite: 'L0 complete; kubeconfig secret + platform config created',
    verify: 'curl http://<node>:30878/health → ok; Console at :30879 loads',
    status: 'verified',
  },
  {
    seq: 5,
    layer: 'L1',
    action: 'Register Argo Application bifrost-platform-stg (selfHeal + autoPrune)',
    prerequisite: 'Platform pods running',
    verify: 'Argo UI shows bifrost-platform-stg Synced+Healthy',
    status: 'verified',
  },
  {
    seq: 6,
    layer: 'L1',
    action: 'Register Tekton pipeline bifrost-deliver-platform (future self-updates flow)',
    prerequisite: 'Argo managing platform',
    verify: 'Console → Delivery → Pipelines shows bifrost-deliver-platform',
    status: 'verified',
  },
  {
    seq: 7,
    layer: 'L2',
    action: 'Deploy trade stack via deliver-stg / deliver-prod pipelines',
    prerequisite: 'L1 platform operational (Console can trigger pipelines)',
    verify: 'Console → Delivery → STG smoke all green; Argo bifrost-stg Synced',
    status: 'partial',
  },
]

// ---------------------------------------------------------------------------
// CI/CD rules per layer
// ---------------------------------------------------------------------------

export type RuleStatus = 'active' | 'planned'

export type CicdLayerRule = {
  layer: BootstrapLayerId
  dimension: string
  rule: string
  status: RuleStatus
}

export const CICD_LAYER_RULES: CicdLayerRule[] = [
  { layer: 'L0', dimension: 'Trigger', rule: 'Manual only — Owner runs install/upgrade scripts', status: 'active' },
  { layer: 'L0', dimension: 'Testing', rule: 'Pre-upgrade checklist (node drain, etcd backup); no automated test gate', status: 'active' },
  { layer: 'L0', dimension: 'Build', rule: 'Helm chart / raw YAML apply; no Kaniko image build', status: 'active' },
  { layer: 'L0', dimension: 'Deploy', rule: 'kubectl apply / helm upgrade; no Argo sync (Argo IS the target)', status: 'active' },
  { layer: 'L0', dimension: 'Rollback', rule: 'helm rollback / kubectl revert; manual only', status: 'active' },

  { layer: 'L1', dimension: 'Trigger', rule: 'Tekton Trigger on Gitea push (EventListener bifrost-ci, platform-ci trigger); also Console manual run', status: 'active' },
  { layer: 'L1', dimension: 'Testing', rule: 'CI gate pipeline bifrost-ci-platform: go test + tsc type-check + check-spine; auto on push, manual via Console', status: 'active' },
  { layer: 'L1', dimension: 'Build', rule: 'Kaniko → internal registry (amd64 CI node); conservative single-replica rollout', status: 'active' },
  { layer: 'L1', dimension: 'Deploy', rule: 'Pipeline rollout task (kubectl rollout restart) + Argo selfHeal', status: 'active' },
  { layer: 'L1', dimension: 'Rollback', rule: 'Argo rollback via Console; escape: kubectl apply -k overlay or local make start', status: 'active' },
  { layer: 'L1', dimension: 'Escape', rule: 'MANDATORY — local "make start" + kubectl overlay always functional; tested quarterly', status: 'planned' },

  { layer: 'L2', dimension: 'Trigger', rule: 'Tekton Trigger on Gitea push (EventListener bifrost-ci); also Console manual run', status: 'active' },
  { layer: 'L2', dimension: 'Testing', rule: 'CI gate pipelines: bifrost-ci-python (ruff + pytest) + bifrost-ci-frontend (lint + build); auto on push, manual via Console', status: 'active' },
  { layer: 'L2', dimension: 'Build', rule: 'Kaniko multi-image (9 APIs + FE + worker + socket); amd64 CI node pinning', status: 'active' },
  { layer: 'L2', dimension: 'Deploy', rule: 'Pipeline rollout → verify-stg → Argo sync; smoke probe gate before promote', status: 'active' },
  { layer: 'L2', dimension: 'Rollback', rule: 'Argo rollback via Console; re-run pipeline with prior revision', status: 'active' },
  { layer: 'L2', dimension: 'Progressive', rule: 'Canary / blue-green (future — after prod stable)', status: 'planned' },
]

// ---------------------------------------------------------------------------
// Differences: Platform vs Trade CI/CD
// ---------------------------------------------------------------------------

export type PlatformTradeContrastRow = {
  dimension: string
  platform: string
  trade: string
  reason: string
}

export const PLATFORM_TRADE_CONTRAST: PlatformTradeContrastRow[] = [
  {
    dimension: 'Bootstrap dependency',
    platform: 'Self-referential — runs on the cluster it manages',
    trade: 'No self-reference — pure workload',
    reason: 'Bootstrap paradox: platform cannot fully automate its own deployment',
  },
  {
    dimension: 'Failure impact',
    platform: 'Loss of Console/API — cluster still runs, but no single-pane ops',
    trade: 'Business interruption — but ops capability unaffected',
    reason: 'Different blast radius → different rollout conservatism',
  },
  {
    dimension: 'Rollout strategy',
    platform: 'Conservative: Recreate or 1-replica rolling; never canary',
    trade: 'Progressive: rolling → canary (future); safe to experiment',
    reason: 'Control plane must be deterministic; workload can tolerate partial rollout',
  },
  {
    dimension: 'Escape hatch',
    platform: 'Required: local make start + kubectl overlay (2 independent paths)',
    trade: 'Not needed — platform itself IS the escape path',
    reason: 'L1 must survive independently of the cluster it governs',
  },
  {
    dimension: 'Dev inner loop',
    platform: 'Local process (go run + vite dev) — intentionally NOT in cluster',
    trade: 'bifrost-dev namespace in K3s (:30882) via deliver pipeline',
    reason: 'Platform dev must work even when cluster is down (cold start development)',
  },
  {
    dimension: 'RBAC scope',
    platform: 'Cluster-wide: reads all namespaces, actuates workloads across envs',
    trade: 'Namespace-scoped: each env (stg/prod/dev) isolated',
    reason: 'Control plane needs cross-namespace visibility by design',
  },
  {
    dimension: 'Config source',
    platform: 'Self-describing catalogs (TS) + spine (YAML) — code IS governance',
    trade: 'config.yaml + .env + Vite env vars — standard app config',
    reason: 'Platform changes simultaneously alter behavior AND governance description',
  },
]

// ---------------------------------------------------------------------------
// Current gaps (P6 backlog reference)
// ---------------------------------------------------------------------------

export type CicdGap = {
  id: string
  layer: BootstrapLayerId
  gap: string
  target: string
  spineTask: string
}

export const CICD_GAPS: CicdGap[] = [
  {
    id: 'ci-trigger-trade',
    layer: 'L2',
    gap: 'Implemented — EventListener bifrost-ci + bifrost-ci-python + bifrost-ci-frontend pipelines',
    target: 'Gitea push webhook → CEL filter (python repos / frontend) → CI PipelineRun; main branch only',
    spineTask: 'p6-ci-gate-trade',
  },
  {
    id: 'ci-trigger-platform',
    layer: 'L1',
    gap: 'Implemented — bifrost-ci-platform pipeline + EventListener platform-ci trigger',
    target: 'Gitea push webhook → CEL filter (bifrost-platform) → CI PipelineRun: go test + tsc type-check + check-spine',
    spineTask: 'p6-ci-gate-platform',
  },
  {
    id: 'deliver-prod-pipeline',
    layer: 'L2',
    gap: 'Implemented — bifrost-deliver-prod pipeline with STG preflight gate + prod verify + Argo sync',
    target: 'STG preflight smoke → build prod images → rollout bifrost-prod → HTTP verify → Argo sync bifrost-prod',
    spineTask: 'p6-deliver-prod',
  },
  {
    id: 'platform-prod',
    layer: 'L1',
    gap: 'Implemented — bifrost-platform-prod overlay + HA ×2 replicas + prod NodePorts (30876/30877) + deliver-platform-prod pipeline with STG preflight',
    target: 'bifrost-platform-prod namespace + >=2 replicas + minimal kubeconfig',
    spineTask: 'p6-platform-prod',
  },
  {
    id: 'self-health',
    layer: 'L1',
    gap: 'Implemented — /api/v1/self-health probes platform-api + console + Argo sync (STG + PROD); SelfHealthPanel on CI/CD Bootstrap page',
    target: 'Console self-health panel: API liveness, Console reachability, Argo sync status',
    spineTask: 'p6-self-health',
  },
  {
    id: 'gate-spine',
    layer: 'L2',
    gap: 'Implemented — RunReleaseGate writes back to spine YAML via yaml.Node; gate history persisted to _history.json; GET /api/v1/promote/gate-history; Promote page shows spine write-back status + chronological gate run log',
    target: 'Gate result writes back to spine via API; Console Promote page shows history',
    spineTask: 'p6-gate-spine-closure',
  },
  {
    id: 'platform-e2e-cicd',
    layer: 'L1',
    gap: 'Implemented — Platform Release page consolidates end-to-end CI/CD: self-health → STG deliver → PROD deliver → independent release gates (platform-stg / platform-prod) → gate history; fully decoupled from Trade CI/CD',
    target: 'Single Platform Release page with deliver actuation (STG + PROD), release gates, gate history, and self-health — complete end-to-end CI/CD for Platform',
    spineTask: 'p6-platform-e2e-cicd',
  },
  {
    id: 'escape-runbook',
    layer: 'L0',
    gap: 'Escape hatch exists (make start, kubectl overlay) but not formalized or visible',
    target: 'Documented runbook; Console shows escape route status; quarterly test schedule',
    spineTask: 'p6-escape-hatch',
  },
]

// ---------------------------------------------------------------------------
// LLM pack
// ---------------------------------------------------------------------------

export function buildCicdBootstrapLlmPack(): string {
  const lines: string[] = [
    '# Bifrost Ops — CI/CD Bootstrap Model (L0 / L1 / L2)',
    `# Source: ${CICD_BOOTSTRAP_SOURCE} v${CICD_BOOTSTRAP_VERSION}`,
    '',
    '## Core principle',
    '',
    'The Ops Platform runs on the K3s cluster it governs (self-hosting).',
    'To resolve the bootstrap paradox, the stack is split into three layers',
    'with strictly different CI/CD rules and recovery paths.',
    '',
    '## Layer definitions',
    '',
    ...BOOTSTRAP_LAYERS.map(l => [
      `### ${l.id} — ${l.label}`,
      `- Scope: ${l.scope}`,
      `- Ownership: ${l.ownership}`,
      `- CI/CD rule: ${l.cicdRule}`,
      `- Recovery: ${l.recoveryPath}`,
      `- Components: ${l.components.map(c => `${c.name} [${c.status}]`).join('; ')}`,
      '',
    ]).flat(),
    '## Bootstrap sequence (cold start)',
    '',
    ...BOOTSTRAP_SEQUENCE.map(
      s => `${s.seq}. [${s.layer}] ${s.action}${s.prerequisite ? ` (after: ${s.prerequisite})` : ''} — verify: ${s.verify}`,
    ),
    '',
    '## CI/CD rules per layer',
    '',
    ...(['L0', 'L1', 'L2'] as BootstrapLayerId[]).map(layer => {
      const rules = CICD_LAYER_RULES.filter(r => r.layer === layer)
      return [
        `### ${layer}`,
        ...rules.map(r => `- **${r.dimension}**: ${r.rule}`),
        '',
      ]
    }).flat(),
    '## Platform vs Trade CI/CD (key differences)',
    '',
    ...PLATFORM_TRADE_CONTRAST.map(
      c => `- **${c.dimension}**: Platform=[${c.platform}] | Trade=[${c.trade}] — ${c.reason}`,
    ),
    '',
    '## Current gaps (P6 tasks)',
    '',
    ...CICD_GAPS.map(
      g => `- [${g.layer}] **${g.id}** (${g.spineTask}): ${g.gap} → ${g.target}`,
    ),
  ]
  return lines.join('\n')
}
