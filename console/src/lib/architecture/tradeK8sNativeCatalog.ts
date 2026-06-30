/**
 * Trade stack K8s-native refactor — Compose lift-and-shift → ideal runtime.
 *
 * Authoritative for Agent Briefing → Migrate → Trade K8s-native lane.
 * Spine stream: tracks.migrate.streams trade-k8s-native
 *
 * Context: STG v2 lift-and-shift is SIGNED; this catalog covers native runtime model
 * (Ingress, Lease HA, IB Edge Gateway, Ops kubernetes executor).
 */

import type { OpsContextResponse } from '@/api/types'
import { projectWaveStatus } from '@/lib/briefing/waveProjection'

export const TRADE_K8S_NATIVE_VERSION = '2026-06-29'
export const TRADE_K8S_NATIVE_SOURCE = 'console/src/lib/architecture/tradeK8sNativeCatalog.ts'
export const TRADE_K8S_NATIVE_MIGRATE_STREAM_ID = 'trade-k8s-native'

// ---------------------------------------------------------------------------
// IB TWS constraints (authoritative for socket/daemon design)
// ---------------------------------------------------------------------------

export type IbConstraintRow = { constraint: string; limit: string; k8sImplication: string }

export const IB_TWS_CONSTRAINTS: IbConstraintRow[] = [
  {
    constraint: 'API clients per TWS/Gateway instance',
    limit: 'Max 32 simultaneous connections (unique clientId each)',
    k8sImplication: 'Never scale IB socket Deployments >1 active; use Lease not replicas',
  },
  {
    constraint: 'clientId collision',
    limit: 'Error 326 — client id already in use',
    k8sImplication: 'Standby must not connect until Active releases Lease; same ID on failover',
  },
  {
    constraint: 'Market data lines',
    limit: 'Account-level budget (~100 default); TWS UI + all API clients share',
    k8sImplication: 'Single ingestor per env; readers via Redis only — no duplicate subscriptions',
  },
  {
    constraint: 'Pacing',
    limit: '~50 msg/s global; data request rate ≈ lines/2 per second',
    k8sImplication: 'Fewer IB connections beats many thin clients',
  },
  {
    constraint: 'Order ownership',
    limit: 'Orders tied to clientId; clientId=0 special (auto-bind TWS orders)',
    k8sImplication: 'Operator gateway uses fixed prod clientId; Lease ensures one executor',
  },
]

export type ClientIdBandRow = {
  env: string
  hostRole: string
  clientId: number
  mergedFrom?: string
}

/** Target: 3 gateways × 2 TWS hosts × 2 envs (prod+stg) = 12 max active; dev uses mock (0). */
export const IB_CLIENT_ID_BANDS: ClientIdBandRow[] = [
  { env: 'prod', hostRole: 'ib-market-gateway @ Host', clientId: 50, mergedFrom: 'ingestor+listener+worker_market' },
  { env: 'prod', hostRole: 'ib-account-gateway @ Host', clientId: 60 },
  { env: 'prod', hostRole: 'ib-order-gateway @ Host', clientId: 20 },
  { env: 'prod', hostRole: 'ib-market-gateway @ Secondary', clientId: 51 },
  { env: 'prod', hostRole: 'ib-account-gateway @ Secondary', clientId: 61 },
  { env: 'prod', hostRole: 'ib-order-gateway @ Secondary', clientId: 21 },
  { env: 'stg', hostRole: 'ib-market-gateway @ Host', clientId: 250 },
  { env: 'stg', hostRole: 'ib-account-gateway @ Host', clientId: 260 },
  { env: 'stg', hostRole: 'ib-order-gateway @ Host', clientId: 220 },
  { env: 'stg', hostRole: 'ib-market-gateway @ Secondary', clientId: 251 },
  { env: 'stg', hostRole: 'ib-account-gateway @ Secondary', clientId: 261 },
  { env: 'stg', hostRole: 'ib-order-gateway @ Secondary', clientId: 221 },
  { env: 'dev', hostRole: 'mock gateway (no TWS)', clientId: 0 },
]

export type TradeGatewayIngressRow = {
  env: string
  host: string
  nodeIp: string
  port: number
  legacyNodePort: string
}

/** W1 Traefik gateway hosts — /etc/hosts or LAN DNS → nodeIp, Traefik entrypoint web (:80). */
export const TRADE_GATEWAY_INGRESS: TradeGatewayIngressRow[] = [
  { env: 'stg', host: 'trade-stg.bifrost.lan', nodeIp: '192.168.10.73', port: 80, legacyNodePort: '30880 (nginx retired)' },
  { env: 'prod', host: 'trade.bifrost.lan', nodeIp: '192.168.10.70', port: 80, legacyNodePort: '30881 (nginx retired)' },
  { env: 'dev', host: 'trade-dev.bifrost.lan', nodeIp: '192.168.10.73', port: 80, legacyNodePort: '30882 (nginx retired)' },
]

export const IB_EDGE_DESIGN_PRINCIPLES = [
  'IB socket layer = singleton accessor to external stateful resource (like CNPG primary)',
  'K8s HA = Active-Standby via coordination.k8s.io/Lease — not Deployment replicas with simultaneous eConnect',
  'Daemon/API/Celery never open IB sockets — Redis decoupling is mandatory (already in bifrost-trade-worker)',
  'DEV must not consume live client_id — ib.mode: mock + redis-replay or recorded ticks',
  'R-DV3: at most one auto-trade daemon per IB account — Daemon Lease separate from IB Lease',
]

// ---------------------------------------------------------------------------
// K8s-native gap analysis (Compose lift-and-shift vs ideal)
// ---------------------------------------------------------------------------

export type GapRow = { area: string; current: string; ideal: string; priority: 'P0' | 'P1' | 'P2' }

export const COMPOSE_ON_K8S_GAPS: GapRow[] = [
  { area: 'Ingress', current: 'Traefik IngressRoute + stripPrefix (W1); nginx retired', ideal: 'Traefik Ingress + ClusterIP; NodePort bootstrap-only', priority: 'P0' },
  { area: 'Ops control', current: 'executor_mode kubernetes + api-ops RBAC (W2); celery-worker Deployment restored', ideal: 'Typed worker profiles via per-queue Deployments (future)', priority: 'P0' },
  { area: 'IB HA', current: 'Deployment replicas:1 Recreate', ideal: 'StatefulSet + Lease; standby hot, active-only eConnect', priority: 'P0' },
  { area: 'IB client budget', current: '6 roles/env × 3 env risk = 18 IDs', ideal: '3 gateways/env; dev mock; Lease prevents double-connect', priority: 'P0' },
  { area: 'Config', current: 'prod aliases config.stg.yaml mount path', ideal: 'Per-env config keys; BIFROST_ENV consistent', priority: 'P1' },
  { area: 'Manifests', current: 'apis/manifest.yaml 673-line copy-paste', ideal: 'Kustomize component; single bifrost-api image + args', priority: 'P1' },
  { area: 'Probes', current: 'API only; socket/worker/daemon missing', ideal: 'readiness/liveness all workloads', priority: 'P1' },
  { area: 'Security', current: 'No NetworkPolicy', ideal: 'LAN-only egress; env isolation dev/stg/prod', priority: 'P2' },
  { area: 'Observability', current: 'No Flower; CNPG PodMonitor off', ideal: 'Celery metrics; ib_active_data_lines gauge', priority: 'P2' },
]

// ---------------------------------------------------------------------------
// Migration waves (spine stream progress = done count of 12)
// ---------------------------------------------------------------------------

export type TradeK8sNativeWave = {
  id: string
  wave: string
  /** D-C: position in the spine `done` count. Status is projected from spine, never held here. */
  spineIndex: number
  label: string
  repo: string
  verify: string
  blockedBy?: string
  /** Short summary of what shipped — spec text for the briefing appendix (NOT a progress field). */
  delivered?: string
}

export const TRADE_K8S_NATIVE_WAVES: TradeK8sNativeWave[] = [
  {
    id: 'w0-dev-mock',
    wave: 'W0',
    spineIndex: 0,
    label: 'Dev IB Mock Gateway — zero live TWS client_id on bifrost-dev',
    repo: 'bifrost-trade-socket + bifrost-trade-infra/k8s/overlays/dev',
    verify: 'bifrost-dev stack healthy; no eConnect to .30/.33; market pages use redis-replay',
    delivered:
      'config get_ib_mode + ib.mode:mock; MockIbGateway for ingestor/account/operator ' +
      '(no eConnect, client_id=0, health mode=mock, synthetic STK quotes); ' +
      'run_ib_* scripts branch on mock; dev overlay config.dev.yaml ib.mode:mock; ' +
      '8 socket tests green (test_ib_mock_gateway.py).',
  },
  {
    id: 'w1-ingress',
    wave: 'W1',
    spineIndex: 1,
    label: 'Traefik Ingress replaces in-cluster nginx gateway',
    repo: 'bifrost-trade-infra/k8s/base + overlays',
    verify: 'STG/PROD via Ingress host; SSE buffering off; NodePort bootstrap-only',
    blockedBy: 'data-layer-k3s step ⑦ (optional parallel on STG)',
    delivered:
      'Retired in-cluster nginx Deployment; Traefik IngressRoute + stripPrefix middlewares per API; ' +
      'SSE flushInterval on market/monitor paths; hosts trade-{stg,dev}.bifrost.lan / trade.bifrost.lan @ :80; ' +
      'smoke scripts + platform clusters.yaml ingress_base; kustomize build stg|prod|dev; cluster verify PASS.',
  },
  {
    id: 'w2-ops-k8s-executor',
    wave: 'W2',
    spineIndex: 2,
    label: 'api-ops executor_mode kubernetes — restore celery-worker Deployment',
    repo: 'bifrost-trade-api + bifrost-trade-infra/k8s/overlays',
    verify: 'Ops Celery page starts worker pod; api-ops-celery.patch replicas≠0; no subprocess',
    delivered:
      'KubernetesExecutor (scale/restart Deployments, delete celery pods); ops.executor_mode kubernetes in stg|prod|dev; ' +
      'api-ops ServiceAccount + Role (deployments patch, pods delete); celery-worker replicas restored (removed B1 patch); ' +
      '/health exposes k8s_reachable + k8s_namespace; market ingest runtime_kind kubernetes. ' +
      'STG cluster verify PASS: health executor_mode=kubernetes k8s_reachable=true; celery-worker 1/1; no subprocess; in-pod executor smoke OK.',
  },
  {
    id: 'w3-manifest-refactor',
    wave: 'W3',
    spineIndex: 3,
    label: 'Kustomize API component + single image; fix prod config mount alias',
    repo: 'bifrost-trade-infra/k8s',
    verify: 'kustomize build overlays/stg|prod; 9 domains from one image tag',
  },
  {
    id: 'w4-ib-lease-lib',
    wave: 'W4',
    spineIndex: 4,
    label: 'K8s Lease leader election module in bifrost-trade-socket',
    repo: 'bifrost-trade-socket',
    verify: 'Unit tests + STG: scale socket to 2 pods — only one holds IB connection',
  },
  {
    id: 'w5-ib-statefulset',
    wave: 'W5',
    spineIndex: 5,
    label: 'IB socket StatefulSet + ServiceAccount/RBAC for Lease',
    repo: 'bifrost-trade-infra/k8s/base/socket',
    verify: 'kubectl get sts -n bifrost-stg; failover <20s; Error 326 never in logs',
  },
  {
    id: 'w6-ib-gateway-merge',
    wave: 'W6',
    spineIndex: 6,
    label: 'Merge ingestor+listener+worker_market → ib-market-gateway (3 gateways total)',
    repo: 'bifrost-trade-socket + config',
    verify: '3 client_id per TWS per env; parity smoke on /market/live',
    blockedBy: 'w4-ib-lease-lib, w5-ib-statefulset',
  },
  {
    id: 'w7-probes-init',
    wave: 'W7',
    spineIndex: 7,
    label: 'Probes + initContainers — socket/worker wait CNPG/Redis ready',
    repo: 'bifrost-trade-infra/k8s/base',
    verify: 'rollout restart during data maintenance — no crashloop before data ready',
  },
  {
    id: 'w8-daemon-lease',
    wave: 'W8',
    spineIndex: 8,
    label: 'Daemon Deployment + Lease — single active auto-trade (R-DV3)',
    repo: 'bifrost-trade-worker + k8s/base/worker',
    verify: '2 daemon pods; only Lease holder runs FSM trading loop',
  },
  {
    id: 'w9-network-policy',
    wave: 'W9',
    spineIndex: 9,
    label: 'NetworkPolicy — env isolation + IB LAN egress allowlist',
    repo: 'bifrost-trade-infra/k8s/base',
    verify: 'bifrost-stg cannot reach bifrost-prod redis; socket reaches .30/.33:7496',
  },
  {
    id: 'w10-observability',
    wave: 'W10',
    spineIndex: 10,
    label: 'IB data-line budget ConfigMap + Celery/Flower metrics',
    repo: 'bifrost-trade-infra/k8s + bifrost-trade-worker',
    verify: 'Grafana or logs show ib_active_data_lines; Flower or celery_exporter',
  },
  {
    id: 'w11-signoff',
    wave: 'W11',
    spineIndex: 11,
    label: 'STG Tier A/B + deliver-prod gate — Compose→K3s native sign-off',
    repo: 'bifrost-platform + bifrost-trade-infra',
    verify: 'make k3s-verify-phase-b-stg-v2; deliver-prod blocked if STG unhealthy',
    blockedBy: 'w1–w8 minimum for prod cutover',
  },
]

export const TRADE_K8S_NATIVE_SESSION_CONSTRAINTS = [
  'Single-variable waves — do not merge W6 gateway merge with W1 Ingress in one PR',
  'Never scale IB socket Deployments without Lease — Error 326 breaks all envs on same TWS',
  'DEV must not use prod/stg client_id bands — W0 mock is prerequisite for parallel dev work',
  'R-DV3 auto-trade prod cutover remains Owner decision — out of scope unless explicitly requested',
  'Authority: this catalog + spine stream trade-k8s-native; manifests in bifrost-trade-infra/k8s/',
  'IB reference: TWS API Connectivity — max 32 clients/instance; clientId unique (Error 326)',
]

// ---------------------------------------------------------------------------
// Briefing appendix
// ---------------------------------------------------------------------------

export function formatTradeK8sNativeBriefingAppendix(ctx?: OpsContextResponse): string {
  const stream = ctx?.tracks?.migrate?.streams.find(s => s.id === TRADE_K8S_NATIVE_MIGRATE_STREAM_ID)

  const lines = [
    '## Trade K8s-native refactor appendix',
    '',
    `Source: ${TRADE_K8S_NATIVE_SOURCE} · spine stream \`${TRADE_K8S_NATIVE_MIGRATE_STREAM_ID}\``,
    stream != null
      ? `Spine progress: ${stream.done}/${stream.total} · status=${stream.status}${stream.next_task != null ? ` · next: ${stream.next_task}` : ''}`
      : 'Spine stream: (not loaded — use waves below)',
    '',
    '### IB TWS constraints (design north star)',
    ...IB_TWS_CONSTRAINTS.map(r => `- **${r.constraint}**: ${r.limit} → ${r.k8sImplication}`),
    '',
    '### IB Edge principles',
    ...IB_EDGE_DESIGN_PRINCIPLES.map(p => `- ${p}`),
    '',
    '### Client ID budget (target)',
    ...IB_CLIENT_ID_BANDS.map(
      r => `- **${r.env}** ${r.hostRole}: ${r.clientId}${r.mergedFrom != null ? ` (${r.mergedFrom})` : ''}`,
    ),
    '',
    '### Trade gateway Ingress (W1)',
    ...TRADE_GATEWAY_INGRESS.map(
      g =>
        `- **${g.env}**: \`http://${g.host}/\` → ${g.nodeIp}:${g.port} (Traefik web); legacy ${g.legacyNodePort}`,
    ),
    '',
    '### Compose-on-K8s gaps',
    ...COMPOSE_ON_K8S_GAPS.map(g => `- [${g.priority}] **${g.area}**: ${g.current} → ${g.ideal}`),
    '',
    '### Waves (W0–W11)',
  ]

  for (const w of TRADE_K8S_NATIVE_WAVES) {
    // Status projected from spine (D-A/D-C) — same projectWaveStatus as the lane queue.
    const projected =
      stream != null
        ? projectWaveStatus(w.spineIndex, {
            done: stream.done,
            readyForSignoff: stream.ready_for_signoff ?? 0,
            streamStatus: stream.status,
          })
        : 'pending'
    const marker =
      projected === 'next'
        ? ' *(spine next)*'
        : projected === 'ready_for_signoff'
          ? ' — ✅ DELIVERED, awaiting Owner sign-off'
          : projected === 'done'
            ? ' — ✔ signed'
            : ''
    lines.push(`${w.wave}. **${w.label}**${marker}`)
    lines.push(`   - id: ${w.id} · repo: ${w.repo}`)
    if (w.delivered) lines.push(`   - delivered: ${w.delivered}`)
    lines.push(`   - verify: ${w.verify}`)
    if (w.blockedBy) lines.push(`   - blocked_by: ${w.blockedBy}`)
    lines.push('')
  }

  lines.push('### Session constraints')
  for (const c of TRADE_K8S_NATIVE_SESSION_CONSTRAINTS) lines.push(`- ${c}`)

  return lines.join('\n')
}

export function buildTradeK8sNativeLlmPack(): string {
  return [
    '# Bifrost Trade — K8s-native refactor + IB Edge Gateway',
    `# Source: ${TRADE_K8S_NATIVE_SOURCE} v${TRADE_K8S_NATIVE_VERSION}`,
    '',
    '## IB constraints',
    ...IB_TWS_CONSTRAINTS.map(r => `- ${r.constraint}: ${r.limit}`),
    '',
    '## Waves',
    ...TRADE_K8S_NATIVE_WAVES.map(w => `${w.wave} ${w.id}: ${w.label}`),
    '',
    '## Session constraints',
    ...TRADE_K8S_NATIVE_SESSION_CONSTRAINTS.map(c => `- ${c}`),
  ].join('\n')
}
