import type { OpsContextResponse, ServiceDomain, ClusterPostgresStatusResponse, ClusterRedisStatusResponse } from '@/api/types'
import {
  PG_DEPLOY_PRINCIPLES,
  REDIS_DEPLOY_PRINCIPLES,
  DATA_LAYER_SESSION_CONSTRAINTS,
  formatDataLayerBriefingAppendix,
} from '@/lib/architecture/dataLayerCatalog'
import type { ClusterCategory } from '@/lib/cluster/clusterCategories'
import {
  APPLICATION_DOMAIN_PURPOSE,
  applicationDomainPurpose,
  isApplicationDomainCategory,
  isInfrastructureCategory,
  INFRASTRUCTURE_CATEGORY_LABELS,
} from '@/lib/cluster/clusterCategories'
import { clusterHealthHint } from '@/lib/cluster/clusterHealthHint'
import type { ClusterLlmContextInput } from '@/lib/cluster/buildClusterLlmContext'
import {
  clusterProbeHeaderLines,
  gapAnalysisGuidance,
  llmSection,
  llmTableRow,
} from '@/lib/cluster/clusterLlmContextHelpers'

export interface ClusterCategoryLlmInput extends ClusterLlmContextInput {
  category: ClusterCategory
  categoryTitle?: string
  opsContext?: OpsContextResponse
  postgresStatus?: ClusterPostgresStatusResponse
  redisStatus?: ClusterRedisStatusResponse
}

function resolveCategoryLabel(category: ClusterCategory, title?: string, domain?: ServiceDomain): string {
  if (title != null && title !== '') return title
  if (isInfrastructureCategory(category)) return INFRASTRUCTURE_CATEGORY_LABELS[category]
  if (domain != null) return domain.label
  return category
}

function buildApplicationDomainSection(domain: ServiceDomain): string[] {
  const purpose =
    applicationDomainPurpose(domain.id) ??
    (isApplicationDomainCategory(domain.id) ? APPLICATION_DOMAIN_PURPOSE[domain.id] : undefined)

  const lines = [
    `- domain_id: ${domain.id}`,
    `- label: ${domain.label}`,
    `- status: ${domain.status}`,
    `- reachability: ${domain.reachability}`,
    `- summary: ${domain.summary || '—'}`,
    ...(purpose != null ? [`- planned_purpose: ${purpose}`] : []),
    '',
    'Dependencies:',
    llmTableRow(['ID', 'Label', 'Reach', 'Detail']),
    llmTableRow(['---', '---', '---', '---']),
    ...domain.dependencies.map(dep =>
      llmTableRow([dep.id, dep.label, dep.reachability, dep.detail ?? '—']),
    ),
  ]

  const failing = domain.dependencies.filter(d => d.reachability !== 'ok')
  if (failing.length > 0) {
    lines.push('', `Gaps: ${failing.length}/${domain.dependencies.length} dependencies not ok`)
    for (const dep of failing) {
      lines.push(`- [${dep.reachability}] ${dep.label}: ${dep.detail ?? '—'}`)
    }
  } else if (domain.dependencies.length > 0) {
    lines.push('', `All ${domain.dependencies.length} dependencies ok`)
  }

  return lines
}

function buildPostgresLiveSection(pg: ClusterPostgresStatusResponse): string[] {
  const lines = [
    `- reachability: ${pg.reachability}`,
    `- summary: ${pg.summary}`,
    `- migration: ${pg.migration_step}/${pg.migration_total} (${pg.migration_phase})`,
    `- instances: ${pg.instances_ready}/${pg.instances_spec} ready`,
    `- rw: ${pg.rw_service}`,
    `- ro: ${pg.ro_service}`,
    `- pgdata: ${pg.storage_size} ${pg.storage_class}`,
    `- primary: ${pg.primary_pod ?? '—'} @ ${pg.primary_node ?? '—'}`,
    '',
    'Instances:',
    llmTableRow(['Pod', 'Role', 'Node', 'Phase', 'Reach']),
    llmTableRow(['---', '---', '---', '---', '---']),
    ...pg.instances.map(i => llmTableRow([i.pod_name, i.role, i.node, i.phase, i.reachability])),
    '',
    'Stack:',
    `- operator: ${pg.operator.reachability} — ${pg.operator.detail ?? '—'}`,
    `- cnpg_cluster: ${pg.cnpg_cluster.reachability} — ${pg.cnpg_cluster.detail ?? '—'}`,
    `- minio: ${pg.minio.reachability} — ${pg.minio.detail ?? '—'}`,
    `- backup: ${pg.backup.reachability} — ${pg.backup.detail ?? '—'}`,
    '',
    'Cutover:',
    ...pg.embedded.map(e => `- embedded ${e.namespace ?? '?'}: ${e.reachability} — ${e.detail ?? '—'}`),
    ...pg.legacy.map(l => `- ${l.kind} ${l.host ?? ''}: ${l.reachability} — ${l.detail ?? '—'}`),
  ]
  return llmSection('Live · CloudNativePG (data namespace)', lines)
}

function buildRedisLiveSection(redis: ClusterRedisStatusResponse): string[] {
  const lines = [
    `- reachability: ${redis.reachability}`,
    `- summary: ${redis.summary}`,
    `- migration: ${redis.migration_step}/${redis.migration_total} (${redis.migration_phase})`,
    `- redis_target_phase: ${redis.migration_redis_step}`,
    `- targets: ${redis.targets_ready}/${redis.targets_total} ready`,
    `- embedded_active: ${redis.embedded_active}`,
    '',
    'Env endpoints (R-DV1):',
    llmTableRow(['Env', 'Live', 'Queue', 'Live reach', 'Queue reach']),
    llmTableRow(['---', '---', '---', '---', '---']),
    ...redis.env_endpoints.map(e =>
      llmTableRow([
        e.environment,
        e.live_service,
        e.queue_service,
        e.live_reachability,
        e.queue_reachability,
      ]),
    ),
    '',
    'Data NS targets:',
    llmTableRow(['Name', 'Env', 'Role', 'Service', 'Reach']),
    llmTableRow(['---', '---', '---', '---', '---']),
    ...redis.target_instances.map(t =>
      llmTableRow([t.name, t.environment, t.role, t.service, t.reachability]),
    ),
    '',
    'Stack:',
    `- minio: ${redis.minio.reachability} — ${redis.minio.detail ?? '—'}`,
    `- backup: ${redis.backup.reachability} — ${redis.backup.detail ?? '—'}`,
    '',
    'Interim embedded:',
    ...redis.embedded.map(e => `- ${e.namespace}: ${e.reachability} — ${e.detail ?? '—'}`),
    ...redis.legacy.map(l => `- ${l.kind} ${l.host ?? ''}: ${l.reachability} — ${l.detail ?? '—'}`),
  ]
  return llmSection('Live · Redis (data namespace targets + embedded interim)', lines)
}

function buildDatabaseTargetSection(ctx?: OpsContextResponse): string[] {
  return [
    ...llmSection('Target architecture (PostgreSQL · static catalog)', [
      ...PG_DEPLOY_PRINCIPLES.map(p => `- **${p.dimension}**: ${p.principle} — ${p.note}`),
    ]),
    ...llmSection('Data layer migration (spine)', [formatDataLayerBriefingAppendix(ctx)]),
    ...llmSection('Session constraints', DATA_LAYER_SESSION_CONSTRAINTS.map(c => `- ${c}`)),
  ]
}

function buildRedisTargetSection(ctx?: OpsContextResponse): string[] {
  return [
    ...llmSection('Target architecture (Redis · static catalog)', [
      ...REDIS_DEPLOY_PRINCIPLES.map(p => `- **${p.dimension}**: ${p.principle} — ${p.note}`),
    ]),
    ...llmSection('Data layer migration (spine)', [formatDataLayerBriefingAppendix(ctx)]),
  ]
}

function relatedNamespacesForDomain(domainId: string): string[] {
  switch (domainId) {
    case 'database':
    case 'redis':
      return ['data', 'bifrost-dev', 'bifrost-stg', 'bifrost-prod', 'cnpg-system']
    case 'gpu':
      return ['ai']
    case 'warehouse':
      return ['data-warehouse']
    case 'workers':
      return ['bifrost-dev', 'bifrost-stg', 'bifrost-prod']
    case 'applications':
      return ['bifrost-dev', 'bifrost-stg', 'bifrost-prod']
    case 'cicd':
      return ['cicd', 'tekton-pipelines', 'gitea']
    default:
      return []
  }
}

function buildRelatedWorkloadsSection(
  domainId: string,
  input: ClusterLlmContextInput,
): string[] {
  const hints = relatedNamespacesForDomain(domainId)
  if (input.namespaces == null || hints.length === 0) return []

  const nsRows = input.namespaces.filter(ns => hints.includes(ns.name))
  if (nsRows.length === 0) return []

  const lines = [
    llmTableRow(['Namespace', 'Status', 'Pods', 'Running', 'Failing']),
    llmTableRow(['---', '---', '---', '---', '---']),
    ...nsRows.map(ns =>
      llmTableRow([
        ns.name,
        ns.status,
        String(ns.pod_count),
        String(ns.running_pods),
        String(ns.failing_pods),
      ]),
    ),
  ]

  if (
    input.selectedNamespace != null &&
    hints.includes(input.selectedNamespace) &&
    input.workloads != null &&
    input.workloads.length > 0
  ) {
    lines.push(
      '',
      `Workloads in selected namespace (${input.selectedNamespace}):`,
      llmTableRow(['Kind', 'Name', 'Ready', 'Status', 'Restarts', 'Age']),
      llmTableRow(['---', '---', '---', '---', '---', '---']),
      ...input.workloads.map(w =>
        llmTableRow([w.kind, w.name, w.ready, w.status, String(w.restarts), w.age]),
      ),
    )
  } else {
    lines.push('', 'Tip: select a namespace under Workloads in Console to include pod-level detail in this pack.')
  }

  return llmSection('Related namespaces (live)', lines)
}

export function buildClusterCategoryLlmContext(input: ClusterCategoryLlmInput): string {
  const { category, categoryTitle, opsContext, postgresStatus, redisStatus, summary, nodes, governance, serviceReadiness, metrics, namespaces, placement, observability, selectedNamespace, workloads } =
    input

  const domain = serviceReadiness?.domains.find(d => d.id === category)
  const label = resolveCategoryLabel(category, categoryTitle, domain)
  const generatedAt = summary?.generated_at ?? serviceReadiness?.generated_at ?? ''

  const lines: string[] = [
    ...clusterProbeHeaderLines(summary?.label, summary?.cluster_id, generatedAt),
    `- category: ${category}`,
    `- focus: ${label}`,
  ]

  if (isApplicationDomainCategory(category) || domain != null) {
    if (domain == null) {
      lines.push('', `(Domain "${category}" not found in service readiness probe)`)
    } else {
      lines.push(...llmSection(`Live · ${label}`, buildApplicationDomainSection(domain)))
      if (category === 'database' && postgresStatus != null) {
        lines.push(...buildPostgresLiveSection(postgresStatus))
      }
      if (category === 'redis' && redisStatus != null) {
        lines.push(...buildRedisLiveSection(redisStatus))
      }
      lines.push(...buildRelatedWorkloadsSection(category, input))
      if (category === 'database') {
        lines.push(...buildDatabaseTargetSection(opsContext))
      } else if (category === 'redis') {
        lines.push(...buildRedisTargetSection(opsContext))
        lines.push(...llmSection('Session constraints', DATA_LAYER_SESSION_CONSTRAINTS.map(c => `- ${c}`)))
      }
    }
  } else if (isInfrastructureCategory(category)) {
    switch (category) {
      case 'nodes': {
        if (summary != null) {
          lines.push(
            ...llmSection('Summary', [
              `- core_nodes: ${summary.nodes_ready}/${summary.nodes_total} Ready`,
              ...(summary.elastic_standby != null && summary.elastic_standby > 0
                ? [`- elastic_standby: ${summary.elastic_standby}`]
                : []),
              ...(summary.elastic_degraded != null && summary.elastic_degraded > 0
                ? [`- elastic_degraded: ${summary.elastic_degraded}`]
                : []),
              `- health: ${summary.reachability}${clusterHealthHint(summary) != null ? ` (${clusterHealthHint(summary)})` : ''}`,
            ]),
          )
        }
        if (nodes != null && nodes.length > 0) {
          lines.push(
            ...llmSection(`Nodes (${nodes.length})`, [
              llmTableRow([
                'Name',
                'Status',
                'Arch',
                'Roles',
                'Workload',
                'Capabilities',
                'CPU',
                'Mem',
                'Storage',
                'IP',
                'Elastic',
              ]),
              llmTableRow(['---', '---', '---', '---', '---', '---', '---', '---', '---', '---', '---']),
              ...nodes.map(n =>
                llmTableRow([
                  n.name,
                  n.unschedulable === true ? `${n.status} (cordoned)` : n.status,
                  n.architecture ?? '—',
                  n.roles,
                  n.workload_label ?? '—',
                  n.capabilities?.map(c => c.id).join(', ') || '—',
                  n.cpu_allocatable ?? '—',
                  n.memory_allocatable ?? '—',
                  n.storage_allocatable ?? '—',
                  n.internal_ip,
                  n.elastic_mode ?? (n.compute_managed === true ? 'compute' : '—'),
                ]),
              ),
            ]),
          )
        }
        if (placement != null && placement.pools.length > 0) {
          lines.push(
            ...llmSection('Node pools (placement)', [
              ...placement.pools.map(
                p =>
                  `- ${p.id}: ${p.label} · status=${p.status} · ready=${p.nodes_ready}/${p.nodes_total}${p.arch != null ? ` · arch=${p.arch}` : ''}${p.node_names.length > 0 ? ` · nodes=[${p.node_names.join(', ')}]` : ''}`,
              ),
            ]),
          )
        }
        break
      }
      case 'workloads': {
        if (summary != null) {
          lines.push(
            ...llmSection('Summary', [
              `- pods: ${summary.running_pods} running, ${summary.failing_pods} failing, ${summary.pending_pods} pending`,
            ]),
          )
          const failing = summary.failing_pod_details ?? []
          if (failing.length > 0) {
            lines.push(
              ...llmSection('Failing pods', [
                llmTableRow(['Namespace', 'Pod', 'Phase', 'Reason', 'Node', 'Age']),
                llmTableRow(['---', '---', '---', '---', '---', '---']),
                ...failing.map(p =>
                  llmTableRow([
                    p.namespace,
                    p.name,
                    p.phase,
                    p.reason,
                    p.node ?? '—',
                    p.age ?? '—',
                  ]),
                ),
              ]),
            )
          }
        }
        if (namespaces != null && namespaces.length > 0) {
          lines.push(
            ...llmSection(`Namespaces (${namespaces.length})`, [
              llmTableRow(['Namespace', 'Status', 'Pods', 'Running', 'Failing']),
              llmTableRow(['---', '---', '---', '---', '---']),
              ...namespaces.map(ns =>
                llmTableRow([
                  ns.name,
                  ns.status,
                  String(ns.pod_count),
                  String(ns.running_pods),
                  String(ns.failing_pods),
                ]),
              ),
            ]),
          )
        }
        if (
          selectedNamespace != null &&
          selectedNamespace !== '' &&
          workloads != null &&
          workloads.length > 0
        ) {
          lines.push(
            ...llmSection(`Workloads in ${selectedNamespace}`, [
              llmTableRow(['Kind', 'Name', 'Ready', 'Status', 'Restarts', 'Age']),
              llmTableRow(['---', '---', '---', '---', '---', '---']),
              ...workloads.map(w =>
                llmTableRow([w.kind, w.name, w.ready, w.status, String(w.restarts), w.age]),
              ),
            ]),
          )
        }
        break
      }
      case 'governance': {
        if (governance != null) {
          const govLines = [
            `- reachability: ${governance.reachability}`,
            ...(governance.detail !== '' ? [`- detail: ${governance.detail}`] : []),
          ]
          if (governance.cluster_capabilities.length > 0) {
            govLines.push('', 'Cluster capabilities:')
            govLines.push(llmTableRow(['ID', 'Label', 'Status', 'Reach', 'Detail']))
            govLines.push(llmTableRow(['---', '---', '---', '---', '---']))
            for (const c of governance.cluster_capabilities) {
              govLines.push(llmTableRow([c.id, c.label, c.status, c.reachability, c.detail]))
            }
            const capGaps = governance.cluster_capabilities.filter(c => c.reachability !== 'ok')
            if (capGaps.length > 0) {
              govLines.push('', `Capability gaps: ${capGaps.length}`)
              for (const c of capGaps) {
                govLines.push(`- [${c.reachability}] ${c.label} (${c.id}): ${c.detail}`)
              }
            }
          }
          if (governance.node_coverage.length > 0) {
            govLines.push('', 'Node capability coverage:')
            govLines.push(llmTableRow(['ID', 'Label', 'Ready', 'Nodes', 'Reach', 'Gap']))
            govLines.push(llmTableRow(['---', '---', '---', '---', '---', '---']))
            for (const c of governance.node_coverage) {
              govLines.push(
                llmTableRow([
                  c.id,
                  c.label,
                  `${c.nodes_ready}/${c.nodes_total}`,
                  c.node_names.join(', ') || '—',
                  c.reachability,
                  c.gap_reason ?? '—',
                ]),
              )
            }
            const covGaps = governance.node_coverage.filter(c => c.reachability !== 'ok')
            if (covGaps.length > 0) {
              govLines.push('', `Coverage gaps: ${covGaps.length}`)
              for (const c of covGaps) {
                govLines.push(`- [${c.reachability}] ${c.label}: ${c.gap_reason ?? '—'}`)
              }
            }
          }
          lines.push(...llmSection('Governance · Capabilities', govLines))
        } else {
          lines.push('', '(Governance probe not loaded)')
        }
        break
      }
      case 'observability': {
        if (metrics != null) {
          lines.push(
            ...llmSection('Metrics (Layer A)', [
              `- metrics_server: ${metrics.metrics_server_available ? 'available' : 'unavailable'}`,
              ...(metrics.metrics_server_detail != null && metrics.metrics_server_detail !== ''
                ? [`- detail: ${metrics.metrics_server_detail}`]
                : []),
              ...(metrics.cpu_usage_percent != null
                ? [`- cluster_cpu_usage: ${metrics.cpu_usage_percent}%`]
                : []),
              ...(metrics.memory_usage_percent != null
                ? [`- cluster_memory_usage: ${metrics.memory_usage_percent}%`]
                : []),
            ]),
          )
        }
        if (observability != null) {
          const obsLines = [
            `- layer_b_status: ${observability.layer_b_status}`,
            `- reachability: ${observability.reachability}`,
            ...(observability.detail !== '' ? [`- detail: ${observability.detail}`] : []),
          ]
          if (observability.components.length > 0) {
            obsLines.push('', 'Components:')
            obsLines.push(llmTableRow(['Label', 'Kind', 'Name', 'Ready', 'Status', 'Reach']))
            obsLines.push(llmTableRow(['---', '---', '---', '---', '---', '---']))
            for (const c of observability.components) {
              obsLines.push(
                llmTableRow([c.label, c.kind, c.name, c.ready, c.status, c.reachability]),
              )
            }
          }
          lines.push(...llmSection('Observability (Layer B)', obsLines))
        } else {
          lines.push('', '(Observability probe not loaded)')
        }
        break
      }
    }
  } else if (domain != null) {
    lines.push(...llmSection(`Live · ${label}`, buildApplicationDomainSection(domain)))
  } else {
    lines.push('', `(Unknown category "${category}")`)
  }

  if (placement != null && category !== 'nodes' && category !== 'workloads') {
    const unsatisfied = placement.rules.filter(r => !r.satisfied)
    if (unsatisfied.length > 0) {
      lines.push(
        ...llmSection('Placement gaps (related)', [
          ...unsatisfied.map(
            r =>
              `- ${r.workload_class} · ns ${r.namespace}: ${r.required_selector} → pool ${r.pool_id}${r.gap_reason != null ? ` (${r.gap_reason})` : ''}`,
          ),
        ]),
      )
    }
  }

  lines.push(...gapAnalysisGuidance(label))

  return lines.join('\n')
}
