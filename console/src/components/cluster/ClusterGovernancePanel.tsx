import {
  DenseDataTable,
  DenseTableBody,
  DenseTableCell,
  DenseTableHead,
  DenseTableHeadRow,
  DenseTableHeader,
  DenseTableRow,
  DenseTag,
} from '@bifrost/ui'
import { useIsFetching, useQueryClient } from '@tanstack/react-query'
import type {
  ClusterCapabilityCoverage,
  ClusterCapabilityProbe,
  ClusterGovernanceResponse,
  Reachability,
} from '@/api/types'
import { OpsSection } from '@/components/layout/OpsSection'
import { SectionRefreshButton } from '@/components/layout/SectionRefreshButton'
import { StatusLamp } from '@/components/StatusLamp'

interface ClusterGovernancePanelProps {
  data: ClusterGovernanceResponse | undefined
  isLoading: boolean
  compact?: boolean
}

type GovernanceCategory = 'storage' | 'placement' | 'compute' | 'infra'

interface GovernanceGroup {
  key: GovernanceCategory
  title: string
  description: string
  clusterCaps: ClusterCapabilityProbe[]
  nodeCoverage: ClusterCapabilityCoverage[]
}

const GROUP_META: Record<GovernanceCategory, { title: string; description: string }> = {
  storage: { title: 'NFS / Storage', description: 'NFS client nodes, provisioners, StorageClass, data roles' },
  placement: { title: 'Scheduling / Placement', description: 'Elastic compute and production pool taints' },
  compute: { title: 'GPU / Compute', description: 'NVIDIA device plugin, GPU pool node labels' },
  infra: { title: 'Infrastructure', description: 'Control plane, bootstrap, Wake-on-LAN, metrics' },
}

const GROUP_ORDER: GovernanceCategory[] = ['storage', 'placement', 'compute', 'infra']

function buildGroups(
  clusterCaps: ClusterCapabilityProbe[],
  nodeCoverage: ClusterCapabilityCoverage[],
): GovernanceGroup[] {
  const groups: GovernanceGroup[] = GROUP_ORDER.map(key => ({
    key,
    ...GROUP_META[key],
    clusterCaps: [],
    nodeCoverage: [],
  }))

  const groupMap = new Map(groups.map(g => [g.key, g]))

  for (const cap of clusterCaps) {
    const g = groupMap.get(cap.category as GovernanceCategory)
    if (g) g.clusterCaps.push(cap)
    else groupMap.get('infra')!.clusterCaps.push(cap)
  }

  for (const cov of nodeCoverage) {
    const g = groupMap.get(cov.category as GovernanceCategory)
    if (g) g.nodeCoverage.push(cov)
    else groupMap.get('infra')!.nodeCoverage.push(cov)
  }

  return groups
}

function reachVariant(reach: Reachability): 'success' | 'warning' | 'danger' | 'neutral' {
  switch (reach) {
    case 'ok':
      return 'success'
    case 'degraded':
      return 'warning'
    case 'fail':
      return 'danger'
    default:
      return 'neutral'
  }
}

function groupSummary(group: GovernanceGroup): { total: number; gaps: number; reach: Reachability } {
  let total = 0
  let gaps = 0
  for (const cap of group.clusterCaps) {
    total++
    if (cap.reachability !== 'ok') gaps++
  }
  for (const cov of group.nodeCoverage) {
    total++
    if (cov.reachability !== 'ok') gaps++
  }
  const reach: Reachability = gaps === 0 ? 'ok' : gaps === total ? 'fail' : 'degraded'
  return { total, gaps, reach }
}

function GroupCard({ group, compact }: { group: GovernanceGroup; compact: boolean }) {
  const { gaps, reach } = groupSummary(group)
  const isEmpty = group.clusterCaps.length === 0 && group.nodeCoverage.length === 0
  if (isEmpty) return null

  return (
    <OpsSection
      title={group.title}
      description={compact ? undefined : group.description}
      leading={<StatusLamp value={reach} kind="reach" />}
      headerExtra={
        gaps > 0 ? (
          <DenseTag variant={reachVariant(reach)}>{gaps} gap{gaps > 1 ? 's' : ''}</DenseTag>
        ) : null
      }
      bodyPadding="none"
    >
      <DenseDataTable>
        <DenseTableHeader>
          <DenseTableHeadRow>
            <DenseTableHead className="w-5" />
            <DenseTableHead>Capability</DenseTableHead>
            <DenseTableHead>Coverage</DenseTableHead>
            <DenseTableHead>Detail</DenseTableHead>
          </DenseTableHeadRow>
        </DenseTableHeader>
        <DenseTableBody>
          {group.clusterCaps.map(cap => (
            <DenseTableRow key={cap.id}>
              <DenseTableCell className="w-5 pr-0">
                <StatusLamp value={cap.reachability} kind="reach" />
              </DenseTableCell>
              <DenseTableCell className="font-medium whitespace-nowrap">{cap.label}</DenseTableCell>
              <DenseTableCell className="font-mono-tabular text-[var(--muted-foreground)]">
                {cap.status}
              </DenseTableCell>
              <DenseTableCell className="text-[var(--muted-foreground)] max-w-[20rem]">
                {cap.detail}
              </DenseTableCell>
            </DenseTableRow>
          ))}
          {group.nodeCoverage.map(cov => (
            <DenseTableRow key={cov.id}>
              <DenseTableCell className="w-5 pr-0">
                <StatusLamp value={cov.reachability} kind="reach" />
              </DenseTableCell>
              <DenseTableCell className="font-medium whitespace-nowrap">{cov.label}</DenseTableCell>
              <DenseTableCell className="font-mono-tabular">
                {cov.nodes_ready}/{cov.nodes_total} nodes
                {cov.node_names.length > 0 && (
                  <span className="text-[var(--muted-foreground)] ml-1" title={cov.node_names.join(', ')}>
                    ({cov.node_names.join(', ')})
                  </span>
                )}
              </DenseTableCell>
              <DenseTableCell className="text-[var(--muted-foreground)] max-w-[20rem]">
                {cov.gap_reason ?? (cov.reachability === 'ok' ? 'Ready' : '—')}
              </DenseTableCell>
            </DenseTableRow>
          ))}
        </DenseTableBody>
      </DenseDataTable>
    </OpsSection>
  )
}

export function ClusterGovernancePanel({ data, isLoading, compact = false }: ClusterGovernancePanelProps) {
  const qc = useQueryClient()
  const fetching = useIsFetching({ queryKey: ['cluster', 'governance'] }) > 0
  const nodeCoverage = data?.node_coverage ?? []
  const clusterCaps = data?.cluster_capabilities ?? []

  const refresh = () => {
    void qc.invalidateQueries({ queryKey: ['cluster', 'governance'] })
    void qc.invalidateQueries({ queryKey: ['cluster', 'nodes'] })
  }

  const groups = buildGroups(clusterCaps, nodeCoverage)
  const totalGaps = groups.reduce((sum, g) => sum + groupSummary(g).gaps, 0)
  const overallReach: Reachability = data?.reachability ?? 'unknown'

  if (isLoading && data == null) {
    return <p className="m-0 px-3 py-4 text-dense-meta text-[var(--muted-foreground)]">Loading governance snapshot…</p>
  }

  if (data == null) {
    return (
      <p className="m-0 px-3 py-4 text-dense-meta text-[var(--muted-foreground)]">
        Cluster unreachable — cannot load governance.
      </p>
    )
  }

  if (compact) {
    return (
      <div className="flex flex-col gap-3 p-3">
        <section className="rounded-md border border-[var(--border)] bg-[var(--background)]/60 px-3 py-2">
          <div className="flex flex-wrap items-center gap-2">
            <StatusLamp value={overallReach} kind="reach" />
            <span className="text-dense-label font-semibold">
              Governance snapshot {totalGaps === 0 ? 'ok' : `${totalGaps} gap${totalGaps > 1 ? 's' : ''}`}
            </span>
            <SectionRefreshButton isFetching={fetching || isLoading} onClick={refresh} />
          </div>
        </section>
        {groups.map(g => (
          <GroupCard key={g.key} group={g} compact />
        ))}
      </div>
    )
  }

  return (
    <div className="flex flex-col gap-3 p-3">
      <section className="rounded-md border border-[var(--border)] bg-[var(--background)]/60 px-3 py-2">
        <div className="flex flex-wrap items-center justify-between gap-2">
          <div className="flex flex-wrap items-center gap-2">
            <StatusLamp value={overallReach} kind="reach" />
            <span className="text-dense-label font-semibold">
              Governance snapshot {totalGaps === 0 ? 'ok' : `${totalGaps} gap${totalGaps > 1 ? 's' : ''}`}
            </span>
          </div>
          <div className="flex flex-wrap items-center gap-2">
            {groups.map(g => {
              const { gaps, reach } = groupSummary(g)
              if (g.clusterCaps.length === 0 && g.nodeCoverage.length === 0) return null
              return (
                <DenseTag key={g.key} variant={gaps > 0 ? reachVariant(reach) : 'success'}>
                  {g.title} {gaps > 0 ? `${gaps} gap` : '✓'}
                </DenseTag>
              )
            })}
            <SectionRefreshButton isFetching={fetching || isLoading} onClick={refresh} />
          </div>
        </div>
        {data.detail !== '' && (
          <p className="m-0 mt-1 text-dense-meta text-[var(--muted-foreground)]">{data.detail}</p>
        )}
      </section>

      <div className="grid gap-3 lg:grid-cols-2">
        {groups.map(g => (
          <GroupCard key={g.key} group={g} compact={false} />
        ))}
      </div>
    </div>
  )
}
