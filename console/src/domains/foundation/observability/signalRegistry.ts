// Foundation composition: Tenant adapters remain explicit dependencies; this is not an isolated deployment.
import { TENANT_TARGET_HINTS } from '@/domains/tenant/observability/targetProfile'
import { TENANT_METRIC_TO_SIGNAL } from '@/domains/tenant/observability/metricProfile'
import { TENANT_SIGNAL_REGISTRY } from '@/domains/tenant/observability/signalRegistry'
const FOUNDATION_SIGNAL_REGISTRY: SignalDef[] = [
  {
    id: 'rocket.layer-b',
    label: 'Layer B (Prometheus stack)',
    domain: 'rocket',
    scope: 'shared',
    role: 'required',
    source: 'cluster_observability',
    detailRoute: 'cluster',
    grafanaDashboardId: 'platform-overview',
    affectsDomains: ['rocket', 'satellite', 'ground-systems', 'subcontractors'],
    description: 'kube-prometheus-stack readiness — shared observability fabric',
  },
  {
    id: 'rocket.prometheus-reachable',
    label: 'Prometheus reachable',
    domain: 'rocket',
    scope: 'shared',
    role: 'required',
    source: 'cluster_observability',
    detailRoute: 'cluster',
    grafanaDashboardId: 'platform-overview',
    affectsDomains: ['rocket', 'satellite'],
  },
  {
    id: 'rocket.scrape-targets',
    label: 'Scrape target health',
    domain: 'rocket',
    scope: 'shared',
    role: 'required',
    source: 'telemetry_target',
    detailRoute: 'observability',
    grafanaDashboardId: 'cluster-compute',
  },
  {
    id: 'rocket.cluster-cpu',
    label: 'Cluster CPU',
    domain: 'rocket',
    scope: 'shared',
    role: 'evidence',
    source: 'cluster_metrics',
    detailRoute: 'cluster',
    grafanaDashboardId: 'cluster-compute',
    description: 'metrics-server CPU — evidence only, not verdict',
  },
  {
    id: 'rocket.cluster-memory',
    label: 'Cluster memory',
    domain: 'rocket',
    scope: 'shared',
    role: 'evidence',
    source: 'cluster_metrics',
    detailRoute: 'cluster',
    grafanaDashboardId: 'cluster-nodes',
  },
  {
    id: 'ground.postgres',
    label: 'PostgreSQL connections',
    domain: 'ground-systems',
    scope: 'shared',
    role: 'required',
    source: 'telemetry_metric',
    detailRoute: 'cluster',
    grafanaDashboardId: 'data-layer',
    affectsDomains: ['ground-systems', 'satellite'],
  },
  {
    id: 'ground.network',
    label: 'Network (UniFi)',
    domain: 'ground-systems',
    scope: 'shared',
    role: 'required',
    source: 'network_probe',
    optionalContract: true,
    detailRoute: 'network',
    description: 'Live UniFi probe via GET /api/v1/network/sla (network-monitoring-ops)',
  },
  {
    id: 'engineer.operator-plane',
    label: 'operator-plane',
    domain: 'engineer',
    scope: 'shared',
    role: 'required',
    source: 'agent_bridge',
    detailRoute: 'queue',
    grafanaDashboardId: 'agent-operations',
  },
  {
    id: 'mission-control.hub',
    label: 'Mission Control runtime',
    domain: 'mission-control',
    scope: 'shared',
    role: 'required',
    source: 'none',
    optionalContract: true,
    detailRoute: 'control-room',
    description: 'Cross-domain hub — no dedicated PromQL contract',
  },
  {
    id: 'governance.catalog',
    label: 'Governance catalogs',
    domain: 'governance',
    scope: 'shared',
    role: 'required',
    source: 'none',
    optionalContract: true,
    detailRoute: 'blueprint',
    description: 'Reference library — NOT OBSERVED at runtime',
  }
]
/**
 * Apollo Domain signal registry — ownership + contract for Observability hub.
 *
 * Rules (Owner-approved):
 * - Only `required` signals affect overall / domain verdicts.
 * - K8s workload / CPU / memory default to `evidence`.
 * - Alerts affect verdict only after domain + severity mapping.
 * - Historical Defects never participate.
 * - Shared dependencies counted once; mark affected domains.
 * - Mission Control / Governance without reliable runtime → NOT OBSERVED (reference / by design).
 */

import type { SystemDomainId } from '@/lib/architecture/systemDomainCatalog'
import { SYSTEM_DOMAINS } from '@/lib/architecture/systemDomainCatalog'
import type { SignalDef } from './types'

/** Stale threshold for freshness (5 minutes). */
export const SIGNAL_STALE_MS = 5 * 60_000

export { TRADE_NS } from '@/domains/tenant/observability/namespace'

/**
 * Fixed Apollo seven-domain order for the hub grid.
 * Keep labels identical to systemDomainCatalog ConsoleNavPlane.
 */
export const OBSERVABILITY_DOMAIN_ORDER: SystemDomainId[] = SYSTEM_DOMAINS.map(d => d.id)

export const SIGNAL_REGISTRY: SignalDef[] = [
  FOUNDATION_SIGNAL_REGISTRY[0]!,
  FOUNDATION_SIGNAL_REGISTRY[1]!,
  FOUNDATION_SIGNAL_REGISTRY[2]!,
  FOUNDATION_SIGNAL_REGISTRY[3]!,
  FOUNDATION_SIGNAL_REGISTRY[4]!,
  TENANT_SIGNAL_REGISTRY[0]!,
  FOUNDATION_SIGNAL_REGISTRY[5]!,
  FOUNDATION_SIGNAL_REGISTRY[6]!,
  TENANT_SIGNAL_REGISTRY[1]!,
  TENANT_SIGNAL_REGISTRY[2]!,
  TENANT_SIGNAL_REGISTRY[3]!,
  TENANT_SIGNAL_REGISTRY[4]!,
  TENANT_SIGNAL_REGISTRY[5]!,
  TENANT_SIGNAL_REGISTRY[6]!,
  FOUNDATION_SIGNAL_REGISTRY[7]!,
  FOUNDATION_SIGNAL_REGISTRY[8]!,
  FOUNDATION_SIGNAL_REGISTRY[9]!
]

const BY_ID = Object.fromEntries(SIGNAL_REGISTRY.map(s => [s.id, s])) as Record<string, SignalDef>

export function getSignalDef(id: string): SignalDef | undefined {
  return BY_ID[id]
}

/**
 * Metric id → signal id for telemetry overview presets.
 *
 * Notes:
 * - `node_cpu_usage` / `node_memory_usage` Go presets are intentionally NOT
 *   mapped here: rocket.cluster-cpu/-memory verdicts come from the
 *   metrics-server `cluster_metrics` source. The presets stay available for
 *   single-query consumers (MCP / query endpoint).
 * - `ib_gateway_up` is evidence-only for subcontractors.ib-gateway: the
 *   ib_gateway API probe remains the single verdict source.
 */
export const METRIC_TO_SIGNAL: Record<string, string> = {
  api_request_rate: TENANT_METRIC_TO_SIGNAL.api_request_rate!,
  api_latency_p99: TENANT_METRIC_TO_SIGNAL.api_latency_p99!,
  api_error_rate: TENANT_METRIC_TO_SIGNAL.api_error_rate!,
  redis_memory_bytes: TENANT_METRIC_TO_SIGNAL.redis_memory_bytes!,
  redis_connected_clients: TENANT_METRIC_TO_SIGNAL.redis_connected_clients!,
  pg_connections: 'ground.postgres',
  pg_replication_lag: 'ground.postgres',
  ib_gateway_up: TENANT_METRIC_TO_SIGNAL.ib_gateway_up!,
}

/** Scrape job / namespace heuristics → domain (whitelist mapping, not free PromQL). */
export const TARGET_DOMAIN_HINTS: Array<{
  match: RegExp
  domain: SystemDomainId
  role: 'required' | 'evidence'
  envHint?: 'dev' | 'stg' | 'prod' | 'shared'
}> = [
  TENANT_TARGET_HINTS[0]!,
  TENANT_TARGET_HINTS[1]!,
  TENANT_TARGET_HINTS[2]!,
  { match: /redis|cnpg|postgres|namespace="data"/i, domain: 'ground-systems', role: 'required', envHint: 'shared' },
  TENANT_TARGET_HINTS[3]!,
  { match: /kubelet|kube-state|node-exporter|cadvisor|apiserver|coredns/i, domain: 'rocket', role: 'evidence', envHint: 'shared' },
  { match: /prometheus|alertmanager|grafana/i, domain: 'rocket', role: 'evidence', envHint: 'shared' },
]
