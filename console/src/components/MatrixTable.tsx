import type { MatrixResponse } from '@/api/types'
import { StatusLamp } from './StatusLamp'

export function MatrixTable({ matrix }: { matrix: MatrixResponse }) {
  return (
    <section className="page-section panel-elevated overflow-hidden">
      <header className="flex flex-wrap items-center justify-between gap-2 px-3 py-2 border-b border-[var(--border)]">
        <div className="flex items-center gap-2">
          <h2 className="m-0 text-sm font-semibold">{matrix.label}</h2>
          <span className="badge-ui">{matrix.environment}</span>
        </div>
        <span className="text-[var(--text-dense-meta)] text-[var(--muted-foreground)] font-mono-tabular">
          {new Date(matrix.generated_at).toLocaleString()}
        </span>
      </header>
      <div className="dense-table-scroll">
        <table className="dense-table">
          <thead>
            <tr>
              <th>Target</th>
              <th>Category</th>
              <th>Reach</th>
              <th>Auth</th>
              <th>Level</th>
              <th>Detail</th>
            </tr>
          </thead>
          <tbody>
            {matrix.targets.map(row => (
              <tr key={`${matrix.environment}-${row.id}`}>
                <td className="font-mono-tabular">{row.id}</td>
                <td>{row.category}</td>
                <td>
                  <StatusLamp value={row.reachability} kind="reach" />{' '}
                  <span className="font-mono-tabular">{row.reachability}</span>
                </td>
                <td>
                  <StatusLamp value={row.auth} kind="auth" />{' '}
                  <span className="font-mono-tabular">{row.auth}</span>
                </td>
                <td className="font-mono-tabular">{row.authorization_level}</td>
                <td className="text-[var(--muted-foreground)] max-w-md">{row.detail}</td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>
    </section>
  )
}
