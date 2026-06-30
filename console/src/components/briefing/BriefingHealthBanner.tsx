import { DenseTag } from '@bifrost/ui'
import type { ClusterSummary, MatrixResponse } from '@/api/types'
import { summarizeCluster } from '@/lib/cluster/clusterHealth'
import { summarizeMatrix } from '@/lib/control-room/matrixSummary'

/** HEALTH axis banner — live matrix/cluster vs operational reality (independent of SYNC). */
export function BriefingHealthBanner({
  matrices,
  clusterSummary,
  platformHealthy,
  loading,
}: {
  matrices: MatrixResponse[]
  clusterSummary: ClusterSummary | undefined
  platformHealthy: boolean | undefined
  loading: boolean
}) {
  if (loading) return null

  const cluster = summarizeCluster(clusterSummary)
  const prod = matrices.find(m => m.environment === 'prod')
  const prodSum = prod != null ? summarizeMatrix(prod) : null

  const issues: { severity: 'fail' | 'degraded'; text: string }[] = []

  if (platformHealthy === false) {
    issues.push({ severity: 'fail', text: 'Platform API unreachable' })
  }
  if (cluster.reach === 'fail') {
    issues.push({ severity: 'fail', text: `Cluster: ${cluster.label}` })
  } else if (cluster.reach === 'degraded') {
    issues.push({ severity: 'degraded', text: `Cluster: ${cluster.label}` })
  }
  if (prodSum?.worstReach === 'fail') {
    issues.push({
      severity: 'fail',
      text: `Prod matrix: ${prodSum.fail} failing target${prodSum.fail !== 1 ? 's' : ''}`,
    })
  } else if (prodSum?.worstReach === 'degraded') {
    issues.push({ severity: 'degraded', text: 'Prod matrix degraded' })
  }

  const hasFail = issues.some(i => i.severity === 'fail')
  const healthy = issues.length === 0 && platformHealthy === true

  return (
    <div
      className={`mt-3 rounded border px-3 py-2 text-[var(--text-dense-meta)] ${
        healthy
          ? 'border-[var(--success)]/40 bg-[var(--success)]/10'
          : hasFail
            ? 'border-[var(--destructive)]/50 bg-[var(--destructive)]/10'
            : 'border-[var(--warning)]/50 bg-[var(--warning)]/10'
      }`}
    >
      <div className="flex items-center gap-2">
        <DenseTag variant={healthy ? 'success' : hasFail ? 'danger' : 'warning'}>HEALTH</DenseTag>
        <span className="font-medium">
          {healthy
            ? 'Operational probes healthy (matrix + cluster)'
            : hasFail
              ? 'Operational degradation detected'
              : 'Partial degradation — verify before actuation'}
        </span>
      </div>
      {issues.length > 0 && (
        <ul className="m-0 mt-1.5 list-disc pl-5">
          {issues.map(issue => (
            <li key={issue.text}>{issue.text}</li>
          ))}
        </ul>
      )}
    </div>
  )
}
