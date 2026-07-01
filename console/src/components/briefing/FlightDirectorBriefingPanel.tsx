import { useQuery } from '@tanstack/react-query'
import { Button, DenseTag, StatusLamp } from '@bifrost/ui'
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
        Daily digest from remediation JobStore — what Agent completed, failed, escalated, and earned-autonomy
        hints. Replaces manual Audit scanning for autonomous outcomes (Mission Signal Phase 6).
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
        <div className="mt-3 grid gap-3 sm:grid-cols-2 lg:grid-cols-5">
          <DigestTile label="Completed" value={String(brief.jobs_completed)} />
          <DigestTile label="Failed" value={String(brief.jobs_failed)} />
          <DigestTile label="Escalations" value={String(brief.escalations)} />
          <DigestTile label="Promo pending" value={String(brief.promotion_pending)} />
          <DigestTile label="Demotions" value={String(brief.demotions)} />
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

function DigestTile({ label, value }: { label: string; value: string }) {
  return (
    <div className="rounded-md border border-[var(--border)] bg-[var(--background)] px-3 py-2">
      <span className="text-[var(--text-dense-caption)] text-[var(--muted-foreground)]">{label}</span>
      <p className="m-0 text-lg font-semibold tabular-nums">{value}</p>
    </div>
  )
}
