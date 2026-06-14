import type { MatrixResponse, TopologyResponse } from '@/api/types'
import { StatusLamp } from '@/components/StatusLamp'
import { summarizeMatrix } from '@/lib/control-room/matrixSummary'
import {
  getAffectedNodeIds,
  getPrimaryFailure,
} from '@/lib/runtime-map/runtimeMapHealth'

interface RuntimeHealthStripProps {
  topology: TopologyResponse | undefined
  matrix: MatrixResponse | undefined
  onSelectTarget: (targetId: string) => void
  onSelectNode: (nodeId: string) => void
}

export function RuntimeHealthStrip({
  topology,
  matrix,
  onSelectTarget,
  onSelectNode,
}: RuntimeHealthStripProps) {
  if (!topology || !matrix) return null

  const summary = summarizeMatrix(matrix)
  const primary = getPrimaryFailure(matrix)

  return (
    <section className="runtime-health-strip page-section panel-elevated px-4 py-2.5">
      <div className="flex flex-wrap items-center gap-x-3 gap-y-2 min-w-0">
        <div className="flex items-center gap-2 shrink-0">
          <StatusLamp value={summary.worstReach} kind="reach" />
          <strong className="text-sm">{matrix.label}</strong>
        </div>

        <span className="runtime-health-strip__stat text-[var(--text-dense-meta)]">
          ok <span className="font-mono-tabular">{summary.ok}</span>
        </span>

        {summary.fail > 0 ? (
          <button
            type="button"
            className="runtime-health-strip__fail btn-ui btn-ui-ghost text-xs lamp-fail"
            onClick={() => {
              if (primary) onSelectTarget(primary.id)
            }}
          >
            fail <span className="font-mono-tabular">{summary.fail}</span>
          </button>
        ) : (
          <span className="runtime-health-strip__stat text-[var(--text-dense-meta)]">
            fail <span className="font-mono-tabular">0</span>
          </span>
        )}

        <span className="runtime-health-strip__stat text-[var(--text-dense-meta)]">
          degraded <span className="font-mono-tabular">{summary.degraded}</span>
        </span>

        {primary != null && (
          <span className="text-[var(--text-dense-meta)] text-[var(--muted-foreground)]">
            worst:{' '}
            <button
              type="button"
              className="btn-ui btn-ui-ghost text-xs font-mono-tabular p-0 min-h-0"
              onClick={() => onSelectTarget(primary.id)}
            >
              {primary.id}
            </button>
          </span>
        )}

        {primary != null && topology != null && (
          <span className="text-[var(--text-dense-meta)] text-[var(--muted-foreground)]">
            hosts:{' '}
            {getAffectedNodeIds(topology, primary.id).map((nodeId, i, arr) => {
              const node = topology.nodes.find(n => n.id === nodeId)
              if (!node) return null
              return (
                <span key={nodeId}>
                  <button
                    type="button"
                    className="btn-ui btn-ui-ghost text-xs p-0 min-h-0"
                    onClick={() => onSelectNode(nodeId)}
                  >
                    {node.label}
                  </button>
                  {i < arr.length - 1 ? ', ' : ''}
                </span>
              )
            })}
          </span>
        )}
      </div>

      <details className="runtime-health-strip__help mt-2">
        <summary className="text-[10px] text-[var(--muted-foreground)] cursor-pointer">
          About Runtime Map
        </summary>
        <p className="m-0 mt-1 text-[var(--text-dense-meta)] text-[var(--muted-foreground)]">
          Hardware topology, SCOPE stack, and live matrix probes. Select a node, edge, or chip to
          sync both panels.
        </p>
      </details>
    </section>
  )
}
