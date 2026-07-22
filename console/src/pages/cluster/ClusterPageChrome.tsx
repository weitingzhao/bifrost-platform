import { Button } from '@bifrost/ui'
import type { AuthCapabilities } from '@/api/matrixTypes'
import { OpsFeedback } from '@/components/feedback/OpsFeedback'
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
}) {
  const actionErrorDisplay =
    actionError != null &&
    (actionError.includes('401') || actionError.includes('operator token required')
      ? 'Operator token required. Set PLATFORM_OPERATOR_TOKEN for the API and VITE_PLATFORM_OPERATOR_TOKEN for the console, then restart platform.'
      : actionError)

  return (
    <>
      <section className="page-section panel-elevated px-4 py-2">
        <div className="flex flex-wrap items-center justify-between gap-x-3 gap-y-1">
          <p className="m-0 min-w-0 flex-1 truncate text-[var(--text-dense-meta)] text-[var(--muted-foreground)]">
            <span>{clusterStatusLabel}</span>
            <span className="mx-1.5 text-[var(--muted-foreground)]/50">·</span>
            <span>
              {clusterFetching
                ? 'Refreshing…'
                : clusterUpdatedAt != null
                  ? `Updated ${clusterUpdatedAt}`
                  : '30s refresh'}
            </span>
            {clusterAuthLabel != null && (
              <>
                <span className="mx-1.5 text-[var(--muted-foreground)]/50">·</span>
                <span>{clusterAuthLabel}</span>
              </>
            )}
            {onOpenAudit != null && (
              <>
                <span className="mx-1.5 text-[var(--muted-foreground)]/50">·</span>
                <button type="button" className="focus-strip-link shrink-0" onClick={onOpenAudit}>
                  Audit
                </button>
              </>
            )}
            {ambientJobId != null && ambientJobId !== '' && onOpenAgentDesk != null && (
              <>
                <span className="mx-1.5 text-[var(--muted-foreground)]/50">·</span>
                <button
                  type="button"
                  className="focus-strip-link shrink-0"
                  onClick={() => onOpenAgentDesk(ambientJobId)}
                >
                  View agent
                </button>
              </>
            )}
          </p>
          <div className="flex shrink-0 items-center gap-2">
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
          </div>
        </div>
        {(syncError != null ||
          syncOkMessage != null ||
          actionErrorDisplay != null ||
          summaryError != null) && (
          <div className="mt-1.5 space-y-1">
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
            {syncOkMessage != null && (
              <OpsFeedback variant="success">{syncOkMessage}</OpsFeedback>
            )}
            {actionErrorDisplay != null && (
              <OpsFeedback variant="warning" title="Actuation">
                {actionErrorDisplay}
              </OpsFeedback>
            )}
          </div>
        )}
      </section>

      {showBootstrapActions && (
        <section className="page-section panel-elevated px-4 py-3">
          <div className="flex flex-wrap items-center justify-between gap-3">
            <div>
              <h2 className="m-0 text-sm font-semibold">Bootstrap shortcuts</h2>
              <p className="m-0 mt-1 text-[var(--text-dense-meta)] text-[var(--muted-foreground)]">
                One-time cluster setup — hidden once metrics-server and core Bifrost namespaces exist.
              </p>
            </div>
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
          </div>
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
        </section>
      )}
    </>
  )
}
