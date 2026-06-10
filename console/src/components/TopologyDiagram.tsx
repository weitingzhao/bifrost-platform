import { useMemo, useState } from 'react'
import { SegmentControl } from '@bifrost/ui'
import type { TopologyResponse } from '@/api/types'
import { StatusLamp } from './StatusLamp'

type RoleView = 'compose' | 'k3s'

const GROUP_LABELS: Record<string, string> = {
  external: 'External',
  linux: 'Linux Mini PC',
  mac: 'Mac',
  compute: 'GPU',
}

export function TopologyDiagram({ data }: { data: TopologyResponse }) {
  const [roleView, setRoleView] = useState<RoleView>('compose')

  const { maxRow, maxCol, nodeMap } = useMemo(() => {
    let maxR = 0
    let maxC = 0
    const map = new Map(data.nodes.map(n => [n.id, n]))
    for (const n of data.nodes) {
      if (n.grid.row > maxR) maxR = n.grid.row
      if (n.grid.col > maxC) maxC = n.grid.col
    }
    return { maxRow: maxR, maxCol: maxC, nodeMap: map }
  }, [data.nodes])

  return (
    <section className="page-section panel-elevated p-3 flex flex-col gap-3">
      <header className="flex flex-wrap items-center justify-between gap-2">
        <div className="flex flex-wrap items-center gap-2 min-w-0">
          <h2 className="m-0 text-sm font-semibold">Network topology</h2>
          <span className="badge-ui">{data.label}</span>
          <span className="badge-ui">phase: {data.deployment_phase}</span>
        </div>
        <SegmentControl
          ariaLabel="Role view"
          value={roleView}
          onChange={v => setRoleView(v as RoleView)}
          options={[
            { value: 'compose', label: 'Compose roles' },
            { value: 'k3s', label: 'K3s target' },
          ]}
        />
      </header>

      <div
        className="topology-grid relative"
        style={{
          display: 'grid',
          gridTemplateColumns: `repeat(${maxCol + 1}, minmax(140px, 1fr))`,
          gridTemplateRows: `repeat(${maxRow + 1}, auto)`,
          gap: '1rem 0.75rem',
          minHeight: '280px',
        }}
      >
        {data.nodes.map(node => (
          <div
            key={node.id}
            className="topology-node panel-elevated p-2"
            style={{
              gridRow: node.grid.row + 1,
              gridColumn: node.grid.col + 1,
            }}
          >
            <div className="flex items-center gap-1.5 mb-1">
              <StatusLamp value={node.status} kind="reach" />
              <strong className="text-sm">{node.label}</strong>
            </div>
            <div className="text-[var(--text-dense-meta)] text-[var(--muted-foreground)]">
              {GROUP_LABELS[node.group] ?? node.group}
              {node.host ? ` · ${node.host}` : ''}
            </div>
            {node.in_k3s_cluster && roleView === 'k3s' && (
              <span className="badge-ui mt-1 text-[10px]">in K3s cluster</span>
            )}
            <ul className="m-0 mt-2 pl-4 text-[var(--text-dense-meta)] text-[var(--muted-foreground)]">
              {(roleView === 'compose' ? node.compose_roles : node.k3s_roles).map(r => (
                <li key={r}>{r}</li>
              ))}
              {roleView === 'k3s' && node.k3s_roles.length === 0 && (
                <li className="list-none -ml-4">—</li>
              )}
            </ul>
            <p className="m-0 mt-1 text-[10px] text-[var(--muted-foreground)] truncate" title={node.detail}>
              {node.detail}
            </p>
          </div>
        ))}
      </div>

      <div className="w-full min-w-0">
        <h3 className="m-0 mb-2 text-xs font-semibold uppercase tracking-wide text-[var(--muted-foreground)]">
          Links ({data.environment})
        </h3>
        <div className="dense-table-scroll">
          <table className="dense-table">
            <thead>
              <tr>
                <th>From</th>
                <th>To</th>
                <th>Label</th>
                <th>Status</th>
                <th>Detail</th>
              </tr>
            </thead>
            <tbody>
              {data.edges.map(edge => (
                <tr key={edge.id}>
                  <td className="font-mono-tabular">{nodeMap.get(edge.from)?.label ?? edge.from}</td>
                  <td className="font-mono-tabular">{nodeMap.get(edge.to)?.label ?? edge.to}</td>
                  <td>{edge.label}</td>
                  <td>
                    <StatusLamp value={edge.status} kind="reach" />{' '}
                    <span className="font-mono-tabular">{edge.status}</span>
                  </td>
                  <td className="text-[var(--muted-foreground)]">{edge.detail}</td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      </div>
    </section>
  )
}
