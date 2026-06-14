import type { ClusterObservabilityResponse, LayerBStatus } from '@/api/types'
import { StatusLamp } from '@/components/StatusLamp'
import { DOC_LINKS } from '@/lib/docsLinks'

interface ClusterObservabilityPanelProps {
  data: ClusterObservabilityResponse | undefined
  isLoading: boolean
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

function resolveK3sDocs(data: ClusterObservabilityResponse | undefined): string {
  const url = data?.docs_url?.trim()
  return url != null && url !== '' ? url : DOC_LINKS.k3sObservability
}

export function ClusterObservabilityPanel({ data, isLoading }: ClusterObservabilityPanelProps) {
  const k3sDocs = resolveK3sDocs(data)
  const components = data?.components ?? []

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
          <a
            className="btn-ui text-[var(--text-dense-meta)]"
            href={k3sDocs}
            target="_blank"
            rel="noreferrer"
          >
            K3s phase 3 docs
          </a>
          <a
            className="btn-ui text-[var(--text-dense-meta)]"
            href={DOC_LINKS.clusterActuation}
            target="_blank"
            rel="noreferrer"
          >
            Platform CLUSTER_ACTUATION
          </a>
          {data?.grafana_url != null && data.grafana_url !== '' && data.layer_b_status === 'ready' && (
            <a
              className="btn-ui btn-ui-primary"
              href={data.grafana_url}
              target="_blank"
              rel="noreferrer"
            >
              Open Grafana
            </a>
          )}
        </div>
      </header>

      <div className="dense-table-scroll">
        <table className="dense-table">
          <thead>
            <tr>
              <th>Component</th>
              <th>Ready</th>
              <th>Status</th>
              <th>Action</th>
            </tr>
          </thead>
          <tbody>
            {isLoading || data == null ? (
              <tr>
                <td colSpan={4} className="text-[var(--muted-foreground)]">
                  Loading…
                </td>
              </tr>
            ) : components.length === 0 ? (
              <tr>
                <td colSpan={4} className="text-[var(--muted-foreground)]">
                  No observability components detected
                </td>
              </tr>
            ) : (
              components.map(component => (
                <tr key={component.id}>
                  <td>
                    <span className="font-mono-tabular">{component.label}</span>
                    {component.name !== '—' && (
                      <span className="ml-2 text-[var(--text-dense-meta)] text-[var(--muted-foreground)]">
                        {component.kind}/{component.name}
                      </span>
                    )}
                  </td>
                  <td className="font-mono-tabular">{component.ready}</td>
                  <td>
                    <StatusLamp value={component.reachability} kind="reach" />{' '}
                    <span className="font-mono-tabular">{component.status}</span>
                  </td>
                  <td>
                    {component.id === 'grafana' &&
                    data.grafana_url != null &&
                    data.grafana_url !== '' &&
                    component.reachability === 'ok' ? (
                      <a
                        className="btn-ui text-[var(--text-dense-meta)]"
                        href={data.grafana_url}
                        target="_blank"
                        rel="noreferrer"
                      >
                        Open Grafana
                      </a>
                    ) : (
                      '—'
                    )}
                  </td>
                </tr>
              ))
            )}
          </tbody>
        </table>
      </div>
    </section>
  )
}
