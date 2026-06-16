import { Button, DenseDataTable, DenseTableHeader, DenseTableBody, DenseTableHeadRow, DenseTableRow, DenseTableHead, DenseTableCell } from '@bifrost/ui'
import type { ClusterWorkload } from '@/api/types'
import { StatusLamp } from '@/components/StatusLamp'

interface ClusterWorkloadsTableProps {
  namespace: string | null
  workloads: ClusterWorkload[]
  isLoading: boolean
  selectedPod: string | null
  onSelectPod: (name: string) => void
  onRestartDeployment: (workload: ClusterWorkload) => void
  onScaleDeployment: (workload: ClusterWorkload) => void
  onDeletePod: (workload: ClusterWorkload) => void
}

export function ClusterWorkloadsTable({
  namespace,
  workloads,
  isLoading,
  selectedPod,
  onSelectPod,
  onRestartDeployment,
  onScaleDeployment,
  onDeletePod,
}: ClusterWorkloadsTableProps) {
  return (
    <section className="page-section panel-elevated overflow-hidden">
      <header className="flex items-center justify-between gap-2 border-b border-[var(--border)] px-3 py-2">
        <h2 className="m-0 text-sm font-semibold">
          Workloads{namespace != null ? ` · ${namespace}` : ''}
        </h2>
        <span className="text-[var(--text-dense-meta)] text-[var(--muted-foreground)]">
          {namespace == null
            ? 'Select a namespace'
            : isLoading
              ? '…'
              : `${workloads.length} workloads`}
        </span>
      </header>
      <DenseDataTable>
        <DenseTableHeader>
          <DenseTableHeadRow>
            <DenseTableHead>Name</DenseTableHead>
            <DenseTableHead>Kind</DenseTableHead>
            <DenseTableHead>Ready</DenseTableHead>
            <DenseTableHead>Status</DenseTableHead>
            <DenseTableHead>Restarts</DenseTableHead>
            <DenseTableHead>Age</DenseTableHead>
            <DenseTableHead>Actions</DenseTableHead>
          </DenseTableHeadRow>
        </DenseTableHeader>
        <DenseTableBody>
          {namespace == null ? (
            <DenseTableRow>
              <DenseTableCell colSpan={7} className="text-[var(--muted-foreground)]">
                Select a namespace to list workloads
              </DenseTableCell>
            </DenseTableRow>
          ) : workloads.length === 0 ? (
            <DenseTableRow>
              <DenseTableCell colSpan={7} className="text-[var(--muted-foreground)]">
                {isLoading ? 'Loading…' : 'No workloads in this namespace'}
              </DenseTableCell>
            </DenseTableRow>
          ) : (
            workloads.map(w => (
              <DenseTableRow
                key={`${w.kind}-${w.name}`}
                className={selectedPod === w.name && w.kind === 'Pod' ? 'dense-table__row--selected' : ''}
                onClick={() => {
                  if (w.kind === 'Pod') onSelectPod(w.name)
                }}
                style={{ cursor: w.kind === 'Pod' ? 'pointer' : 'default' }}
              >
                <DenseTableCell className="font-mono-tabular">{w.name}</DenseTableCell>
                <DenseTableCell className="font-mono-tabular">{w.kind}</DenseTableCell>
                <DenseTableCell className="font-mono-tabular">{w.ready}</DenseTableCell>
                <DenseTableCell>
                  <StatusLamp value={w.reachability} kind="reach" />{' '}
                  <span className="font-mono-tabular">{w.status}</span>
                </DenseTableCell>
                <DenseTableCell className="font-mono-tabular">{w.restarts}</DenseTableCell>
                <DenseTableCell className="font-mono-tabular text-[var(--text-dense-meta)]">{w.age}</DenseTableCell>
                <DenseTableCell>
                  <div className="flex flex-wrap gap-1">
                    {w.kind === 'Deployment' && (
                      <>
                        <Button
                          variant="outline"
                          size="sm"
                          onClick={event => {
                            event.stopPropagation()
                            onRestartDeployment(w)
                          }}
                        >
                          Restart
                        </Button>
                        <Button
                          variant="outline"
                          size="sm"
                          onClick={event => {
                            event.stopPropagation()
                            onScaleDeployment(w)
                          }}
                        >
                          Scale
                        </Button>
                      </>
                    )}
                    {w.kind === 'Pod' && (
                      <Button
                        variant="outline"
                        size="sm"
                        onClick={event => {
                          event.stopPropagation()
                          onDeletePod(w)
                        }}
                      >
                        Delete pod
                      </Button>
                    )}
                  </div>
                </DenseTableCell>
              </DenseTableRow>
            ))
          )}
        </DenseTableBody>
      </DenseDataTable>
    </section>
  )
}
