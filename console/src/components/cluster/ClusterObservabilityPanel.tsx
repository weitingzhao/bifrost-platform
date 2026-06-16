import { Button, DenseDataTable, DenseTableHeader, DenseTableBody, DenseTableHeadRow, DenseTableRow, DenseTableHead, DenseTableCell } from '@bifrost/ui'
import type { ClusterObservabilityResponse, LayerBStatus } from '@/api/types'
import { StatusLamp } from '@/components/StatusLamp'

interface ClusterObservabilityPanelProps {
  data: ClusterObservabilityResponse | undefined
  isLoading: boolean
  onOpenStandards?: () => void
  onOpenEnvironments?: () => void
}

function layerBHeadline(status: LayerBStatus | undefined): string {
  switch (status) {
    case 'ready':
      return 'Ready · observability stack detected'
    case 'partial':
      return 'Partial · some observability components running'
    case 'not_installed':
    default:
      return 'Planned · kube-prometheus-stack not detected'
  }
}

function layerBLamp(status: LayerBStatus | undefined) {
  switch (status) {
    case 'ready':
      return 'ok' as const
    case 'partial':
      return 'degraded' as const
    default:
      return 'unknown' as const
  }
}

export function ClusterObservabilityPanel({
  data,
  isLoading,
  onOpenStandards,
  onOpenEnvironments,
}: ClusterObservabilityPanelProps) {
  const components = data?.components ?? []
  const docsUrl = data?.docs_url?.trim()

  return (
    <section className="page-section panel-elevated overflow-hidden">
      <header className="border-b border-[var(--border)] px-3 py-2">
        <div className="flex flex-wrap items-center justify-between gap-2">
          <h2 className="m-0 text-sm font-semibold">Observability — Layer B</h2>
          <span className="text-[var(--text-dense-meta)] text-[var(--muted-foreground)]">
            {isLoading ? '…' : data?.namespace ?? 'monitoring'}
          </span>
        </div>
        {!isLoading && data != null && (
          <p className="m-0 mt-2 flex flex-wrap items-center gap-2 text-[var(--text-dense-meta)]">
            <StatusLamp value={layerBLamp(data.layer_b_status)} kind="reach" />
            <span>{layerBHeadline(data.layer_b_status)}</span>
          </p>
        )}
        {data?.layer_b_status === 'not_installed' && (
          <p className="m-0 mt-2 text-[var(--text-dense-meta)] text-[var(--muted-foreground)]">
            Layer A above uses metrics-server only. Layer B adds historical metrics, disk I/O, logs,
            and alerts.
          </p>
        )}
        {data?.layer_b_status === 'partial' && data.detail !== '' && (
          <p className="m-0 mt-2 text-[var(--text-dense-meta)] text-[var(--muted-foreground)]">
            {data.detail}
          </p>
        )}
        <div className="mt-2 flex flex-wrap gap-2">
          {onOpenStandards != null && (
            <Button variant="outline" size="sm" className="text-[var(--text-dense-meta)]" onClick={onOpenStandards}>
              Open Standards
            </Button>
          )}
          {onOpenEnvironments != null && (
            <Button variant="outline" size="sm" className="text-[var(--text-dense-meta)]" onClick={onOpenEnvironments}>
              Open Environments
            </Button>
          )}
          {docsUrl != null && docsUrl !== '' && (
            <Button variant="outline" size="sm" className="text-[var(--text-dense-meta)]" asChild>
              <a href={docsUrl} target="_blank" rel="noreferrer">
                External docs
              </a>
            </Button>
          )}
          {data?.grafana_url != null && data.grafana_url !== '' && data.layer_b_status === 'ready' && (
            <Button size="sm" asChild>
              <a href={data.grafana_url} target="_blank" rel="noreferrer">
                Open Grafana
              </a>
            </Button>
          )}
        </div>
      </header>

      <DenseDataTable>
        <DenseTableHeader>
          <DenseTableHeadRow>
            <DenseTableHead>Component</DenseTableHead>
            <DenseTableHead>Ready</DenseTableHead>
            <DenseTableHead>Status</DenseTableHead>
            <DenseTableHead>Action</DenseTableHead>
          </DenseTableHeadRow>
        </DenseTableHeader>
        <DenseTableBody>
          {isLoading || data == null ? (
            <DenseTableRow>
              <DenseTableCell colSpan={4} className="text-[var(--muted-foreground)]">
                Loading…
              </DenseTableCell>
            </DenseTableRow>
          ) : components.length === 0 ? (
            <DenseTableRow>
              <DenseTableCell colSpan={4} className="text-[var(--muted-foreground)]">
                No observability components detected
              </DenseTableCell>
            </DenseTableRow>
          ) : (
            components.map(component => (
              <DenseTableRow key={component.id}>
                <DenseTableCell>
                  <span className="font-mono-tabular">{component.label}</span>
                  {component.name !== '—' && (
                    <span className="ml-2 text-[var(--text-dense-meta)] text-[var(--muted-foreground)]">
                      {component.kind}/{component.name}
                    </span>
                  )}
                </DenseTableCell>
                <DenseTableCell className="font-mono-tabular">{component.ready}</DenseTableCell>
                <DenseTableCell>
                  <StatusLamp value={component.reachability} kind="reach" />{' '}
                  <span className="font-mono-tabular">{component.status}</span>
                </DenseTableCell>
                <DenseTableCell>
                  {component.id === 'grafana' &&
                  data.grafana_url != null &&
                  data.grafana_url !== '' &&
                  component.reachability === 'ok' ? (
                    <Button variant="outline" size="sm" className="text-[var(--text-dense-meta)]" asChild>
                      <a href={data.grafana_url} target="_blank" rel="noreferrer">
                        Open Grafana
                      </a>
                    </Button>
                  ) : (
                    '—'
                  )}
                </DenseTableCell>
              </DenseTableRow>
            ))
          )}
        </DenseTableBody>
      </DenseDataTable>
    </section>
  )
}
