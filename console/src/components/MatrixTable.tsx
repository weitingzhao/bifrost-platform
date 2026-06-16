import { DenseTag, DenseDataTable, DenseTableHeader, DenseTableBody, DenseTableHeadRow, DenseTableRow, DenseTableHead, DenseTableCell } from '@bifrost/ui'
import type { MatrixResponse } from '@/api/types'
import { StatusLamp } from './StatusLamp'

export function MatrixTable({ matrix }: { matrix: MatrixResponse }) {
  return (
    <section className="page-section panel-elevated overflow-hidden">
      <header className="flex flex-wrap items-center justify-between gap-2 px-3 py-2 border-b border-[var(--border)]">
        <div className="flex items-center gap-2">
          <h2 className="m-0 text-sm font-semibold">{matrix.label}</h2>
          <DenseTag variant="category">{matrix.environment}</DenseTag>
        </div>
        <span className="text-[var(--text-dense-meta)] text-[var(--muted-foreground)] font-mono-tabular">
          {new Date(matrix.generated_at).toLocaleString()}
        </span>
      </header>
      <DenseDataTable>
        <DenseTableHeader>
          <DenseTableHeadRow>
            <DenseTableHead>Target</DenseTableHead>
            <DenseTableHead>Category</DenseTableHead>
            <DenseTableHead>Reach</DenseTableHead>
            <DenseTableHead>Auth</DenseTableHead>
            <DenseTableHead>Level</DenseTableHead>
            <DenseTableHead>Detail</DenseTableHead>
          </DenseTableHeadRow>
        </DenseTableHeader>
        <DenseTableBody>
          {matrix.targets.map(row => (
            <DenseTableRow key={`${matrix.environment}-${row.id}`}>
              <DenseTableCell className="font-mono-tabular">{row.id}</DenseTableCell>
              <DenseTableCell>{row.category}</DenseTableCell>
              <DenseTableCell>
                <StatusLamp value={row.reachability} kind="reach" />{' '}
                <span className="font-mono-tabular">{row.reachability}</span>
              </DenseTableCell>
              <DenseTableCell>
                <StatusLamp value={row.auth} kind="auth" />{' '}
                <span className="font-mono-tabular">{row.auth}</span>
              </DenseTableCell>
              <DenseTableCell className="font-mono-tabular">{row.authorization_level}</DenseTableCell>
              <DenseTableCell className="text-[var(--muted-foreground)] max-w-md">{row.detail}</DenseTableCell>
            </DenseTableRow>
          ))}
        </DenseTableBody>
      </DenseDataTable>
    </section>
  )
}
