import { CiDomainDirections } from './CiDomainDirections'
import { useMemo } from 'react'
import { BaseEdge, EdgeLabelRenderer, Handle, MarkerType, Position, ReactFlow, getBezierPath, getStraightPath, type Edge, type EdgeProps, type Node, type NodeProps } from '@xyflow/react'
import '@xyflow/react/dist/style.css'
import type { CiDomainGraph } from '@/api/ciDomains'
import { useNarrow } from './useNarrow'
import { domainPanel, edgeSides, graphLayout, graphSize, median, touchedRuns, type ChangeMap, type PageRun } from './ciPageModel'

/*
 * Work > CI > By dependency: a node a domain, an arrow the imports of one domain's packages from another's with their
 * count. An arrow that points the wrong way (the platform's rule, the same one the domain score uses) is red and
 * thicker. Selecting a domain lights what it depends on and what depends on it; the side panel says it in words.
 */

type DomainNodeData = { label: string; sub: string; selected: boolean; dim: boolean; width: number; height: number }
type DomainNode = Node<DomainNodeData, 'domain'>

const SIDES = [['top', Position.Top], ['bottom', Position.Bottom], ['left', Position.Left], ['right', Position.Right]] as const
const hidden = { opacity: 0, width: 1, height: 1, minWidth: 0, minHeight: 0, border: 0 }

/** A source and a target handle on every side, apart from each other, so two arrows between the same nodes do not lie on one line. */
function DomainNodeView({ data }: NodeProps<DomainNode>) {
  return (
    <div data-testid={`ci-dep-node-${data.label}`} role="button" tabIndex={0} aria-pressed={data.selected}
      className={`flex cursor-pointer flex-col justify-center rounded-lg border px-2.5 ${data.selected ? 'border-2 border-[var(--primary)] bg-[var(--accent)]' : 'border-[var(--muted-foreground)] bg-[var(--card-fill)]'}`}
      style={{ width: data.width, height: data.height, opacity: data.dim ? 0.4 : 1 }}>
      <span className="text-sm font-medium text-[var(--foreground)]">{data.label}</span>
      <span className="text-[11px] text-[var(--muted-foreground)]">{data.sub}</span>
      {SIDES.map(([side, position]) => {
        const along = side === 'top' || side === 'bottom' ? 'left' : 'top'
        return [
          <Handle key={`s-${side}`} id={`s-${side}`} type="source" position={position} isConnectable={false} style={{ ...hidden, [along]: '38%' }} />,
          <Handle key={`t-${side}`} id={`t-${side}`} type="target" position={position} isConnectable={false} style={{ ...hidden, [along]: '62%' }} />,
        ]
      })}
    </div>
  )
}
const nodeTypes = { domain: DomainNodeView }

type ImportEdgeData = { count: number; wrongWay: boolean; hot: boolean; arc: boolean; opacity: number }
type ImportEdge = Edge<ImportEdgeData, 'imports'>

/** An arrow with its count on it. The count is an HTML label at the arrow's middle, so it needs no text measuring. */
function ImportEdgeView({ sourceX, sourceY, targetX, targetY, sourcePosition, targetPosition, data, style, markerEnd }: EdgeProps<ImportEdge>) {
  const [path, labelX, labelY] = data?.arc
    ? getBezierPath({ sourceX, sourceY, targetX, targetY, sourcePosition, targetPosition })
    : getStraightPath({ sourceX, sourceY, targetX, targetY })
  return <>
    <BaseEdge path={path} style={style} markerEnd={markerEnd} />
    <EdgeLabelRenderer>
      <span className="pointer-events-none absolute rounded bg-[var(--card-fill)] px-1 text-[11px] leading-4" style={{ transform: `translate(-50%, -50%) translate(${labelX}px, ${labelY}px)`, opacity: data?.opacity, color: data?.wrongWay ? 'var(--color-lamp-red)' : 'var(--foreground)' }}>
        {`${data?.count}${data?.wrongWay ? ' wrong way' : ''}`}
      </span>
    </EdgeLabelRenderer>
  </>
}
const edgeTypes = { imports: ImportEdgeView }

export function CiDependencyGraph({ graph, runs, changes, domain, asOf, onSelectDomain }: {
  graph: CiDomainGraph; runs: PageRun[]; changes: ChangeMap; domain: string; asOf: string; onSelectDomain: (id: string) => void
}) {
  const selected = graph.domains.some(d => d.id === domain) ? domain : ''
  const narrow = useNarrow()
  const { nodes, edges, size } = useMemo(() => {
    const layout = graphLayout(graph, narrow)
    const at = new Map(layout.map(n => [n.id, n]))
    const near = new Set([selected])
    for (const e of graph.edges) { if (e.from === selected) near.add(e.to); if (e.to === selected) near.add(e.from) }
    const nodes: DomainNode[] = layout.map(n => {
      const mine = touchedRuns(runs, n.id, changes)
      const red = mine.filter(r => r.ok === false).length
      return {
        id: n.id, type: 'domain', position: { x: n.x, y: n.y }, draggable: false, selectable: false, connectable: false,
        data: { label: n.id, sub: `${n.packages} packages · ${mine.length} runs${red ? ` · ${red} red` : ''}`, selected: n.id === selected, dim: selected !== '' && !near.has(n.id), width: n.width, height: n.height },
      }
    })
    const edges: ImportEdge[] = graph.edges.filter(e => at.has(e.from) && at.has(e.to)).map(e => {
      const hot = selected !== '' && (e.from === selected || e.to === selected)
      const [out, into, shape] = edgeSides(at.get(e.from)!, at.get(e.to)!)
      // Every arrow is plainly visible with nothing selected; with a selection the others step back, a wrong-way one less.
      const color = e.wrong_way ? 'var(--color-lamp-red)' : hot ? 'var(--primary)' : 'var(--muted-foreground)'
      const opacity = selected === '' || hot ? 1 : e.wrong_way ? 0.75 : 0.3
      return {
        id: `${e.from}>${e.to}`, source: e.from, target: e.to, sourceHandle: `s-${out}`, targetHandle: `t-${into}`, type: 'imports', selectable: false, focusable: false,
        style: { stroke: color, strokeWidth: e.wrong_way ? 2.6 : hot ? 2.2 : 1.5, opacity },
        markerEnd: { type: MarkerType.ArrowClosed, color, width: 16, height: 16 },
        data: { count: e.imports, wrongWay: e.wrong_way, hot, arc: shape === 'default', opacity },
      }
    })
    return { nodes, edges, size: graphSize(layout) }
  }, [graph, runs, changes, selected, narrow])

  const panel = selected ? domainPanel(graph, selected) : null
  const dayAgo = Date.parse(asOf) - 24 * 3600_000
  const mine = selected ? touchedRuns(runs, selected, changes).filter(r => Date.parse(r.at) >= dayAgo) : []
  const took = median(mine.filter(r => r.ok != null && r.took != null).map(r => r.took!))
  const edgeText = (list: { id: string; imports: number; wrongWay: boolean }[], none: string) => list.map(e => `${e.id} (${e.imports}${e.wrongWay ? ', wrong way' : ''})`).join(', ') || none
  const wrong = graph.edges.filter(e => e.wrong_way)
  return (
    <div className="flex min-w-0 flex-col gap-4">
    <div className="grid min-w-0 gap-3.5 lg:grid-cols-[minmax(0,2.4fr)_minmax(220px,1fr)]">
      <div className="min-w-0">
        {/* The frame has the drawing's own shape, so the graph fills it. */}
        <div key={narrow ? 'narrow' : 'wide'} className="ci-dep-graph w-full min-w-0 rounded-lg border border-[var(--table-rule)]" style={{ aspectRatio: `${size.width + 60} / ${size.height + 130}` }} data-testid="ci-dep-graph" role="group" aria-label="Domain dependency graph">
          <ReactFlow nodes={nodes} edges={edges} nodeTypes={nodeTypes} edgeTypes={edgeTypes} fitView fitViewOptions={{ padding: 0.1 }} minZoom={0.2} maxZoom={2}
            nodesDraggable={false} nodesConnectable={false} elementsSelectable={false} zoomOnScroll={false} zoomOnDoubleClick={false} panOnDrag={false} preventScrolling={false}
            onNodeClick={(_, node) => onSelectDomain(node.id === selected ? '' : node.id)} />
        </div>
        <p className="mb-0 mt-1.5 text-xs text-[var(--muted-foreground)]">
          {`An arrow points from the user to the used (A → B: A imports B). Production code at ${graph.commit.slice(0, 8)}; the number on an arrow is how many package pairs. Red = points the wrong way, by the domain score's rule: ${wrong.length === 0 ? 'none now' : wrong.map(e => `${e.from} → ${e.to}`).join(', ')}. `}
          {graph.shared_packages.length > 0 && `The ${graph.shared_packages.length} packages several domains share are not a node; they import every domain.`}
        </p>
      </div>
      <div className="min-w-0" data-testid="ci-dep-panel">
        {!panel ? <>
          <h3 className="ops-section-title m-0">No domain selected</h3>
          <p className="mb-0 mt-1 text-sm text-[var(--muted-foreground)]">Click a domain: what it depends on, who depends on it, and whose tests a change to it pulls in. The selection carries to By domain and By time.</p>
        </> : <>
          <h3 className="ops-section-title m-0">{selected}</h3>
          <dl className="m-0 mt-2.5 grid grid-cols-[minmax(72px,120px)_minmax(0,1fr)] gap-x-2.5 gap-y-1.5 text-sm">
            <dt className="text-[var(--muted-foreground)]">Depends on</dt><dd className="m-0">{edgeText(panel.dependsOn, 'nothing')}</dd>
            <dt className="text-[var(--muted-foreground)]">Depended on by</dt><dd className="m-0">{edgeText(panel.dependedOnBy, 'nobody')}</dd>
            <dt className="text-[var(--muted-foreground)]">A change here pulls in</dt>
            <dd className="m-0">{`its own ${panel.packages} packages${panel.dependedOnBy.length ? `, the importers in ${panel.dependedOnBy.map(e => e.id).join(', ')}` : ''}${panel.shared ? ', and the shared packages that import it' : ''}`}</dd>
            <dt className="text-[var(--muted-foreground)]">Last 24 h</dt>
            <dd className="m-0">{`${mine.length} runs, ${mine.filter(r => r.ok === false).length} red${took != null ? `, median ${Math.round(took)} s` : ''}`}</dd>
          </dl>
        </>}
      </div>
    </div>
    <CiDomainDirections graph={graph} domain={selected} onSelectDomain={onSelectDomain} />
    </div>
  )
}
