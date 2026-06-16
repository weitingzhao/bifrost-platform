import { DenseDataTable, DenseTableHeader, DenseTableBody, DenseTableHeadRow, DenseTableRow, DenseTableHead, DenseTableCell, SegmentControl } from '@bifrost/ui'
import type { ClusterNamespace } from '@/api/types'

type NsFilter = 'all' | 'bifrost'

interface ClusterNamespacesPanelProps {
  namespaces: ClusterNamespace[]
  filter: NsFilter
  selectedNs: string | null
  isLoading: boolean
  onFilterChange: (filter: NsFilter) => void
  onSelectNs: (name: string) => void
}

export function ClusterNamespacesPanel({
  namespaces,
  filter,
  selectedNs,
  isLoading,
  onFilterChange,
  onSelectNs,
}: ClusterNamespacesPanelProps) {
  return (
    <section className="page-section panel-elevated overflow-hidden">
      <header className="flex flex-wrap items-center justify-between gap-2 border-b border-[var(--border)] px-3 py-2">
        <h2 className="m-0 text-sm font-semibold">Namespaces</h2>
        <SegmentControl
          value={filter}
          onChange={(v) => onFilterChange(v as NsFilter)}
          options={[
            { value: 'all', label: 'All' },
            { value: 'bifrost', label: 'Bifrost' },
          ]}
          size="sm"
        />
      </header>
      <DenseDataTable wrapClassName="max-h-64">
        <DenseTableHeader>
          <DenseTableHeadRow>
            <DenseTableHead>Name</DenseTableHead>
            <DenseTableHead>Pods</DenseTableHead>
            <DenseTableHead>Running</DenseTableHead>
            <DenseTableHead>Failing</DenseTableHead>
          </DenseTableHeadRow>
        </DenseTableHeader>
        <DenseTableBody>
          {namespaces.length === 0 ? (
            <DenseTableRow>
              <DenseTableCell colSpan={4} className="text-[var(--muted-foreground)]">
                {isLoading ? 'Loading…' : 'No namespaces'}
              </DenseTableCell>
            </DenseTableRow>
          ) : (
            namespaces.map(ns => (
              <DenseTableRow
                key={ns.name}
                className={selectedNs === ns.name ? 'dense-table__row--selected' : ''}
                onClick={() => onSelectNs(ns.name)}
                style={{ cursor: 'pointer' }}
              >
                <DenseTableCell className="font-mono-tabular">{ns.name}</DenseTableCell>
                <DenseTableCell className="font-mono-tabular">{ns.pod_count}</DenseTableCell>
                <DenseTableCell className="font-mono-tabular">{ns.running_pods}</DenseTableCell>
                <DenseTableCell className={`font-mono-tabular ${ns.failing_pods > 0 ? 'lamp-fail' : ''}`}>
                  {ns.failing_pods}
                </DenseTableCell>
              </DenseTableRow>
            ))
          )}
        </DenseTableBody>
      </DenseDataTable>
    </section>
  )
}
