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
        <div className="segment-control" role="group" aria-label="Namespace filter">
          <button
            type="button"
            className={filter === 'all' ? 'segment-control__btn segment-control__btn--active' : 'segment-control__btn'}
            onClick={() => onFilterChange('all')}
          >
            All
          </button>
          <button
            type="button"
            className={
              filter === 'bifrost'
                ? 'segment-control__btn segment-control__btn--active'
                : 'segment-control__btn'
            }
            onClick={() => onFilterChange('bifrost')}
          >
            Bifrost
          </button>
        </div>
      </header>
      <div className="dense-table-scroll max-h-64">
        <table className="dense-table">
          <thead>
            <tr>
              <th>Name</th>
              <th>Pods</th>
              <th>Running</th>
              <th>Failing</th>
            </tr>
          </thead>
          <tbody>
            {namespaces.length === 0 ? (
              <tr>
                <td colSpan={4} className="text-[var(--muted-foreground)]">
                  {isLoading ? 'Loading…' : 'No namespaces'}
                </td>
              </tr>
            ) : (
              namespaces.map(ns => (
                <tr
                  key={ns.name}
                  className={selectedNs === ns.name ? 'dense-table__row--selected' : ''}
                  onClick={() => onSelectNs(ns.name)}
                  style={{ cursor: 'pointer' }}
                >
                  <td className="font-mono-tabular">{ns.name}</td>
                  <td className="font-mono-tabular">{ns.pod_count}</td>
                  <td className="font-mono-tabular">{ns.running_pods}</td>
                  <td className={`font-mono-tabular ${ns.failing_pods > 0 ? 'lamp-fail' : ''}`}>
                    {ns.failing_pods}
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
