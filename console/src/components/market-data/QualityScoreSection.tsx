import { useQuery } from '@tanstack/react-query'
import { DenseTag } from '@bifrost/ui'
import {
  fetchQualityScore,
  isProxyError,
  type QualityCheckItem,
  type QualityScoreResponse,
} from '@/api/marketDataPlugin'
import { OpsSection } from '@/components/layout/OpsSection'

const CHECK_LABELS: Record<string, string> = {
  stock_daily_coverage: 'Stock daily coverage',
  option_snapshot_coverage: 'Option snapshot coverage',
  option_oi_coverage: 'Option OI coverage',
  freshness: 'Freshness',
}

function checkLabel(check: string): string {
  return CHECK_LABELS[check] ?? check.replace(/_/g, ' ')
}

function CheckRow({ item }: { item: QualityCheckItem }) {
  const pass = item.ok === true
  return (
    <div className="flex flex-wrap items-start gap-2">
      <DenseTag variant={pass ? 'success' : 'danger'}>{pass ? 'PASS' : 'FAIL'}</DenseTag>
      <div className="min-w-0 flex-1">
        <p className="m-0 text-[var(--text-dense-label)] font-medium">{checkLabel(item.check)}</p>
        {item.detail ? (
          <p className="m-0 mt-0.5 text-[var(--text-dense-caption)] text-[var(--muted-foreground)]">
            {item.detail}
          </p>
        ) : null}
      </div>
    </div>
  )
}

export function QualityScoreSection() {
  const q = useQuery({
    queryKey: ['market-data', 'coverage', 'quality-score'],
    queryFn: fetchQualityScore,
    refetchInterval: 60_000,
    retry: 1,
  })

  const data = q.data
  const proxyErr = data != null && isProxyError(data) ? data : null
  const score: QualityScoreResponse | null =
    data != null && !isProxyError(data) ? data : null
  const err =
    q.isError
      ? q.error instanceof Error
        ? q.error.message
        : 'Failed to load quality score'
      : proxyErr?.error ?? null
  const summary =
    score?.summary ?? (score?.ok === true ? 'PASS' : score != null ? 'FAIL' : null)
  const checks: QualityCheckItem[] = score?.checks ?? []
  const overallPass = summary === 'PASS' || score?.ok === true

  return (
    <OpsSection
      title="Data Quality Score"
      description="Plugin GET /market/coverage/quality-score — four acceptance checks"
      headerExtra={
        q.isLoading || err != null || summary == null ? null : (
          <DenseTag variant={overallPass ? 'success' : 'danger'}>{summary}</DenseTag>
        )
      }
      bodyPadding="default"
      overflow="visible"
      collapsible
      defaultCollapsed={false}
    >
      {q.isLoading ? (
        <p className="m-0 text-[var(--text-dense-meta)] text-[var(--muted-foreground)]">
          Loading quality score…
        </p>
      ) : err != null ? (
        <p className="m-0 text-[var(--text-dense-meta)] text-[var(--destructive)]">{err}</p>
      ) : checks.length === 0 ? (
        <p className="m-0 text-[var(--text-dense-meta)] text-[var(--muted-foreground)]">
          No quality checks returned
        </p>
      ) : (
        <div className="flex flex-col gap-3">
          {checks.map(item => (
            <CheckRow key={item.check} item={item} />
          ))}
        </div>
      )}
    </OpsSection>
  )
}
