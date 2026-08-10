import { useMemo, useState } from 'react'
import {
  AGENT_TASK_RELATIONS,
  agentTaskRelationKindLabel,
  agentTasksByDomain,
  allAgentTasks,
  type AgentTaskEntry,
  type AgentTaskRelationKind,
} from '@/lib/agent/agentTaskCatalog'
import {
  liveStatusStroke,
  type AgentCapabilityLiveStatus,
} from '@/lib/agent/agentCapabilityViewModel'

/**
 * Deterministic node+edge graph of the agent capabilities.
 * Rows = domains (top→bottom). Columns = action depth band:
 *   0 Read/Observe · 1 Act/Write · 2 Escalation.
 * Edges = AGENT_TASK_RELATIONS (escalation / approval / on-failure), each
 * with a distinct stroke so "who triggers whom" reads at a glance.
 *
 * Optional liveStatusByTaskId overlays StatusLamp-consistent colors without
 * changing Governance → Agent System (design-only default).
 */

const COL_X = [168, 410, 638]
const COL_LABELS = ['Read · Observe', 'Act · Write', 'Escalation']
const NODE_W = 152
const NODE_H = 44
const ROW_GAP = 72
const TOP_Y = 64
const LABEL_X = 8
const VB_W = 740

const READ_ACTIONS = new Set(['Session', 'Brief', 'Check'])
const ESCALATION_ACTIONS = new Set(['Release Fix'])

function actionBand(action: string): number {
  if (ESCALATION_ACTIONS.has(action)) return 2
  if (READ_ACTIONS.has(action)) return 0
  return 1
}

function tierStroke(tier: AgentTaskEntry['tier']): string {
  if (tier === 'manual') return 'var(--color-lamp-green)'
  if (tier === 'automated') return 'var(--color-lamp-blue, var(--primary))'
  return 'var(--color-lamp-yellow)'
}

function edgeClass(kind: AgentTaskRelationKind): string {
  return `agent-graph-edge agent-graph-edge--${kind}`
}

interface NodePos {
  task: AgentTaskEntry
  cx: number
  cy: number
}

export type AgentSystemGraphProps = {
  /** When set, node stroke/fill follows live status instead of design tier. */
  liveStatusByTaskId?: Record<string, AgentCapabilityLiveStatus>
  /** Soft-highlight relation keys `fromId→toId` (Wave 2 escalation focus). */
  highlightedEdgeKeys?: ReadonlySet<string> | string[]
  /** Dim nodes whose ids are not in this set (filter chips). */
  visibleTaskIds?: ReadonlySet<string> | string[]
  onNodeClick?: (taskId: string) => void
}

function asIdSet(ids: ReadonlySet<string> | string[] | undefined): Set<string> | null {
  if (ids == null) return null
  return ids instanceof Set ? ids : new Set(ids)
}

export function AgentSystemGraph({
  liveStatusByTaskId,
  highlightedEdgeKeys,
  visibleTaskIds,
  onNodeClick,
}: AgentSystemGraphProps = {}) {
  const [hoverId, setHoverId] = useState<string | null>(null)
  // Catalog hydrates async from GET /api/v1/agent-tasks — must recompute when it fills.
  const tasks = allAgentTasks()
  const liveMode = liveStatusByTaskId != null
  const highlightSet = useMemo(() => asIdSet(highlightedEdgeKeys), [highlightedEdgeKeys])
  const visibleSet = useMemo(() => asIdSet(visibleTaskIds), [visibleTaskIds])

  const { nodes, vbHeight } = useMemo(() => {
    const groups = agentTasksByDomain()
    const placed: NodePos[] = []
    groups.forEach((group, rowIdx) => {
      const cy = TOP_Y + rowIdx * ROW_GAP
      for (const task of group.tasks) {
        const col = actionBand(task.action)
        placed.push({ task, cx: COL_X[col], cy })
      }
    })
    const height =
      tasks.length === 0 || groups.length === 0
        ? 80
        : TOP_Y + (groups.length - 1) * ROW_GAP + NODE_H / 2 + 28
    return { nodes: placed, vbHeight: height }
  }, [tasks])

  const posById = useMemo(() => {
    const m = new Map<string, NodePos>()
    for (const n of nodes) m.set(n.task.id, n)
    return m
  }, [nodes])

  const domainRows = useMemo(() => {
    const seen = new Map<string, number>()
    for (const n of nodes) if (!seen.has(n.task.domain)) seen.set(n.task.domain, n.cy)
    return [...seen.entries()].map(([domain, cy]) => ({ domain, cy }))
  }, [nodes])

  const edges = AGENT_TASK_RELATIONS.map(rel => {
    const from = posById.get(rel.fromId)
    const to = posById.get(rel.toId)
    if (from == null || to == null) return null
    const sx = from.cx + NODE_W / 2
    const sy = from.cy
    const tx = to.cx - NODE_W / 2
    const ty = to.cy
    const dx = Math.max(28, (tx - sx) / 2)
    const path = `M ${sx} ${sy} C ${sx + dx} ${sy}, ${tx - dx} ${ty}, ${tx} ${ty}`
    const edgeKey = `${rel.fromId}→${rel.toId}`
    const softHot = highlightSet?.has(edgeKey) ?? false
    const active =
      hoverId == null || hoverId === rel.fromId || hoverId === rel.toId || softHot
    return { rel, path, active, softHot, edgeKey, mx: (sx + tx) / 2, my: (sy + ty) / 2 }
  }).filter((e): e is NonNullable<typeof e> => e != null)

  return (
    <div className="agent-graph">
      {tasks.length === 0 ? (
        <p className="m-0 text-[var(--text-dense-meta)] text-[var(--muted-foreground)]">
          Loading capability map from platform-api…
        </p>
      ) : (
      <>
      <svg
        className="agent-graph__svg"
        viewBox={`0 0 ${VB_W} ${vbHeight}`}
        role="img"
        aria-label={liveMode ? 'Live agent capability map' : 'Agent capability relationship graph'}
        preserveAspectRatio="xMidYMid meet"
      >
        <defs>
          {(['escalation', 'approval', 'on-failure'] as AgentTaskRelationKind[]).map(kind => (
            <marker
              key={kind}
              id={`agent-graph-arrow-${kind}`}
              viewBox="0 0 10 10"
              refX="9"
              refY="5"
              markerWidth="7"
              markerHeight="7"
              orient="auto-start-reverse"
            >
              <path d="M 0 0 L 10 5 L 0 10 z" className={`agent-graph-arrow agent-graph-arrow--${kind}`} />
            </marker>
          ))}
        </defs>

        {/* Column headers */}
        {COL_LABELS.map((label, i) => (
          <text key={label} x={COL_X[i]} y={26} textAnchor="middle" className="agent-graph__col-label">
            {label}
          </text>
        ))}

        {/* Domain row labels */}
        {domainRows.map(row => (
          <text key={row.domain} x={LABEL_X} y={row.cy + 3} className="agent-graph__row-label">
            {row.domain}
          </text>
        ))}

        {/* Edges */}
        {edges.map(({ rel, path, active, softHot }) => (
          <path
            key={`${rel.fromId}-${rel.toId}`}
            d={path}
            className={`${edgeClass(rel.kind)}${active ? '' : ' agent-graph-edge--dim'}${softHot ? ' agent-graph-edge--live-hot' : ''}`}
            markerEnd={`url(#agent-graph-arrow-${rel.kind})`}
          >
            <title>{`${agentTaskRelationKindLabel(rel.kind)} — ${rel.label}`}</title>
          </path>
        ))}

        {/* Nodes */}
        {nodes.map(({ task, cx, cy }) => {
          const filteredOut = visibleSet != null && !visibleSet.has(task.id)
          const dim =
            filteredOut ||
            (hoverId != null &&
              hoverId !== task.id &&
              !edges.some(
                e =>
                  (e.rel.fromId === hoverId && e.rel.toId === task.id) ||
                  (e.rel.toId === hoverId && e.rel.fromId === task.id),
              ))
          const liveStatus = liveStatusByTaskId?.[task.id]
          const stroke =
            liveStatus != null ? liveStatusStroke(liveStatus) : tierStroke(task.tier)
          const clickable = onNodeClick != null
          return (
            <g
              key={task.id}
              className={`agent-graph-node${dim ? ' agent-graph-node--dim' : ''}${clickable ? ' agent-graph-node--clickable' : ''}${liveStatus != null ? ` agent-graph-node--live-${liveStatus}` : ''}`}
              onMouseEnter={() => setHoverId(task.id)}
              onMouseLeave={() => setHoverId(null)}
              onClick={clickable ? () => onNodeClick(task.id) : undefined}
              style={clickable ? { cursor: 'pointer' } : undefined}
            >
              <title>
                {liveStatus != null
                  ? `${task.label} — ${liveStatus}${task.description ? ` · ${task.description}` : ''}`
                  : `${task.label} — ${task.description}`}
              </title>
              <rect
                x={cx - NODE_W / 2}
                y={cy - NODE_H / 2}
                width={NODE_W}
                height={NODE_H}
                rx={8}
                className="agent-graph-node__box"
                style={{
                  stroke,
                  ...(liveStatus === 'running' || liveStatus === 'awaiting'
                    ? {
                        fill: `color-mix(in srgb, ${stroke} 12%, var(--card))`,
                      }
                    : liveStatus === 'failed'
                      ? {
                          fill: `color-mix(in srgb, ${stroke} 10%, var(--card))`,
                        }
                      : {}),
                }}
              />
              <text x={cx} y={cy - 3} textAnchor="middle" className="agent-graph-node__label">
                {task.label}
              </text>
              <text x={cx} y={cy + 12} textAnchor="middle" className="agent-graph-node__action">
                {liveStatus != null ? `${task.action} · ${liveStatus}` : task.action}
              </text>
            </g>
          )
        })}
      </svg>

      <div className="agent-graph__legend">
        {(['escalation', 'approval', 'on-failure'] as AgentTaskRelationKind[]).map(kind => (
          <span key={kind} className="agent-graph__legend-item">
            <span className={`agent-graph__legend-line agent-graph__legend-line--${kind}`} />
            {agentTaskRelationKindLabel(kind)}
          </span>
        ))}
      </div>
      </>
      )}
    </div>
  )
}
