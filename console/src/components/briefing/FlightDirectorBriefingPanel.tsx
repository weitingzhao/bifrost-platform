import { useQuery } from '@tanstack/react-query'
import { Button, DenseTag, StatusLamp } from '@bifrost/ui'
import type { ReactNode } from 'react'
import { fetchFlightDirectorSnapshot } from '@/api/platform'

interface FlightDirectorBriefingPanelProps {
  onOpenTrustAutonomy?: () => void
}

export function FlightDirectorBriefingPanel({ onOpenTrustAutonomy }: FlightDirectorBriefingPanelProps) {
  const snapshotQ = useQuery({
    queryKey: ['briefing', 'flight-director-snapshot'],
    queryFn: fetchFlightDirectorSnapshot,
    refetchInterval: 60_000,
  })

  const brief = snapshotQ.data?.briefing
  const trustSource = snapshotQ.data?.trust_matrix.data_source ?? ''

  return (
    <section className="page-section panel-elevated px-4 py-3">
      <div className="flex flex-wrap items-center gap-2">
        <p className="briefing-section-kicker m-0">Flight Director · 24h digest</p>
        <StatusLamp value={snapshotQ.isSuccess ? 'ok' : snapshotQ.isError ? 'fail' : 'unknown'} kind="reach" />
        {trustSource.includes('owner_overrides') && (
          <DenseTag variant="warning">Owner overrides active</DenseTag>
        )}
      </div>
      <p className="m-0 mt-1 text-[var(--text-dense-meta)] text-[var(--muted-foreground)]">
        Last {brief?.period_hours ?? 24} hours from remediation JobStore and the current trust matrix.
        Completed and failed count jobs; escalations count approval-request events, so escalations can
        exceed completed jobs. Promotion and demotion values count skills.
      </p>

      {snapshotQ.isLoading && (
        <p className="mt-2 text-[var(--text-dense-meta)] text-[var(--muted-foreground)]">Loading digest…</p>
      )}
      {snapshotQ.error != null && (
        <p className="mt-2 text-[var(--text-dense-meta)] text-[var(--destructive)]">
          Failed to load Flight Director snapshot: {(snapshotQ.error as Error).message}
        </p>
      )}
      {brief != null && (
        <div className="agent-desk-digest-grid mt-3">
          <DigestTile label="Completed jobs" value={String(brief.jobs_completed)} />
          <DigestTile label="Failed jobs" value={String(brief.jobs_failed)} />
          <DigestTile label="Approval events" value={String(brief.escalations)} />
          <DigestTile
            label="Skills eligible"
            value={String(brief.promotion_pending)}
            action={
              brief.promotion_pending > 0 && onOpenTrustAutonomy != null
                ? (
                    <Button variant="ghost" size="sm" onClick={onOpenTrustAutonomy}>
                      Review promotions
                    </Button>
                  )
                : undefined
            }
          />
          <DigestTile label="Skills flagged" value={String(brief.demotions)} />
        </div>
      )}
      {brief?.summary != null && (
        <p className="m-0 mt-3 text-[var(--text-dense-body)]">{brief.summary}</p>
      )}
      {onOpenTrustAutonomy != null && (
        <div className="mt-3">
          <Button variant="outline" size="sm" onClick={onOpenTrustAutonomy}>
            Open Trust & Autonomy
          </Button>
        </div>
      )}
    </section>
  )
}

function DigestTile({
  label,
  value,
  action,
}: {
  label: string
  value: string
  action?: ReactNode
}) {
  return (
    <div className="min-w-0 rounded-md border border-[var(--border)] bg-[var(--background)] px-3 py-2">
      <span className="text-[var(--text-dense-caption)] text-[var(--muted-foreground)]">{label}</span>
      <p className="m-0 text-lg font-semibold tabular-nums">{value}</p>
      {action}
    </div>
  )
}
