import { useMemo, useState } from 'react'
import { useQuery } from '@tanstack/react-query'
import { Handle, MarkerType, Position, ReactFlow, type Edge, type Node, type NodeProps } from '@xyflow/react'
import '@xyflow/react/dist/style.css'
import { OpsSection } from '@/components/layout/OpsSection'
import { contractGraph, fetchBusContract, type BusContract } from './busNextgen'
import { fetchBusClients } from './useBusStream'

type Col = 'publisher' | 'topic' | 'subscriber'
type BoxData = { label: string; col: Col; tone: 'live' | 'lost' | 'plain'; dim: boolean; sub?: string }
type BoxNode = Node<BoxData, 'box'>

const W = 210
const H = 30
const GAP = 8
const X: Record<Col, number> = { publisher: 0, topic: 300, subscriber: 600 }
const hidden = { opacity: 0, width: 1, height: 1, minWidth: 0, minHeight: 0, border: 0 }

function BoxView({ data }: NodeProps<BoxNode>) {
  const border = data.tone === 'lost' ? 'var(--color-lamp-red)' : data.tone === 'live' ? 'var(--color-lamp-green)' : 'var(--muted-foreground)'
  return (
    <div className="flex items-center rounded border bg-[var(--card-fill)] px-2" style={{ width: W, height: H, borderColor: border, opacity: data.dim ? 0.3 : 1 }}
      title={data.sub ? `${data.label} · ${data.sub}` : data.label}>
      <span className="min-w-0 truncate font-mono-tabular text-[11px] text-[var(--foreground)]">{data.label}</span>
      <Handle type="target" position={Position.Left} isConnectable={false} style={hidden} />
      <Handle type="source" position={Position.Right} isConnectable={false} style={hidden} />
    </div>
  )
}
const nodeTypes = { box: BoxView }

/** Status of a declared user from the live roster, matched by name. */
function toneOf(name: string, roster: Map<string, string>): BoxData['tone'] {
  const s = roster.get(name)
  if (s === 'lost') return 'lost'
  if (s === 'active') return 'live'
  return 'plain'
}

export function ContractGraph({ contract, roster }: { contract: BusContract; roster: Map<string, string> }) {
  const [focus, setFocus] = useState<string | null>(null)
  const model = useMemo(() => contractGraph(contract), [contract])
  const { nodes, edges, height } = useMemo(() => {
    const near = new Set<string>()
    if (focus) {
      near.add(focus)
      for (const e of model.edges) {
        if (e.from === focus) near.add(e.to)
        if (e.to === focus) near.add(e.from)
      }
    }
    const col = (ids: string[], c: Col, prefix: string, sub?: (id: string) => string | undefined): BoxNode[] =>
      ids.map((id, i) => ({
        id: `${prefix}:${id}`, type: 'box', position: { x: X[c], y: i * (H + GAP) }, draggable: false,
        data: { label: id, col: c, tone: c === 'subscriber' ? toneOf(id, roster) : 'plain', dim: focus != null && !near.has(`${prefix}:${id}`), sub: sub?.(id) },
      }))
    const versions = new Map(contract.topics.map(t => [t.type, `v${t.version} · ${t.guarantee}`]))
    const ns = [...col(model.publishers, 'publisher', 'p'), ...col(model.topics, 'topic', 't', id => versions.get(id)), ...col(model.subscribers, 'subscriber', 's')]
    const es: Edge[] = model.edges.map((e, i) => {
      const hot = focus != null && (e.from === focus || e.to === focus)
      return {
        id: `e${i}`, source: e.from, target: e.to, label: hot ? e.label : undefined,
        style: { stroke: e.kind === 'wakes' ? 'var(--primary)' : 'var(--muted-foreground)', strokeWidth: hot ? 2 : 1, opacity: focus == null || hot ? 0.8 : 0.08 },
        markerEnd: { type: MarkerType.ArrowClosed, width: 12, height: 12 },
      }
    })
    const rows = Math.max(model.publishers.length, model.topics.length, model.subscribers.length)
    return { nodes: ns, edges: es, height: Math.min(640, Math.max(240, rows * (H + GAP) * 0.6)) }
  }, [model, contract, roster, focus])
  return (
    <div className="flex w-full min-w-0 flex-col gap-1">
      <div className="flex gap-6 text-xs text-muted-foreground">
        <span style={{ width: W }}>Publishers ({model.publishers.length})</span>
        <span style={{ width: W }}>Topics ({model.topics.length})</span>
        <span>Subscribers ({model.subscribers.length}) · green live, red lost, blue arrow a wake</span>
      </div>
      <div className="w-full rounded border border-[var(--card-border)]" style={{ height }} data-testid="bus-contract-graph">
        <ReactFlow nodes={nodes} edges={edges} nodeTypes={nodeTypes} fitView minZoom={0.2} nodesConnectable={false} nodesDraggable={false}
          onNodeClick={(_, n) => setFocus(f => (f === n.id ? null : n.id))} onPaneClick={() => setFocus(null)} proOptions={{ hideAttribution: true }} />
      </div>
      <p className="m-0 text-xs text-muted-foreground">Click a box to see what it is joined to.</p>
    </div>
  )
}

/**
 * Bus → Contract: what the bus promises and to whom, from its registers
 * (topics with version and publisher, who may read them, the subscription
 * rows' wakes), with who is live or lost from the roster.
 */
export function BusContractSection() {
  const contract = useQuery({ queryKey: ['bus-contract'], queryFn: fetchBusContract, staleTime: 300_000, retry: false })
  const clients = useQuery({ queryKey: ['bus', 'clients'], queryFn: fetchBusClients, refetchInterval: 60_000 })
  const roster = useMemo(() => new Map((clients.data?.clients ?? []).map(c => [c.name, c.status])), [clients.data])
  const c = contract.data
  let body
  if (contract.isError) body = <p className="m-0 text-sm text-muted-foreground">{`Could not read the contract: ${contract.error instanceof Error ? contract.error.message : 'request failed'}`}</p>
  else if (!c) body = <p className="m-0 text-sm text-muted-foreground">Loading…</p>
  else body = (
    <div className="flex w-full min-w-0 flex-col gap-2">
      <ContractGraph contract={c} roster={roster} />
      <p className="m-0 text-xs text-muted-foreground">
        {c.topics.length} topics, {c.subscriptions.length} subscription rows, {c.clients.length} declared users, read from the registers compiled into the platform (bus/registry). Who is live or lost: Clients below.
      </p>
    </div>
  )
  return <OpsSection title="Contract" bodyPadding="compact">{body}</OpsSection>
}
