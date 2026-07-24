import { Button } from '@bifrost/ui'
import type { ClusterSummary } from '@/api/clusterTypes'
import type { AuthCapabilities } from '@/api/matrixTypes'
import { OpsFeedback } from '@/components/feedback/OpsFeedback'
import { OpsSection } from '@/components/layout/OpsSection'
import { OpsVerdictStrip } from '@/components/layout/OpsVerdictStrip'
import { deriveClusterVerdict } from '@/lib/cluster/clusterHealth'
import { scrollToSection } from '@/lib/dom/scrollToSection'
import type { CopyState } from './useClusterPageQueries'

export function ClusterPageChrome({
  clusterStatusLabel,
  clusterFetching,
  clusterUpdatedAt,
  clusterAuthLabel,
  onOpenAudit,
  copyState,
  onCopyForLlm,
  onRefresh,
  syncPending,
  onSyncKubeconfig,
  syncError,
  syncOkMessage,
  actionError,
  summaryError,
  ambientJobId,
  onOpenAgentDesk,
  showBootstrapActions,
  canOperate,
  canAdmin,
  caps,
  capsLoading,
  metricsOk,
  bifrostNsReady,
  metricsServerPending,
  ensurePending,
  onEnsureMetricsServer,
  onEnsureNamespaces,
  unreachable,
  clusterSummary,
  summaryFailed,
  isProbing,
}: {
  clusterStatusLabel: string
  clusterFetching: boolean
  clusterUpdatedAt: string | null
  clusterAuthLabel: string | null
  onOpenAudit?: () => void
  copyState: CopyState
  onCopyForLlm: () => void
  onRefresh: () => void
  syncPending: boolean
  onSyncKubeconfig: () => void
  syncError: string | null
  syncOkMessage?: string
  actionError: string | null
  summaryError?: string | null
  ambientJobId?: string | null
  onOpenAgentDesk?: (arg?: string | { prefill: string }) => void
  showBootstrapActions: boolean
  canOperate: boolean
  canAdmin: boolean
  caps: AuthCapabilities | undefined
  capsLoading: boolean
  metricsOk: boolean
  bifrostNsReady: boolean
  metricsServerPending: boolean
  ensurePending: boolean
  onEnsureMetricsServer: () => void
  onEnsureNamespaces: () => void
  unreachable: boolean
  clusterSummary: ClusterSummary | undefined
  summaryFailed: boolean
  isProbing: boolean
}) {
  const actionErrorDisplay =
    actionError != null &&
    (actionError.includes('401') || actionError.includes('operator token required')
      ? 'Operator token required. Set PLATFORM_OPERATOR_TOKEN for the API and VITE_PLATFORM_OPERATOR_TOKEN for the console, then restart platform.'
      : actionError)

  const verdict = deriveClusterVerdict({
    summary: clusterSummary,
    unreachable,
    showBootstrapActions,
    summaryFailed,
    isProbing,
  })

  const failingPods = clusterSummary?.failing_pods ?? 0
  const nodesReady = clusterSummary?.nodes_ready
  const nodesTotal = clusterSummary?.nodes_total
  const showTriageLink = verdict.tagLabel === 'DEGRADED'

  return (
    <>
      <OpsVerdictStrip
        ariaLabel="Cluster verdict"
        title="CLUSTER VERDICT"
        lamp={verdict.lamp}
        tagLabel={verdict.tagLabel}
        tagVariant={verdict.tagVariant}
        summary={verdict.summaryLine}
        actions={
          <>
            <Button size="sm" onClick={() => void onCopyForLlm()}>
              {copyState === 'copied'
                ? 'Copied!'
                : copyState === 'error'
                  ? 'Copy failed'
                  : 'Copy for LLM'}
            </Button>
            <Button variant="outline" size="sm" disabled={clusterFetching} onClick={onRefresh}>
              {clusterFetching ? 'Refreshing…' : 'Refresh'}
            </Button>
            <Button size="sm" disabled={syncPending} onClick={onSyncKubeconfig}>
              {syncPending ? 'Syncing…' : 'Sync kubeconfig'}
            </Button>
          </>
        }
        meta={
          <>
            <span>{clusterStatusLabel}</span>
            {nodesTotal != null && nodesTotal > 0 ? (
              <span>
                {nodesReady}/{nodesTotal} nodes
              </span>
            ) : null}
            {failingPods > 0 ? (
              <button
                type="button"
                className="font-mono-tabular text-warning hover:underline"
                title="Scroll to Cluster issues"
                onClick={() => scrollToSection('cluster-issues')}
              >
                {failingPods} failing pod{failingPods === 1 ? '' : 's'}
              </button>
            ) : null}
            {showTriageLink ? (
              <button
                type="button"
                className="font-mono-tabular text-warning hover:underline"
                title="Scroll to Failure triage"
                onClick={() => scrollToSection('cluster-failure-triage')}
              >
                View triage
              </button>
            ) : null}
            <span>
              {clusterFetching
                ? 'Refreshing…'
                : clusterUpdatedAt != null
                  ? `Updated ${clusterUpdatedAt}`
                  : '30s refresh'}
            </span>
            {clusterAuthLabel != null ? <span>{clusterAuthLabel}</span> : null}
            {onOpenAudit != null ? (
              <button type="button" className="focus-strip-link shrink-0" onClick={onOpenAudit}>
                Audit
              </button>
            ) : null}
            {ambientJobId != null && ambientJobId !== '' && onOpenAgentDesk != null ? (
              <button
                type="button"
                className="focus-strip-link shrink-0"
                onClick={() => onOpenAgentDesk(ambientJobId)}
              >
                View agent
              </button>
            ) : null}
          </>
        }
      />

      {(syncError != null ||
        syncOkMessage != null ||
        actionErrorDisplay != null ||
        summaryError != null) && (
        <div className="space-y-1">
          {summaryError != null && (
            <OpsFeedback variant="error" title="Cluster summary failed">
              {summaryError}
            </OpsFeedback>
          )}
          {syncError != null && (
            <OpsFeedback variant="error" title="Kubeconfig sync failed">
              {syncError}
            </OpsFeedback>
          )}
          {syncOkMessage != null && <OpsFeedback variant="success">{syncOkMessage}</OpsFeedback>}
          {actionErrorDisplay != null && (
            <OpsFeedback variant="warning" title="Actuation">
              {actionErrorDisplay}
            </OpsFeedback>
          )}
        </div>
      )}

      {showBootstrapActions && (
        <OpsSection
          title="Bootstrap"
          description="One-time cluster setup — hidden once metrics-server and core Bifrost namespaces exist."
          actions={
            <div className="flex flex-wrap items-center gap-2">
              {!metricsOk && (
                <Button
                  size="sm"
                  disabled={!canOperate || metricsServerPending}
                  title={
                    !canOperate
                      ? capsLoading
                        ? 'Checking auth…'
                        : 'Authenticate with an operator token to actuate'
                      : undefined
                  }
                  onClick={onEnsureMetricsServer}
                >
                  {metricsServerPending ? 'Installing…' : 'Ensure metrics-server'}
                </Button>
              )}
              {!bifrostNsReady && (
                <Button
                  size="sm"
                  disabled={!canOperate || ensurePending}
                  title={
                    !canOperate
                      ? capsLoading
                        ? 'Checking auth…'
                        : 'Authenticate with an operator token to actuate'
                      : canAdmin
                        ? undefined
                        : 'Operator can ensure namespaces'
                  }
                  onClick={onEnsureNamespaces}
                >
                  {ensurePending ? 'Ensuring…' : 'Ensure Bifrost namespaces'}
                </Button>
              )}
            </div>
          }
          overflow="visible"
          headerExtra={
            <>
              {unreachable && (
                <OpsFeedback variant="warning" title="Cluster API unreachable" className="mt-2">
                  Sync kubeconfig or check platform-api cluster access before bootstrap actions.
                </OpsFeedback>
              )}
              {caps != null && !canOperate && !capsLoading && (
                <p className="m-0 mt-2 text-[var(--text-dense-caption)] text-[var(--muted-foreground)]">
                  Authenticate (operator token) to run bootstrap actuations.
                </p>
              )}
            </>
          }
        />
      )}
    </>
  )
}
