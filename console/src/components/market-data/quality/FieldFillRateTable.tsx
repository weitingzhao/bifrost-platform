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
import { OpsSection } from '@/components/layout/OpsSection'

type FillRow = {
  table: string
  field: string
  filled: number
}

function flattenTables(tables: Record<string, Record<string, number>> | undefined): FillRow[] {
  if (tables == null) return []
  const rows: FillRow[] = []
  for (const [table, fields] of Object.entries(tables)) {
    for (const [field, filled] of Object.entries(fields)) {
      rows.push({ table, field, filled })
    }
  }
  return rows.sort((a, b) => a.table.localeCompare(b.table) || a.field.localeCompare(b.field))
}

export function FieldFillRateTable({
  tables,
  loading,
  error,
}: {
  tables: Record<string, Record<string, number>> | undefined
  loading: boolean
  error: string | null
}) {
  const rows = flattenTables(tables)

  return (
    <OpsSection
      title="Field fill rate"
      description="JSONB key presence counts — Plugin GET /market/readiness/financials-fill-rate"
      bodyPadding="none"
      overflow="visible"
      collapsible
      defaultCollapsed={rows.length === 0 && !loading}
    >
      {loading ? (
        <p className="m-0 px-3 py-3 text-[var(--text-dense-meta)] text-[var(--muted-foreground)]">
          Loading fill rates…
        </p>
      ) : error != null ? (
        <p className="m-0 px-3 py-3 text-[var(--text-dense-meta)] text-[var(--destructive)]">
          {error}
        </p>
      ) : rows.length === 0 ? (
        <p className="m-0 px-3 py-3 text-[var(--text-dense-meta)] text-[var(--muted-foreground)]">
          No fill-rate data
        </p>
      ) : (
        <DenseDataTable>
          <DenseTableHeader>
            <DenseTableHeadRow>
              <DenseTableHead>Table alias</DenseTableHead>
              <DenseTableHead>Field</DenseTableHead>
              <DenseTableHead className="text-right">Distinct symbols</DenseTableHead>
            </DenseTableHeadRow>
          </DenseTableHeader>
          <DenseTableBody>
            {rows.map(r => (
              <DenseTableRow key={`${r.table}:${r.field}`}>
                <DenseTableCell className="font-mono text-xs">{r.table}</DenseTableCell>
                <DenseTableCell className="font-mono text-xs">{r.field}</DenseTableCell>
                <DenseTableCell className={denseTableNumCell}>
                  {r.filled.toLocaleString('en-US')}
                </DenseTableCell>
              </DenseTableRow>
            ))}
          </DenseTableBody>
        </DenseDataTable>
      )}
    </OpsSection>
  )
}
