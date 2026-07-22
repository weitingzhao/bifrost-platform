/**
 * Engineer → Agent Capability — live situational map.
 * Design blueprint stays on Governance → Agent System; this page overlays runtime status.
 * Operate / approve only via Agent Desk deep-links (no start/approve here).
 */

import { useMemo, useState } from 'react'
import { useQuery } from '@tanstack/react-query'
import { DenseTag, SegmentControl, StatusLamp } from '@bifrost/ui'
import { fetchAgentBridge } from '@/api/agentOps'
import { fetchRemediationHealth, fetchRemediationJobs } from '@/api/remediation'
import { AgentSystemGraph } from '@/components/agent/AgentSystemGraph'
import { CatalogSection } from '@/components/CatalogSection'
import { OpsSection } from '@/components/layout/OpsSection'
import { useAgentTaskCatalog } from '@/hooks/useAgentTaskCatalog'
import type { OpenAgentDeskArg } from '@/lib/agent/openAgentDesk'
import { allAgentTasks } from '@/lib/agent/agentTaskCatalog'
import {
  activeJobIdForTask,
  buildAgentCapabilityViewModel,
  liveStatusLabel,
  liveStatusStroke,
  nodeMatchesFilter,
  type AgentCapabilityFilter,
  type AgentCapabilityLiveStatus,
} from '@/lib/agent/agentCapabilityViewModel'

const LIVE_LEGEND: AgentCapabilityLiveStatus[] = [
  'ready',
  'running',
  'awaiting',
  'failed',
  'degraded',
  'idle',
]

const FILTER_OPTIONS = [
  { value: 'all', label: 'All' },
  { value: 'attention', label: 'Attention' },
  { value: 'ready', label: 'Ready' },
] as const

function stripLamp(runtimeReachable: boolean) {
  return runtimeReachable ? ('ok' as const) : ('fail' as const)
}

interface AgentCapabilityPageProps {
  onOpenAgentDesk?: (arg?: OpenAgentDeskArg) => void
}

export function AgentCapabilityPage({ onOpenAgentDesk }: AgentCapabilityPageProps) {
  useAgentTaskCatalog()
  const [filter, setFilter] = useState<AgentCapabilityFilter>('all')

  const healthQuery = useQuery({
    queryKey: ['remediation', 'health'],
    queryFn: fetchRemediationHealth,
    refetchInterval: 60_000,
  })

  const bridgeQuery = useQuery({
    queryKey: ['agent', 'bridge'],
    queryFn: fetchAgentBridge,
    refetchInterval: 60_000,
  })

  const jobsQuery = useQuery({
    queryKey: ['remediation', 'jobs'],
    queryFn: fetchRemediationJobs,
    refetchInterval: 15_000,
  })

  const taskCount = allAgentTasks().length
  const vm = useMemo(
    () =>
      buildAgentCapabilityViewModel({
        tasks: allAgentTasks(),
        jobs: jobsQuery.data?.jobs ?? [],
        bridge: bridgeQuery.data,
        health: healthQuery.data,
      }),
    [taskCount, jobsQuery.data?.jobs, bridgeQuery.data, healthQuery.data],
  )

  const visibleTaskIds = useMemo(() => {
    if (filter === 'all') return undefined
    return new Set(
      vm.nodes.filter(n => nodeMatchesFilter(n.status, filter)).map(n => n.task.id),
    )
  }, [vm.nodes, filter])

  const nodeById = useMemo(() => {
    const m = new Map(vm.nodes.map(n => [n.task.id, n]))
    return m
  }, [vm.nodes])

  const handleNodeClick = (taskId: string) => {
    if (onOpenAgentDesk == null) return
    const node = nodeById.get(taskId)
    const jobId = node != null ? activeJobIdForTask(node) : null
    if (jobId != null) onOpenAgentDesk(jobId)
    else onOpenAgentDesk()
  }

  const { strip } = vm
  const probing = bridgeQuery.isLoading && healthQuery.isLoading

  return (
    <div className="flex w-full min-w-0 flex-col gap-3">
      <OpsSection
        title="Live readiness"
        description="Runtime probe + per-scope job status. Click a node to open Agent Desk (operate / approve there)."
        bodyPadding="compact"
        overflow="visible"
      >
        <div className="agent-system-kpis">
          <div className="agent-system-kpi flex items-center gap-2">
            <StatusLamp value={probing ? 'unknown' : stripLamp(strip.runtimeReachable)} kind="reach" />
            <div className="flex flex-col">
              <span className="agent-system-kpi__value text-[var(--text-dense-body)]">
                {probing ? '…' : strip.runtimeReachable ? 'Ready' : 'Down'}
              </span>
              <span className="agent-system-kpi__label">{strip.runtimeLabel}</span>
            </div>
          </div>
          <div className="agent-system-kpi">
            <span className="agent-system-kpi__value">{strip.running}</span>
            <span className="agent-system-kpi__label">Running</span>
          </div>
          <div className="agent-system-kpi">
            <span className="agent-system-kpi__value">{strip.awaiting}</span>
            <span className="agent-system-kpi__label">Awaiting</span>
          </div>
          <div className="agent-system-kpi">
            <span className="agent-system-kpi__value">{strip.failed}</span>
            <span className="agent-system-kpi__label">Failed</span>
          </div>
          <div className="agent-system-kpi agent-system-kpi--meta">
            <span className="agent-system-kpi__label">
              {strip.ready} ready · {strip.idle} idle · {strip.degraded} degraded · {strip.total}{' '}
              scopes
            </span>
          </div>
        </div>
        <p className="m-0 mt-2 text-[var(--text-dense-caption)] text-[var(--muted-foreground)]">
          {vm.summaryLine}
        </p>
      </OpsSection>

      <CatalogSection title="Capability map">
        <div className="flex flex-wrap items-center gap-2 px-3 pt-2">
          <span className="text-[var(--text-dense-caption)] font-medium text-[var(--muted-foreground)] shrink-0">
            Filter:
          </span>
          <SegmentControl
            value={filter}
            onChange={v => setFilter(v as AgentCapabilityFilter)}
            options={[...FILTER_OPTIONS]}
          />
        </div>
        <div className="flex flex-wrap items-center gap-x-3 gap-y-1 px-3 pt-2">
          {LIVE_LEGEND.map(status => (
            <span key={status} className="inline-flex items-center gap-1.5">
              <span
                className="inline-block size-2 rounded-full"
                style={{ background: liveStatusStroke(status) }}
                aria-hidden
              />
              <span className="text-[var(--text-dense-caption)] text-[var(--muted-foreground)]">
                {liveStatusLabel(status)}
              </span>
            </span>
          ))}
        </div>
        <div className="min-w-0 overflow-x-auto px-3 py-3">
          <AgentSystemGraph
            liveStatusByTaskId={vm.statusByTaskId}
            highlightedEdgeKeys={vm.highlightedEdgeKeys}
            visibleTaskIds={visibleTaskIds}
            onNodeClick={handleNodeClick}
          />
        </div>
        {(strip.awaiting > 0 || strip.failed > 0 || strip.running > 0) && (
          <div className="flex flex-wrap gap-1.5 px-3 pb-3">
            {vm.nodes
              .filter(n =>
                n.status === 'running' || n.status === 'awaiting' || n.status === 'failed',
              )
              .slice(0, 8)
              .map(n => (
                <button
                  key={n.task.id}
                  type="button"
                  className="focus-strip-link inline-flex items-center gap-1"
                  onClick={() => handleNodeClick(n.task.id)}
                >
                  <DenseTag
                    variant={
                      n.status === 'failed'
                        ? 'danger'
                        : n.status === 'awaiting'
                          ? 'warning'
                          : 'category'
                    }
                  >
                    {liveStatusLabel(n.status)}
                  </DenseTag>
                  <span className="text-[var(--text-dense-caption)]">{n.task.label}</span>
                </button>
              ))}
          </div>
        )}
      </CatalogSection>
    </div>
  )
}
