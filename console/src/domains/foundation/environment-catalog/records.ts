import type { ScopeRow, HardwareRow } from './contracts'

export const foundationScopeRows: Record<string, ScopeRow> = {
  'PG': {
    tag: 'PG',
    component: 'PostgreSQL',
    technology: 'CloudNativePG bifrost-postgres @ data NS · bifrost_dev / bifrost_stg / bifrost_prod',
    notes:
      'D2-prime: CNPG is truth. Legacy bare .80 / options_db decommissioned 2026-06-29 (box → ubt-k3s-06 @ .79). LAN DBeaver: NodePort 30432.',
  },
  'GITHUB': {
    tag: 'GITHUB',
    component: 'Source control & CI (target)',
    technology: 'Git · Gitea on K3s (internal) · Mac Mini #2 runner (near-term)',
    notes: 'Near-term: pytest · npm run build · check:legacy-css on PR. Target: Tekton Pipeline in K3s cicd namespace.',
  },
  'K3S': {
    tag: 'K3S',
    component: 'K3s cluster (target)',
    technology: 'K3s HA · Traefik Ingress · CloudNativePG · local-path PVC',
    notes:
      'Phase B GitOps: Gitea · ArgoCD · Tekton. Namespaces: data · cicd · monitoring · ai · bifrost. deployment_phase in topology.yaml.',
  },
  'OBSERVE': {
    tag: 'OBSERVE',
    component: 'Observability (target)',
    technology: 'Prometheus · Loki · Grafana · AlertManager · AIOps webhook',
    notes: 'Phase C on mini-pc-c. External watchdog cron on Mac Mini #2 (monitor the monitors).',
  },
  'AGENT': {
    tag: 'AGENT',
    component: 'Out-of-Band Operator Plane (L-1)',
    technology: 'Mac minis run the operator plane out of band and peer-watchdog; .50 relays alerts · Git Bridge (Mac Pro, Dev-only)',
    notes:
      'The engineer on the ground — recovers Ops Platform (rocket) + Trade (payload) from OUTSIDE K8s; never an in-cluster Pod. Mutual watchdog + platform-api failover. See cicdBootstrapCatalog.ts L-1 · k3sBootstrapCatalog.ts · Flywheel Vision.',
  },
}

export const foundationHardware: Record<string, HardwareRow> = {
  'mini-pc-a': {
    id: 'mini-pc-a',
    host: '192.168.10.70',
    roleCompose: 'Prod compose · Redis · nginx',
    roleK3s: 'Server ① · API · Redis · Ingress · Gitea · ArgoCD',
  },
  'ubt-k3s-06': {
    id: 'ubt-k3s-06',
    host: '192.168.10.79',
    roleCompose: '— (former bare PG .80 decommissioned 2026-06-29)',
    roleK3s: 'Outside the cluster since 2026-10-17 · Agent host (agentd for PROD and STG)',
  },
  'mini-pc-c': {
    id: 'mini-pc-c',
    host: '192.168.10.73 (ubt-k3s-01)',
    roleCompose: 'K3s bootstrap server (first node)',
    roleK3s: 'Live K3s bootstrap @ .73 · monitoring · Tekton (planned)',
  },
  'mac-mini-1': {
    id: 'mac-mini-1',
    host: '192.168.10.50 (macOS · Agent)',
    roleCompose: 'Out-of-band operator-plane · mutual watch of .52 · alert relay on .50',
    roleK3s: 'UTM Agent ops-vm-ubt-01 · frontend dev (Ready) · operator-plane stays OUTSIDE cluster',
  },
  'mac-mini-2': {
    id: 'mac-mini-2',
    host: '192.168.10.52 (macOS · Agent)',
    roleCompose: 'Out-of-band operator-plane · mutual watch of .50',
    roleK3s: 'UTM Agent ops-vm-ubt-02 · CI · external watchdog (Ready) · operator-plane stays OUTSIDE cluster',
  },
  'gpu-server': {
    id: 'gpu-server',
    host: '192.168.10.60',
    roleCompose: 'Data warehouse · Ollama dev trial — no Prod Redis',
    roleK3s: 'Agent compute · warehouse · GPU/AI · Tekton heavy (WOL eno1)',
  },
  'ubt-k3s-04': {
    id: 'ubt-k3s-04',
    host: '192.168.10.75',
    roleCompose: '—',
    roleK3s: 'Agent · CNPG primary · data-primary pool · NFS client',
  },
  'ubt-k3s-05': {
    id: 'ubt-k3s-05',
    host: '192.168.10.77',
    roleCompose: '—',
    roleK3s: 'Agent · general runtime pool · stg/dev/CI offload · NFS client',
  },
  'macbook': {
    id: 'macbook',
    host: '127.0.0.1',
    roleCompose: 'Cursor dev · platform console · local prod smoke',
    roleK3s: 'kubectl · MCP client',
  },
}
