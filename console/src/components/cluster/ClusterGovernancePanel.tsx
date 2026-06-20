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
import type { ClusterGovernanceResponse } from '@/api/types'
import { OpsSection } from '@/components/layout/OpsSection'
import { SectionRefreshButton } from '@/components/layout/SectionRefreshButton'
import { StatusLamp } from '@/components/StatusLamp'
import { capabilityTagVariant } from '@/lib/cluster/nodeCapabilitiesCatalog'

interface ClusterGovernancePanelProps {
  data: ClusterGovernanceResponse | undefined
  isLoading: boolean
}

function categoryVariant(category: string): 'neutral' | 'category' | 'info' {
  switch (category) {
    case 'storage':
      return 'info'
    case 'placement':
      return 'category'
    default:
      return 'neutral'
  }
}

export function ClusterGovernancePanel({ data, isLoading }: ClusterGovernancePanelProps) {
  const qc = useQueryClient()
  const fetching = useIsFetching({ queryKey: ['cluster', 'governance'] }) > 0
  const nodeCoverage = data?.node_coverage ?? []
  const clusterCaps = data?.cluster_capabilities ?? []

  const refresh = () => {
    void qc.invalidateQueries({ queryKey: ['cluster', 'governance'] })
    void qc.invalidateQueries({ queryKey: ['cluster', 'nodes'] })
  }

  return (
    <OpsSection
      title="Governance · Capabilities"
      description={
        <>
          Authoritative K3s prep snapshot — node labels + cluster probes. Confirm host prep and
          storage here instead of kubectl.
        </>
      }
      actions={
        <div className="flex flex-wrap items-center gap-2">
          {data != null && (
            <span className="text-[var(--text-dense-meta)] text-[var(--muted-foreground)] inline-flex items-center gap-1">
              <StatusLamp value={data.reachability} kind="reach" />
              {data.detail}
            </span>
          )}
          <SectionRefreshButton isFetching={fetching || isLoading} onClick={refresh} />
        </div>
      }
      bodyPadding="none"
      overflow="hidden"
    >
      <div className="flex flex-col gap-0 divide-y divide-[var(--border)]">
        <section className="px-3 py-2">
          <h4 className="m-0 mb-2 text-xs font-semibold uppercase tracking-wide text-[var(--muted-foreground)]">
            Cluster capabilities
          </h4>
          <DenseDataTable>
            <DenseTableHeader>
              <DenseTableHeadRow>
                <DenseTableHead>Capability</DenseTableHead>
                <DenseTableHead>Category</DenseTableHead>
                <DenseTableHead>Status</DenseTableHead>
                <DenseTableHead>Detail</DenseTableHead>
              </DenseTableHeadRow>
            </DenseTableHeader>
            <DenseTableBody>
              {clusterCaps.length === 0 ? (
                <DenseTableRow>
                  <DenseTableCell colSpan={4} className="text-[var(--muted-foreground)]">
                    {isLoading ? 'Loading…' : 'Cluster unreachable'}
                  </DenseTableCell>
                </DenseTableRow>
              ) : (
                clusterCaps.map(cap => (
                  <DenseTableRow key={cap.id}>
                    <DenseTableCell className="font-medium">{cap.label}</DenseTableCell>
                    <DenseTableCell>
                      <DenseTag variant={categoryVariant(cap.category)}>{cap.category}</DenseTag>
                    </DenseTableCell>
                    <DenseTableCell>
                      <span className="inline-flex items-center gap-1">
                        <StatusLamp value={cap.reachability} kind="reach" />
                        <span className="font-mono-tabular">{cap.status}</span>
                      </span>
                    </DenseTableCell>
                    <DenseTableCell className="text-[var(--muted-foreground)] max-w-md">
                      {cap.detail}
                    </DenseTableCell>
                  </DenseTableRow>
                ))
              )}
            </DenseTableBody>
          </DenseDataTable>
        </section>

        <section className="px-3 py-2">
          <h4 className="m-0 mb-2 text-xs font-semibold uppercase tracking-wide text-[var(--muted-foreground)]">
            Node capability coverage
          </h4>
          <DenseDataTable>
            <DenseTableHeader>
              <DenseTableHeadRow>
                <DenseTableHead>Capability</DenseTableHead>
                <DenseTableHead>Label</DenseTableHead>
                <DenseTableHead>Ready</DenseTableHead>
                <DenseTableHead>Nodes</DenseTableHead>
                <DenseTableHead>Reach</DenseTableHead>
              </DenseTableHeadRow>
            </DenseTableHeader>
            <DenseTableBody>
              {nodeCoverage.length === 0 ? (
                <DenseTableRow>
                  <DenseTableCell colSpan={5} className="text-[var(--muted-foreground)]">
                    {isLoading ? 'Loading…' : 'No node catalog'}
                  </DenseTableCell>
                </DenseTableRow>
              ) : (
                nodeCoverage.map(row => (
                  <DenseTableRow key={row.id}>
                    <DenseTableCell>
                      <span className="inline-flex flex-wrap items-center gap-1">
                        <DenseTag variant={capabilityTagVariant(row.id)}>{row.label}</DenseTag>
                        <DenseTag variant={categoryVariant(row.category)}>{row.category}</DenseTag>
                      </span>
                    </DenseTableCell>
                    <DenseTableCell className="font-mono-tabular text-[var(--muted-foreground)]">
                      {row.label_hint ?? '—'}
                    </DenseTableCell>
                    <DenseTableCell className="font-mono-tabular">
                      {row.nodes_ready}/{row.nodes_total}
                    </DenseTableCell>
                    <DenseTableCell className="font-mono-tabular max-w-[14rem] truncate" title={row.node_names.join(', ')}>
                      {row.node_names.length > 0 ? row.node_names.join(', ') : '—'}
                    </DenseTableCell>
                    <DenseTableCell>
                      <span className="inline-flex items-center gap-1" title={row.gap_reason}>
                        <StatusLamp value={row.reachability} kind="reach" />
                        <span className="font-mono-tabular">{row.reachability}</span>
                      </span>
                    </DenseTableCell>
                  </DenseTableRow>
                ))
              )}
            </DenseTableBody>
          </DenseDataTable>
        </section>
      </div>
    </OpsSection>
  )
}
