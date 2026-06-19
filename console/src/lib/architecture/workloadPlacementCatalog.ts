/**
 * Workload placement governance — Ops Console catalog + LLM packs.
 * Live evaluation: GET /api/v1/cluster/placement
 */

import { NAMESPACE_ALLOCATION, type NamespaceRow } from '@/lib/architecture/k3sArchitectureCatalog'

export const PLACEMENT_CATALOG_VERSION = '2026-06-19-gpu-server'
export const PLACEMENT_CATALOG_SOURCE = 'console/src/lib/architecture/workloadPlacementCatalog.ts'

export type NodePoolId = 'amd64_ci' | 'amd64_general' | 'arm64_edge' | 'gpu' | 'compute_warehouse'

export type WorkloadClass =
  | 'cicd_build'
  | 'cicd_control'
  | 'stg_runtime'
  | 'data'
  | 'monitoring'
  | 'ai'
  | 'warehouse'
  | 'frontend_edge'

export type NodePoolDef = {
  id: NodePoolId
  label: string
  arch?: string
  workloadLabel?: string
  status: 'live' | 'planned'
  plannedHost?: string
}

export const NODE_POOLS: NodePoolDef[] = [
  { id: 'amd64_ci', label: 'amd64 CI / Kaniko', arch: 'amd64', status: 'live' },
  { id: 'amd64_general', label: 'amd64 general runtime', arch: 'amd64', status: 'live' },
  { id: 'arm64_edge', label: 'arm64 edge / frontend', arch: 'arm64', status: 'live' },
  {
    id: 'compute_warehouse',
    label: '4090 compute · data warehouse · solution',
    workloadLabel: 'warehouse',
    status: 'live',
    plannedHost: 'gpu-server @ 192.168.10.60',
  },
  {
    id: 'gpu',
    label: 'GPU / AI inference · heavy CI',
    workloadLabel: 'gpu',
    status: 'live',
    plannedHost: 'gpu-server @ 192.168.10.60',
  },
]

export type PlacementRuleDef = {
  workloadClass: WorkloadClass
  namespace: string
  services: string
  requiredSelector: string
  poolId: NodePoolId
  plannedBinding: string
}

export const PLACEMENT_RULES: PlacementRuleDef[] = [
  {
    workloadClass: 'cicd_build',
    namespace: 'cicd',
    services: 'Tekton Kaniko build tasks',
    requiredSelector: 'kubernetes.io/arch=amd64',
    poolId: 'amd64_ci',
    plannedBinding: 'mini-pc-a / ubt-k3s-01 control-plane',
  },
  {
    workloadClass: 'cicd_control',
    namespace: 'cicd',
    services: 'Gitea · ArgoCD · Registry',
    requiredSelector: 'kubernetes.io/arch=amd64',
    poolId: 'amd64_general',
    plannedBinding: 'mini-pc-a',
  },
  {
    workloadClass: 'stg_runtime',
    namespace: 'bifrost-stg',
    services: '9 APIs · worker · socket · frontend',
    requiredSelector: 'kubernetes.io/arch=amd64',
    poolId: 'amd64_general',
    plannedBinding: 'ubt-k3s-01 bootstrap',
  },
  {
    workloadClass: 'data',
    namespace: 'data',
    services: 'PostgreSQL · Redis · MinIO',
    requiredSelector: 'node-role=postgres (planned)',
    poolId: 'amd64_general',
    plannedBinding: 'mini-pc-b / mini-pc-a',
  },
  {
    workloadClass: 'monitoring',
    namespace: 'monitoring',
    services: 'Prometheus · Loki · Grafana',
    requiredSelector: 'kubernetes.io/arch=amd64',
    poolId: 'amd64_general',
    plannedBinding: 'mini-pc-c (second batch)',
  },
  {
    workloadClass: 'warehouse',
    namespace: 'data',
    services: 'MinIO · ClickHouse · OLAP · warehouse sync · local analytics',
    requiredSelector: 'node-role=warehouse',
    poolId: 'compute_warehouse',
    plannedBinding: 'gpu-server @ 192.168.10.60',
  },
  {
    workloadClass: 'ai',
    namespace: 'ai',
    services: 'Ollama · Open-WebUI',
    requiredSelector: 'workload=gpu',
    poolId: 'gpu',
    plannedBinding: 'gpu-server @ 192.168.10.60',
  },
  {
    workloadClass: 'frontend_edge',
    namespace: 'bifrost',
    services: 'trade-frontend (edge)',
    requiredSelector: 'kubernetes.io/arch=arm64 (optional)',
    poolId: 'arm64_edge',
    plannedBinding: 'ops-vm-ubt-01',
  },
]

export { NAMESPACE_ALLOCATION, type NamespaceRow }

export const AMD64_CI_TASK_RUN_TEMPLATE = {
  nodeSelector: { 'kubernetes.io/arch': 'amd64' },
  tolerations: [
    {
      key: 'node-role.kubernetes.io/control-plane',
      operator: 'Exists',
      effect: 'NoSchedule',
    },
  ],
} as const

export const KANIKO_PIPELINE_NAMES = [
  'bifrost-deliver-stg',
  'bifrost-build-stg',
  'bifrost-build-frontend-stg',
] as const

export function buildPlacementLlmPack(liveSummary?: {
  reachability?: string
  detail?: string
  violations?: { severity: string; message: string }[]
}): string {
  const lines = [
    'Mode: Ops',
    '',
    '## Workload placement governance',
    `Catalog: ${PLACEMENT_CATALOG_SOURCE} v${PLACEMENT_CATALOG_VERSION}`,
    '',
    '## Node pools',
    ...NODE_POOLS.map(
      p =>
        `- ${p.id}: ${p.label}${p.arch != null ? ` (arch=${p.arch})` : ''}${p.workloadLabel != null ? ` (workload=${p.workloadLabel})` : ''} [${p.status}]${p.plannedHost != null ? ` → planned ${p.plannedHost}` : ''}`,
    ),
    '',
    '## Placement rules',
    ...PLACEMENT_RULES.map(
      r =>
        `- ${r.workloadClass} · ns ${r.namespace}: ${r.requiredSelector} · pool ${r.poolId} · target ${r.plannedBinding}`,
    ),
    '',
    '## CI scheduling contract',
    'Tekton PipelineRuns with Kaniko must use taskRunTemplate:',
    '  nodeSelector.kubernetes.io/arch=amd64',
    '  tolerations: control-plane NoSchedule',
    '',
  ]
  if (liveSummary != null) {
    lines.push('## Live cluster (GET /api/v1/cluster/placement)')
    if (liveSummary.reachability != null) lines.push(`- reachability: ${liveSummary.reachability}`)
    if (liveSummary.detail != null) lines.push(`- detail: ${liveSummary.detail}`)
    if (liveSummary.violations != null && liveSummary.violations.length > 0) {
      lines.push('- violations:')
      for (const v of liveSummary.violations) {
        lines.push(`  - [${v.severity}] ${v.message}`)
      }
    }
    lines.push('')
  }
  return lines.join('\n')
}
