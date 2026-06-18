import { useQuery } from '@tanstack/react-query'
import { useCallback, useMemo, useState } from 'react'
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
import { fetchClusterPlacement, fetchDeliveryPipelines } from '@/api/platform'
import { NodeArchLabel } from '@/components/cluster/NodeArchLabel'
import { OpsSection } from '@/components/layout/OpsSection'
import { StatusLamp } from '@/components/StatusLamp'
import {
  buildPlacementLlmPack,
  KANIKO_PIPELINE_NAMES,
  PLACEMENT_CATALOG_VERSION,
} from '@/lib/architecture/workloadPlacementCatalog'

interface PlacementPageProps {
  onOpenDelivery?: () => void
}

export function PlacementPage({ onOpenDelivery }: PlacementPageProps) {
  const [copyState, setCopyState] = useState<'idle' | 'copied' | 'error'>('idle')

  const placementQuery = useQuery({
    queryKey: ['cluster', 'placement'],
    queryFn: fetchClusterPlacement,
    refetchInterval: 30_000,
  })

  const pipelinesQuery = useQuery({
    queryKey: ['delivery', 'pipelines'],
    queryFn: fetchDeliveryPipelines,
    refetchInterval: 30_000,
  })

  const placement = placementQuery.data
  const amd64CiPool = placement?.pools.find(p => p.id === 'amd64_ci')
  const criticalCount = placement?.violations.filter(v => v.severity === 'critical').length ?? 0

  const deliverPreflight = useMemo(() => {
    const p = pipelinesQuery.data?.pipelines.find(x => x.name === 'bifrost-deliver-stg')
    if (p?.build_ready != null) {
      return { ready: p.build_ready, reason: p.block_reason }
    }
    return { ready: amd64CiPool != null && amd64CiPool.nodes_ready > 0, reason: undefined as string | undefined }
  }, [pipelinesQuery.data, amd64CiPool])

  const llmPack = useMemo(
    () =>
      buildPlacementLlmPack(
        placement != null
          ? {
              reachability: placement.reachability,
              detail: placement.detail,
              violations: placement.violations,
            }
          : undefined,
      ),
    [placement],
  )

  const handleCopy = useCallback(async () => {
    try {
      await navigator.clipboard.writeText(llmPack)
      setCopyState('copied')
      window.setTimeout(() => setCopyState('idle'), 2000)
    } catch {
      setCopyState('error')
      window.setTimeout(() => setCopyState('idle'), 3000)
    }
  }, [llmPack])

  return (
    <div className="flex w-full min-w-0 flex-col gap-4">
      <OpsSection
        title="Summary"
        description={`Workload placement governance — catalog v${PLACEMENT_CATALOG_VERSION}. Live vs planned node pools and scheduling policy.`}
        actions={
          <Button size="sm" onClick={() => void handleCopy()}>
            {copyState === 'copied' ? 'Copied!' : copyState === 'error' ? 'Copy failed' : 'Copy LLM pack'}
          </Button>
        }
        headerExtra={
          placementQuery.isLoading ? (
            <p className="m-0 mt-2 text-[var(--text-dense-meta)] text-[var(--muted-foreground)]">Loading…</p>
          ) : placement != null ? (
            <div className="m-0 mt-2 flex flex-wrap items-center gap-3 text-[var(--text-dense-meta)]">
              <span className="inline-flex items-center gap-1.5">
                <StatusLamp value={placement.reachability} kind="reach" />
                {placement.detail}
              </span>
              <DenseTag variant={amd64CiPool != null && amd64CiPool.nodes_ready > 0 ? 'success' : 'danger'}>
                amd64_ci Ready: {amd64CiPool?.nodes_ready ?? 0}
              </DenseTag>
              <DenseTag variant={criticalCount === 0 ? 'success' : 'danger'}>
                Violations: {criticalCount} critical
              </DenseTag>
            </div>
          ) : null
        }
        overflow="visible"
      />

      <OpsSection title="Node pools" bodyPadding="none" overflow="hidden">
        <div className="grid gap-3 p-3 sm:grid-cols-2">
          {(placement?.pools ?? []).map(pool => (
            <div
              key={pool.id}
              className="rounded-md border border-[var(--border)] bg-[var(--secondary)] px-3 py-2"
            >
              <div className="flex items-center justify-between gap-2">
                <span className="font-medium text-[var(--text-dense-label)]">{pool.label}</span>
                <DenseTag variant={pool.status === 'live' ? 'success' : pool.status === 'planned' ? 'neutral' : 'warning'}>
                  {pool.status}
                </DenseTag>
              </div>
              <p className="m-0 mt-1 flex flex-wrap items-center gap-2 font-mono-tabular text-[var(--text-dense-meta)] text-[var(--muted-foreground)]">
                {pool.arch != null && pool.arch !== '' ? <NodeArchLabel arch={pool.arch} showTooltip={false} /> : null}
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
          {placementQuery.isLoading && (
            <p className="col-span-2 px-1 text-[var(--muted-foreground)]">Loading pools…</p>
          )}
        </div>
      </OpsSection>

      <OpsSection title="Policy matrix" bodyPadding="none" overflow="hidden" bodyClassName="ops-section-body--table">
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
            {(placement?.rules ?? []).map(rule => (
              <DenseTableRow key={`${rule.workload_class}-${rule.namespace}`}>
                <DenseTableCell className="font-mono-tabular">{rule.workload_class}</DenseTableCell>
                <DenseTableCell className="font-mono-tabular">{rule.namespace}</DenseTableCell>
                <DenseTableCell className="text-[var(--text-dense-meta)]">{rule.required_selector}</DenseTableCell>
                <DenseTableCell>
                  <StatusLamp value={rule.reachability} kind="reach" />{' '}
                  {rule.satisfied ? 'OK' : 'Gap'}
                </DenseTableCell>
                <DenseTableCell className="text-[var(--text-dense-meta)] text-[var(--muted-foreground)]">
                  {rule.gap_reason ?? (rule.planned_binding != null ? `target ${rule.planned_binding}` : '—')}
                </DenseTableCell>
              </DenseTableRow>
            ))}
            {placementQuery.isLoading && (
              <DenseTableRow>
                <DenseTableCell colSpan={5} className="text-[var(--muted-foreground)]">
                  Loading…
                </DenseTableCell>
              </DenseTableRow>
            )}
          </DenseTableBody>
        </DenseDataTable>
      </OpsSection>

      <OpsSection
        title="CI readiness"
        description="Kaniko pipelines require amd64_ci pool ≥1 Ready node."
        overflow="visible"
        actions={
          onOpenDelivery != null ? (
            <Button size="sm" variant="outline" onClick={onOpenDelivery}>
              Open Delivery
            </Button>
          ) : undefined
        }
        headerExtra={
          <div className="m-0 mt-2 flex flex-col gap-2 text-[var(--text-dense-meta)]">
            <p className="m-0 inline-flex items-center gap-2">
              <StatusLamp value={deliverPreflight.ready ? 'ok' : 'fail'} kind="reach" />
              <span>
                bifrost-deliver-stg: {deliverPreflight.ready ? 'build ready' : 'blocked'}
              </span>
            </p>
            {!deliverPreflight.ready && deliverPreflight.reason != null && deliverPreflight.reason !== '' && (
              <p className="m-0 text-[var(--destructive)]">{deliverPreflight.reason}</p>
            )}
            <p className="m-0 text-[var(--muted-foreground)]">
              Kaniko pipelines: {KANIKO_PIPELINE_NAMES.join(', ')}
            </p>
          </div>
        }
      />

      {placement != null && placement.violations.length > 0 && (
        <OpsSection title="Violations" bodyPadding="none" overflow="hidden" bodyClassName="ops-section-body--table">
          <DenseDataTable>
            <DenseTableHeader>
              <DenseTableHeadRow>
                <DenseTableHead>Severity</DenseTableHead>
                <DenseTableHead>Code</DenseTableHead>
                <DenseTableHead>Message</DenseTableHead>
              </DenseTableHeadRow>
            </DenseTableHeader>
            <DenseTableBody>
              {placement.violations.map(v => (
                <DenseTableRow key={`${v.code}-${v.message}`}>
                  <DenseTableCell>
                    <DenseTag variant={v.severity === 'critical' ? 'danger' : 'warning'}>{v.severity}</DenseTag>
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
