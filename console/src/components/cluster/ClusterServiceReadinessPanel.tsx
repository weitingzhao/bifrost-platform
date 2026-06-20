import { useState } from 'react'
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
import type { ClusterServiceReadinessResponse, ServiceDomain, ServiceDomainStatus } from '@/api/types'
import { OpsSection } from '@/components/layout/OpsSection'
import { SectionRefreshButton } from '@/components/layout/SectionRefreshButton'
import { StatusLamp } from '@/components/StatusLamp'

interface ClusterServiceReadinessPanelProps {
  data: ClusterServiceReadinessResponse | undefined
  isLoading: boolean
}

function statusVariant(status: ServiceDomainStatus | string): 'success' | 'warning' | 'danger' | 'neutral' {
  switch (status) {
    case 'ready':
      return 'success'
    case 'partial':
    case 'standby':
      return 'warning'
    case 'unavailable':
      return 'danger'
    default:
      return 'neutral'
  }
}

function statusLabel(status: ServiceDomainStatus | string): string {
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

function DependencyList({ domain }: { domain: ServiceDomain }) {
  return (
    <ul className="m-0 list-none space-y-1 py-1">
      {domain.dependencies.map(dep => (
        <li key={dep.id} className="flex items-start gap-1.5 text-dense-meta">
          <StatusLamp value={dep.reachability} kind="reach" />
          <span>
            <span className="font-medium">{dep.label}</span>
            {dep.detail != null && dep.detail !== '' ? (
              <span className="text-[var(--muted-foreground)]"> — {dep.detail}</span>
            ) : null}
          </span>
        </li>
      ))}
    </ul>
  )
}

export function ClusterServiceReadinessPanel({ data, isLoading }: ClusterServiceReadinessPanelProps) {
  const qc = useQueryClient()
  const fetching = useIsFetching({ queryKey: ['cluster', 'service-readiness'] }) > 0
  const [expandedId, setExpandedId] = useState<string | null>(null)
  const domains = data?.domains ?? []

  const refresh = () => {
    void qc.invalidateQueries({ queryKey: ['cluster', 'service-readiness'] })
  }

  return (
    <OpsSection
      title="Service readiness"
      description={
        <>
          Workload-domain view — PostgreSQL, Redis, GPU, warehouse, workers, applications, and CI/CD.
          Aggregates governance, placement, and live deployments.
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
      <DenseDataTable>
        <DenseTableHeader>
          <DenseTableHeadRow>
            <DenseTableHead>Domain</DenseTableHead>
            <DenseTableHead>Status</DenseTableHead>
            <DenseTableHead>Summary</DenseTableHead>
            <DenseTableHead>Deps</DenseTableHead>
          </DenseTableHeadRow>
        </DenseTableHeader>
        <DenseTableBody>
          {domains.length === 0 ? (
            <DenseTableRow>
              <DenseTableCell colSpan={4} className="text-[var(--muted-foreground)]">
                {isLoading ? 'Loading…' : 'Cluster unreachable'}
              </DenseTableCell>
            </DenseTableRow>
          ) : (
            domains.flatMap(domain => {
              const expanded = expandedId === domain.id
              const rows = [
                <DenseTableRow
                  key={domain.id}
                  className="cursor-pointer hover:bg-[var(--secondary)]/60"
                  onClick={() => setExpandedId(expanded ? null : domain.id)}
                >
                  <DenseTableCell className="font-medium">{domain.label}</DenseTableCell>
                  <DenseTableCell>
                    <span className="inline-flex items-center gap-1">
                      <StatusLamp value={domain.reachability} kind="reach" />
                      <DenseTag variant={statusVariant(domain.status)}>{statusLabel(domain.status)}</DenseTag>
                    </span>
                  </DenseTableCell>
                  <DenseTableCell className="text-[var(--muted-foreground)] max-w-md">
                    {domain.summary}
                  </DenseTableCell>
                  <DenseTableCell className="font-mono-tabular text-[var(--muted-foreground)]">
                    {domain.dependencies.length}
                    {expanded ? ' ▾' : ' ▸'}
                  </DenseTableCell>
                </DenseTableRow>,
              ]
              if (expanded) {
                rows.push(
                  <DenseTableRow key={`${domain.id}-deps`}>
                    <DenseTableCell colSpan={4} className="!whitespace-normal bg-[var(--background)]/60">
                      <DependencyList domain={domain} />
                    </DenseTableCell>
                  </DenseTableRow>,
                )
              }
              return rows
            })
          )}
        </DenseTableBody>
      </DenseDataTable>
    </OpsSection>
  )
}
