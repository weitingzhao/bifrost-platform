import { useQuery } from '@tanstack/react-query'
import { Button } from '@bifrost/ui'
import { fetchAgentNightlyReport } from '@/api/platform'
import { DriftProposalPanel } from '@/components/briefing/DriftProposalPanel'
import { OpsFeedback } from '@/components/feedback/OpsFeedback'

export function NightlyBriefingPanel({
  onOpenAgentDesk,
}: {
  onOpenAgentDesk?: (jobId?: string) => void
}) {
  const { data, isLoading, isError, refetch } = useQuery({
    queryKey: ['agent', 'nightly-report'],
    queryFn: fetchAgentNightlyReport,
    refetchInterval: 120_000,
  })

  return (
    <>
      <section id="nightly-agent-report" className="page-section panel-elevated px-4 py-3">
        <div className="flex flex-wrap items-start justify-between gap-3">
          <div className="min-w-0 flex-1">
            <p className="briefing-section-kicker m-0">0 · Morning briefing</p>
            <h2 className="m-0 mt-1 text-sm font-semibold">Nightly agent report</h2>
            <p className="m-0 mt-1 text-[var(--text-dense-meta)] text-[var(--muted-foreground)]">
              Layer 1–4 drift scan from Mac Mini agent host (3:00 AM). Layer 4 auto-fix requires
              Owner approval below — fixes never run unattended.
            </p>
            {data?.available && data.generated_at != null && (
              <p className="m-0 mt-2 text-[var(--text-dense-meta)] text-[var(--muted-foreground)]">
                Last report:{' '}
                <span className="font-mono-tabular text-[var(--foreground)]">
                  {new Date(data.generated_at).toLocaleString()}
                </span>
                {data.source != null && (
                  <span className="text-[var(--muted-foreground)]"> · {data.source}</span>
                )}
              </p>
            )}
          </div>
          <Button type="button" variant="outline" size="sm" onClick={() => void refetch()}>
            Refresh
          </Button>
        </div>

        {isLoading && (
          <p className="m-0 mt-3 text-[var(--text-dense-meta)] text-[var(--muted-foreground)]">
            Loading nightly report…
          </p>
        )}
        {isError && (
          <OpsFeedback variant="error" title="Failed to load nightly report" className="mt-3">
            Report could not be loaded from platform-api or agent host.
          </OpsFeedback>
        )}
        {!isLoading && !isError && data != null && !data.available && (
          <p className="m-0 mt-3 text-[var(--text-dense-meta)] text-[var(--muted-foreground)]">
            {data.hint ?? 'No report available yet.'}
          </p>
        )}
        {!isLoading && data?.available && data.content != null && (
          <div className="llm-content-panel mt-3">
            <pre className="llm-content-pre font-mono-tabular text-[var(--text-dense-meta)]">
              {data.content}
            </pre>
          </div>
        )}
      </section>

      <DriftProposalPanel onOpenAgentDesk={onOpenAgentDesk} />
    </>
  )
}
