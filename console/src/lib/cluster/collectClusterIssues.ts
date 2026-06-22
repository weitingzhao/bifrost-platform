import type {
  ClusterPostgresStatusResponse,
  ClusterServiceReadinessResponse,
  ClusterSummary,
  Reachability,
} from '@/api/types'

export type ClusterIssueCategory = 'pods' | 'nodes' | 'elastic' | 'data'

export interface ClusterIssueRow {
  id: string
  category: ClusterIssueCategory
  severity: 'fail' | 'degraded'
  title: string
  detail: string
}

export interface CollectClusterIssuesInput {
  summary: ClusterSummary
  serviceReadiness?: ClusterServiceReadinessResponse
  postgresStatus?: ClusterPostgresStatusResponse
}

function collectInfrastructureIssues(summary: ClusterSummary): ClusterIssueRow[] {
  const issues: ClusterIssueRow[] = []

  if (summary.failing_pods > 0) {
    const pods = summary.failing_pod_details ?? []
    const reasons = [...new Set(pods.map(p => p.reason))]
    issues.push({
      id: 'failing-pods',
      category: 'pods',
      severity: 'fail',
      title: `${summary.failing_pods} failing pod${summary.failing_pods === 1 ? '' : 's'}`,
      detail: reasons.length > 0 ? reasons.join(', ') : 'CrashLoopBackOff or Failed phase',
    })
  }

  if (summary.nodes_total > 0 && summary.nodes_ready < summary.nodes_total) {
    issues.push({
      id: 'core-nodes',
      category: 'nodes',
      severity: summary.nodes_ready === 0 ? 'fail' : 'degraded',
      title: `${summary.nodes_ready}/${summary.nodes_total} core nodes ready`,
      detail: 'One or more core (non-elastic) nodes are NotReady',
    })
  }

  const elasticDegraded = summary.elastic_degraded ?? 0
  if (elasticDegraded > 0) {
    issues.push({
      id: 'elastic-degraded',
      category: 'elastic',
      severity: 'degraded',
      title: `${elasticDegraded} elastic node${elasticDegraded === 1 ? '' : 's'} degraded`,
      detail: 'On-demand node needed but not Ready — pending compute workloads or host online but K3s agent down',
    })
  }

  return issues
}

function failingDependencyDetail(deps: { label: string; detail?: string; reachability: Reachability }[]): string {
  const failing = deps.filter(d => d.reachability !== 'ok')
  if (failing.length === 0) return ''
  return failing
    .map(d => (d.detail != null && d.detail !== '' ? `${d.label}: ${d.detail}` : d.label))
    .join(' · ')
}

function collectServiceDomainIssues(
  readiness: ClusterServiceReadinessResponse,
  skipDomainIds: Set<string>,
): ClusterIssueRow[] {
  const issues: ClusterIssueRow[] = []

  for (const domain of readiness.domains) {
    if (skipDomainIds.has(domain.id)) continue
    const isHealthy = domain.reachability === 'ok' && domain.status === 'ready'
    if (isHealthy) continue

    const severity: ClusterIssueRow['severity'] = domain.reachability === 'fail' ? 'fail' : 'degraded'
    const depDetail = failingDependencyDetail(
      (domain.dependencies ?? []).filter(
        d => !(d.id === 'pool-arm64_edge' && d.reachability === 'ok'),
      ),
    )
    const detail =
      depDetail !== ''
        ? depDetail
        : domain.summary !== ''
          ? domain.summary
          : `${domain.status} — see category detail below`

    issues.push({
      id: `domain-${domain.id}`,
      category: 'data',
      severity,
      title: `${domain.label} (${domain.status})`,
      detail,
    })
  }

  return issues
}

function collectPostgresIssues(pg: ClusterPostgresStatusResponse): ClusterIssueRow[] {
  if (pg.reachability === 'ok') return []

  const issues: ClusterIssueRow[] = []

  if (pg.instances_spec > 0 && pg.instances_ready < pg.instances_spec) {
    issues.push({
      id: 'postgres-ha',
      category: 'data',
      severity: 'degraded',
      title: `PostgreSQL HA: ${pg.instances_ready}/${pg.instances_spec} instances ready`,
      detail: pg.summary,
    })
  }

  if (pg.minio.reachability !== 'ok') {
    issues.push({
      id: 'postgres-minio',
      category: 'data',
      severity: pg.minio.reachability === 'fail' ? 'fail' : 'degraded',
      title: 'MinIO backup target not ready',
      detail: pg.minio.detail ?? 'data/minio deployment not ready',
    })
  }

  if (pg.backup.reachability !== 'ok') {
    issues.push({
      id: 'postgres-backup',
      category: 'data',
      severity: pg.backup.reachability === 'fail' ? 'fail' : 'degraded',
      title: 'PostgreSQL WAL backup path incomplete',
      detail: pg.backup.detail ?? 'barmanObjectStore / ScheduledBackup blocked',
    })
  }

  if (issues.length === 0) {
    issues.push({
      id: 'postgres-status',
      category: 'data',
      severity: pg.reachability === 'fail' ? 'fail' : 'degraded',
      title: pg.summary,
      detail: pg.cnpg_cluster.detail ?? pg.operator.detail ?? '',
    })
  }

  return issues
}

export function collectClusterIssues(input: CollectClusterIssuesInput): ClusterIssueRow[] {
  const infra = collectInfrastructureIssues(input.summary)
  const skipDomains = new Set<string>()
  if (input.postgresStatus != null && input.postgresStatus.reachability !== 'ok') {
    skipDomains.add('database')
  }

  const dataFromReadiness =
    input.serviceReadiness != null
      ? collectServiceDomainIssues(input.serviceReadiness, skipDomains)
      : []

  const postgresIssues =
    input.postgresStatus != null ? collectPostgresIssues(input.postgresStatus) : []

  return [...infra, ...dataFromReadiness, ...postgresIssues]
}

export function clusterIssuesReachability(issues: ClusterIssueRow[]): Reachability {
  if (issues.length === 0) return 'ok'
  if (issues.some(i => i.severity === 'fail')) return 'fail'
  return 'degraded'
}
