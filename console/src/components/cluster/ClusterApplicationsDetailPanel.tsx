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
import { useQueryClient } from '@tanstack/react-query'
import type {
  ClusterServiceReadinessResponse,
  Reachability,
  ServiceDependency,
  ServiceDomain,
} from '@/api/types'
import { OpsSection } from '@/components/layout/OpsSection'
import { SectionRefreshButton } from '@/components/layout/SectionRefreshButton'
import { StatusLamp } from '@/components/StatusLamp'

interface ClusterApplicationsDetailPanelProps {
  serviceReadiness: ClusterServiceReadinessResponse | undefined
  isLoading: boolean
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

function statusLabel(status: string): string {
  switch (status) {
    case 'ready':
      return 'Ready'
    case 'partial':
      return 'Partial'
    case 'standby':
      return 'Standby'
    case 'unavailable':
      return 'Unavailable'
    default:
      return status
  }
}

function applicationsDomain(
  readiness: ClusterServiceReadinessResponse | undefined,
): ServiceDomain | undefined {
  return readiness?.domains.find(d => d.id === 'applications')
}

function isOptionalDep(dep: ServiceDependency): boolean {
  return dep.id === 'pool-arm64_edge' || dep.label.toLowerCase().includes('optional')
}

function isRuntimeDep(dep: ServiceDependency): boolean {
  return dep.id.startsWith('schedulable') || (dep.id.startsWith('pool-') && !isOptionalDep(dep))
}

function isStackDep(dep: ServiceDependency): boolean {
  return !isOptionalDep(dep) && !isRuntimeDep(dep)
}

function DepRow({ dep }: { dep: ServiceDependency }) {
  return (
    <div className="flex items-start gap-2 text-dense-meta">
      <StatusLamp value={dep.reachability} kind="reach" />
      <span>
        <span className="font-medium">{dep.label}</span>
        {dep.detail != null && dep.detail !== '' ? (
          <span className="text-[var(--muted-foreground)]"> — {dep.detail}</span>
        ) : null}
      </span>
    </div>
  )
}

function stackRows(deps: ServiceDependency[]): ServiceDependency[] {
  return deps.filter(isStackDep)
}

function runtimeRows(deps: ServiceDependency[]): ServiceDependency[] {
  return deps.filter(isRuntimeDep)
}

function optionalRows(deps: ServiceDependency[]): ServiceDependency[] {
  return deps.filter(isOptionalDep)
}

export function ClusterApplicationsDetailPanel({
  serviceReadiness,
  isLoading,
}: ClusterApplicationsDetailPanelProps) {
  const qc = useQueryClient()
  const domain = applicationsDomain(serviceReadiness)
  const deps = domain?.dependencies ?? []
  const stack = stackRows(deps)
  const runtime = runtimeRows(deps)
  const optional = optionalRows(deps)

  const refresh = () => {
    void qc.invalidateQueries({ queryKey: ['cluster', 'service-readiness'] })
  }

  if (isLoading && domain == null) {
    return (
      <p className="m-0 px-3 py-4 text-dense-meta text-[var(--muted-foreground)]">
        Loading Trade stack status…
      </p>
    )
  }

  if (domain == null) {
    return (
      <p className="m-0 px-3 py-4 text-dense-meta text-[var(--muted-foreground)]">
        Cluster unreachable — cannot load application domain status.
      </p>
    )
  }

  const stgStack = stack.filter(d => d.label.includes('(stg)') || d.id === 'apis')
  const envIngress = stack.filter(d => d.label.includes('(dev)') || d.label.includes('(prod)'))

  return (
    <div className="cluster-applications-detail flex flex-col gap-3 p-3">
      <section className="rounded-md border border-[var(--border)] bg-[var(--background)]/60 px-3 py-2">
        <div className="flex flex-wrap items-center justify-between gap-2">
          <div className="flex flex-wrap items-center gap-2">
            <StatusLamp value={domain.reachability} kind="reach" />
            <span className="text-dense-label font-semibold">{domain.summary}</span>
          </div>
          <div className="flex flex-wrap items-center gap-2">
            <DenseTag variant={reachVariant(domain.reachability)}>{statusLabel(domain.status)}</DenseTag>
            <DenseTag variant="category">amd64 Trade stack</DenseTag>
            <SectionRefreshButton isFetching={isLoading} onClick={refresh} />
          </div>
        </div>
        <p className="m-0 mt-1 text-dense-meta text-[var(--muted-foreground)]">
          nginx · frontend · 9 FastAPI domains on amd64. arm64 edge pool is optional — absent nodes do not
          block this domain.
        </p>
      </section>

      <div className="grid gap-3 lg:grid-cols-2">
        <OpsSection
          title="STG exposure"
          description="Primary Trade gateway — bifrost-stg namespace"
          bodyPadding="compact"
        >
          <div className="space-y-2">
            {stgStack.length === 0 ? (
              <p className="m-0 text-dense-meta text-[var(--muted-foreground)]">No stg workloads probed.</p>
            ) : (
              stgStack.map(dep => <DepRow key={dep.id} dep={dep} />)
            )}
          </div>
        </OpsSection>

        <OpsSection title="Runtime placement" description="Schedulable capacity for app pods" bodyPadding="compact">
          <div className="space-y-2">
            {runtime.length === 0 ? (
              <p className="m-0 text-dense-meta text-[var(--muted-foreground)]">No runtime deps.</p>
            ) : (
              runtime.map(dep => <DepRow key={dep.id} dep={dep} />)
            )}
          </div>
        </OpsSection>
      </div>

      <div className="grid gap-3 lg:grid-cols-2">
        <OpsSection
          title="Other environments"
          description="Ingress probes for dev / prod overlays"
          bodyPadding="none"
        >
          <DenseDataTable>
            <DenseTableHeader>
              <DenseTableHeadRow>
                <DenseTableHead>Component</DenseTableHead>
                <DenseTableHead>Status</DenseTableHead>
                <DenseTableHead>Detail</DenseTableHead>
              </DenseTableHeadRow>
            </DenseTableHeader>
            <DenseTableBody>
              {envIngress.length === 0 ? (
                <DenseTableRow>
                  <DenseTableCell colSpan={3} className="text-[var(--muted-foreground)]">
                    No dev/prod ingress deployed yet
                  </DenseTableCell>
                </DenseTableRow>
              ) : (
                envIngress.map(dep => (
                  <DenseTableRow key={dep.id}>
                    <DenseTableCell className="font-medium">{dep.label}</DenseTableCell>
                    <DenseTableCell>
                      <span className="inline-flex items-center gap-1">
                        <StatusLamp value={dep.reachability} kind="reach" />
                        <DenseTag variant={reachVariant(dep.reachability)}>
                          {dep.reachability === 'ok' ? 'Ready' : dep.reachability}
                        </DenseTag>
                      </span>
                    </DenseTableCell>
                    <DenseTableCell className="text-[var(--muted-foreground)]">{dep.detail ?? '—'}</DenseTableCell>
                  </DenseTableRow>
                ))
              )}
            </DenseTableBody>
          </DenseDataTable>
        </OpsSection>

        <OpsSection
          title="Optional edge pools"
          description="Not required when running amd64-only (no arm64 nodes in cluster)"
          bodyPadding="compact"
        >
          <div className="space-y-2">
            {optional.length === 0 ? (
              <p className="m-0 text-dense-meta text-[var(--muted-foreground)]">No optional pools in catalog.</p>
            ) : (
              optional.map(dep => (
                <div
                  key={dep.id}
                  className="rounded border border-dashed border-[var(--border)] bg-[var(--background)]/40 px-2 py-1.5"
                >
                  <DepRow dep={dep} />
                </div>
              ))
            )}
          </div>
        </OpsSection>
      </div>
    </div>
  )
}
