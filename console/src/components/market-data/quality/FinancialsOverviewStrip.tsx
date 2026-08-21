import { Skeleton } from '@bifrost/ui'
import { OpsSection } from '@/components/layout/OpsSection'

const REPORT_LABELS: { key: keyof FinancialsCounts; label: string }[] = [
  { key: 'income_statement_symbols', label: 'Income statement' },
  { key: 'balance_sheet_symbols', label: 'Balance sheet' },
  { key: 'cash_flow_symbols', label: 'Cash flow' },
  { key: 'ratio_symbols', label: 'Ratios' },
]

export type FinancialsCounts = {
  income_statement_symbols?: number
  balance_sheet_symbols?: number
  cash_flow_symbols?: number
  ratio_symbols?: number
  short_interest?: number
  short_volume?: number
}

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
    <div className="min-w-0 flex-1 basis-[140px]">
      <p className="m-0 text-[var(--text-dense-meta)] text-[var(--muted-foreground)]">{label}</p>
      {loading ? (
        <Skeleton className="mt-1 h-5 w-20" />
      ) : (
        <>
          <p className="m-0 mt-0.5 font-semibold tabular-nums text-[var(--text-dense-body)]">
            {value}
          </p>
          {detail ? (
            <p className="m-0 mt-0.5 text-[var(--text-dense-caption)] text-[var(--muted-foreground)]">
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

export function FinancialsOverviewStrip({
  loading,
  universeTickers,
  counts,
  shortInterest,
  shortVolume,
  error,
}: {
  loading: boolean
  universeTickers: number
  counts: FinancialsCounts
  shortInterest: number
  shortVolume: number
  error: string | null
}) {
  const universeLabel = universeTickers > 0 ? `vs universe ${fmt(universeTickers)}` : 'vs universe —'

  return (
    <OpsSection
      title="Financials overview"
      description="Distinct symbols per report_type — Plugin GET /market/readiness/financials-by-instrument-type"
      bodyPadding="default"
      overflow="visible"
      collapsible
      defaultCollapsed={false}
    >
      {error != null ? (
        <p className="m-0 text-[var(--text-dense-meta)] text-[var(--destructive)]">{error}</p>
      ) : (
        <div className="flex flex-wrap gap-4">
          {REPORT_LABELS.map(({ key, label }) => {
            const n = counts[key] ?? 0
            return (
              <KpiCell
                key={key}
                label={label}
                loading={loading}
                value={fmt(n)}
                detail={universeLabel}
              />
            )
          })}
          <KpiCell
            label="Short interest"
            loading={loading}
            value={fmt(shortInterest)}
            detail={universeLabel}
          />
          <KpiCell
            label="Short volume"
            loading={loading}
            value={fmt(shortVolume)}
            detail={universeLabel}
          />
        </div>
      )}
    </OpsSection>
  )
}
