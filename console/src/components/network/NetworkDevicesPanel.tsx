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
import type { NetworkDevicesResponse } from '@/api/networkTypes'
import { OpsSection } from '@/components/layout/OpsSection'

interface NetworkDevicesPanelProps {
  data: NetworkDevicesResponse | undefined
  isLoading: boolean
}

function formatUptime(seconds: number | undefined): string {
  if (seconds == null || seconds <= 0) return '—'
  const d = Math.floor(seconds / 86400)
  const h = Math.floor((seconds % 86400) / 3600)
  if (d > 0) return `${d}d ${h}h`
  const m = Math.floor((seconds % 3600) / 60)
  return h > 0 ? `${h}h ${m}m` : `${m}m`
}

function formatRate(rate: number | undefined): string {
  if (rate == null || Number.isNaN(rate)) return '—'
  if (rate >= 1_000_000) return `${(rate / 1_000_000).toFixed(1)} MB/s`
  if (rate >= 1_000) return `${(rate / 1_000).toFixed(1)} KB/s`
  return `${rate.toFixed(0)} B/s`
}

function formatBytes(bytes: number | undefined): string {
  if (bytes == null || Number.isNaN(bytes) || bytes < 0) return '—'
  if (bytes >= 1_000_000_000_000) return `${(bytes / 1_000_000_000_000).toFixed(1)} TB`
  if (bytes >= 1_000_000_000) return `${(bytes / 1_000_000_000).toFixed(1)} GB`
  if (bytes >= 1_000_000) return `${(bytes / 1_000_000).toFixed(1)} MB`
  if (bytes >= 1_000) return `${(bytes / 1_000).toFixed(1)} KB`
  return `${bytes.toFixed(0)} B`
}

/** Prefer instantaneous rate; fall back to cumulative bytes (UniFi devices often omit rates). */
function TrafficCell({ rate, bytes }: { rate: number | undefined; bytes: number | undefined }) {
  if (rate != null && rate > 0) {
    const pct = Math.min(100, Math.max(4, Math.log10(rate + 1) * 18))
    return (
      <div className="flex min-w-[4.5rem] items-center gap-1.5">
        <div className="h-1.5 flex-1 overflow-hidden rounded-sm bg-[var(--background)]">
          <div className="h-full bg-[var(--color-info,theme(colors.sky.500))]" style={{ width: `${pct}%` }} />
        </div>
        <span className="font-mono-tabular text-[var(--text-dense-caption)]">{formatRate(rate)}</span>
      </div>
    )
  }
  if (bytes != null && bytes > 0) {
    return <span className="font-mono-tabular text-[var(--text-dense-caption)]">{formatBytes(bytes)}</span>
  }
  return <span className="text-[var(--muted-foreground)]">—</span>
}

function deviceReach(device: { state_label?: string; state?: number }): 'ok' | 'fail' | 'unknown' {
  if (device.state_label === 'online' || device.state === 1) return 'ok'
  if (device.state_label === 'offline' || device.state === 0) return 'fail'
  return 'unknown'
}

export function NetworkDevicesPanel({ data, isLoading }: NetworkDevicesPanelProps) {
  const devices = data?.devices ?? []

  return (
    <OpsSection
      title="UniFi devices"
      description="APs, switches, and gateways from GET /api/v1/network/devices — health + traffic (rate or cumulative bytes)."
      bodyPadding="none"
      overflow="hidden"
    >
      {data?.error != null && (
        <p className="m-0 px-3 py-2 text-[var(--text-dense-meta)] lamp-warn">{data.error}</p>
      )}
      {data?.hint != null && data.error == null && devices.length === 0 && (
        <p className="m-0 px-3 py-2 text-[var(--text-dense-meta)] text-[var(--muted-foreground)]">{data.hint}</p>
      )}
      {data?.devices_up != null && data?.devices_total != null && (
        <p className="m-0 px-3 py-2 text-[var(--text-dense-meta)] text-[var(--muted-foreground)]">
          {data.devices_up}/{data.devices_total} online
        </p>
      )}
      <DenseDataTable>
        <DenseTableHeader>
          <DenseTableHeadRow>
            <DenseTableHead>Status</DenseTableHead>
            <DenseTableHead>Name</DenseTableHead>
            <DenseTableHead>Model</DenseTableHead>
            <DenseTableHead>IP</DenseTableHead>
            <DenseTableHead>Uptime</DenseTableHead>
            <DenseTableHead>Rx</DenseTableHead>
            <DenseTableHead>Tx</DenseTableHead>
            <DenseTableHead>Version</DenseTableHead>
          </DenseTableHeadRow>
        </DenseTableHeader>
        <DenseTableBody>
          {isLoading ? (
            <DenseTableRow>
              <DenseTableCell colSpan={8} className="text-[var(--muted-foreground)]">
                Loading…
              </DenseTableCell>
            </DenseTableRow>
          ) : devices.length === 0 ? (
            <DenseTableRow>
              <DenseTableCell colSpan={8} className="text-[var(--muted-foreground)]">
                No devices returned
              </DenseTableCell>
            </DenseTableRow>
          ) : (
            devices.map((device, index) => (
              <DenseTableRow key={`${device.mac ?? device.name ?? index}`}>
                <DenseTableCell>
                  <StatusLamp value={deviceReach(device)} kind="reach" />
                  <DenseTag
                    variant={deviceReach(device) === 'ok' ? 'success' : deviceReach(device) === 'fail' ? 'danger' : 'neutral'}
                    className="ml-2"
                  >
                    {device.state_label ?? 'unknown'}
                  </DenseTag>
                  {device.adopted === false && (
                    <DenseTag variant="warning" className="ml-1">
                      not adopted
                    </DenseTag>
                  )}
                </DenseTableCell>
                <DenseTableCell className="font-medium">{device.name ?? '—'}</DenseTableCell>
                <DenseTableCell>{device.model ?? '—'}</DenseTableCell>
                <DenseTableCell className="font-mono-tabular">{device.ip ?? '—'}</DenseTableCell>
                <DenseTableCell className="font-mono-tabular">{formatUptime(device.uptime)}</DenseTableCell>
                <DenseTableCell>
                  <TrafficCell rate={device.rx_rate} bytes={device.rx_bytes} />
                </DenseTableCell>
                <DenseTableCell>
                  <TrafficCell rate={device.tx_rate} bytes={device.tx_bytes} />
                </DenseTableCell>
                <DenseTableCell className="font-mono-tabular">{device.version ?? '—'}</DenseTableCell>
              </DenseTableRow>
            ))
          )}
        </DenseTableBody>
      </DenseDataTable>
    </OpsSection>
  )
}
