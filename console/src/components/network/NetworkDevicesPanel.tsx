import {
  DenseDataTable,
  DenseTableBody,
  DenseTableCell,
  DenseTableHead,
  DenseTableHeadRow,
  DenseTableHeader,
  DenseTableRow,
} from '@bifrost/ui'
import type { NetworkDevicesResponse } from '@/api/types'
import { OpsSection } from '@/components/layout/OpsSection'

interface NetworkDevicesPanelProps {
  data: NetworkDevicesResponse | undefined
  isLoading: boolean
}

export function NetworkDevicesPanel({ data, isLoading }: NetworkDevicesPanelProps) {
  const devices = data?.devices ?? []

  return (
    <OpsSection title="UniFi devices" description="APs, switches, and gateways from GET /api/v1/network/devices." bodyPadding="none" overflow="hidden">
      {data?.error != null && (
        <p className="m-0 px-3 py-2 text-[var(--text-dense-meta)] lamp-warn">{data.error}</p>
      )}
      {data?.hint != null && data.error == null && devices.length === 0 && (
        <p className="m-0 px-3 py-2 text-[var(--text-dense-meta)] text-[var(--muted-foreground)]">{data.hint}</p>
      )}
      <DenseDataTable>
        <DenseTableHeader>
          <DenseTableHeadRow>
            <DenseTableHead>Name</DenseTableHead>
            <DenseTableHead>Model</DenseTableHead>
            <DenseTableHead>Type</DenseTableHead>
            <DenseTableHead>IP</DenseTableHead>
            <DenseTableHead>Version</DenseTableHead>
          </DenseTableHeadRow>
        </DenseTableHeader>
        <DenseTableBody>
          {isLoading ? (
            <DenseTableRow>
              <DenseTableCell colSpan={5} className="text-[var(--muted-foreground)]">
                Loading…
              </DenseTableCell>
            </DenseTableRow>
          ) : devices.length === 0 ? (
            <DenseTableRow>
              <DenseTableCell colSpan={5} className="text-[var(--muted-foreground)]">
                No devices returned
              </DenseTableCell>
            </DenseTableRow>
          ) : (
            devices.map((device, index) => (
              <DenseTableRow key={`${device.mac ?? device.name ?? index}`}>
                <DenseTableCell className="font-medium">{device.name ?? '—'}</DenseTableCell>
                <DenseTableCell>{device.model ?? '—'}</DenseTableCell>
                <DenseTableCell className="font-mono-tabular">{device.type ?? '—'}</DenseTableCell>
                <DenseTableCell className="font-mono-tabular">{device.ip ?? '—'}</DenseTableCell>
                <DenseTableCell className="font-mono-tabular">{device.version ?? '—'}</DenseTableCell>
              </DenseTableRow>
            ))
          )}
        </DenseTableBody>
      </DenseDataTable>
    </OpsSection>
  )
}
