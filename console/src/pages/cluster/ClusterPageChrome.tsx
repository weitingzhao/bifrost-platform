import type { ReactNode } from 'react'
import { Button } from '@bifrost/ui'
import type { ClusterSummary } from '@/api/clusterTypes'
import type { AuthCapabilities, Reachability } from '@/api/matrixTypes'
import { OpsFeedback } from '@/components/feedback/OpsFeedback'
import { OpsSection } from '@/components/layout/OpsSection'
import { OpsVerdictStrip } from '@/components/layout/OpsVerdictStrip'
import { deriveClusterVerdict } from '@/lib/cluster/clusterHealth'
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
  actionSuccess,
  summaryError,
  opsReach,
  opsSummaryLine,
  ambientJobId,
  onOpenAgentDesk,
  onExpandAgentDock,
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
  healthBody,
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
  /** Optional — only for Bootstrap unreachable recovery; not a Verdict primary action. */
  onSyncKubeconfig?: () => void
  syncError: string | null
  syncOkMessage?: string
  actionError: string | null
  /** Auto-Check / actuation success body (plain or markdown). */
  actionSuccess?: string | null
  summaryError?: string | null
  /** Same Ops plane as Cluster Issues — drives unified Verdict grade. */
  opsReach?: Reachability
  opsSummaryLine?: string | null
  ambientJobId?: string | null
  onOpenAgentDesk?: (arg?: string | { prefill: string }) => void
  /** Prefer Operator Dock over Agent Desk when viewing ambient session. */
  onExpandAgentDock?: () => void
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
  /** Ranked issues + failing pods — nested in the same health panel. */
  healthBody?: ReactNode
}) {
  // Must be string | null — never boolean. (`x && msg` yields `false` when x is null,
  // and `false != null` is true → empty "Actuation" OpsFeedback banner.)
  const actionErrorDisplay: string | null =
    actionError == null
      ? null
      : actionError.includes('401') || actionError.includes('operator token required')
        ? 'Operator token required. Set PLATFORM_OPERATOR_TOKEN for the API and VITE_PLATFORM_OPERATOR_TOKEN for the console, then restart platform.'
        : actionError

  const verdict = deriveClusterVerdict({
    summary: clusterSummary,
    unreachable,
    showBootstrapActions,
    summaryFailed,
    isProbing,
    opsReach,
    opsSummaryLine,
  })

  const failingPods = clusterSummary?.failing_pods ?? 0
  const nodesReady = clusterSummary?.nodes_ready
  const nodesTotal = clusterSummary?.nodes_total
  const unified = healthBody != null

  return (
    <>
      <OpsVerdictStrip
        ariaLabel="Cluster health"
        title="CLUSTER HEALTH"
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
              <span className="font-mono-tabular text-warning">
                {failingPods} failing pod{failingPods === 1 ? '' : 's'}
              </span>
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
            {ambientJobId != null && ambientJobId !== '' ? (
              onExpandAgentDock != null ? (
                <button
                  type="button"
                  className="focus-strip-link shrink-0"
                  onClick={onExpandAgentDock}
                >
                  View agent
                </button>
              ) : onOpenAgentDesk != null ? (
                <button
                  type="button"
                  className="focus-strip-link shrink-0"
                  onClick={() => onOpenAgentDesk(ambientJobId)}
                >
                  View agent
                </button>
              ) : null
            ) : null}
          </>
        }
        body={unified ? healthBody : undefined}
      />

      {(syncError != null ||
        syncOkMessage != null ||
        actionErrorDisplay != null ||
        (actionSuccess != null && actionSuccess !== '') ||
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
          {actionSuccess != null && actionSuccess !== '' && (
            <OpsFeedback variant="success" title="AI Auto-Check / actuation">
              {actionSuccess}
            </OpsFeedback>
          )}
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
                  AI Agent can call ensure_kubeconfig_secret / sync_cluster_kubeconfig when Auto-Check
                  runs. Manual sync is only a bootstrap fallback.
                  {onSyncKubeconfig != null && (
                    <>
                      {' '}
                      <button
                        type="button"
                        className="focus-strip-link"
                        disabled={syncPending}
                        onClick={onSyncKubeconfig}
                      >
                        {syncPending ? 'Syncing…' : 'Sync kubeconfig (fallback)'}
                      </button>
                    </>
                  )}
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
