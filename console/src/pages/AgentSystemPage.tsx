import {
  Button,
  DenseDataTable,
  DenseTableBody,
  DenseTableCell,
  DenseTableHead,
  DenseTableHeadRow,
  DenseTableHeader,
  DenseTableRow,
  DenseTag,
} from '@bifrost/ui'
import { CatalogSection } from '@/components/CatalogSection'
import { AgentSystemGraph } from '@/components/agent/AgentSystemGraph'
import { OpsSection } from '@/components/layout/OpsSection'
import {
  AGENT_RUNTIME,
  AGENT_TASK_CATALOG,
  AGENT_TASK_DOCTRINE_LINKS,
  AGENT_TASK_RELATIONS,
  agentTaskRelationKindLabel,
  agentTaskTierLabel,
  agentTasksByDomain,
  agentSystemSummary,
  catalogTaskById,
  type AgentTaskTier,
} from '@/lib/agent/agentTaskCatalog'

type DoctrineTab = (typeof AGENT_TASK_DOCTRINE_LINKS)[number]['tab']

function tierVariant(tier: AgentTaskTier): 'success' | 'warning' | 'category' {
  if (tier === 'manual') return 'success'
  if (tier === 'automated') return 'category'
  return 'warning'
}

interface AgentSystemPageProps {
  onOpenDoctrine?: (tab: DoctrineTab) => void
}

export function AgentSystemPage({ onOpenDoctrine }: AgentSystemPageProps) {
  const summary = agentSystemSummary()
  const domainGroups = agentTasksByDomain()

  return (
    <div className="flex w-full min-w-0 flex-col gap-4">
      <OpsSection
        title="Mental model"
        description="One remediation runner executes every capability below. Domains group related tasks; actions deepen from read → write → release."
        bodyPadding="compact"
        overflow="visible"
      >
        <div className="agent-system-kpis">
          <div className="agent-system-kpi">
            <span className="agent-system-kpi__value">{summary.runtimeCount}</span>
            <span className="agent-system-kpi__label">Runtime</span>
          </div>
          <div className="agent-system-kpi">
            <span className="agent-system-kpi__value">{summary.capabilityCount}</span>
            <span className="agent-system-kpi__label">Capabilities</span>
          </div>
          <div className="agent-system-kpi">
            <span className="agent-system-kpi__value">{summary.domainCount}</span>
            <span className="agent-system-kpi__label">Domains</span>
          </div>
          <div className="agent-system-kpi">
            <span className="agent-system-kpi__value">{summary.relationCount}</span>
            <span className="agent-system-kpi__label">Task chains</span>
          </div>
          <div className="agent-system-kpi agent-system-kpi--meta">
            <span className="agent-system-kpi__label">
              {summary.manualCount} manual · {summary.scheduledCount} scheduled ·{' '}
              {summary.escalationCount} escalation
            </span>
          </div>
        </div>
      </OpsSection>

      <CatalogSection title="Runtime">
        <div className="agent-system-runtime px-3 py-3">
          <div className="agent-system-runtime__head">
            <span className="agent-system-runtime__name">{AGENT_RUNTIME.label}</span>
            <code className="font-mono-tabular text-[var(--text-dense-meta)]">:{AGENT_RUNTIME.port}</code>
            <DenseTag variant="success">{AGENT_RUNTIME.sdk}</DenseTag>
          </div>
          <p className="agent-system-runtime__desc">{AGENT_RUNTIME.description}</p>
          <p className="agent-system-runtime__meta">
            Host: {AGENT_RUNTIME.host} · Storage: JSON per job (runner + platform-api archive)
          </p>
        </div>
      </CatalogSection>

      <CatalogSection title="Capabilities by domain">
        <div className="agent-system-domains px-3 py-3">
          {domainGroups.map(({ domain, tasks }) => (
            <div key={domain} className="agent-system-domain">
              <p className="agent-system-domain__title">{domain}</p>
              <ul className="agent-system-domain__list">
                {tasks.map(task => (
                  <li key={task.id} className="agent-system-domain__item">
                    <div className="agent-system-domain__item-head">
                      <span className="agent-system-domain__label">{task.label}</span>
                      <DenseTag variant={tierVariant(task.tier)}>{agentTaskTierLabel(task.tier)}</DenseTag>
                    </div>
                    <p className="agent-system-domain__action">{task.action}</p>
                    {task.parentId != null && (
                      <p className="agent-system-domain__parent">
                        Escalation of {catalogTaskById(task.parentId)?.label ?? task.parentId}
                      </p>
                    )}
                  </li>
                ))}
              </ul>
            </div>
          ))}
          <div className="agent-system-domain agent-system-domain--reserved">
            <p className="agent-system-domain__title">Trade</p>
            <p className="agent-system-domain__reserved">Reserved — Trade · Release (future)</p>
          </div>
        </div>
      </CatalogSection>

      <CatalogSection title="Capability map">
        <div className="px-3 py-3">
          <AgentSystemGraph />
          <div className="agent-system-chains agent-system-chains--under-graph">
            {AGENT_TASK_RELATIONS.map(rel => {
              const from = catalogTaskById(rel.fromId)
              const to = catalogTaskById(rel.toId)
              if (from == null || to == null) return null
              return (
                <div key={`${rel.fromId}-${rel.toId}`} className="agent-system-chain">
                  <span className="agent-system-chain__node">{from.label}</span>
                  <span className="agent-system-chain__edge">
                    <span className="agent-system-chain__kind">{agentTaskRelationKindLabel(rel.kind)}</span>
                    <span className="agent-system-chain__arrow" aria-hidden>
                      →
                    </span>
                    <span className="agent-system-chain__hint">{rel.label}</span>
                  </span>
                  <span className="agent-system-chain__node agent-system-chain__node--target">{to.label}</span>
                </div>
              )
            })}
          </div>
        </div>
      </CatalogSection>

      <CatalogSection title="Capability registry">
        <DenseDataTable>
          <DenseTableHeader>
            <DenseTableHeadRow>
              <DenseTableHead>Label</DenseTableHead>
              <DenseTableHead>Domain</DenseTableHead>
              <DenseTableHead>Action</DenseTableHead>
              <DenseTableHead>Tier</DenseTableHead>
              <DenseTableHead>Scope</DenseTableHead>
              <DenseTableHead>Entry point</DenseTableHead>
              <DenseTableHead>Trigger</DenseTableHead>
            </DenseTableHeadRow>
          </DenseTableHeader>
          <DenseTableBody>
            {AGENT_TASK_CATALOG.map(task => (
              <DenseTableRow key={task.id}>
                <DenseTableCell className="font-medium whitespace-nowrap">{task.label}</DenseTableCell>
                <DenseTableCell>{task.domain}</DenseTableCell>
                <DenseTableCell>{task.action}</DenseTableCell>
                <DenseTableCell>
                  <DenseTag variant={tierVariant(task.tier)}>{agentTaskTierLabel(task.tier)}</DenseTag>
                </DenseTableCell>
                <DenseTableCell className="font-mono-tabular text-xs">{task.scope}</DenseTableCell>
                <DenseTableCell className="text-[var(--muted-foreground)]">{task.entryPoint}</DenseTableCell>
                <DenseTableCell className="text-[var(--muted-foreground)]">{task.trigger}</DenseTableCell>
              </DenseTableRow>
            ))}
          </DenseTableBody>
        </DenseDataTable>
      </CatalogSection>

      {onOpenDoctrine != null && (
        <OpsSection title="Governance" bodyPadding="compact" overflow="visible">
          <p className="m-0 mb-2 text-[var(--text-dense-meta)] text-[var(--muted-foreground)]">
            Rules and tool contracts — this page is the map; doctrine pages are authoritative for behavior.
          </p>
          <div className="flex flex-wrap gap-2">
            {AGENT_TASK_DOCTRINE_LINKS.map(link => (
              <Button key={link.tab} variant="outline" size="sm" onClick={() => onOpenDoctrine(link.tab)}>
                {link.label}
              </Button>
            ))}
          </div>
        </OpsSection>
      )}
    </div>
  )
}
