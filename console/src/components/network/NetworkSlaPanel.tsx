import { DenseTag, StatusLamp } from '@bifrost/ui'
import type { NetworkAnomaliesResponse, NetworkSlaResponse } from '@/api/networkTypes'
import { OpsSection } from '@/components/layout/OpsSection'

interface NetworkSlaPanelProps {
  sla: NetworkSlaResponse | undefined
  anomalies: NetworkAnomaliesResponse | undefined
  isLoading: boolean
}

function slaReach(sla: NetworkSlaResponse | undefined): 'ok' | 'degraded' | 'fail' | 'unknown' {
  if (sla == null) return 'unknown'
  if (sla.error != null && sla.error !== '') return 'fail'
  if (sla.probe_ok !== true) return 'fail'
  const frac = sla.devices_up_fraction ?? 1
  if (frac < 1) return 'degraded'
  return 'ok'
}

export function NetworkSlaPanel({ sla, anomalies, isLoading }: NetworkSlaPanelProps) {
  const reach = slaReach(sla)
  const alerts = anomalies?.alerts ?? []
  const tips = sla?.tips?.length ? sla.tips : anomalies?.tips ?? []

  return (
    <OpsSection
      title="SLA & predictive-lite"
      description="Probe success / device up fraction from GET /api/v1/network/sla · rule tips from anomalies (L0)."
      bodyPadding="default"
      overflow="visible"
    >
      <div className="mb-3 flex flex-wrap items-center gap-2">
        <StatusLamp value={reach} kind="reach" />
        <p className="m-0 text-[var(--text-dense-label)] font-medium">
          {isLoading ? 'Loading SLA…' : (sla?.summary ?? 'No SLA data')}
        </p>
        <DenseTag variant="info">L0</DenseTag>
        <DenseTag variant={reach === 'ok' ? 'success' : reach === 'degraded' ? 'warning' : reach === 'fail' ? 'danger' : 'neutral'}>
          {reach.toUpperCase()}
        </DenseTag>
        <code className="text-[var(--text-dense-caption)] font-mono">source: {sla?.source ?? 'network_probe'}</code>
      </div>

      <div className="mb-3 flex flex-wrap gap-4 text-[var(--text-dense-meta)] text-[var(--muted-foreground)]">
        <span>
          Devices{' '}
          <strong className="font-mono-tabular text-[var(--foreground)]">
            {sla?.devices_up ?? '—'}/{sla?.devices_total ?? '—'}
          </strong>
        </span>
        <span>
          Up fraction{' '}
          <strong className="font-mono-tabular text-[var(--foreground)]">
            {sla?.devices_up_fraction != null ? `${Math.round(sla.devices_up_fraction * 100)}%` : '—'}
          </strong>
        </span>
        <span>
          Probe streak{' '}
          <strong className="font-mono-tabular text-[var(--foreground)]">{sla?.probe_fail_streak ?? 0}</strong>
        </span>
      </div>

      {alerts.length > 0 && (
        <div className="mb-3 rounded-md border border-[var(--warning)]/40 bg-[var(--secondary)] px-3 py-2">
          <p className="m-0 mb-1 text-[var(--text-dense-label)] font-medium">Anomaly alerts</p>
          <ul className="m-0 list-none space-y-1 p-0">
            {alerts.map((a, i) => (
              <li key={`${a.rule ?? 'r'}-${i}`} className="flex flex-wrap items-center gap-2 text-[var(--text-dense-meta)]">
                <DenseTag variant={a.severity === 'warning' ? 'warning' : a.severity === 'danger' ? 'danger' : 'info'}>
                  {a.rule ?? 'rule'}
                </DenseTag>
                <span>{a.message}</span>
              </li>
            ))}
          </ul>
        </div>
      )}

      {tips.length > 0 && (
        <div className="rounded-md border border-[var(--border)] bg-[var(--background)] px-3 py-2">
          <p className="m-0 mb-1 text-[var(--text-dense-label)] font-medium">Predictive-lite</p>
          <ul className="m-0 list-disc space-y-1 pl-4 text-[var(--text-dense-meta)] text-[var(--muted-foreground)]">
            {tips.map((tip, i) => (
              <li key={`${tip}-${i}`}>{tip}</li>
            ))}
          </ul>
        </div>
      )}

      {!isLoading && alerts.length === 0 && tips.length === 0 && (
        <p className="m-0 text-[var(--text-dense-meta)] text-[var(--muted-foreground)]">No anomaly tips.</p>
      )}
    </OpsSection>
  )
}
