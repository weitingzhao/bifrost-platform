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

export const OBSERVABILITY_REGISTRY_VERSION = '2026-07-21'
export const OBSERVABILITY_REGISTRY_SOURCE =
  'console/src/lib/observability/signalRegistry.ts'

/** Stale threshold for freshness (5 minutes). */
export const SIGNAL_STALE_MS = 5 * 60_000

export const TRADE_NS: Record<'dev' | 'stg' | 'prod', string> = {
  dev: 'bifrost-dev',
  stg: 'bifrost-stg',
  prod: 'bifrost-prod',
}

/**
 * Fixed Apollo seven-domain order for the hub grid.
 * Keep labels identical to systemDomainCatalog / consoleNavConfig.
 */
export const OBSERVABILITY_DOMAIN_ORDER: SystemDomainId[] = SYSTEM_DOMAINS.map(d => d.id)

export const SIGNAL_REGISTRY: SignalDef[] = [
  /* ── Rocket (Ops Platform / cluster) ── */
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

  /* ── Ground Systems (shared data plane) ── */
  {
    id: 'ground.redis-ib',
    label: 'redis-ib memory',
    domain: 'ground-systems',
    scope: 'shared',
    role: 'required',
    source: 'telemetry_metric',
    detailRoute: 'cluster',
    grafanaDashboardId: 'data-layer',
    affectsDomains: ['ground-systems', 'satellite', 'subcontractors'],
    description: 'Shared IB Redis in data NS',
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

  /* ── Satellite (Trade payload) ── */
  {
    id: 'satellite.api-request-rate',
    label: 'API request rate',
    domain: 'satellite',
    scope: 'env',
    role: 'required',
    source: 'telemetry_metric',
    detailRoute: 'satellite-telemetry',
    grafanaDashboardId: 'satellite-trade-overview',
  },
  {
    id: 'satellite.api-latency-p99',
    label: 'API latency P99',
    domain: 'satellite',
    scope: 'env',
    role: 'required',
    source: 'telemetry_metric',
    detailRoute: 'satellite-telemetry',
    grafanaDashboardId: 'satellite-trade-overview',
  },
  {
    id: 'satellite.api-error-rate',
    label: 'API 5xx error rate',
    domain: 'satellite',
    scope: 'env',
    role: 'required',
    source: 'telemetry_metric',
    detailRoute: 'satellite-telemetry',
    grafanaDashboardId: 'satellite-trade-overview',
  },
  {
    id: 'satellite.bus-health',
    label: 'Bus health (selected env)',
    domain: 'satellite',
    scope: 'env',
    role: 'required',
    source: 'bus_deep',
    detailRoute: 'satellite-bus',
    grafanaDashboardId: 'satellite-trade-overview',
  },
  {
    id: 'satellite.k8s-workloads',
    label: 'K8s workloads',
    domain: 'satellite',
    scope: 'env',
    role: 'evidence',
    source: 'matrix',
    detailRoute: 'satellite-bus',
    description: 'Workload readiness — evidence only (never affects verdict)',
  },

  /* ── Subcontractors (IB Gateway plugin) ── */
  {
    id: 'subcontractors.ib-gateway',
    label: 'Platform IB Gateway',
    domain: 'subcontractors',
    scope: 'shared',
    role: 'required',
    source: 'ib_gateway',
    detailRoute: 'plugin-gallery',
    grafanaDashboardId: 'ib-gateway',
    affectsDomains: ['subcontractors', 'satellite'],
  },

  /* ── Engineer (Agent plane) ── */
  {
    id: 'engineer.remediation-runner',
    label: 'Remediation runner',
    domain: 'engineer',
    scope: 'shared',
    role: 'required',
    source: 'remediation',
    detailRoute: 'queue',
    grafanaDashboardId: 'agent-operations',
  },
  {
    id: 'engineer.agent-bridge',
    label: 'Agent / probe bridge',
    domain: 'engineer',
    scope: 'shared',
    role: 'required',
    source: 'agent_bridge',
    optionalContract: true,
    detailRoute: 'operator-plane',
    grafanaDashboardId: 'agent-operations',
    description: 'Mac-adjacent bridge — NOT OBSERVED on prod/stg seats',
  },

  /* ── Mission Control / Governance — no invented metrics ── */
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
  },
]

const BY_ID = Object.fromEntries(SIGNAL_REGISTRY.map(s => [s.id, s])) as Record<string, SignalDef>

export function getSignalDef(id: string): SignalDef | undefined {
  return BY_ID[id]
}

export function signalsForDomain(domain: SystemDomainId): SignalDef[] {
  return SIGNAL_REGISTRY.filter(s => s.domain === domain)
}

export function requiredSignals(domain?: SystemDomainId): SignalDef[] {
  return SIGNAL_REGISTRY.filter(
    s => s.role === 'required' && (domain == null || s.domain === domain),
  )
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
  api_request_rate: 'satellite.api-request-rate',
  api_latency_p99: 'satellite.api-latency-p99',
  api_error_rate: 'satellite.api-error-rate',
  redis_memory_bytes: 'ground.redis-ib',
  redis_connected_clients: 'ground.redis-ib',
  pg_connections: 'ground.postgres',
  pg_replication_lag: 'ground.postgres',
  ib_gateway_up: 'subcontractors.ib-gateway',
}

/** Scrape job / namespace heuristics → domain (whitelist mapping, not free PromQL). */
export const TARGET_DOMAIN_HINTS: Array<{
  match: RegExp
  domain: SystemDomainId
  role: 'required' | 'evidence'
  envHint?: 'dev' | 'stg' | 'prod' | 'shared'
}> = [
  { match: /bifrost-dev|namespace="bifrost-dev"/i, domain: 'satellite', role: 'required', envHint: 'dev' },
  { match: /bifrost-stg|namespace="bifrost-stg"/i, domain: 'satellite', role: 'required', envHint: 'stg' },
  { match: /bifrost-prod|namespace="bifrost-prod"/i, domain: 'satellite', role: 'required', envHint: 'prod' },
  { match: /redis|cnpg|postgres|namespace="data"/i, domain: 'ground-systems', role: 'required', envHint: 'shared' },
  { match: /ib-gateway|ib_gateway/i, domain: 'subcontractors', role: 'required', envHint: 'shared' },
  { match: /kubelet|kube-state|node-exporter|cadvisor|apiserver|coredns/i, domain: 'rocket', role: 'evidence', envHint: 'shared' },
  { match: /prometheus|alertmanager|grafana/i, domain: 'rocket', role: 'evidence', envHint: 'shared' },
]
