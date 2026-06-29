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
  DenseTableSubheadRow,
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
  HardDrive,
  LayoutGrid,
  LayoutPanelLeft,
  Layers,
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
import {
  aggregateStorageCategoryDeployStats,
  buildCnpgServiceView,
  collectWorkloadsForStorageService,
  computeStorageServiceChipStats,
  getStorageService,
  STORAGE_SERVICES,
  storageServiceChipTitle,
  type StorageServiceId,
} from '@/lib/cluster/storageServiceCatalog'

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
  selectedStorageService: StorageServiceId | null
  workloads: ClusterWorkload[]
  isLoadingNamespaces: boolean
  isLoadingWorkloads: boolean
  selectedPod: string | null
  onFilterChange: (filter: NsFilterType) => void
  onSelectNs: (name: string) => void
  onSelectStorageService: (id: StorageServiceId) => void
  onSelectPod: (workload: ClusterWorkload) => void
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

const STORAGE_SERVICE_ICONS = {
  cnpg: Database,
  redis: Layers,
  minio: HardDrive,
} as const

function StorageServiceChip({
  serviceId,
  active,
  stats,
  loading,
  onSelect,
}: {
  serviceId: StorageServiceId
  active: boolean
  stats: ReturnType<typeof computeStorageServiceChipStats> | null
  loading: boolean
  onSelect: () => void
}) {
  const service = getStorageService(serviceId)
  const Icon = STORAGE_SERVICE_ICONS[serviceId]
  const hasFailing = (stats?.failingPods ?? 0) > 0 || (stats?.readyStats.deploymentFailed ?? 0) > 0

  return (
    <button
      type="button"
      role="tab"
      aria-selected={active}
      className={`cluster-ns-chip cluster-ns-chip--service${active ? ' cluster-ns-chip--active' : ''}${hasFailing ? ' cluster-ns-chip--warn' : ''}`}
      onClick={onSelect}
      title={stats != null ? storageServiceChipTitle(service, stats) : service.description}
    >
      <span className="cluster-ns-chip__name">
        <Icon className="cluster-ns-chip__icon" aria-hidden />
        <span className="inline-flex min-w-0 flex-col items-start gap-0.5 text-left">
          <span>{service.label}</span>
          <span className="text-dense-caption font-normal normal-case tracking-normal text-[var(--muted-foreground)]">
            {service.role}
          </span>
        </span>
      </span>
      {stats != null ? (
        <NsChipReadySummary
          stats={stats.readyStats}
          tones={stats.readyTones}
          loading={loading}
          totalPods={stats.podCount}
          failingPods={stats.failingPods}
        />
      ) : loading ? (
        <span className="text-dense-meta text-[var(--muted-foreground)] px-1">…</span>
      ) : null}
    </button>
  )
}

function StorageServiceInventoryBar({
  serviceId,
  workloads,
  loading,
}: {
  serviceId: StorageServiceId
  workloads: ClusterWorkload[]
  loading: boolean
}) {
  const service = getStorageService(serviceId)
  const inventory = buildNamespacePodInventory(workloads, null)
  if (inventory == null) return null

  const cnpgView = serviceId === 'cnpg' ? buildCnpgServiceView(workloads) : null

  return (
    <span className="cluster-explorer-ns-inventory font-mono-tabular">
      <span className="cluster-explorer-ns-inventory__total">{inventory.totalPods} pods in service</span>
      {!loading ? (
        <>
          {serviceId === 'cnpg' && cnpgView != null ? (
            <>
              <span className="cluster-explorer-ns-inventory__sep">·</span>
              <span>
                {cnpgView.operator != null ? '1 operator' : '0 operator'}
                <span className="cluster-explorer-ns-inventory__sep"> · </span>
                {cnpgView.instances.length} instance{cnpgView.instances.length === 1 ? '' : 's'}
              </span>
            </>
          ) : (
            <>
              <span className="cluster-explorer-ns-inventory__sep">·</span>
              <span>{inventory.deploymentCount} deployments</span>
            </>
          )}
          <span className="cluster-explorer-ns-inventory__sep">·</span>
          <span className="text-dense-meta text-[var(--muted-foreground)]" title="K8s namespaces">
            {service.sourceNamespaces.join(', ')}
          </span>
        </>
      ) : (
        <>
          <span className="cluster-explorer-ns-inventory__sep">·</span>
          <span className="cluster-explorer-ns-inventory__loading">loading breakdown…</span>
        </>
      )}
    </span>
  )
}

function K8sNamespaceHint({ namespace }: { namespace: string }) {
  return (
    <span className="text-dense-caption font-mono-tabular text-[var(--muted-foreground)]" title="K8s namespace">
      {namespace}
    </span>
  )
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

function NamespaceIdealArchCell({ namespace }: { namespace: string }) {
  const summary = getNamespacePlacementSummary(namespace)
  if (!summary.mapped) {
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
  selectedStorageService,
  workloads,
  isLoadingNamespaces,
  isLoadingWorkloads,
  selectedPod,
  onFilterChange,
  onSelectNs,
  onSelectStorageService,
  onSelectPod,
  onRestartDeployment,
  onScaleDeployment,
  onDeletePod,
}: ClusterWorkloadsExplorerProps) {
  const [workloadView, setWorkloadView] = useState<WorkloadView>('tree')
  const [expandedDeployments, setExpandedDeployments] = useState<Set<string>>(new Set())
  const isStorageMode = nsFilter === 'storage'

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

  const workloadsByNamespace = useMemo(() => {
    const map = new Map<string, ClusterWorkload[]>()
    allNamespaceNames.forEach((name, index) => {
      const query = workloadQueries[index]
      let wls = query?.data?.workloads ?? []
      if (name === selectedNs && workloads.length > 0) wls = workloads
      map.set(name, wls)
    })
    return map
  }, [allNamespaceNames, workloadQueries, selectedNs, workloads])

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
      const wls = workloadsByNamespace.get(name)
      if (wls == null) return
      const stats = computeNamespaceReadyStats(wls)
      statsMap.set(name, stats)
      tonesMap.set(name, computeReadyTones(wls, stats))
    })

    return { readyStatsByNs: statsMap, readyTonesByNs: tonesMap, loadingNsNames: loading }
  }, [allNamespaceNames, workloadQueries, workloadsByNamespace])

  const categoryDeployStats = useMemo(() => {
    const filters: NsFilterType[] = ['trade', 'platform', 'storage', 'gpu', 'cicd', 'infra', 'all']
    const result = {} as Record<NsFilterType, CategoryDeployStats>
    for (const filter of filters) {
      if (filter === 'storage') {
        result[filter] = aggregateStorageCategoryDeployStats(workloadsByNamespace, loadingNsNames)
        continue
      }
      const names = namespaceNamesForFilter(filter, allNamespaceNames)
      result[filter] = aggregateCategoryDeployStats(names, readyStatsByNs, loadingNsNames)
    }
    return result
  }, [allNamespaceNames, readyStatsByNs, loadingNsNames, workloadsByNamespace])

  const selectedNamespace = useMemo(
    () => namespaces.find(ns => ns.name === selectedNs),
    [namespaces, selectedNs],
  )

  const storageServiceWorkloads = useMemo(() => {
    if (!isStorageMode || selectedStorageService == null) return []
    return collectWorkloadsForStorageService(workloadsByNamespace, selectedStorageService)
  }, [isStorageMode, selectedStorageService, workloadsByNamespace])

  const storageServiceStatsById = useMemo(() => {
    const stats = new Map<StorageServiceId, ReturnType<typeof computeStorageServiceChipStats>>()
    for (const service of STORAGE_SERVICES) {
      const serviceWorkloads = collectWorkloadsForStorageService(workloadsByNamespace, service.id)
      stats.set(service.id, computeStorageServiceChipStats(serviceWorkloads))
    }
    return stats
  }, [workloadsByNamespace])

  const activeWorkloads = isStorageMode ? storageServiceWorkloads : workloads
  const isActiveWorkloadsLoading = isStorageMode
    ? getStorageService(selectedStorageService ?? 'cnpg').sourceNamespaces.some(namespace =>
        loadingNsNames.has(namespace),
      )
    : isLoadingWorkloads

  const { groups, orphanPods } = useMemo(
    () => groupWorkloadsByDeployment(activeWorkloads),
    [activeWorkloads],
  )

  const cnpgView = useMemo(() => {
    if (!isStorageMode || selectedStorageService !== 'cnpg') return null
    return buildCnpgServiceView(storageServiceWorkloads)
  }, [isStorageMode, selectedStorageService, storageServiceWorkloads])

  const showStandaloneToggle = !isStorageMode || selectedStorageService !== 'cnpg'

  useEffect(() => {
    const next = new Set<string>()
    for (const group of groups) {
      if (groupNeedsAttention(group)) {
        next.add(group.deployment.name)
      }
    }
    if (isStorageMode && selectedStorageService === 'cnpg' && cnpgView?.operator != null) {
      next.add(cnpgView.operator.deployment.name)
    }
    setExpandedDeployments(next)
  }, [selectedNs, selectedStorageService, groups, isStorageMode, cnpgView])

  useEffect(() => {
    if (isStorageMode) setWorkloadView('tree')
  }, [isStorageMode, selectedStorageService])

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

  const podInventory = useMemo(() => {
    if (isStorageMode) {
      return buildNamespacePodInventory(storageServiceWorkloads, null)
    }
    return buildNamespacePodInventory(workloads, selectedNamespace ?? null)
  }, [isStorageMode, storageServiceWorkloads, workloads, selectedNamespace])

  const colSpan = 7
  const selectedService =
    isStorageMode && selectedStorageService != null
      ? getStorageService(selectedStorageService)
      : null

  return (
    <OpsSection
      title="Namespaces & workloads"
      description={
        isStorageMode
          ? 'Storage services grouped by persistence role — CNPG (SQL), Redis (cache & queue), MinIO (object store). K8s namespace names appear as technical detail only.'
          : 'Pod counts include every Pod object in the namespace (deployments, jobs, CI builds). Browse long-running services under By deployment; orphaned pods under Standalone.'
      }
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
        <div
          className="cluster-explorer-ns-rail"
          role="tablist"
          aria-label={isStorageMode ? 'Storage services' : 'Namespaces'}
        >
          {isStorageMode ? (
            STORAGE_SERVICES.map(service => {
              const active = selectedStorageService === service.id
              const serviceLoading = service.sourceNamespaces.some(namespace => loadingNsNames.has(namespace))
              return (
                <StorageServiceChip
                  key={service.id}
                  serviceId={service.id}
                  active={active}
                  stats={storageServiceStatsById.get(service.id) ?? null}
                  loading={serviceLoading}
                  onSelect={() => onSelectStorageService(service.id)}
                />
              )
            })
          ) : isLoadingNamespaces && visibleNamespaces.length === 0 ? (
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

      {(isStorageMode ? selectedStorageService != null : selectedNs != null) && (
        <div className="cluster-explorer-subtoolbar">
          <div className="flex min-w-0 flex-wrap items-center gap-2">
            {isStorageMode && selectedService != null ? (
              <>
                <span className="cluster-explorer-ns-label inline-flex items-center gap-1.5">
                  {(() => {
                    const ServiceIcon = STORAGE_SERVICE_ICONS[selectedService.id]
                    return <ServiceIcon className="cluster-ns-chip__icon cluster-ns-chip__icon--subtoolbar" aria-hidden />
                  })()}
                  <span className="inline-flex min-w-0 flex-col">
                    <span>{selectedService.label}</span>
                    <span className="text-dense-caption font-normal text-[var(--muted-foreground)]">
                      {selectedService.role}
                    </span>
                  </span>
                </span>
                <StorageServiceInventoryBar
                  serviceId={selectedService.id}
                  workloads={storageServiceWorkloads}
                  loading={isActiveWorkloadsLoading}
                />
              </>
            ) : selectedNs != null ? (
              <>
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
                {readyStatsByNs.has(selectedNs) ? (
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
              </>
            ) : null}
          </div>
          {showStandaloneToggle ? (
            <SegmentControl
              value={workloadView}
              onChange={v => setWorkloadView(v as WorkloadView)}
              options={viewOptions}
              size="sm"
            />
          ) : null}
        </div>
      )}

      <DenseDataTable
        wrapClassName="cluster-explorer-table-scroll dense-scroll-x"
        tableClassName="cluster-explorer-table"
      >
        <colgroup>
          <col className="cluster-col-workload" />
          <col className="cluster-col-arch" />
          <col className="cluster-col-ready" />
          <col className="cluster-col-status" />
          <col className="cluster-col-restarts" />
          <col className="cluster-col-age" />
          <col className="cluster-col-actions" />
        </colgroup>
        <DenseTableHeader>
          <DenseTableHeadRow>
            <DenseTableHead>Workload</DenseTableHead>
            <DenseTableHead>Ideal arch</DenseTableHead>
            <DenseTableHead>Ready</DenseTableHead>
            <DenseTableHead>Status</DenseTableHead>
            <DenseTableHead>Restarts</DenseTableHead>
            <DenseTableHead>Age</DenseTableHead>
            <DenseTableHead title="Pod actions" aria-label="Pod actions">Pod</DenseTableHead>
          </DenseTableHeadRow>
        </DenseTableHeader>
        <DenseTableBody>
          {(isStorageMode ? selectedStorageService == null : selectedNs == null) ? (
            <DenseTableRow>
              <DenseTableCell colSpan={colSpan} className="text-[var(--muted-foreground)]">
                {isStorageMode ? 'Select a storage service above' : 'Select a namespace above'}
              </DenseTableCell>
            </DenseTableRow>
          ) : isActiveWorkloadsLoading ? (
            <DenseTableRow>
              <DenseTableCell colSpan={colSpan} className="text-[var(--muted-foreground)]">
                Loading workloads…
              </DenseTableCell>
            </DenseTableRow>
          ) : isStorageMode && selectedStorageService === 'cnpg' && cnpgView != null ? (
            <>
              <DenseTableSubheadRow>
                <DenseTableCell colSpan={colSpan} className="font-semibold uppercase tracking-wide">
                  Control plane
                </DenseTableCell>
              </DenseTableSubheadRow>
              {cnpgView.operator == null ? (
                <DenseTableRow>
                  <DenseTableCell colSpan={colSpan} className="text-[var(--muted-foreground)]">
                    CNPG operator deployment not found in cnpg-system
                  </DenseTableCell>
                </DenseTableRow>
              ) : (
                <>
                  {(() => {
                    const { deployment, pods } = cnpgView.operator
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
                                <span className="cluster-deployment-row__kind inline-flex flex-wrap items-center gap-1">
                                  CNPG operator
                                  <K8sNamespaceHint namespace={deployment.namespace} />
                                </span>
                                {hasPods ? (
                                  <span className="cluster-deployment-row__pod-count font-mono-tabular">
                                    {pods.length} {pods.length === 1 ? 'pod' : 'pods'}
                                  </span>
                                ) : null}
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
                          <NamespaceIdealArchCell namespace={deployment.namespace} />
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
                              <DenseTableCell className="cursor-pointer" onClick={() => onSelectPod(pod)}>
                                <div className="cluster-pod-row">
                                  <span className="cluster-pod-row__branch" aria-hidden />
                                  <span className="cluster-pod-row__name font-mono-tabular truncate" title={pod.name}>
                                    {pod.name}
                                  </span>
                                </div>
                              </DenseTableCell>
                              <NamespaceIdealArchCell namespace={pod.namespace} />
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
                  })()}
                </>
              )}
              <DenseTableSubheadRow>
                <DenseTableCell colSpan={colSpan} className="font-semibold uppercase tracking-wide">
                  Database cluster · bifrost-postgres
                </DenseTableCell>
              </DenseTableSubheadRow>
              {cnpgView.instances.length === 0 ? (
                <DenseTableRow>
                  <DenseTableCell colSpan={colSpan} className="text-[var(--muted-foreground)]">
                    No bifrost-postgres instances found in data namespace
                  </DenseTableCell>
                </DenseTableRow>
              ) : (
                cnpgView.instances.map(pod => (
                  <DenseTableRow
                    key={pod.name}
                    className={selectedPod === pod.name ? 'dense-table__row--selected' : ''}
                    onClick={() => onSelectPod(pod)}
                    style={{ cursor: 'pointer' }}
                  >
                    <DenseTableCell className="font-mono-tabular break-words [overflow-wrap:anywhere] whitespace-normal" title={pod.name}>
                      <div className="flex min-w-0 flex-col gap-0.5">
                        <span>{pod.name}</span>
                        <span className="cluster-deployment-row__kind inline-flex flex-wrap items-center gap-1 normal-case">
                          Cluster instance
                          <K8sNamespaceHint namespace={pod.namespace} />
                        </span>
                      </div>
                    </DenseTableCell>
                    <NamespaceIdealArchCell namespace={pod.namespace} />
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
              )}
            </>
          ) : workloadView === 'standalone' ? (
            orphanPods.length === 0 ? (
              <DenseTableRow>
                <DenseTableCell colSpan={colSpan} className="text-[var(--muted-foreground)]">
                  No standalone pods — every pod is owned by a deployment in this scope.
                </DenseTableCell>
              </DenseTableRow>
            ) : (
              orphanPods.map(pod => (
                <DenseTableRow
                  key={`${pod.namespace}/${pod.name}`}
                  className={selectedPod === pod.name ? 'dense-table__row--selected' : ''}
                  onClick={() => onSelectPod(pod)}
                  style={{ cursor: 'pointer' }}
                >
                  <DenseTableCell className="font-mono-tabular break-words [overflow-wrap:anywhere] whitespace-normal" title={pod.name}>
                    {pod.name}
                  </DenseTableCell>
                  <NamespaceIdealArchCell namespace={pod.namespace} />
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
                {isStorageMode ? 'No deployments in this storage service' : 'No deployments in this namespace'}
              </DenseTableCell>
            </DenseTableRow>
          ) : (
            groups.map(group => {
              const { deployment, pods } = group
              const expanded = expandedDeployments.has(deployment.name)
              const hasPods = pods.length > 0
              return (
                <Fragment key={`${deployment.namespace}/${deployment.name}`}>
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
                          <span className="cluster-deployment-row__kind inline-flex flex-wrap items-center gap-1">
                            Deployment
                            {isStorageMode ? <K8sNamespaceHint namespace={deployment.namespace} /> : null}
                          </span>
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
                    <NamespaceIdealArchCell namespace={deployment.namespace} />
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
                        <DenseTableCell className="cursor-pointer" onClick={() => onSelectPod(pod)}>
                          <div className="cluster-pod-row">
                            <span className="cluster-pod-row__branch" aria-hidden />
                            <span className="cluster-pod-row__name font-mono-tabular truncate" title={pod.name}>
                              {pod.name}
                            </span>
                          </div>
                        </DenseTableCell>
                        <NamespaceIdealArchCell namespace={pod.namespace} />
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
