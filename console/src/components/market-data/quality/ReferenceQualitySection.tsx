import { DenseTag, Skeleton } from '@bifrost/ui'
import type { ReferenceCoverageResponse } from '@/api/marketDataPlugin'
import { OpsSection } from '@/components/layout/OpsSection'

function CoverageBlock({
  title,
  loading,
  error,
  data,
}: {
  title: string
  loading: boolean
  error: string | null
  data: ReferenceCoverageResponse | null
}) {
  const total = data?.total ?? 0
  const filled = data?.filled ?? 0
  const missing = data?.missing ?? 0
  const pct = total > 0 ? ((filled / total) * 100).toFixed(1) : null
  const ok = !loading && error == null && missing === 0 && total > 0

  return (
    <div className="rounded-md border border-[var(--border)] bg-[var(--secondary)] px-3 py-2">
      <div className="flex flex-wrap items-center gap-2">
        <p className="m-0 text-[var(--text-dense-label)] font-semibold">{title}</p>
        {loading || error != null ? null : (
          <DenseTag variant={ok ? 'success' : missing > 0 ? 'warning' : 'neutral'}>
            {ok ? 'COMPLETE' : pct != null ? `${pct}%` : '—'}
          </DenseTag>
        )}
      </div>
      {loading ? (
        <Skeleton className="mt-2 h-4 w-40" />
      ) : error != null ? (
        <p className="m-0 mt-1 text-[var(--text-dense-meta)] text-[var(--destructive)]">{error}</p>
      ) : (
        <p className="m-0 mt-1 text-[var(--text-dense-meta)] text-[var(--muted-foreground)]">
          filled {filled.toLocaleString('en-US')} / {total.toLocaleString('en-US')} · missing{' '}
          {missing.toLocaleString('en-US')}
          {data?.source ? ` · ${data.source}` : ''}
          {data?.note ? ` · ${data.note}` : ''}
        </p>
      )}
    </div>
  )
}

export function ReferenceQualitySection({
  overview,
  overviewLoading,
  overviewError,
  related,
  relatedLoading,
  relatedError,
}: {
  overview: ReferenceCoverageResponse | null
  overviewLoading: boolean
  overviewError: string | null
  related: ReferenceCoverageResponse | null
  relatedLoading: boolean
  relatedError: string | null
}) {
  return (
    <OpsSection
      title="Reference quality"
      description="Ticker overview + related-companies completeness"
      bodyPadding="default"
      overflow="visible"
      collapsible
      defaultCollapsed={false}
    >
      <div className="flex flex-col gap-2">
        <CoverageBlock
          title="Overview coverage"
          loading={overviewLoading}
          error={overviewError}
          data={overview}
        />
        <CoverageBlock
          title="Related coverage"
          loading={relatedLoading}
          error={relatedError}
          data={related}
        />
      </div>
    </OpsSection>
  )
}
