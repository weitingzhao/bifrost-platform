/**
 * Facility constraint detail panels — former Placement page Body.
 * Hosted under Cluster → Categories → Facility.
 */

import { useMemo } from 'react'
import { useQuery } from '@tanstack/react-query'
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
import { fetchDeliveryPipelines } from '@/api/delivery'
import type { ClusterPlacementResponse } from '@/api/clusterTypes'
import { NodeArchLabel } from '@/components/cluster/NodeArchLabel'
import { OpsSection } from '@/components/layout/OpsSection'
import { StatusLamp } from '@/components/StatusLamp'
import { KANIKO_PIPELINE_NAMES, PLACEMENT_CATALOG_VERSION } from '@/lib/architecture/workloadPlacementCatalog'
import type { FacilityCategory } from '@/lib/cluster/clusterCategories'

interface FacilityPanelProps {
  placement: ClusterPlacementResponse | undefined
  isLoading: boolean
  onSelectNodes?: () => void
  onOpenDelivery?: () => void
}

export function ClusterFacilityPoolsPanel({ placement, isLoading }: FacilityPanelProps) {
  const pools = placement?.pools ?? []
  return (
    <div className="flex flex-col gap-3">
      <p className="m-0 text-dense-caption text-muted-foreground">
        Fleet facility pools (catalog v{PLACEMENT_CATALOG_VERSION}) — Rocket CI, Satellite runtime,
        shared infra.
      </p>
      <div className="grid gap-3 sm:grid-cols-2">
        {pools.map(pool => (
          <div
            key={pool.id}
            className="rounded-md border border-[var(--border)] bg-[var(--secondary)] px-3 py-2"
          >
            <div className="flex items-center justify-between gap-2">
              <span className="font-medium text-[var(--text-dense-label)]">{pool.label}</span>
              <DenseTag
                variant={
                  pool.status === 'live' ? 'success' : pool.status === 'planned' ? 'neutral' : 'warning'
                }
              >
                {pool.status}
              </DenseTag>
            </div>
            <p className="m-0 mt-1 flex flex-wrap items-center gap-2 font-mono-tabular text-[var(--text-dense-meta)] text-[var(--muted-foreground)]">
              {pool.arch != null && pool.arch !== '' ? (
                <NodeArchLabel arch={pool.arch} showTooltip={false} />
              ) : null}
              {pool.workload_label != null && pool.workload_label !== '' ? (
                <span>workload={pool.workload_label}</span>
              ) : null}
            </p>
            <p className="m-0 mt-1 text-[var(--text-dense-meta)]">
              Ready {pool.nodes_ready}/{pool.nodes_total}
              {pool.planned_host != null && pool.planned_host !== '' ? (
                <span className="text-[var(--muted-foreground)]"> · planned {pool.planned_host}</span>
              ) : null}
            </p>
            {pool.node_names.length > 0 ? (
              <p className="m-0 mt-1 font-mono-tabular text-[var(--text-dense-caption)] text-[var(--muted-foreground)]">
                {pool.node_names.join(', ')}
              </p>
            ) : null}
          </div>
        ))}
        {isLoading && (
          <p className="col-span-2 px-1 text-[var(--muted-foreground)]">Loading pools…</p>
        )}
        {!isLoading && pools.length === 0 && (
          <p className="col-span-2 m-0 text-dense-meta text-muted-foreground">No placement pools.</p>
        )}
      </div>
    </div>
  )
}

export function ClusterFacilityPolicyPanel({
  placement,
  isLoading,
  onSelectNodes,
}: FacilityPanelProps) {
  const rules = placement?.rules ?? []
  const violations = placement?.violations ?? []

  return (
    <div className="flex flex-col gap-3">
      <DenseDataTable>
        <DenseTableHeader>
          <DenseTableHeadRow>
            <DenseTableHead>Workload</DenseTableHead>
            <DenseTableHead>Namespace</DenseTableHead>
            <DenseTableHead>Required selector</DenseTableHead>
            <DenseTableHead>Status</DenseTableHead>
            <DenseTableHead>Gap</DenseTableHead>
          </DenseTableHeadRow>
        </DenseTableHeader>
        <DenseTableBody>
          {rules.map(rule => (
            <DenseTableRow key={`${rule.workload_class}-${rule.namespace}`}>
              <DenseTableCell className="font-mono-tabular">{rule.workload_class}</DenseTableCell>
              <DenseTableCell className="font-mono-tabular">{rule.namespace}</DenseTableCell>
              <DenseTableCell className="text-[var(--text-dense-meta)]">
                {rule.required_selector}
              </DenseTableCell>
              <DenseTableCell>
                <StatusLamp value={rule.reachability} kind="reach" />{' '}
                {rule.satisfied ? 'OK' : 'Gap'}
              </DenseTableCell>
              <DenseTableCell className="text-[var(--text-dense-meta)] text-[var(--muted-foreground)]">
                <span className="inline-flex flex-wrap items-center gap-2">
                  <span>
                    {rule.gap_reason ??
                      (rule.planned_binding != null ? `target ${rule.planned_binding}` : '—')}
                  </span>
                  {!rule.satisfied && onSelectNodes != null ? (
                    <Button
                      type="button"
                      variant="link"
                      size="sm"
                      className="h-auto px-0 py-0 text-[var(--text-dense-caption)]"
                      onClick={onSelectNodes}
                    >
                      View nodes
                    </Button>
                  ) : null}
                </span>
              </DenseTableCell>
            </DenseTableRow>
          ))}
          {isLoading && (
            <DenseTableRow>
              <DenseTableCell colSpan={5} className="text-[var(--muted-foreground)]">
                Loading…
              </DenseTableCell>
            </DenseTableRow>
          )}
          {!isLoading && rules.length === 0 && (
            <DenseTableRow>
              <DenseTableCell colSpan={5} className="text-[var(--muted-foreground)]">
                No placement rules.
              </DenseTableCell>
            </DenseTableRow>
          )}
        </DenseTableBody>
      </DenseDataTable>

      {violations.length > 0 && (
        <OpsSection
          id="placement-violations"
          className="scroll-mt-16"
          title="Violations"
          bodyPadding="none"
          overflow="hidden"
          bodyClassName="ops-section-body--table"
        >
          <DenseDataTable>
            <DenseTableHeader>
              <DenseTableHeadRow>
                <DenseTableHead>Severity</DenseTableHead>
                <DenseTableHead>Code</DenseTableHead>
                <DenseTableHead>Message</DenseTableHead>
              </DenseTableHeadRow>
            </DenseTableHeader>
            <DenseTableBody>
              {violations.map(v => (
                <DenseTableRow key={`${v.code}-${v.message}`}>
                  <DenseTableCell>
                    <DenseTag variant={v.severity === 'critical' ? 'danger' : 'warning'}>
                      {v.severity}
                    </DenseTag>
                  </DenseTableCell>
                  <DenseTableCell className="font-mono-tabular">{v.code}</DenseTableCell>
                  <DenseTableCell className="text-[var(--text-dense-meta)]">{v.message}</DenseTableCell>
                </DenseTableRow>
              ))}
            </DenseTableBody>
          </DenseDataTable>
        </OpsSection>
      )}
    </div>
  )
}

export function ClusterFacilityCiPanel({
  placement,
  onOpenDelivery,
}: FacilityPanelProps) {
  const amd64CiPool = placement?.pools.find(p => p.id === 'amd64_ci')
  const pipelinesQuery = useQuery({
    queryKey: ['delivery', 'pipelines'],
    queryFn: fetchDeliveryPipelines,
    refetchInterval: 30_000,
  })

  const deliverPreflight = useMemo(() => {
    const p = pipelinesQuery.data?.pipelines.find(x => x.name === 'bifrost-deliver-stg')
    if (p?.build_ready != null) {
      return { ready: p.build_ready, reason: p.block_reason }
    }
    return {
      ready: amd64CiPool != null && amd64CiPool.nodes_ready > 0,
      reason: undefined as string | undefined,
    }
  }, [pipelinesQuery.data, amd64CiPool])

  return (
    <div className="flex flex-col gap-3">
      <p className="m-0 text-dense-meta text-muted-foreground">
        Kaniko pipelines require amd64_ci pool ≥1 Ready node.
      </p>
      <p className="m-0 inline-flex items-center gap-2 text-dense-meta">
        <StatusLamp value={deliverPreflight.ready ? 'ok' : 'fail'} kind="reach" />
        <span>bifrost-deliver-stg: {deliverPreflight.ready ? 'build ready' : 'blocked'}</span>
      </p>
      {!deliverPreflight.ready &&
        deliverPreflight.reason != null &&
        deliverPreflight.reason !== '' && (
          <p className="m-0 text-dense-meta text-[var(--destructive)]">{deliverPreflight.reason}</p>
        )}
      <p className="m-0 text-dense-caption text-muted-foreground">
        Kaniko pipelines: {KANIKO_PIPELINE_NAMES.join(', ')}
      </p>
      {amd64CiPool != null && (
        <p className="m-0 text-dense-meta">
          amd64_ci Ready: {amd64CiPool.nodes_ready}/{amd64CiPool.nodes_total}
        </p>
      )}
      {onOpenDelivery != null && (
        <div>
          <Button size="sm" variant="outline" onClick={onOpenDelivery}>
            Open Delivery
          </Button>
        </div>
      )}
    </div>
  )
}

export function ClusterFacilityDetailBody({
  category,
  placement,
  isLoading,
  onSelectNodes,
  onOpenDelivery,
}: FacilityPanelProps & { category: FacilityCategory }) {
  switch (category) {
    case 'node_pools':
      return <ClusterFacilityPoolsPanel placement={placement} isLoading={isLoading} />
    case 'policy_matrix':
      return (
        <ClusterFacilityPolicyPanel
          placement={placement}
          isLoading={isLoading}
          onSelectNodes={onSelectNodes}
        />
      )
    case 'ci_readiness':
      return (
        <ClusterFacilityCiPanel
          placement={placement}
          isLoading={isLoading}
          onOpenDelivery={onOpenDelivery}
        />
      )
    default:
      return null
  }
}
