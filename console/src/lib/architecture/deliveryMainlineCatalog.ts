/**
 * STG delivery release workflow — authoritative for Ops Console → Operate → Delivery.
 * Infra detail: bifrost-trade-infra/docs/DELIVER_STG.md
 */

export const DELIVERY_MAINLINE_VERSION = '2026-06-18'
export const DELIVERY_MAINLINE_SOURCE = 'console/src/lib/architecture/deliveryMainlineCatalog.ts'

export type DeliveryPhaseStatus = 'done' | 'active' | 'blocked' | 'planned'

export type DeliveryReleasePhase = {
  id: string
  seq: number
  title: string
  owner: string
  status: DeliveryPhaseStatus
  summary: string
  actions: string[]
}

/** End-to-end STG release (post Local Prod Final). Prod cutover is a separate track (Deploy Mainline D1). */
export const STG_RELEASE_PHASES: DeliveryReleasePhase[] = [
  {
    id: 'push-upstream',
    seq: 1,
    title: 'Push to GitHub',
    owner: 'Developer',
    status: 'done',
    summary: 'Commit bifrost-trade-{api,socket,worker,frontend,core} + bifrost-trade-infra. Gitea mirrors pull from GitHub.',
    actions: [
      'Console: Operate → Delivery → Sync mirrors',
      'Console: Operate → Delivery → Refresh Dockerfile CMs (4/4 summary)',
      'Verify Dockerfile CMs: api · frontend · worker · socket tags all green',
      'Optional: make k3s-sync-gitea-mirrors (bootstrap-gitea-mirrors.sh)',
    ],
  },
  {
    id: 'config-overlay',
    seq: 2,
    title: 'STG config & overlay',
    owner: 'Ops',
    status: 'done',
    summary: 'IB client_id 210段、Massive delayed WS、Secrets — ConfigMap bifrost-config + bifrost-stg-secrets.',
    actions: [
      'make sync-stg-config (from .env)',
      'kubectl apply -k k8s/overlays/stg',
      'kubectl apply -f k8s/base/secrets/bifrost-stg-secrets.yaml -n bifrost-stg',
    ],
  },
  {
    id: 'deliver-stg',
    seq: 3,
    title: 'bifrost-deliver-stg',
    owner: 'Tekton / Console Delivery',
    status: 'done',
    summary:
      'Pipeline: prepare (mirror-sync + Dockerfile CMs) → Kaniko (9 API + FE + worker/socket) → rollout → verify-stg → Argo sync.',
    actions: [
      'Console: Operate → Delivery (revision + Sync mirrors + Refresh Dockerfile CMs + Run deliver-stg)',
      'Delivery Operate tab → Active deliver run: 6-phase progress bar (Clone → … → GitOps)',
      'Console: Operate → Delivery → Observe tab → Pipeline runs (full history + logs)',
      'CLI: make k3s-deliver-stg',
      'Preflight: amd64 CI node required (Observe → Scheduling → Placement)',
    ],
  },
  {
    id: 'verify-stg',
    seq: 4,
    title: 'STG acceptance',
    owner: 'Ops / Agent',
    status: 'done',
    summary: 'Automated HTTP verify runs inside pipeline; Tier B (IB/Massive/Celery) signed off 2026-06-18.',
    actions: [
      'Pipeline task verify-stg (gateway + 9 APIs)',
      'Operate → Delivery → Verify STG (or Observe tab → Stg smoke)',
      'make k3s-verify-phase-b-stg-v2 (rollout + HTTP)',
      'Seed watchlist if Massive WS empty: scripts/k3s/seed-stg-watchlist.sh',
    ],
  },
  {
    id: 'stg-gate',
    seq: 5,
    title: 'STG release gate',
    owner: 'Promote (stg tier)',
    status: 'done',
    summary: 'STG deliver + smoke + STG release gate pass + Tier B sign-off — staging track SIGNED 2026-06-18.',
    actions: [
      'Promote → Run STG release gate (tier=stg)',
      'Promote → Tier B sign-off after IB/Massive manual checks',
      'Evaluate stgDeliverReady on Delivery coupling panel',
    ],
  },
  {
    id: 'prod-cutover',
    seq: 6,
    title: 'Prod cutover',
    owner: 'Deploy Mainline / D1',
    status: 'active',
    summary: 'Prod overlay + bifrost-deliver-prod + prod matrix. Milestone 2c-b-prod-cutover IN_PROGRESS (D1 SIGNED).',
    actions: [
      'Ops Console → Operate → Deploy Mainline (seq 5 prod overlay)',
      'Implement pipeline-deliver-prod + k8s/overlays/prod',
      'Promote → Prod cutover gate (deliver-prod + prod matrix)',
    ],
  },
]

export const DELIVERY_PIPELINE_CATALOG = [
  {
    name: 'bifrost-deliver-platform',
    tier: 'primary' as const,
    purpose: 'Ops Platform STG: platform-api + platform-console → bifrost-platform-stg',
    legacy: false,
  },
  {
    name: 'bifrost-deliver-platform-prod',
    tier: 'primary' as const,
    purpose: 'Ops Platform PROD: STG preflight → build :prod → rollout HA ×2 → Argo sync bifrost-platform-prod',
    legacy: false,
  },
  {
    name: 'bifrost-deliver-prod',
    tier: 'primary' as const,
    purpose: 'Prod stack deliver — STG preflight gate → build → rollout → verify-prod → Argo sync',
    legacy: false,
  },
  {
    name: 'bifrost-deliver-stg',
    tier: 'primary' as const,
    purpose: 'Full STG stack: prepare → build → rollout → verify → GitOps sync',
    legacy: false,
  },
  {
    name: 'bifrost-build-frontend-stg',
    tier: 'auxiliary' as const,
    purpose: 'Frontend-only Kaniko (S8 smoke)',
    legacy: false,
  },
  {
    name: 'bifrost-build-stg',
    tier: 'legacy' as const,
    purpose: 'Early smoke — api-monitor only; prefer bifrost-deliver-stg',
    legacy: true,
  },
  {
    name: 'bifrost-smoke',
    tier: 'auxiliary' as const,
    purpose: 'CI stack health check',
    legacy: false,
  },
  {
    name: 'bifrost-ci-python',
    tier: 'primary' as const,
    purpose: 'CI gate for Python Trade repos (core/api/worker/socket): lint + test on push',
    legacy: false,
  },
  {
    name: 'bifrost-ci-frontend',
    tier: 'primary' as const,
    purpose: 'CI gate for frontend + bifrost-ui: lint + build + legacy-css check on push',
    legacy: false,
  },
  {
    name: 'bifrost-ci-platform',
    tier: 'primary' as const,
    purpose: 'CI gate for Ops Platform (L1): go test + tsc type-check + spine catalog check on push',
    legacy: false,
  },
]

export const DELIVERY_RUNBOOK_COMMANDS = {
  deliver: 'make k3s-deliver-stg',
  mirrorSync: 'make k3s-sync-gitea-mirrors',
  syncConfig: 'make sync-stg-config',
  verify: 'make k3s-verify-phase-b-stg-v2',
  gateway: 'http://192.168.10.73:30880/',
} as const

export function buildDeliveryMainlineLlmPack(): string {
  const lines = [
    `# Delivery mainline (${DELIVERY_MAINLINE_VERSION})`,
    `Source: ${DELIVERY_MAINLINE_SOURCE}`,
    '',
    '## STG release phases',
    ...STG_RELEASE_PHASES.map(
      p => `${p.seq}. ${p.title} [${p.status}] — ${p.summary}\n   Actions: ${p.actions.join('; ')}`,
    ),
    '',
    '## Pipelines',
    ...DELIVERY_PIPELINE_CATALOG.map(p => `- ${p.name} (${p.tier}): ${p.purpose}`),
  ]
  return lines.join('\n')
}
