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
      <div className="dense-table-scroll">
        <table className="dense-table">
          <thead>
            <tr>
              <th>Name</th>
              <th>Kind</th>
              <th>Ready</th>
              <th>Status</th>
              <th>Restarts</th>
              <th>Age</th>
              <th>Actions</th>
            </tr>
          </thead>
          <tbody>
            {namespace == null ? (
              <tr>
                <td colSpan={7} className="text-[var(--muted-foreground)]">
                  Select a namespace to list workloads
                </td>
              </tr>
            ) : workloads.length === 0 ? (
              <tr>
                <td colSpan={7} className="text-[var(--muted-foreground)]">
                  {isLoading ? 'Loading…' : 'No workloads in this namespace'}
                </td>
              </tr>
            ) : (
              workloads.map(w => (
                <tr
                  key={`${w.kind}-${w.name}`}
                  className={selectedPod === w.name && w.kind === 'Pod' ? 'dense-table__row--selected' : ''}
                  onClick={() => {
                    if (w.kind === 'Pod') onSelectPod(w.name)
                  }}
                  style={{ cursor: w.kind === 'Pod' ? 'pointer' : 'default' }}
                >
                  <td className="font-mono-tabular">{w.name}</td>
                  <td className="font-mono-tabular">{w.kind}</td>
                  <td className="font-mono-tabular">{w.ready}</td>
                  <td>
                    <StatusLamp value={w.reachability} kind="reach" />{' '}
                    <span className="font-mono-tabular">{w.status}</span>
                  </td>
                  <td className="font-mono-tabular">{w.restarts}</td>
                  <td className="font-mono-tabular text-[var(--text-dense-meta)]">{w.age}</td>
                  <td>
                    <div className="flex flex-wrap gap-1">
                      {w.kind === 'Deployment' && (
                        <>
                          <button
                            type="button"
                            className="btn-ui"
                            onClick={event => {
                              event.stopPropagation()
                              onRestartDeployment(w)
                            }}
                          >
                            Restart
                          </button>
                          <button
                            type="button"
                            className="btn-ui"
                            onClick={event => {
                              event.stopPropagation()
                              onScaleDeployment(w)
                            }}
                          >
                            Scale
                          </button>
                        </>
                      )}
                      {w.kind === 'Pod' && (
                        <button
                          type="button"
                          className="btn-ui"
                          onClick={event => {
                            event.stopPropagation()
                            onDeletePod(w)
                          }}
                        >
                          Delete pod
                        </button>
                      )}
                    </div>
                  </td>
                </tr>
              ))
            )}
          </tbody>
        </table>
      </div>
    </section>
  )
}
