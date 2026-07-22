/**
 * Workload placement governance — Ops Console catalog + LLM packs.
 * Product framing: fleet facility constraints (Rocket CI, Satellite STG, shared infra),
 * hosted under Rocket — not satellite-only planning.
 * Live evaluation SSOT: api/internal/placement/evaluate.go via GET /api/v1/cluster/placement.
 * This file mirrors that contract for Cluster namespace strip + offline LLM packs.
 */

import { NAMESPACE_ALLOCATION, type NamespaceRow } from '@/lib/architecture/k3sArchitectureCatalog'
import type { ClusterPlacementPool, ClusterPlacementRule } from '@/api/clusterTypes'

export const PLACEMENT_CATALOG_VERSION = '2026-07-21-sync-go-evaluator'
export const PLACEMENT_CATALOG_SOURCE = 'console/src/lib/architecture/workloadPlacementCatalog.ts'

/** Pool ids aligned with api/internal/placement/evaluate.go poolDefs. */
export type NodePoolId = 'amd64_ci' | 'amd64_general' | 'arm64_edge' | 'nfs_client' | 'gpu'

export type WorkloadClass =
  | 'cicd_build'
  | 'cicd_control'
  | 'stg_runtime'
  | 'data'
  | 'nfs_storage'
  | 'monitoring'
  | 'ai'
  | 'frontend_edge'

export type NodePoolDef = {
  id: NodePoolId
  label: string
  arch?: string
  workloadLabel?: string
  /** Capability id mirror (e.g. nfs-client) — optional metadata for LLM pack. */
  capabilityId?: string
  status: 'live' | 'planned'
  plannedHost?: string
}

export const NODE_POOLS: NodePoolDef[] = [
  { id: 'amd64_ci', label: 'amd64 CI / Kaniko', arch: 'amd64', status: 'live' },
  { id: 'amd64_general', label: 'amd64 general runtime', arch: 'amd64', status: 'live' },
  { id: 'arm64_edge', label: 'arm64 edge / frontend', arch: 'arm64', status: 'live' },
  {
    id: 'nfs_client',
    label: 'NFS PV clients',
    capabilityId: 'nfs-client',
    status: 'live',
  },
  {
    id: 'gpu',
    label: 'GPU workloads',
    workloadLabel: 'gpu',
    status: 'planned',
    plannedHost: 'gpu-server',
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

/** Rules aligned with api/internal/placement/evaluate.go ruleDefs. */
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
    workloadClass: 'nfs_storage',
    namespace: 'kube-system · bifrost-*',
    services: 'nfs-subdir-provisioner · NFS PVC mounts',
    requiredSelector: 'storage.nfs/client=true',
    poolId: 'nfs_client',
    plannedBinding: 'ubt-k3s-01/02/04',
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
    workloadClass: 'ai',
    namespace: 'ai',
    services: 'Ollama · Open-WebUI',
    requiredSelector: 'workload=gpu',
    poolId: 'gpu',
    plannedBinding: 'gpu-server',
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
  'bifrost-deliver-platform',
  'bifrost-build-stg',
  'bifrost-build-frontend-stg',
] as const

export function buildPlacementLlmPack(liveSummary?: {
  reachability?: string
  detail?: string
  violations?: { severity: string; message: string }[]
  pools?: ClusterPlacementPool[]
  rules?: ClusterPlacementRule[]
}): string {
  const lines = [
    'Mode: Ops',
    '',
    '## Fleet facility constraints (Placement)',
    `Catalog: ${PLACEMENT_CATALOG_SOURCE} v${PLACEMENT_CATALOG_VERSION}`,
    'Scope: Rocket CI, Satellite STG runtime, and shared infra — not satellite-only planning.',
    'Live SSOT: api/internal/placement/evaluate.go via GET /api/v1/cluster/placement',
    '',
    '## Node pools',
  ]

  if (liveSummary?.pools != null && liveSummary.pools.length > 0) {
    for (const p of liveSummary.pools) {
      const arch = p.arch != null && p.arch !== '' ? ` (arch=${p.arch})` : ''
      const wl =
        p.workload_label != null && p.workload_label !== '' ? ` (workload=${p.workload_label})` : ''
      const planned =
        p.planned_host != null && p.planned_host !== '' ? ` → planned ${p.planned_host}` : ''
      lines.push(
        `- ${p.id}: ${p.label}${arch}${wl} [${p.status}] ready ${p.nodes_ready}/${p.nodes_total}${planned}`,
      )
    }
  } else {
    for (const p of NODE_POOLS) {
      lines.push(
        `- ${p.id}: ${p.label}${p.arch != null ? ` (arch=${p.arch})` : ''}${p.workloadLabel != null ? ` (workload=${p.workloadLabel})` : ''}${p.capabilityId != null ? ` (capability=${p.capabilityId})` : ''} [${p.status}]${p.plannedHost != null ? ` → planned ${p.plannedHost}` : ''}`,
      )
    }
  }

  lines.push('', '## Placement rules')

  if (liveSummary?.rules != null && liveSummary.rules.length > 0) {
    for (const r of liveSummary.rules) {
      const gap = r.gap_reason != null && r.gap_reason !== '' ? ` · gap: ${r.gap_reason}` : ''
      const binding =
        r.planned_binding != null && r.planned_binding !== '' ? ` · target ${r.planned_binding}` : ''
      lines.push(
        `- ${r.workload_class} · ns ${r.namespace}: ${r.required_selector} · pool ${r.pool_id}${binding} · ${r.satisfied ? 'OK' : 'Gap'}${gap}`,
      )
    }
  } else {
    for (const r of PLACEMENT_RULES) {
      lines.push(
        `- ${r.workloadClass} · ns ${r.namespace}: ${r.requiredSelector} · pool ${r.poolId} · target ${r.plannedBinding}`,
      )
    }
  }

  lines.push(
    '',
    '## CI scheduling contract',
    'Tekton PipelineRuns with Kaniko must use taskRunTemplate:',
    '  nodeSelector.kubernetes.io/arch=amd64',
    '  tolerations: control-plane NoSchedule',
    '',
  )

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
