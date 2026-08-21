import { Skeleton } from '@bifrost/ui'
import { OpsSection } from '@/components/layout/OpsSection'
import type { SnapshotCoverageDerived } from '@/components/market-data/quality/readinessChecks'

function KpiCell({
  label,
  loading,
  value,
  detail,
}: {
  label: string
  loading: boolean
  value: string
  detail?: string
}) {
  return (
    <div className="min-w-0 flex-1">
      <p className="m-0 text-[var(--text-dense-meta)] text-[var(--muted-foreground)]">{label}</p>
      {loading ? (
        <Skeleton className="mt-1 h-5 w-24" />
      ) : (
        <>
          <p className="m-0 mt-0.5 font-semibold tabular-nums text-[var(--text-dense-body)] text-[var(--foreground)]">
            {value}
          </p>
          {detail ? (
            <p className="m-0 mt-0.5 truncate text-[var(--text-dense-caption)] text-[var(--muted-foreground)]">
              {detail}
            </p>
          ) : null}
        </>
      )}
    </div>
  )
}

function fmt(n: number): string {
  return n.toLocaleString('en-US')
}

export function ReadinessKpiStrip({
  loading,
  universeTickers,
  snapshot,
  vendorGapCount,
  lowCoverageDates,
}: {
  loading: boolean
  universeTickers: number
  snapshot: SnapshotCoverageDerived
  vendorGapCount: number
  lowCoverageDates: number
}) {
  return (
    <OpsSection
      title="Readiness KPIs"
      description="Universe count · snapshot coverage · vendor gaps · date holes"
      bodyPadding="default"
      overflow="visible"
      collapsible
      defaultCollapsed={false}
    >
      <div className="flex flex-wrap gap-4">
        <KpiCell
          label="Universe"
          loading={loading}
          value={fmt(universeTickers)}
          detail="market.ticker total"
        />
        <KpiCell
          label="Snapshot covered"
          loading={loading}
          value={
            snapshot.universe > 0
              ? `${fmt(snapshot.covered)} / ${fmt(snapshot.universe)}`
              : fmt(snapshot.rowCount)
          }
          detail={
            snapshot.coveragePct != null
              ? `${snapshot.coveragePct.toFixed(1)}% · session ${snapshot.sessionDate ?? '—'}`
              : `rows ${fmt(snapshot.rowCount)}`
          }
        />
        <KpiCell
          label="Vendor gaps"
          loading={loading}
          value={fmt(vendorGapCount)}
          detail="snapshot vs latest bar"
        />
        <KpiCell
          label="Date coverage"
          loading={loading}
          value={lowCoverageDates === 0 ? 'OK' : fmt(lowCoverageDates)}
          detail={lowCoverageDates === 0 ? 'no low-coverage dates' : 'low-coverage dates'}
        />
      </div>
    </OpsSection>
  )
}
