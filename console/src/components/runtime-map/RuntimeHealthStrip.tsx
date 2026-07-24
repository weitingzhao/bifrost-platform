import { Button } from '@bifrost/ui'
import type { MatrixResponse, TopologyResponse } from '@/api/matrixTypes'
import { StatusLamp } from '@/components/StatusLamp'
import { summarizeMatrix } from '@/lib/control-room/matrixSummary'
import type { GapOverview } from '@/lib/runtime-map/gapAnalysis'
import {
  getAffectedNodeIds,
  getPrimaryFailure,
} from '@/lib/runtime-map/runtimeMapHealth'

function gapPctClass(pct: number): string {
  if (pct >= 80) return 'gap-pct--high'
  if (pct >= 40) return 'gap-pct--mid'
  if (pct > 0) return 'gap-pct--low'
  return 'gap-pct--zero'
}

interface RuntimeHealthStripProps {
  topology: TopologyResponse | undefined
  matrix: MatrixResponse | undefined
  gapOverview?: GapOverview
  onSelectTarget: (targetId: string) => void
  onSelectNode: (nodeId: string) => void
  onOpenCluster?: () => void
}

export function RuntimeHealthStrip({
  topology,
  matrix,
  gapOverview,
  onSelectTarget,
  onSelectNode,
  onOpenCluster,
}: RuntimeHealthStripProps) {
  if (!topology || !matrix) return null

  const summary = summarizeMatrix(matrix)
  const primary = getPrimaryFailure(matrix)

  return (
    <section
      className="runtime-health-strip page-section panel-elevated px-4 py-2.5"
      aria-label="Runtime map verdict"
    >
      <div className="flex flex-wrap items-center gap-x-3 gap-y-2 min-w-0">
        <div className="flex items-center gap-2 shrink-0">
          <StatusLamp value={summary.worstReach} kind="reach" />
          <strong className="text-sm tracking-wide">RUNTIME VERDICT</strong>
          <span className="text-[var(--text-dense-meta)] text-muted-foreground">{matrix.label}</span>
        </div>

        <span className="runtime-health-strip__stat text-[var(--text-dense-meta)]">
          ok <span className="font-mono-tabular">{summary.ok}</span>
        </span>

        {summary.fail > 0 ? (
          <Button
            variant="ghost"
            size="xs"
            className="runtime-health-strip__fail lamp-fail"
            onClick={() => {
              if (primary) onSelectTarget(primary.id)
            }}
          >
            fail <span className="font-mono-tabular">{summary.fail}</span>
          </Button>
        ) : (
          <span className="runtime-health-strip__stat text-[var(--text-dense-meta)]">
            fail <span className="font-mono-tabular">0</span>
          </span>
        )}

        <span className="runtime-health-strip__stat text-[var(--text-dense-meta)]">
          degraded <span className="font-mono-tabular">{summary.degraded}</span>
        </span>

        {primary != null && (
          <span className="min-w-0 flex-1 truncate text-[var(--text-dense-meta)] text-[var(--muted-foreground)]">
            worst:{' '}
            <Button
              variant="ghost"
              size="xs"
              className="font-mono-tabular p-0 min-h-0"
              onClick={() => onSelectTarget(primary.id)}
            >
              {primary.id}
            </Button>
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
                  <Button
                    variant="ghost"
                    size="xs"
                    className="p-0 min-h-0"
                    onClick={() => onSelectNode(nodeId)}
                  >
                    {node.label}
                  </Button>
                  {i < arr.length - 1 ? ', ' : ''}
                </span>
              )
            })}
          </span>
        )}

        {onOpenCluster != null && (
          <Button
            type="button"
            size="sm"
            variant="outline"
            className="ml-auto shrink-0"
            onClick={onOpenCluster}
          >
            Open Cluster
          </Button>
        )}
      </div>

      {gapOverview != null && gapOverview.totalComponents > 0 && (
        <div className="gap-overview-strip mt-2">
          <span className="gap-overview-strip__item">
            completion{' '}
            <span className={`font-mono-tabular gap-pct ${gapPctClass(gapOverview.overallCompletionPct)}`}>
              {gapOverview.overallCompletionPct}%
            </span>
          </span>
          <span className="gap-overview-strip__item">
            live <span className="font-mono-tabular">{gapOverview.liveComponents}</span>
          </span>
          <span className="gap-overview-strip__item">
            planned <span className="font-mono-tabular">{gapOverview.plannedComponents}</span>
          </span>
          {gapOverview.failComponents > 0 && (
            <span className="gap-overview-strip__item">
              fail <span className="font-mono-tabular lamp-fail">{gapOverview.failComponents}</span>
            </span>
          )}
          <span className="gap-overview-strip__item text-[var(--muted-foreground)]">
            gap{' '}
            <span className="font-mono-tabular">
              {gapOverview.plannedComponents + gapOverview.failComponents}
            </span>
          </span>
        </div>
      )}
    </section>
  )
}
