import { useQuery } from '@tanstack/react-query'
import {
  DenseDataTable,
  DenseTableBody,
  DenseTableCell,
  DenseTableHead,
  DenseTableHeadRow,
  DenseTableHeader,
  DenseTableRow,
  denseTableNumCell,
} from '@bifrost/ui'
import { fetchSepaStats, isProxyError } from '@/api/marketDataPlugin'
import { OpsSection } from '@/components/layout/OpsSection'

export function SepaStatsSection() {
  const q = useQuery({
    queryKey: ['market-data', 'coverage', 'sepa-stats'],
    queryFn: fetchSepaStats,
    refetchInterval: 60_000,
    retry: 1,
  })

  const proxyErr = q.data != null && isProxyError(q.data) ? q.data.error : null
  const tables =
    q.data != null && !isProxyError(q.data) ? (q.data.tables ?? []) : []

  return (
    <OpsSection
      title="SEPA table stats"
      description="Row counts + latest — Plugin GET /market/coverage/sepa-stats"
      bodyPadding="none"
      overflow="visible"
      collapsible
      defaultCollapsed={false}
    >
      {q.isLoading ? (
        <p className="m-0 px-3 py-3 text-[var(--text-dense-meta)] text-[var(--muted-foreground)]">
          Loading SEPA stats…
        </p>
      ) : proxyErr != null ? (
        <p className="m-0 px-3 py-3 text-[var(--text-dense-meta)] text-[var(--destructive)]">
          {proxyErr}
        </p>
      ) : tables.length === 0 ? (
        <p className="m-0 px-3 py-3 text-[var(--text-dense-meta)] text-[var(--muted-foreground)]">
          No SEPA table stats
        </p>
      ) : (
        <DenseDataTable>
          <DenseTableHeader>
            <DenseTableHeadRow>
              <DenseTableHead>Table</DenseTableHead>
              <DenseTableHead className="text-right">Rows</DenseTableHead>
              <DenseTableHead>Latest</DenseTableHead>
            </DenseTableHeadRow>
          </DenseTableHeader>
          <DenseTableBody>
            {tables.map(t => (
              <DenseTableRow key={t.table ?? 'unknown'}>
                <DenseTableCell className="font-mono text-xs">{t.table ?? '—'}</DenseTableCell>
                <DenseTableCell className={denseTableNumCell}>
                  {t.row_count != null ? t.row_count.toLocaleString('en-US') : '—'}
                </DenseTableCell>
                <DenseTableCell className="font-mono text-xs">{t.latest ?? '—'}</DenseTableCell>
              </DenseTableRow>
            ))}
          </DenseTableBody>
        </DenseDataTable>
      )}
    </OpsSection>
  )
}
