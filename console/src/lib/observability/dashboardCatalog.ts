/**
 * Grafana dashboard catalog for Observability hub.
 *
 * Only UIDs that exist in the live Grafana (kube-prometheus-stack + Bifrost
 * ConfigMaps) may be non-null. Unavailable entries keep uid:null — never emit
 * broken deep links.
 *
 * Live inventory (2026-08): Bifrost-authored boards — Trade Overview, Data
 * Layer, IB Gateway (K8s MVP), Platform Control Plane (platform K8s surface).
 * Rocket links reuse kube-prometheus-stack stock dashboards.
 *
 * `defaultNamespace` drives deep-link `var-namespace` so Ground/IB/Agent boards
 * are not polluted with Trade NS (bifrost-dev/stg/prod).
 */

import type { GrafanaDashboardEntry } from './types'

export const GRAFANA_DASHBOARD_CATALOG: GrafanaDashboardEntry[] = [
  {
    id: 'cluster-compute',
    title: 'Kubernetes / Compute Resources / Cluster',
    domain: 'rocket',
    env: 'all',
    purpose: 'Cluster pod/node compute (kube-prometheus-stack stock)',
    uid: 'efa86fd1d0c121a26444b636a3f509a8',
    slug: 'kubernetes-compute-resources-cluster',
  },
  {
    id: 'cluster-nodes',
    title: 'Node Exporter / Nodes',
    domain: 'rocket',
    env: 'all',
    purpose: 'Node CPU/memory evidence (kube-prometheus-stack stock)',
    uid: '7d57716318ee0dddbac5a7f451fb7753',
    slug: 'node-exporter-nodes',
  },
  {
    id: 'platform-overview',
    title: 'Prometheus / Overview',
    domain: 'rocket',
    env: 'all',
    purpose: 'Layer B Prometheus stack (kube-prometheus-stack stock)',
    uid: '9fa0d141-d019-4ad7-8bc5-42196ee308bd',
    slug: 'prometheus-overview',
  },
  {
    id: 'data-layer',
    title: 'Data Layer',
    domain: 'ground-systems',
    env: 'shared',
    purpose: 'Shared Redis + CNPG golden signals (data NS)',
    uid: 'bifrost-data-layer',
    slug: 'bifrost-data-layer',
    defaultNamespace: 'data',
  },
  {
    id: 'satellite-trade-overview',
    title: 'Satellite Trade Overview',
    domain: 'satellite',
    env: 'all',
    purpose: 'Trade API golden signals per namespace (Bifrost)',
    // Deployed via bifrost-trade-infra k8s/monitoring/grafana-dashboards-configmap.yaml
    uid: 'bifrost-trade-overview',
    slug: 'bifrost-trade-overview',
    // no defaultNamespace — builder falls back to TRADE_NS[env]
  },
  {
    id: 'ib-gateway',
    title: 'IB Gateway',
    domain: 'subcontractors',
    env: 'shared',
    purpose: 'IB Gateway K8s MVP — ready/restarts/CPU/mem @ data NS',
    uid: 'bifrost-ib-gateway',
    slug: 'bifrost-ib-gateway',
    defaultNamespace: 'data',
  },
  {
    id: 'agent-operations',
    title: 'Platform Control Plane',
    domain: 'engineer',
    env: 'all',
    purpose:
      'platform-api/console Deployment K8s health; Agent job status → Operator Dock / Agent Desk',
    uid: 'bifrost-agent-operations',
    slug: 'bifrost-agent-operations',
    defaultNamespace: 'bifrost-platform-stg',
  },
]

const BY_ID = Object.fromEntries(GRAFANA_DASHBOARD_CATALOG.map(d => [d.id, d])) as Record<
  string,
  GrafanaDashboardEntry
>

export function getDashboard(id: string): GrafanaDashboardEntry | undefined {
  return BY_ID[id]
}
