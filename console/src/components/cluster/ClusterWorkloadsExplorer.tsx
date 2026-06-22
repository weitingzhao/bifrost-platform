import { Fragment, useEffect, useMemo, useState, type ReactNode } from 'react'
import { useQueries } from '@tanstack/react-query'
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
import { fetchClusterWorkloads } from '@/api/platform'
import { groupNeedsAttention, groupWorkloadsByDeployment } from '@/lib/cluster/workloadTree'
import { buildNamespacePodInventory } from '@/lib/cluster/workloadPodInventory'
import {
  aggregateCategoryDeployStats,
  computeNamespaceReadyStats,
  computeReadyTones,
  namespaceNamesForFilter,
  type CategoryDeployStats,
  type NamespaceReadyStats,
  type ReadyCount,
  type ReadyTone,
} from '@/lib/cluster/workloadReadyStats'
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
  categoryDeployStats,
}: {
  value: NsFilterType
  onChange: (filter: NsFilterType) => void
  categoryDeployStats: Record<NsFilterType, CategoryDeployStats>
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
            const deployStats = categoryDeployStats[filter]
            return (
              <button
                key={filter}
                type="button"
                className={segmentButtonClass(value === filter, 'sm')}
                aria-pressed={value === filter}
                onClick={() => onChange(filter)}
                title={
                  deployStats.loading
                    ? 'Loading deployment ready stats…'
                    : `${deployStats.ok} deployment${deployStats.ok === 1 ? '' : 's'} ready · ${deployStats.failed} not ready`
                }
              >
                <span className="inline-flex items-center gap-1">
                  {opt.label}
                  <CategoryDeployBadge stats={deployStats} />
                </span>
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

function NsChipPodTotal({ total, failing }: { total: number; failing: number }) {
  return (
    <span className="cluster-ns-chip__pods-total font-mono-tabular">
      <span className="cluster-ns-chip__total">{total}</span>
      <span className="cluster-ns-chip__suffix"> pods</span>
      {failing > 0 ? (
        <>
          <span className="cluster-ns-chip__sep">·</span>
          <span className="cluster-ns-chip__failing">{failing} fail</span>
        </>
      ) : null}
    </span>
  )
}

function ReadyFraction({
  label,
  count,
  tone,
  loading,
}: {
  label: string
  count: ReadyCount
  tone: ReadyTone
  loading?: boolean
}) {
  if (loading) {
    return (
      <span className="cluster-ns-chip__ready-row">
        <span className="cluster-ns-chip__ready-label">{label}</span>
        <span className="cluster-ns-chip__ready-val cluster-ns-chip__ready-val--idle">…</span>
      </span>
    )
  }
  if (tone === 'idle' || count.planned === 0) {
    return (
      <span className="cluster-ns-chip__ready-row">
        <span className="cluster-ns-chip__ready-label">{label}</span>
        <span className="cluster-ns-chip__ready-val cluster-ns-chip__ready-val--idle">—</span>
      </span>
    )
  }
  return (
    <span className="cluster-ns-chip__ready-row">
      <span className="cluster-ns-chip__ready-label">{label}</span>
      <span className={`cluster-ns-chip__ready-val cluster-ns-chip__ready-val--${tone}`}>
        {count.actual}/{count.planned}
      </span>
    </span>
  )
}

function CategoryDeployBadge({ stats }: { stats: CategoryDeployStats }) {
  if (stats.loading) {
    return <span className="segment-deploy-stats segment-deploy-stats--loading">…</span>
  }
  if (stats.ok === 0 && stats.failed === 0) {
    return null
  }
  return (
    <span className="segment-deploy-stats font-mono-tabular">
      <span className="segment-deploy-stats__ok">{stats.ok}</span>
      {stats.failed > 0 ? (
        <>
          <span className="segment-deploy-stats__sep">/</span>
          <span className="segment-deploy-stats__fail">{stats.failed}</span>
        </>
      ) : null}
    </span>
  )
}

function NsChipReadySummary({
  stats,
  tones,
  loading,
  totalPods,
  failingPods,
}: {
  stats: NamespaceReadyStats | undefined
  tones: { deployment: ReadyTone; standalone: ReadyTone } | undefined
  loading: boolean
  totalPods: number
  failingPods: number
}) {
  return (
    <span className="cluster-ns-chip__ready-block">
      <ReadyFraction
        label="Deploy"
        count={stats?.deploymentReady ?? { actual: 0, planned: 0 }}
        tone={tones?.deployment ?? 'idle'}
        loading={loading}
      />
      <ReadyFraction
        label="Standalone"
        count={stats?.standaloneReady ?? { actual: 0, planned: 0 }}
        tone={tones?.standalone ?? 'idle'}
        loading={loading}
      />
      <NsChipPodTotal total={totalPods} failing={failingPods} />
    </span>
  )
}

function nsChipTitle(ns: ClusterNamespace): string | undefined {
  const parts: string[] = []
  if (namespaceShowsK8sHint(ns.name)) parts.push(`K8s namespace: ${ns.name}`)
  parts.push(`${ns.pod_count} Pod objects (deployments, jobs, CI builds, etc.)`)
  return parts.join(' · ')
}

function NamespacePodInventoryBar({
  inventory,
  loading,
}: {
  inventory: ReturnType<typeof buildNamespacePodInventory> | null
  loading: boolean
}) {
  if (inventory == null) return null

  const { totalPods, deploymentCount, standalonePodCount, phases, failingPods } = inventory
  const hasPhaseDetail =
    !loading &&
    (phases.running > 0 || phases.succeeded > 0 || phases.pending > 0 || phases.failed > 0 || phases.other > 0)

  return (
    <span className="cluster-explorer-ns-inventory font-mono-tabular" title="All Pod objects in this namespace">
      <span className="cluster-explorer-ns-inventory__total">{totalPods} pods total</span>
      {!loading ? (
        <>
          <span className="cluster-explorer-ns-inventory__sep">·</span>
          <span>{deploymentCount} deployments</span>
          <span className="cluster-explorer-ns-inventory__sep">·</span>
          <span>{standalonePodCount} standalone</span>
        </>
      ) : (
        <>
          <span className="cluster-explorer-ns-inventory__sep">·</span>
          <span className="cluster-explorer-ns-inventory__loading">loading breakdown…</span>
        </>
      )}
      {hasPhaseDetail ? (
        <>
          {phases.running > 0 ? (
            <>
              <span className="cluster-explorer-ns-inventory__sep">·</span>
              <span className="cluster-explorer-ns-inventory__running">{phases.running} running</span>
            </>
          ) : null}
          {phases.succeeded > 0 ? (
            <>
              <span className="cluster-explorer-ns-inventory__sep">·</span>
              <span className="cluster-explorer-ns-inventory__succeeded">{phases.succeeded} succeeded</span>
            </>
          ) : null}
          {phases.pending > 0 ? (
            <>
              <span className="cluster-explorer-ns-inventory__sep">·</span>
              <span className="cluster-explorer-ns-inventory__pending">{phases.pending} pending</span>
            </>
          ) : null}
          {phases.failed > 0 ? (
            <>
              <span className="cluster-explorer-ns-inventory__sep">·</span>
              <span className="cluster-explorer-ns-inventory__failing">{phases.failed} failed</span>
            </>
          ) : null}
        </>
      ) : null}
      {failingPods > 0 && phases.failed === 0 ? (
        <>
          <span className="cluster-explorer-ns-inventory__sep">·</span>
          <span className="cluster-explorer-ns-inventory__failing">{failingPods} failing</span>
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

  const allNamespaceNames = useMemo(() => namespaces.map(ns => ns.name), [namespaces])

  const workloadQueries = useQueries({
    queries: allNamespaceNames.map(name => ({
      queryKey: ['cluster', 'workloads', name],
      queryFn: () => fetchClusterWorkloads(name),
      staleTime: 30_000,
      enabled: allNamespaceNames.length > 0,
    })),
  })

  const { readyStatsByNs, readyTonesByNs, loadingNsNames } = useMemo(() => {
    const statsMap = new Map<string, NamespaceReadyStats>()
    const tonesMap = new Map<string, { deployment: ReadyTone; standalone: ReadyTone }>()
    const loading = new Set<string>()

    allNamespaceNames.forEach((name, index) => {
      const query = workloadQueries[index]
      if (query?.isLoading && query.data == null) {
        loading.add(name)
        return
      }
      let wls = query?.data?.workloads
      if (name === selectedNs && workloads.length > 0) wls = workloads
      if (wls == null) return
      const stats = computeNamespaceReadyStats(wls)
      statsMap.set(name, stats)
      tonesMap.set(name, computeReadyTones(wls, stats))
    })

    return { readyStatsByNs: statsMap, readyTonesByNs: tonesMap, loadingNsNames: loading }
  }, [allNamespaceNames, workloadQueries, selectedNs, workloads])

  const categoryDeployStats = useMemo(() => {
    const filters: NsFilterType[] = ['trade', 'platform', 'storage', 'gpu', 'cicd', 'infra', 'all']
    const result = {} as Record<NsFilterType, CategoryDeployStats>
    for (const filter of filters) {
      const names = namespaceNamesForFilter(filter, allNamespaceNames)
      result[filter] = aggregateCategoryDeployStats(names, readyStatsByNs, loadingNsNames)
    }
    return result
  }, [allNamespaceNames, readyStatsByNs, loadingNsNames])

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

  const podInventory = useMemo(
    () => buildNamespacePodInventory(workloads, selectedNamespace ?? null),
    [workloads, selectedNamespace],
  )

  const colSpan = 7

  return (
    <OpsSection
      title="Namespaces & workloads"
      description="Pod counts include every Pod object in the namespace (deployments, jobs, CI builds). Browse long-running services under By deployment; orphaned pods under Standalone."
      actions={
        <GroupedNsFilterControl
          value={nsFilter}
          onChange={onFilterChange}
          categoryDeployStats={categoryDeployStats}
        />
      }
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
              const nsLoading = loadingNsNames.has(ns.name)
              const nsStats = readyStatsByNs.get(ns.name)
              const nsTones = readyTonesByNs.get(ns.name)
              return (
                <button
                  key={ns.name}
                  type="button"
                  role="tab"
                  aria-selected={active}
                  className={`cluster-ns-chip${active ? ' cluster-ns-chip--active' : ''}${hasFailing || (nsStats?.deploymentFailed ?? 0) > 0 ? ' cluster-ns-chip--warn' : ''}`}
                  onClick={() => onSelectNs(ns.name)}
                  title={nsChipTitle(ns)}
                >
                  <span className="cluster-ns-chip__name">
                    <NsIcon className="cluster-ns-chip__icon" aria-hidden />
                    {namespaceDisplayLabel(ns.name)}
                  </span>
                  <NsChipReadySummary
                    stats={nsStats}
                    tones={nsTones}
                    loading={nsLoading}
                    totalPods={ns.pod_count}
                    failingPods={ns.failing_pods}
                  />
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
            <NamespacePodInventoryBar inventory={podInventory} loading={isLoadingWorkloads} />
            {selectedNs != null && readyStatsByNs.has(selectedNs) ? (
              <span className="cluster-explorer-ns-inventory font-mono-tabular">
                <span className="cluster-explorer-ns-inventory__sep">·</span>
                <span>
                  Deploy ready{' '}
                  <span
                    className={`cluster-explorer-ns-inventory__${readyTonesByNs.get(selectedNs)?.deployment === 'ok' ? 'running' : readyTonesByNs.get(selectedNs)?.deployment === 'fail' ? 'failing' : 'pending'}`}
                  >
                    {readyStatsByNs.get(selectedNs)?.deploymentReady.actual}/
                    {readyStatsByNs.get(selectedNs)?.deploymentReady.planned}
                  </span>
                </span>
                <span className="cluster-explorer-ns-inventory__sep">·</span>
                <span>
                  Standalone ready{' '}
                  <span
                    className={`cluster-explorer-ns-inventory__${readyTonesByNs.get(selectedNs)?.standalone === 'ok' ? 'running' : readyTonesByNs.get(selectedNs)?.standalone === 'fail' ? 'failing' : 'pending'}`}
                  >
                    {readyStatsByNs.get(selectedNs)?.standaloneReady.actual}/
                    {readyStatsByNs.get(selectedNs)?.standaloneReady.planned}
                  </span>
                </span>
              </span>
            ) : null}
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
    </OpsSection>
  )
}
