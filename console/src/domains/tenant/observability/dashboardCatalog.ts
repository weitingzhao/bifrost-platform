import type { GrafanaDashboardEntry } from '@/domains/foundation/observability/types'

export const TENANT_GRAFANA_DASHBOARD_CATALOG: GrafanaDashboardEntry[] = [
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
    soloPanel: { panelId: 1, title: 'Trade API request rate', height: 180 },
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
    soloPanel: { panelId: 1, title: 'Ready replicas', height: 180 },
  }
]
