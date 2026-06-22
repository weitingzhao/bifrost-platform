import { Fragment, useEffect, useMemo, useState, type ReactNode } from 'react'
import {
  DenseDataTable,
  DenseTableBody,
  DenseTableCell,
  DenseTableDetailRow,
  DenseTableHead,
  DenseTableHeadRow,
  DenseTableHeader,
  DenseTableRow,
  IconActionButton,
  SegmentControl,
  segmentButtonClass,
  segmentGroupClass,
} from '@bifrost/ui'
import {
  BarChart3,
  Cpu,
  Database,
  GitBranch,
  LayoutGrid,
  LayoutPanelLeft,
  RotateCw,
  Scaling,
  Server,
  Trash2,
} from 'lucide-react'
import type { ClusterNamespace, ClusterWorkload } from '@/api/types'
import { groupNeedsAttention, groupWorkloadsByDeployment } from '@/lib/cluster/workloadTree'
import { StatusLamp } from '@/components/StatusLamp'
import { OpsSection } from '@/components/layout/OpsSection'
import { WorkloadExpandToggle } from '@/components/cluster/WorkloadExpandToggle'
import { NodeArchLabel } from '@/components/cluster/NodeArchLabel'
import { getNamespacePlacementSummary } from '@/lib/cluster/namespacePlacement'
import {
  allowedNamespaceNames,
  NS_FILTER_LABELS,
  namespaceDisplayLabel,
  namespaceShowsK8sHint,
  type NsFilterType,
} from '@/lib/cluster/namespaceCatalog'
import { namespaceIcon } from '@/lib/cluster/namespaceIcons'

export type { NsFilterType } from '@/lib/cluster/namespaceCatalog'
export {
  DEPRECATED_NAMESPACES,
  NS_DISPLAY_LABELS,
  NS_FILTER_GROUPS,
  NS_GROUPS,
} from '@/lib/cluster/namespaceCatalog'

function segmentTab(icon: ReactNode, label: string) {
  return (
    <span className="inline-flex items-center gap-1">
      {icon}
      <span>{label}</span>
    </span>
  )
}

const NS_FILTER_SEGMENT_OPTIONS: { value: NsFilterType; label: ReactNode }[] = [
  {
    value: 'trade',
    label: segmentTab(<BarChart3 className="size-3 shrink-0 opacity-80" aria-hidden />, NS_FILTER_LABELS.trade),
  },
  {
    value: 'platform',
    label: segmentTab(
      <LayoutPanelLeft className="size-3 shrink-0 opacity-80" aria-hidden />,
      NS_FILTER_LABELS.platform,
    ),
  },
  {
    value: 'storage',
    label: segmentTab(<Database className="size-3 shrink-0 opacity-80" aria-hidden />, NS_FILTER_LABELS.storage),
  },
  {
    value: 'gpu',
    label: segmentTab(<Cpu className="size-3 shrink-0 opacity-80" aria-hidden />, NS_FILTER_LABELS.gpu),
  },
  {
    value: 'cicd',
    label: segmentTab(<GitBranch className="size-3 shrink-0 opacity-80" aria-hidden />, NS_FILTER_LABELS.cicd),
  },
  {
    value: 'infra',
    label: segmentTab(<Server className="size-3 shrink-0 opacity-80" aria-hidden />, NS_FILTER_LABELS.infra),
  },
  {
    value: 'all',
    label: segmentTab(<LayoutGrid className="size-3 shrink-0 opacity-80" aria-hidden />, 'All'),
  },
]

/** Visual groups: Trade·Platform | Storage·GPU | CI/CD·Infra | All */
const NS_FILTER_SEGMENT_GROUPS: NsFilterType[][] = [
  ['trade', 'platform'],
  ['storage', 'gpu'],
  ['cicd', 'infra'],
  ['all'],
]

const NS_FILTER_OPTION_BY_VALUE = new Map(NS_FILTER_SEGMENT_OPTIONS.map(opt => [opt.value, opt]))

function GroupedNsFilterControl({
  value,
  onChange,
}: {
  value: NsFilterType
  onChange: (filter: NsFilterType) => void
}) {
  return (
    <div className={segmentGroupClass('sm')} role="group" aria-label="Namespace category">
      {NS_FILTER_SEGMENT_GROUPS.map((group, groupIndex) => (
        <Fragment key={group.join('-')}>
          {groupIndex > 0 ? (
            <span className="mx-0.5 h-4 w-px shrink-0 self-center bg-[var(--border)]" aria-hidden />
          ) : null}
          {group.map(filter => {
            const opt = NS_FILTER_OPTION_BY_VALUE.get(filter)
            if (opt == null) return null
            return (
              <button
                key={filter}
                type="button"
                className={segmentButtonClass(value === filter, 'sm')}
                aria-pressed={value === filter}
                onClick={() => onChange(filter)}
              >
                {opt.label}
              </button>
            )
          })}
        </Fragment>
      ))}
    </div>
  )
}

type WorkloadView = 'tree' | 'standalone'

interface ClusterWorkloadsExplorerProps {
  namespaces: ClusterNamespace[]
  nsFilter: NsFilterType
  selectedNs: string | null
  workloads: ClusterWorkload[]
  isLoadingNamespaces: boolean
  isLoadingWorkloads: boolean
  selectedPod: string | null
  onFilterChange: (filter: NsFilterType) => void
  onSelectNs: (name: string) => void
  onSelectPod: (name: string) => void
  onRestartDeployment: (workload: ClusterWorkload) => void
  onScaleDeployment: (workload: ClusterWorkload) => void
  onDeletePod: (workload: ClusterWorkload) => void
}

function NsChipStats({
  running,
  total,
  failing,
}: {
  running: number
  total: number
  failing: number
}) {
  return (
    <span className="cluster-ns-chip__stats font-mono-tabular">
      <span className={running > 0 ? 'cluster-ns-chip__running' : 'cluster-ns-chip__idle'}>{running}</span>
      <span className="cluster-ns-chip__sep">/</span>
      <span className="cluster-ns-chip__total">{total}</span>
      {failing > 0 ? (
        <>
          <span className="cluster-ns-chip__sep">·</span>
          <span className="cluster-ns-chip__failing">{failing} fail</span>
        </>
      ) : null}
    </span>
  )
}

function WorkloadStatusCell({ workload }: { workload: ClusterWorkload }) {
  return (
    <span className="inline-flex items-center gap-1.5">
      <StatusLamp value={workload.reachability} kind="reach" />
      <span className="font-mono-tabular">{workload.status}</span>
    </span>
  )
}

function NamespaceIdealArchCell({
  summary,
}: {
  summary: ReturnType<typeof getNamespacePlacementSummary> | null
}) {
  if (summary == null || !summary.mapped) {
    return <DenseTableCell className="text-[var(--muted-foreground)]">—</DenseTableCell>
  }
  const archs = summary.idealArchs.filter(arch => arch !== 'amd64')
  if (archs.length === 0) {
    return <DenseTableCell className="text-[var(--muted-foreground)]">—</DenseTableCell>
  }
  return (
    <DenseTableCell>
      <span className="inline-flex flex-wrap items-center gap-1">
        {archs.map(arch => (
          <NodeArchLabel key={arch} arch={arch} showTooltip={false} />
        ))}
      </span>
    </DenseTableCell>
  )
}

function PodActionsCell({
  pod,
  onDeletePod,
}: {
  pod: ClusterWorkload
  onDeletePod: (workload: ClusterWorkload) => void
}) {
  return (
    <IconActionButton
      title="Delete pod"
      ariaLabel={`Delete pod ${pod.name}`}
      tone="danger"
      onClick={e => {
        e.stopPropagation()
        onDeletePod(pod)
      }}
    >
      <Trash2 className="size-3.5" />
    </IconActionButton>
  )
}

export function ClusterWorkloadsExplorer({
  namespaces,
  nsFilter,
  selectedNs,
  workloads,
  isLoadingNamespaces,
  isLoadingWorkloads,
  selectedPod,
  onFilterChange,
  onSelectNs,
  onSelectPod,
  onRestartDeployment,
  onScaleDeployment,
  onDeletePod,
}: ClusterWorkloadsExplorerProps) {
  const [workloadView, setWorkloadView] = useState<WorkloadView>('tree')
  const [expandedDeployments, setExpandedDeployments] = useState<Set<string>>(new Set())

  const visibleNamespaces = useMemo(() => {
    const allowed = allowedNamespaceNames(nsFilter)
    if (allowed == null) return namespaces
    const set = new Set(allowed)
    return namespaces.filter(ns => set.has(ns.name))
  }, [namespaces, nsFilter])

  const selectedNamespace = useMemo(
    () => namespaces.find(ns => ns.name === selectedNs),
    [namespaces, selectedNs],
  )

  const { groups, orphanPods } = useMemo(() => groupWorkloadsByDeployment(workloads), [workloads])

  useEffect(() => {
    const next = new Set<string>()
    for (const group of groups) {
      if (groupNeedsAttention(group)) {
        next.add(group.deployment.name)
      }
    }
    setExpandedDeployments(next)
  }, [selectedNs, groups])

  function toggleDeployment(name: string) {
    setExpandedDeployments(prev => {
      const next = new Set(prev)
      if (next.has(name)) next.delete(name)
      else next.add(name)
      return next
    })
  }

  const viewOptions = useMemo(
    () => [
      { value: 'tree' as const, label: `By deployment (${groups.length})` },
      { value: 'standalone' as const, label: `Standalone (${orphanPods.length})` },
    ],
    [groups.length, orphanPods.length],
  )

  const selectedPlacementSummary = useMemo(
    () => (selectedNs != null ? getNamespacePlacementSummary(selectedNs) : null),
    [selectedNs],
  )

  const colSpan = 7

  return (
    <OpsSection
      title="Namespaces & workloads"
      description="Deployments own their pods — expand a deployment to inspect pods, logs, and pod-level actions."
      actions={<GroupedNsFilterControl value={nsFilter} onChange={onFilterChange} />}
      bodyPadding="none"
      overflow="hidden"
      bodyClassName="ops-section-body--table"
    >
      <div className="cluster-explorer-toolbar">
        <div className="cluster-explorer-ns-rail" role="tablist" aria-label="Namespaces">
          {isLoadingNamespaces && visibleNamespaces.length === 0 ? (
            <span className="text-[var(--text-dense-meta)] text-[var(--muted-foreground)] px-3">Loading namespaces…</span>
          ) : visibleNamespaces.length === 0 ? (
            <span className="text-[var(--text-dense-meta)] text-[var(--muted-foreground)] px-3">No namespaces</span>
          ) : (
            visibleNamespaces.map(ns => {
              const active = selectedNs === ns.name
              const hasFailing = ns.failing_pods > 0
              const NsIcon = namespaceIcon(ns.name)
              return (
                <button
                  key={ns.name}
                  type="button"
                  role="tab"
                  aria-selected={active}
                  className={`cluster-ns-chip${active ? ' cluster-ns-chip--active' : ''}${hasFailing ? ' cluster-ns-chip--warn' : ''}`}
                  onClick={() => onSelectNs(ns.name)}
                  title={namespaceShowsK8sHint(ns.name) ? `K8s namespace: ${ns.name}` : undefined}
                >
                  <span className="cluster-ns-chip__name">
                    <NsIcon className="cluster-ns-chip__icon" aria-hidden />
                    {namespaceDisplayLabel(ns.name)}
                  </span>
                  <NsChipStats running={ns.running_pods} total={ns.pod_count} failing={ns.failing_pods} />
                </button>
              )
            })
          )}
        </div>
      </div>

      {selectedNs != null && (
        <div className="cluster-explorer-subtoolbar">
          <div className="flex min-w-0 flex-wrap items-center gap-2">
            {(() => {
              const SelectedNsIcon = namespaceIcon(selectedNs)
              return (
                <span className="cluster-explorer-ns-label inline-flex items-center gap-1.5 font-mono-tabular">
                  <SelectedNsIcon className="cluster-ns-chip__icon cluster-ns-chip__icon--subtoolbar" aria-hidden />
                  {namespaceDisplayLabel(selectedNs)}
                </span>
              )
            })()}
            {namespaceShowsK8sHint(selectedNs) ? (
              <span className="text-dense-meta text-[var(--muted-foreground)] font-mono-tabular" title="K8s namespace">
                ({selectedNs})
              </span>
            ) : null}
            {selectedNamespace != null && (
              <span className="cluster-explorer-ns-summary font-mono-tabular">
                <span className="cluster-explorer-ns-summary__total">{selectedNamespace.pod_count} pods</span>
                <span className="cluster-explorer-ns-summary__sep">·</span>
                <span className="cluster-explorer-ns-summary__running">{selectedNamespace.running_pods} running</span>
                {selectedNamespace.failing_pods > 0 ? (
                  <>
                    <span className="cluster-explorer-ns-summary__sep">·</span>
                    <span className="cluster-explorer-ns-summary__failing">
                      {selectedNamespace.failing_pods} failing
                    </span>
                  </>
                ) : null}
              </span>
            )}
          </div>
          <SegmentControl
            value={workloadView}
            onChange={v => setWorkloadView(v as WorkloadView)}
            options={viewOptions}
            size="sm"
          />
        </div>
      )}

      <DenseDataTable wrapClassName="cluster-explorer-table-scroll dense-scroll-x">
        <DenseTableHeader>
          <DenseTableHeadRow>
            <DenseTableHead>Workload</DenseTableHead>
            <DenseTableHead>Ideal arch</DenseTableHead>
            <DenseTableHead>Ready</DenseTableHead>
            <DenseTableHead>Status</DenseTableHead>
            <DenseTableHead>Restarts</DenseTableHead>
            <DenseTableHead>Age</DenseTableHead>
            <DenseTableHead className="w-[3rem]">Pod</DenseTableHead>
          </DenseTableHeadRow>
        </DenseTableHeader>
        <DenseTableBody>
          {selectedNs == null ? (
            <DenseTableRow>
              <DenseTableCell colSpan={colSpan} className="text-[var(--muted-foreground)]">
                Select a namespace above
              </DenseTableCell>
            </DenseTableRow>
          ) : isLoadingWorkloads ? (
            <DenseTableRow>
              <DenseTableCell colSpan={colSpan} className="text-[var(--muted-foreground)]">
                Loading workloads…
              </DenseTableCell>
            </DenseTableRow>
          ) : workloadView === 'standalone' ? (
            orphanPods.length === 0 ? (
              <DenseTableRow>
                <DenseTableCell colSpan={colSpan} className="text-[var(--muted-foreground)]">
                  No standalone pods — every pod is owned by a deployment in this namespace.
                </DenseTableCell>
              </DenseTableRow>
            ) : (
              orphanPods.map(pod => (
                <DenseTableRow
                  key={pod.name}
                  className={selectedPod === pod.name ? 'dense-table__row--selected' : ''}
                  onClick={() => onSelectPod(pod.name)}
                  style={{ cursor: 'pointer' }}
                >
                  <DenseTableCell className="font-mono-tabular max-w-[18rem] truncate" title={pod.name}>
                    {pod.name}
                  </DenseTableCell>
                  <NamespaceIdealArchCell summary={selectedPlacementSummary} />
                  <DenseTableCell className="font-mono-tabular">{pod.ready}</DenseTableCell>
                  <DenseTableCell>
                    <WorkloadStatusCell workload={pod} />
                  </DenseTableCell>
                  <DenseTableCell className="font-mono-tabular">{pod.restarts}</DenseTableCell>
                  <DenseTableCell className="font-mono-tabular text-[var(--text-dense-meta)]">{pod.age}</DenseTableCell>
                  <DenseTableCell>
                    <PodActionsCell pod={pod} onDeletePod={onDeletePod} />
                  </DenseTableCell>
                </DenseTableRow>
              ))
            )
          ) : groups.length === 0 ? (
            <DenseTableRow>
              <DenseTableCell colSpan={colSpan} className="text-[var(--muted-foreground)]">
                No deployments in this namespace
              </DenseTableCell>
            </DenseTableRow>
          ) : (
            groups.map(group => {
              const { deployment, pods } = group
              const expanded = expandedDeployments.has(deployment.name)
              const hasPods = pods.length > 0
              return (
                <Fragment key={deployment.name}>
                  <DenseTableRow className="cluster-deployment-parent-row">
                    <DenseTableCell>
                      <div className="cluster-deployment-row">
                        <WorkloadExpandToggle
                          expanded={expanded}
                          disabled={!hasPods}
                          label={hasPods ? `Toggle pods for ${deployment.name}` : `No pods for ${deployment.name}`}
                          onToggle={() => toggleDeployment(deployment.name)}
                        />
                        <div className="cluster-deployment-row__identity min-w-0">
                          <span className="cluster-deployment-row__name font-mono-tabular" title={deployment.name}>
                            {deployment.name}
                          </span>
                          <span className="cluster-deployment-row__kind">Deployment</span>
                          {hasPods ? (
                            <span className="cluster-deployment-row__pod-count font-mono-tabular">
                              {pods.length} {pods.length === 1 ? 'pod' : 'pods'}
                            </span>
                          ) : (
                            <span className="cluster-deployment-row__pod-count cluster-deployment-row__pod-count--empty">
                              no pods
                            </span>
                          )}
                        </div>
                        <div className="cluster-deployment-row__actions">
                          <IconActionButton
                            title="Rollout restart"
                            ariaLabel={`Restart ${deployment.name}`}
                            onClick={e => {
                              e.stopPropagation()
                              onRestartDeployment(deployment)
                            }}
                          >
                            <RotateCw className="size-3.5" />
                          </IconActionButton>
                          <IconActionButton
                            title="Scale replicas"
                            ariaLabel={`Scale ${deployment.name}`}
                            onClick={e => {
                              e.stopPropagation()
                              onScaleDeployment(deployment)
                            }}
                          >
                            <Scaling className="size-3.5" />
                          </IconActionButton>
                        </div>
                      </div>
                    </DenseTableCell>
                    <NamespaceIdealArchCell summary={selectedPlacementSummary} />
                    <DenseTableCell className="font-mono-tabular">{deployment.ready}</DenseTableCell>
                    <DenseTableCell>
                      <WorkloadStatusCell workload={deployment} />
                    </DenseTableCell>
                    <DenseTableCell className="font-mono-tabular text-[var(--muted-foreground)]">—</DenseTableCell>
                    <DenseTableCell className="font-mono-tabular text-[var(--text-dense-meta)]">{deployment.age}</DenseTableCell>
                    <DenseTableCell />
                  </DenseTableRow>
                  {expanded &&
                    pods.map(pod => (
                      <DenseTableDetailRow
                        key={pod.name}
                        className={selectedPod === pod.name ? 'dense-table__row--selected' : ''}
                      >
                        <DenseTableCell
                          className="cursor-pointer"
                          onClick={() => onSelectPod(pod.name)}
                        >
                          <div className="cluster-pod-row">
                            <span className="cluster-pod-row__branch" aria-hidden />
                            <span className="cluster-pod-row__name font-mono-tabular truncate" title={pod.name}>
                              {pod.name}
                            </span>
                          </div>
                        </DenseTableCell>
                        <NamespaceIdealArchCell summary={selectedPlacementSummary} />
                        <DenseTableCell className="font-mono-tabular">{pod.ready}</DenseTableCell>
                        <DenseTableCell>
                          <WorkloadStatusCell workload={pod} />
                        </DenseTableCell>
                        <DenseTableCell className="font-mono-tabular">{pod.restarts}</DenseTableCell>
                        <DenseTableCell className="font-mono-tabular text-[var(--text-dense-meta)]">{pod.age}</DenseTableCell>
                        <DenseTableCell>
                          <PodActionsCell pod={pod} onDeletePod={onDeletePod} />
                        </DenseTableCell>
                      </DenseTableDetailRow>
                    ))}
                </Fragment>
              )
            })
          )}
        </DenseTableBody>
      </DenseDataTable>

      {selectedNs != null && workloadView === 'tree' && orphanPods.length > 0 && (
        <p className="cluster-explorer-footnote">
          {orphanPods.length} pod{orphanPods.length === 1 ? '' : 's'} not owned by a deployment — see Standalone tab.
        </p>
      )}
    </OpsSection>
  )
}
