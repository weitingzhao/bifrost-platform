/**
 * STG delivery release workflow — authoritative for Ops Console → Program → Delivery.
 * Infra detail: bifrost-trade-infra/docs/DELIVER_STG.md
 */

export const DELIVERY_MAINLINE_VERSION = '2026-06-19'
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
    status: 'active',
    summary: 'Commit bifrost-trade-{api,socket,worker,frontend,core} + bifrost-trade-infra. Gitea mirrors pull from GitHub.',
    actions: [
      'git push origin main (each changed repo)',
      'Optional: make k3s-sync-gitea-mirrors (Platform Makefile or bootstrap-gitea-mirrors.sh)',
    ],
  },
  {
    id: 'config-overlay',
    seq: 2,
    title: 'STG config & overlay',
    owner: 'Ops',
    status: 'active',
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
    status: 'active',
    summary:
      'Pipeline: prepare (mirror-sync + Dockerfile CMs) → Kaniko (9 API + FE + worker/socket) → rollout → verify-stg → Argo sync.',
    actions: [
      'Console: Delivery → Pipeline runs → bifrost-deliver-stg → Run',
      'CLI: make k3s-deliver-stg',
      'Preflight: amd64 CI node required (Placement page)',
    ],
  },
  {
    id: 'verify-stg',
    seq: 4,
    title: 'STG acceptance',
    owner: 'Ops / Agent',
    status: 'active',
    summary: 'Automated HTTP verify runs inside pipeline; extend with manual Tier B (IB/Massive) as needed.',
    actions: [
      'Pipeline task verify-stg (gateway + 9 APIs)',
      'Delivery → Stg smoke panel refresh',
      'make k3s-verify-phase-b-stg-v2 (rollout + HTTP)',
      'Seed watchlist if Massive WS empty: scripts/k3s/seed-stg-watchlist.sh',
    ],
  },
  {
    id: 'stg-gate',
    seq: 5,
    title: 'STG release gate',
    owner: 'Promote (stg tier)',
    status: 'active',
    summary: 'STG deliver + smoke green = staging track complete. Does not require prod matrix or D1 cutover.',
    actions: [
      'Promote → Run release gate (records stg-api-* checks)',
      'Evaluate stgDeliverReady on Delivery coupling panel',
    ],
  },
  {
    id: 'prod-cutover',
    seq: 6,
    title: 'Prod cutover',
    owner: 'Deploy Mainline / D1',
    status: 'blocked',
    summary: 'Separate track: prod matrix + milestone 2c-b-prod-cutover + Owner D1. No bifrost-deliver-prod yet.',
    actions: [
      'Ops Console → Program → Deploy Mainline',
      'Resolve D1: K3s migration path vs Compose prod host',
    ],
  },
]

export const DELIVERY_PIPELINE_CATALOG = [
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
