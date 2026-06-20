import type {
  ClusterGovernanceResponse,
  ClusterServiceReadinessResponse,
  ClusterMetricsResponse,
  ClusterNamespace,
  ClusterNode,
  ClusterObservabilityResponse,
  ClusterPlacementResponse,
  ClusterSummary,
  ClusterWorkload,
} from '@/api/types'
import { clusterHealthHint } from '@/lib/cluster/clusterHealthHint'

export interface ClusterLlmContextInput {
  summary?: ClusterSummary
  nodes?: ClusterNode[]
  governance?: ClusterGovernanceResponse
  serviceReadiness?: ClusterServiceReadinessResponse
  metrics?: ClusterMetricsResponse
  namespaces?: ClusterNamespace[]
  placement?: ClusterPlacementResponse
  observability?: ClusterObservabilityResponse
  selectedNamespace?: string | null
  workloads?: ClusterWorkload[]
}

function section(title: string, lines: string[]): string[] {
  return ['', `## ${title}`, '', ...lines]
}

function tableRow(cells: string[]): string {
  return `| ${cells.join(' | ')} |`
}

export function buildClusterLlmContext(input: ClusterLlmContextInput): string {
  const { summary, nodes, governance, serviceReadiness, metrics, namespaces, placement, observability, selectedNamespace, workloads } =
    input

  const lines: string[] = [
    'Mode: Ops',
    '',
    '## K3s cluster snapshot',
    'Source: Ops Console → Operate → Cluster (platform-api GET /api/v1/cluster/*)',
  ]

  if (summary?.generated_at != null && summary.generated_at !== '') {
    lines.push(`Probe time: ${summary.generated_at}`)
  }

  if (summary != null) {
    lines.push(
      ...section('Summary', [
        `- cluster: ${summary.label} (${summary.cluster_id})`,
        `- distribution: ${summary.distribution}`,
        `- api_server: ${summary.api_server}`,
        `- server_version: ${summary.server_version ?? 'unknown'}`,
        `- health: ${summary.reachability}${clusterHealthHint(summary) != null ? ` (${clusterHealthHint(summary)})` : ''}`,
        `- api_reachability: ${summary.api_reachability ?? 'ok'}`,
        `- core_nodes: ${summary.nodes_ready}/${summary.nodes_total} Ready`,
        ...(summary.elastic_standby != null && summary.elastic_standby > 0
          ? [`- elastic_standby: ${summary.elastic_standby}`]
          : []),
        ...(summary.elastic_degraded != null && summary.elastic_degraded > 0
          ? [`- elastic_degraded: ${summary.elastic_degraded}`]
          : []),
        `- pods: ${summary.running_pods} running, ${summary.failing_pods} failing, ${summary.pending_pods} pending`,
        ...(summary.cpu_allocatable != null ? [`- cpu_allocatable: ${summary.cpu_allocatable}`] : []),
        ...(summary.memory_allocatable != null
          ? [`- memory_allocatable: ${summary.memory_allocatable}`]
          : []),
        ...(summary.detail !== '' ? [`- detail: ${summary.detail}`] : []),
      ]),
    )

    const pods = summary.failing_pod_details ?? []
    if (pods.length > 0) {
      const podLines = [
        tableRow(['Namespace', 'Pod', 'Phase', 'Reason', 'Node', 'Age']),
        tableRow(['---', '---', '---', '---', '---', '---']),
        ...pods.map(p =>
          tableRow([
            p.namespace,
            p.name,
            p.phase,
            p.reason,
            p.node ?? '—',
            p.age ?? '—',
          ]),
        ),
      ]
      lines.push(...section('Failing pods', podLines))
    }
  } else {
    lines.push('', '(Cluster summary not loaded)')
  }

  if (metrics != null) {
    const metricsLines = [
      `- metrics_server: ${metrics.metrics_server_available ? 'available' : 'unavailable'}`,
      ...(metrics.metrics_server_detail != null && metrics.metrics_server_detail !== ''
        ? [`- metrics_server_detail: ${metrics.metrics_server_detail}`]
        : []),
    ]
    if (metrics.metrics_server_available) {
      if (metrics.cpu_usage_percent != null) {
        metricsLines.push(`- cluster_cpu_usage: ${metrics.cpu_usage_percent}%`)
      }
      if (metrics.memory_usage_percent != null) {
        metricsLines.push(`- cluster_memory_usage: ${metrics.memory_usage_percent}%`)
      }
    }
    if (metrics.top_pods.length > 0) {
      metricsLines.push('', 'Top pods by usage:')
      metricsLines.push(tableRow(['Namespace', 'Pod', 'CPU', 'Memory']))
      metricsLines.push(tableRow(['---', '---', '---', '---']))
      for (const pod of metrics.top_pods) {
        metricsLines.push(tableRow([pod.namespace, pod.name, pod.cpu, pod.memory]))
      }
    }
    lines.push(...section('Metrics (Layer A · metrics-server)', metricsLines))
  }

  if (nodes != null && nodes.length > 0) {
    const nodeLines = [
      tableRow([
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
      tableRow(['---', '---', '---', '---', '---', '---', '---', '---', '---', '---', '---']),
      ...nodes.map(n =>
        tableRow([
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
    ]
    lines.push(...section(`Nodes (${nodes.length})`, nodeLines))
  }

  if (governance != null) {
    const govLines = [
      `- reachability: ${governance.reachability}`,
      ...(governance.detail !== '' ? [`- detail: ${governance.detail}`] : []),
    ]
    if (governance.cluster_capabilities.length > 0) {
      govLines.push('', 'Cluster capabilities:')
      govLines.push(tableRow(['ID', 'Status', 'Reach', 'Detail']))
      govLines.push(tableRow(['---', '---', '---', '---']))
      for (const c of governance.cluster_capabilities) {
        govLines.push(tableRow([c.id, c.status, c.reachability, c.detail]))
      }
    }
    if (governance.node_coverage.length > 0) {
      govLines.push('', 'Node coverage:')
      govLines.push(tableRow(['ID', 'Ready', 'Nodes', 'Reach']))
      govLines.push(tableRow(['---', '---', '---', '---']))
      for (const c of governance.node_coverage) {
        govLines.push(
          tableRow([
            c.id,
            `${c.nodes_ready}/${c.nodes_total}`,
            c.node_names.join(', ') || '—',
            c.reachability,
          ]),
        )
      }
    }
    lines.push(...section('Governance · Capabilities', govLines))
  }

  if (serviceReadiness != null && serviceReadiness.domains.length > 0) {
    const srLines = [
      `- reachability: ${serviceReadiness.reachability}`,
      ...(serviceReadiness.detail !== '' ? [`- detail: ${serviceReadiness.detail}`] : []),
      '',
      tableRow(['Domain', 'Status', 'Summary']),
      tableRow(['---', '---', '---']),
      ...serviceReadiness.domains.map(d => tableRow([d.label, d.status, d.summary])),
    ]
    lines.push(...section('Service readiness', srLines))
  }

  if (namespaces != null && namespaces.length > 0) {
    const nsLines = [
      tableRow(['Namespace', 'Status', 'Pods', 'Running', 'Failing']),
      tableRow(['---', '---', '---', '---', '---']),
      ...namespaces.map(ns =>
        tableRow([
          ns.name,
          ns.status,
          String(ns.pod_count),
          String(ns.running_pods),
          String(ns.failing_pods),
        ]),
      ),
    ]
    lines.push(...section(`Namespaces (${namespaces.length})`, nsLines))
  }

  if (
    selectedNamespace != null &&
    selectedNamespace !== '' &&
    workloads != null &&
    workloads.length > 0
  ) {
    const wlLines = [
      `Selected namespace: ${selectedNamespace}`,
      '',
      tableRow(['Kind', 'Name', 'Ready', 'Status', 'Restarts', 'Age']),
      tableRow(['---', '---', '---', '---', '---', '---']),
      ...workloads.map(w =>
        tableRow([w.kind, w.name, w.ready, w.status, String(w.restarts), w.age]),
      ),
    ]
    lines.push(...section(`Workloads in ${selectedNamespace}`, wlLines))
  }

  if (placement != null) {
    const placementLines = [
      `- reachability: ${placement.reachability}`,
      ...(placement.detail !== '' ? [`- detail: ${placement.detail}`] : []),
    ]
    if (placement.pools.length > 0) {
      placementLines.push('', 'Node pools:')
      for (const pool of placement.pools) {
        placementLines.push(
          `- ${pool.id}: ${pool.label} · status=${pool.status} · ready=${pool.nodes_ready}/${pool.nodes_total}${pool.arch != null ? ` · arch=${pool.arch}` : ''}${pool.node_names.length > 0 ? ` · nodes=[${pool.node_names.join(', ')}]` : ''}`,
        )
      }
    }
    const unsatisfied = placement.rules.filter(r => !r.satisfied)
    if (unsatisfied.length > 0) {
      placementLines.push('', 'Unsatisfied placement rules:')
      for (const rule of unsatisfied) {
        placementLines.push(
          `- ${rule.workload_class} · ns ${rule.namespace}: ${rule.required_selector} → pool ${rule.pool_id}${rule.gap_reason != null ? ` (${rule.gap_reason})` : ''}`,
        )
      }
    }
    if (placement.violations.length > 0) {
      placementLines.push('', 'Violations:')
      for (const v of placement.violations) {
        placementLines.push(`- [${v.severity}] ${v.code}: ${v.message}`)
      }
    }
    lines.push(...section('Placement (live)', placementLines))
  }

  if (observability != null) {
    const obsLines = [
      `- layer_b_status: ${observability.layer_b_status}`,
      `- reachability: ${observability.reachability}`,
      ...(observability.detail !== '' ? [`- detail: ${observability.detail}`] : []),
    ]
    if (observability.components.length > 0) {
      obsLines.push('', 'Components:')
      for (const c of observability.components) {
        obsLines.push(`- ${c.label} (${c.kind}/${c.name}): ${c.ready} · ${c.status}`)
      }
    }
    lines.push(...section('Observability (Layer B)', obsLines))
  }

  lines.push(
    ...section('Agent guidance', [
      '- Mode Ops: cluster/infrastructure work only — do not edit bifrost-trade-frontend pages unless cross-linked.',
      '- Prefer Ops Console actuation (platform-api) over ad-hoc kubectl for mutations.',
      '- bifrost-trader-engine/ is read-only reference — never edit.',
      '- For placement/CI scheduling details also see Architecture → Placement (Copy LLM pack).',
      '- For hardware/env topology also see Architecture → Environments (Copy for LLM).',
      '',
      'When diagnosing issues:',
      '1. Start from failing pods and placement violations above.',
      '2. Correlate pod node assignment with node pool / arch requirements.',
      '3. Suggest read-only kubectl describe/logs/events before proposing remediation.',
    ]),
  )

  return lines.join('\n')
}
