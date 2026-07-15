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
import { GENERATED_TRADE_K8S_NATIVE_WAVES } from './migrateWaves.generated'

export type { TradeK8sNativeWave } from './tradeK8sNativeCatalogTypes'
export const TRADE_K8S_NATIVE_WAVES = GENERATED_TRADE_K8S_NATIVE_WAVES

export const TRADE_K8S_NATIVE_VERSION = '2026-06-29'
export const TRADE_K8S_NATIVE_SOURCE = 'config/migrate-waves/trade-k8s-native.yaml'
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

/** W1 Traefik gateway hosts — UniFi LAN DNS → kube-vip; Traefik websecure (:443) + HTTP→HTTPS. */
export const TRADE_GATEWAY_INGRESS: TradeGatewayIngressRow[] = [
  { env: 'stg', host: 'stg.trader.bifrost.lan', nodeIp: '192.168.10.100', port: 443, legacyNodePort: '30880 (HTTP escape)' },
  { env: 'prod', host: 'trader.bifrost.lan', nodeIp: '192.168.10.100', port: 443, legacyNodePort: 'node .70 HTTP escape' },
  { env: 'dev', host: 'dev.trader.bifrost.lan', nodeIp: '192.168.10.100', port: 443, legacyNodePort: '30882 (HTTP escape)' },
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
  { area: 'Security', current: 'Redis ingress per env + IB socket LAN egress (W9)', ideal: 'LAN-only egress; env isolation dev/stg/prod', priority: 'P2' },
  { area: 'Observability', current: 'Flower Deployment :5555; ib_active_data_lines gauge in gateway logs + Redis health (W10)', ideal: 'Celery metrics; ib_active_data_lines gauge', priority: 'P2' },
]

// ---------------------------------------------------------------------------
// Migration waves (spine stream progress = done count of 12)
// ---------------------------------------------------------------------------

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
