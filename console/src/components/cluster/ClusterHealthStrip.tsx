import type { ClusterSummary } from '@/api/types'
import { StatusLamp } from '@/components/StatusLamp'

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
          <StatusLamp value={summary.reachability} kind="reach" />
          <span className="font-medium">API reach</span>
          <span className="text-[var(--muted-foreground)] font-mono-tabular">
            {summary.reachability}
          </span>
        </div>
        <span className="text-[var(--muted-foreground)]">·</span>
        <span>
          Ready nodes{' '}
          <code className="font-mono-tabular">
            {summary.nodes_ready}/{summary.nodes_total}
          </code>
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
