import type { ClusterSummary } from '@/api/types'
import { DenseTag } from '@bifrost/ui'
import { StatusLamp } from '@/components/StatusLamp'
import { apiReachability, clusterHealthHint } from '@/lib/cluster/clusterHealthHint'

interface ClusterHealthStripProps {
  summary: ClusterSummary | undefined
  isLoading: boolean
}

export function ClusterHealthStrip({ summary, isLoading }: ClusterHealthStripProps) {
  if (isLoading || !summary) {
    return (
      <section className="page-section panel-elevated px-4 py-3">
        <span className="text-[var(--muted-foreground)]">Loading cluster health…</span>
      </section>
    )
  }

  return (
    <section className="page-section panel-elevated px-4 py-3">
      <div className="flex flex-wrap items-center gap-x-4 gap-y-2 text-[var(--text-dense)]">
        <div className="flex items-center gap-2">
          <StatusLamp value={apiReachability(summary)} kind="reach" />
          <span className="font-medium">API reach</span>
          <span className="text-[var(--muted-foreground)] font-mono-tabular">
            {apiReachability(summary)}
          </span>
        </div>
        <span className="text-[var(--muted-foreground)]">·</span>
        <div className="flex items-center gap-2">
          <StatusLamp value={summary.reachability} kind="reach" />
          <span className="font-medium">Cluster health</span>
          <span className="text-[var(--muted-foreground)] font-mono-tabular">
            {summary.reachability}
          </span>
          {summary.reachability !== 'ok' && clusterHealthHint(summary) != null && (
            <span className="text-[var(--text-dense-meta)] text-[var(--muted-foreground)]">
              ({clusterHealthHint(summary)})
            </span>
          )}
        </div>
        <span className="text-[var(--muted-foreground)]">·</span>
        <span>
          Core nodes{' '}
          <code className="font-mono-tabular">
            {summary.nodes_ready}/{summary.nodes_total}
          </code>
          {(summary.elastic_standby ?? 0) > 0 && (
            <>
              {' '}
              <DenseTag variant="neutral">+{summary.elastic_standby} standby</DenseTag>
            </>
          )}
          {(summary.elastic_degraded ?? 0) > 0 && (
            <>
              {' '}
              <DenseTag variant="warning">{summary.elastic_degraded} elastic degraded</DenseTag>
            </>
          )}
        </span>
        <span className="text-[var(--muted-foreground)]">·</span>
        <span>
          Failing pods{' '}
          <code className="font-mono-tabular">{summary.failing_pods}</code>
        </span>
        <span className="text-[var(--muted-foreground)]">·</span>
        <span>
          {summary.distribution.toUpperCase()} @ {summary.api_server.replace('https://', '')}
        </span>
        {summary.server_version != null && summary.server_version !== '' && (
          <>
            <span className="text-[var(--muted-foreground)]">·</span>
            <span className="font-mono-tabular text-[var(--text-dense-meta)]">
              {summary.server_version}
            </span>
          </>
        )}
      </div>
      {summary.detail !== '' && (
        <p className="m-0 mt-2 text-[var(--text-dense-meta)] text-[var(--muted-foreground)]">
          {summary.detail}
        </p>
      )}
    </section>
  )
}
