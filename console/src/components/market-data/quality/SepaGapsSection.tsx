import { useQueries } from '@tanstack/react-query'
import {
  DenseDataTable,
  DenseTableBody,
  DenseTableCell,
  DenseTableHead,
  DenseTableHeadRow,
  DenseTableHeader,
  DenseTableRow,
  DenseTag,
} from '@bifrost/ui'
import { fetchSepaGaps, isProxyError } from '@/api/marketDataPlugin'
import { OpsSection } from '@/components/layout/OpsSection'

const SEPA_REPORT_TYPES = [
  'income_statement',
  'balance_sheet',
  'cash_flow_statement',
  'ratios',
  'short_interest',
  'short_volume',
] as const

function reportLabel(t: string): string {
  return t.replace(/_/g, ' ')
}

function GapTable({ symbols }: { symbols: string[] }) {
  const preview = symbols.slice(0, 80)
  return (
    <DenseDataTable>
      <DenseTableHeader>
        <DenseTableHeadRow>
          <DenseTableHead>Symbol</DenseTableHead>
        </DenseTableHeadRow>
      </DenseTableHeader>
      <DenseTableBody>
        {preview.map(sym => (
          <DenseTableRow key={sym}>
            <DenseTableCell className="font-mono text-xs font-semibold">{sym}</DenseTableCell>
          </DenseTableRow>
        ))}
      </DenseTableBody>
    </DenseDataTable>
  )
}

export function SepaGapsSection() {
  const queries = useQueries({
    queries: SEPA_REPORT_TYPES.map(report_type => ({
      queryKey: ['market-data', 'financials', 'sepa-gaps', report_type],
      queryFn: () => fetchSepaGaps({ report_type, limit: 200 }),
      refetchInterval: 120_000,
      retry: 1,
    })),
  })

  return (
    <div className="flex flex-col gap-3">
      {SEPA_REPORT_TYPES.map((report_type, i) => {
        const q = queries[i]
        const proxyErr = q.data != null && isProxyError(q.data) ? q.data.error : null
        const data = q.data != null && !isProxyError(q.data) ? q.data : null
        const count = data?.count ?? data?.symbols?.length ?? 0
        const symbols = data?.symbols ?? []
        const ok = !q.isLoading && proxyErr == null && count === 0

        return (
          <OpsSection
            key={report_type}
            title={`SEPA gaps — ${reportLabel(report_type)}`}
            description="Plugin GET /market/stocks/fundamentals/sepa/gaps"
            headerExtra={
              q.isLoading || proxyErr != null ? null : (
                <DenseTag variant={ok ? 'success' : 'warning'}>
                  {ok ? 'OK' : `${count} gaps`}
                </DenseTag>
              )
            }
            bodyPadding="none"
            overflow="visible"
            collapsible
            defaultCollapsed={ok}
          >
            {q.isLoading ? (
              <p className="m-0 px-3 py-3 text-[var(--text-dense-meta)] text-[var(--muted-foreground)]">
                Loading gaps…
              </p>
            ) : proxyErr != null ? (
              <p className="m-0 px-3 py-3 text-[var(--text-dense-meta)] text-[var(--destructive)]">
                {proxyErr}
              </p>
            ) : symbols.length === 0 ? (
              <p className="m-0 px-3 py-3 text-[var(--text-dense-meta)] text-[var(--muted-foreground)]">
                No gaps
                {data?.note ? ` — ${data.note}` : ''}
              </p>
            ) : (
              <div className="flex flex-col gap-2">
                {symbols.length > 80 ? (
                  <p className="m-0 px-3 pt-2 text-[var(--text-dense-caption)] text-[var(--muted-foreground)]">
                    Showing 80 of {symbols.length.toLocaleString('en-US')} symbols
                  </p>
                ) : null}
                <GapTable symbols={symbols} />
              </div>
            )}
          </OpsSection>
        )
      })}
    </div>
  )
}
