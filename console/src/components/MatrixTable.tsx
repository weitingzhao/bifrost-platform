import { DenseTag, DenseDataTable, DenseTableHeader, DenseTableBody, DenseTableHeadRow, DenseTableRow, DenseTableHead, DenseTableCell } from '@bifrost/ui'
import type { MatrixResponse } from '@/api/types'
import { OpsSection } from '@/components/layout/OpsSection'
import { StatusLamp } from './StatusLamp'

export function MatrixTable({ matrix }: { matrix: MatrixResponse }) {
  return (
    <OpsSection
      title={
        <span className="inline-flex flex-wrap items-center gap-2 normal-case">
          {matrix.label}
          <DenseTag variant="category">{matrix.environment}</DenseTag>
        </span>
      }
      actions={
        <span className="text-[var(--text-dense-meta)] text-[var(--muted-foreground)] font-mono-tabular">
          {new Date(matrix.generated_at).toLocaleString()}
        </span>
      }
      bodyPadding="none"
      overflow="clip-x"
      bodyClassName="ops-section-body--table"
    >
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
    </OpsSection>
  )
}
