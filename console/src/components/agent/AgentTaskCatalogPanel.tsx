import { useState } from 'react'
import { ChevronDown, ChevronRight } from 'lucide-react'
import { DenseTag } from '@bifrost/ui'
import {
  AGENT_TASK_CATALOG,
  AGENT_TASK_DOCTRINE_LINKS,
  agentTaskTierLabel,
  escalationChildren,
  manualAgentTasks,
  type AgentTaskEntry,
  type AgentTaskTier,
} from '@/lib/agent/agentTaskCatalog'

type DoctrineTab = (typeof AGENT_TASK_DOCTRINE_LINKS)[number]['tab']

interface AgentTaskCatalogPanelProps {
  onOpenDoctrine?: (tab: DoctrineTab) => void
}

function tierVariant(tier: AgentTaskTier): 'success' | 'warning' | 'category' {
  if (tier === 'manual') return 'success'
  if (tier === 'automated') return 'category'
  return 'warning'
}

function TaskRow({ task, child }: { task: AgentTaskEntry; child?: boolean }) {
  const children = escalationChildren(task.id)
  return (
    <>
      <div className={`agent-task-catalog__row${child ? ' agent-task-catalog__row--child' : ''}`}>
        <div className="agent-task-catalog__row-head">
          <span className="agent-task-catalog__label">{task.label}</span>
          <DenseTag variant={tierVariant(task.tier)}>{agentTaskTierLabel(task.tier)}</DenseTag>
          <code className="agent-task-catalog__scope font-mono-tabular">{task.scope}</code>
        </div>
        <p className="agent-task-catalog__desc">{task.description}</p>
        <p className="agent-task-catalog__meta">
          <span className="agent-task-catalog__meta-label">Start:</span> {task.entryPoint}
          <span className="agent-task-catalog__meta-sep"> · </span>
          <span className="agent-task-catalog__meta-label">Trigger:</span> {task.trigger}
        </p>
      </div>
      {children.map(c => (
        <TaskRow key={c.id} task={c} child />
      ))}
    </>
  )
}

/** Top-level manual tasks + their escalation children; then automated tasks. */
function catalogRoots(): AgentTaskEntry[] {
  return AGENT_TASK_CATALOG.filter(t => t.tier !== 'escalation')
}

export function AgentTaskCatalogPanel({ onOpenDoctrine }: AgentTaskCatalogPanelProps) {
  const [open, setOpen] = useState(false)

  return (
    <section className="agent-task-catalog">
      <button
        type="button"
        className="agent-task-catalog__toggle"
        onClick={() => setOpen(v => !v)}
        aria-expanded={open}
      >
        {open ? <ChevronDown size={14} /> : <ChevronRight size={14} />}
        <span>Task capabilities</span>
        <span className="agent-task-catalog__summary">
          {manualAgentTasks().length} manual · {AGENT_TASK_CATALOG.filter(t => t.tier === 'automated').length}{' '}
          scheduled · Release → Release Fix escalation
        </span>
      </button>

      {open && (
        <div className="agent-task-catalog__body">
          <p className="agent-task-catalog__intro">
            <strong>Agent Desk</strong> exposes top-level scopes only (Ops, Release). Secondary tasks spawn from
            other pages or from a running Release when a phase fails.
          </p>

          <div className="agent-task-catalog__tree">
            {catalogRoots().map(task => (
              <TaskRow key={task.id} task={task} />
            ))}
          </div>

          {onOpenDoctrine != null && (
            <div className="agent-task-catalog__links">
              {AGENT_TASK_DOCTRINE_LINKS.map(link => (
                <button
                  key={link.tab}
                  type="button"
                  className="agent-task-catalog__link"
                  onClick={() => onOpenDoctrine(link.tab)}
                >
                  {link.label}
                  <span className="agent-task-catalog__link-hint">{link.hint}</span>
                </button>
              ))}
            </div>
          )}
        </div>
      )}
    </section>
  )
}
