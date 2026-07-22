/**
 * Grafana dashboard catalog for Observability hub.
 * Unavailable dashboards are marked — never emit broken links.
 */

import type { GrafanaDashboardEntry } from './types'

export const GRAFANA_DASHBOARD_CATALOG: GrafanaDashboardEntry[] = [
  {
    id: 'platform-overview',
    title: 'Platform Overview',
    domain: 'rocket',
    env: 'all',
    purpose: 'Layer B / Prometheus / Grafana stack health',
    uid: 'bifrost-platform-overview',
    slug: 'bifrost-platform-overview',
  },
  {
    id: 'cluster-nodes',
    title: 'Cluster / Nodes',
    domain: 'rocket',
    env: 'all',
    purpose: 'Node CPU/memory, kubelet, control-plane evidence',
    uid: 'bifrost-cluster-nodes',
    slug: 'bifrost-cluster-nodes',
  },
  {
    id: 'data-layer',
    title: 'Data Layer',
    domain: 'ground-systems',
    env: 'shared',
    purpose: 'redis-ib + CNPG shared datastore golden signals',
    uid: 'bifrost-data-layer',
    slug: 'bifrost-data-layer',
  },
  {
    id: 'satellite-trade-overview',
    title: 'Satellite Trade Overview',
    domain: 'satellite',
    env: 'all',
    purpose: 'Trade API golden signals per namespace',
    // Known deployed dashboard (Satellite Telemetry already links this uid).
    uid: 'bifrost-trade-overview',
    slug: 'bifrost-trade-overview',
  },
  {
    id: 'ib-gateway',
    title: 'IB Gateway',
    domain: 'subcontractors',
    env: 'shared',
    purpose: 'Platform IB Gateway plugin diagnostics',
    uid: 'bifrost-ib-gateway',
    slug: 'bifrost-ib-gateway',
  },
  {
    id: 'agent-operations',
    title: 'Agent Operations',
    domain: 'engineer',
    env: 'all',
    purpose: 'Remediation runner / agent bridge operations',
    uid: null,
    slug: 'bifrost-agent-operations',
  },
]

const BY_ID = Object.fromEntries(GRAFANA_DASHBOARD_CATALOG.map(d => [d.id, d])) as Record<
  string,
  GrafanaDashboardEntry
>

export function getDashboard(id: string): GrafanaDashboardEntry | undefined {
  return BY_ID[id]
}
