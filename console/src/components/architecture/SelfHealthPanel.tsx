import {
  DenseDataTable,
  DenseTableBody,
  DenseTableCell,
  DenseTableHead,
  DenseTableHeadRow,
  DenseTableHeader,
  DenseTableRow,
  DenseTag,
  StatusLamp,
} from '@bifrost/ui'
import { useQuery } from '@tanstack/react-query'
import { fetchSelfHealth } from '@/api/platform'
import type { SelfHealthProbeStatus } from '@/api/types'
import { OpsSection } from '@/components/layout/OpsSection'

const STATUS_TAG: Record<SelfHealthProbeStatus, { variant: 'success' | 'warning' | 'danger' | 'category'; label: string }> = {
  ok: { variant: 'success', label: 'ok' },
  degraded: { variant: 'warning', label: 'degraded' },
  fail: { variant: 'danger', label: 'fail' },
  unknown: { variant: 'category', label: 'unknown' },
}

const STATUS_LAMP: Record<SelfHealthProbeStatus, 'ok' | 'degraded' | 'fail' | 'unknown'> = {
  ok: 'ok',
  degraded: 'degraded',
  fail: 'fail',
  unknown: 'unknown',
}

const CATEGORY_LABEL: Record<string, string> = {
  api: 'Platform API',
  console: 'Platform Console',
  gitops: 'Argo CD sync',
}

export function SelfHealthPanel() {
  const { data, isLoading, error } = useQuery({
    queryKey: ['platform', 'self-health'],
    queryFn: fetchSelfHealth,
    refetchInterval: 30_000,
  })

  const overall = data?.overall ?? 'unknown'
  const probes = data?.probes ?? []

  return (
    <OpsSection
      title="L1 control plane self-health"
      leading={<StatusLamp value={STATUS_LAMP[overall]} kind="reach" />}
      description="Platform probes its own API, Console, and Argo Application status across STG and PROD."
      actions={
        isLoading ? (
          <DenseTag variant="category">Loading…</DenseTag>
        ) : error ? (
          <DenseTag variant="danger">Error</DenseTag>
        ) : (
          <DenseTag variant={STATUS_TAG[overall].variant}>{STATUS_TAG[overall].label}</DenseTag>
        )
      }
      bodyPadding="default"
      overflow="visible"
    >
      {error && (
        <p className="text-dense-meta text-destructive m-0">
          {error instanceof Error ? error.message : 'Failed to load self-health'}
        </p>
      )}
      {probes.length > 0 && (
        <DenseDataTable>
          <DenseTableHeader>
            <DenseTableHeadRow>
              <DenseTableHead>Component</DenseTableHead>
              <DenseTableHead>Env</DenseTableHead>
              <DenseTableHead>Status</DenseTableHead>
              <DenseTableHead>Detail</DenseTableHead>
              <DenseTableHead className="text-right">Latency</DenseTableHead>
            </DenseTableHeadRow>
          </DenseTableHeader>
          <DenseTableBody>
            {probes.map(p => {
              const tag = STATUS_TAG[p.status]
              return (
                <DenseTableRow key={p.id}>
                  <DenseTableCell className="font-medium">
                    {CATEGORY_LABEL[p.category] ?? p.category}
                  </DenseTableCell>
                  <DenseTableCell>
                    <DenseTag variant={p.env === 'prod' ? 'info' : 'category'}>{p.env}</DenseTag>
                  </DenseTableCell>
                  <DenseTableCell>
                    <DenseTag variant={tag.variant}>{tag.label}</DenseTag>
                  </DenseTableCell>
                  <DenseTableCell className="text-[var(--muted-foreground)]">
                    {p.url ? (
                      <a href={p.url} className="text-primary underline" target="_blank" rel="noreferrer">
                        {p.detail}
                      </a>
                    ) : (
                      p.detail
                    )}
                  </DenseTableCell>
                  <DenseTableCell className="text-right font-mono-tabular text-[var(--muted-foreground)]">
                    {p.latency_ms > 0 ? `${p.latency_ms}ms` : '—'}
                  </DenseTableCell>
                </DenseTableRow>
              )
            })}
          </DenseTableBody>
        </DenseDataTable>
      )}
      {data?.generated_at && (
        <p className="m-0 mt-2 text-[var(--text-dense-caption)] text-[var(--muted-foreground)]">
          Last probe: {new Date(data.generated_at).toLocaleString()}
        </p>
      )}
    </OpsSection>
  )
}
