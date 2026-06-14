import type { MatrixResponse, OpsContextResponse, TopologyResponse } from '@/api/types'
import { MatrixTable } from '@/components/MatrixTable'
import {
  buildScopeLayers,
  filterTargetsForNode,
  getEdge,
  getNode,
  getTarget,
  nodeToHardwareMeta,
  type RuntimeMapSelection,
} from '@/lib/runtime-map/runtimeMapRegistry'

const TRADE_APP_URL = import.meta.env.VITE_TRADE_FRONTEND_URL ?? 'http://127.0.0.1:5173'

interface RuntimeMapDrawerProps {
  selection: RuntimeMapSelection
  topology: TopologyResponse | undefined
  matrix: MatrixResponse | undefined
  context: OpsContextResponse | undefined
  showFullMatrix: boolean
  onClose: () => void
  onToggleFullMatrix: () => void
}

export function RuntimeMapDrawer({
  selection,
  topology,
  matrix,
  context,
  showFullMatrix,
  onClose,
  onToggleFullMatrix,
}: RuntimeMapDrawerProps) {
  if (selection == null && !showFullMatrix) return null

  const title = showFullMatrix
    ? 'Full probe table'
    : selection?.kind === 'node'
      ? (getNode(topology!, selection.id)?.label ?? selection.id)
      : selection?.kind === 'target'
        ? selection.id
        : selection?.kind === 'scope'
          ? `SCOPE: ${selection.tag}`
          : selection?.kind === 'edge'
            ? (getEdge(topology!, selection.id)?.label ?? selection.id)
            : 'Detail'

  const showTradeReactor =
    selection?.kind === 'target' &&
    (selection.id.startsWith('api-') || selection.id.includes('socket'))

  return (
    <aside className="bay-detail-drawer runtime-map-drawer" role="dialog" aria-label="Runtime detail">
      <header className="bay-detail-drawer-header">
        <h3 className="m-0 text-sm font-semibold">{title}</h3>
        <button type="button" className="btn-ui btn-ui-ghost" onClick={onClose} aria-label="Close">
          Close
        </button>
      </header>

      <div className="bay-detail-drawer-body">
        {showFullMatrix && matrix != null && <MatrixTable matrix={matrix} />}

        {!showFullMatrix && selection?.kind === 'node' && topology != null && (
          <NodeDetail nodeId={selection.id} topology={topology} matrix={matrix} />
        )}

        {!showFullMatrix && selection?.kind === 'target' && matrix != null && (
          <TargetDetail targetId={selection.id} matrix={matrix} context={context} />
        )}

        {!showFullMatrix && selection?.kind === 'scope' && (
          <ScopeDetail tag={selection.tag} matrix={matrix} />
        )}

        {!showFullMatrix && selection?.kind === 'edge' && topology != null && (
          <EdgeDetail edgeId={selection.id} topology={topology} matrix={matrix} />
        )}
      </div>

      <footer className="bay-detail-drawer-footer">
        <button type="button" className="btn-ui btn-ui-ghost" onClick={onToggleFullMatrix}>
          {showFullMatrix ? 'Back to selection' : 'Open full probe table'}
        </button>
        {showTradeReactor && (
          <a
            className="btn-ui btn-ui-primary"
            href={TRADE_APP_URL}
            target="_blank"
            rel="noreferrer"
          >
            Open Trade Reactor
          </a>
        )}
      </footer>
    </aside>
  )
}

function NodeDetail({
  nodeId,
  topology,
  matrix,
}: {
  nodeId: string
  topology: TopologyResponse
  matrix: MatrixResponse | undefined
}) {
  const node = getNode(topology, nodeId)
  if (!node) return <p className="m-0 text-[var(--muted-foreground)]">Node not found.</p>
  const hw = nodeToHardwareMeta(nodeId)
  const targets = filterTargetsForNode(node, matrix)
  const edges = topology.edges.filter(e => e.from === nodeId || e.to === nodeId)

  return (
    <>
      <p className="m-0 text-[var(--text-dense)]">
        Host: <span className="font-mono-tabular">{node.host ?? '—'}</span>
      </p>
      <p className="m-0 mt-1 text-[var(--text-dense-meta)] text-[var(--muted-foreground)]">{node.detail}</p>
      {hw != null && (
        <>
          <p className="m-0 mt-2 text-[var(--text-dense-meta)]">
            Compose: {hw.roleCompose}
          </p>
          <p className="m-0 text-[var(--text-dense-meta)] text-[var(--muted-foreground)]">
            K3s target: {hw.roleK3s}
          </p>
        </>
      )}
      {targets.length > 0 && (
        <table className="dense-table mt-3">
          <thead>
            <tr>
              <th>Service</th>
              <th>Reach</th>
              <th>Detail</th>
            </tr>
          </thead>
          <tbody>
            {targets.map(t => (
              <tr key={t.id}>
                <td className="font-mono-tabular">{t.id}</td>
                <td>{t.reachability}</td>
                <td className="text-[var(--muted-foreground)]">{t.detail}</td>
              </tr>
            ))}
          </tbody>
        </table>
      )}
      {edges.length > 0 && (
        <>
          <h4 className="mt-3 text-xs font-semibold uppercase text-[var(--muted-foreground)]">Links</h4>
          <ul className="m-0 pl-4 text-[var(--text-dense-meta)]">
            {edges.map(e => (
              <li key={e.id}>
                {e.label} → {e.status}
              </li>
            ))}
          </ul>
        </>
      )}
    </>
  )
}

function TargetDetail({
  targetId,
  matrix,
  context,
}: {
  targetId: string
  matrix: MatrixResponse
  context: OpsContextResponse | undefined
}) {
  const t = getTarget(matrix, targetId)
  const layers = buildScopeLayers(matrix)
  const layer = layers.find(l => l.targetIds.includes(targetId))
  const hint = context?.probe_hints.find(h => h.target_id === targetId)

  return (
    <>
      {t != null ? (
        <table className="dense-table">
          <tbody>
            <tr>
              <th className="text-left">Reachability</th>
              <td>{t.reachability}</td>
            </tr>
            <tr>
              <th className="text-left">Category</th>
              <td>{t.category}</td>
            </tr>
            <tr>
              <th className="text-left">Detail</th>
              <td className="text-[var(--muted-foreground)]">{t.detail}</td>
            </tr>
            {t.url != null && (
              <tr>
                <th className="text-left">URL</th>
                <td className="font-mono-tabular text-[var(--muted-foreground)]">{t.url}</td>
              </tr>
            )}
          </tbody>
        </table>
      ) : (
        <p className="m-0 text-[var(--muted-foreground)]">No probe row for this target.</p>
      )}
      {layer != null && (
        <p className="m-0 mt-3 text-[var(--text-dense-meta)]">
          <strong>{layer.tag}</strong> — {layer.technology}
        </p>
      )}
      {hint != null && (
        <p className="m-0 mt-2 text-[var(--text-dense-meta)] text-[var(--muted-foreground)]">
          Trade route: <code className="font-mono-tabular">{hint.trade_route}</code> — {hint.hint}
        </p>
      )}
    </>
  )
}

function ScopeDetail({
  tag,
  matrix,
}: {
  tag: string
  matrix: MatrixResponse | undefined
}) {
  const layer = buildScopeLayers(matrix).find(l => l.tag === tag)
  if (!layer) return null
  return (
    <>
      <p className="m-0 font-medium">{layer.component}</p>
      <p className="m-0 mt-1 text-[var(--text-dense)]">{layer.technology}</p>
      <p className="m-0 mt-2 text-[var(--text-dense-meta)] text-[var(--muted-foreground)]">{layer.notes}</p>
    </>
  )
}

function EdgeDetail({
  edgeId,
  topology,
  matrix,
}: {
  edgeId: string
  topology: TopologyResponse
  matrix: MatrixResponse | undefined
}) {
  const edge = getEdge(topology, edgeId)
  if (!edge) return <p className="m-0 text-[var(--muted-foreground)]">Edge not found.</p>
  const fromNode = getNode(topology, edge.from)
  const toNode = getNode(topology, edge.to)
  const target = edge.matrix_target ? getTarget(matrix, edge.matrix_target) : undefined

  return (
    <>
      <table className="dense-table">
        <tbody>
          <tr>
            <th className="text-left">From</th>
            <td>{fromNode?.label ?? edge.from}</td>
          </tr>
          <tr>
            <th className="text-left">To</th>
            <td>{toNode?.label ?? edge.to}</td>
          </tr>
          <tr>
            <th className="text-left">Kind</th>
            <td>{edge.kind}</td>
          </tr>
          <tr>
            <th className="text-left">Status</th>
            <td>{edge.status}</td>
          </tr>
          {edge.matrix_target && (
            <tr>
              <th className="text-left">Matrix target</th>
              <td className="font-mono-tabular">{edge.matrix_target}</td>
            </tr>
          )}
        </tbody>
      </table>
      {target != null && (
        <p className="m-0 mt-3 text-[var(--text-dense-meta)]">
          Probe: <strong>{target.reachability}</strong> — {target.detail}
        </p>
      )}
      {edge.detail && (
        <p className="m-0 mt-2 text-[var(--text-dense-meta)] text-[var(--muted-foreground)]">
          {edge.detail}
        </p>
      )}
    </>
  )
}
