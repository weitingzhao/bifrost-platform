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
import type { NetworkClientsResponse } from '@/api/types'
import { OpsSection } from '@/components/layout/OpsSection'

interface NetworkClientsPanelProps {
  data: NetworkClientsResponse | undefined
  isLoading: boolean
}

export function NetworkClientsPanel({ data, isLoading }: NetworkClientsPanelProps) {
  const clients = data?.clients ?? []

  return (
    <OpsSection title="LAN clients" description="Active clients from GET /api/v1/network/clients." bodyPadding="none" overflow="hidden">
      {data?.error != null && (
        <p className="m-0 px-3 py-2 text-[var(--text-dense-meta)] lamp-warn">{data.error}</p>
      )}
      {data?.hint != null && data.error == null && clients.length === 0 && (
        <p className="m-0 px-3 py-2 text-[var(--text-dense-meta)] text-[var(--muted-foreground)]">{data.hint}</p>
      )}
      <DenseDataTable>
        <DenseTableHeader>
          <DenseTableHeadRow>
            <DenseTableHead>Host</DenseTableHead>
            <DenseTableHead>IP</DenseTableHead>
            <DenseTableHead>MAC</DenseTableHead>
            <DenseTableHead>Network</DenseTableHead>
            <DenseTableHead>Link</DenseTableHead>
          </DenseTableHeadRow>
        </DenseTableHeader>
        <DenseTableBody>
          {isLoading ? (
            <DenseTableRow>
              <DenseTableCell colSpan={5} className="text-[var(--muted-foreground)]">
                Loading…
              </DenseTableCell>
            </DenseTableRow>
          ) : clients.length === 0 ? (
            <DenseTableRow>
              <DenseTableCell colSpan={5} className="text-[var(--muted-foreground)]">
                No clients returned
              </DenseTableCell>
            </DenseTableRow>
          ) : (
            clients.slice(0, 50).map((client, index) => (
              <DenseTableRow key={`${client.mac ?? client.ip ?? index}`}>
                <DenseTableCell className="font-medium">{client.hostname ?? client.name ?? '—'}</DenseTableCell>
                <DenseTableCell className="font-mono-tabular">{client.ip ?? '—'}</DenseTableCell>
                <DenseTableCell className="font-mono-tabular text-[var(--text-dense-meta)]">
                  {client.mac ?? '—'}
                </DenseTableCell>
                <DenseTableCell>{client.network ?? '—'}</DenseTableCell>
                <DenseTableCell>
                  <DenseTag variant={client.is_wired === true ? 'neutral' : 'info'}>
                    {client.is_wired === true ? 'Wired' : 'Wireless'}
                  </DenseTag>
                </DenseTableCell>
              </DenseTableRow>
            ))
          )}
        </DenseTableBody>
      </DenseDataTable>
      {clients.length > 50 && (
        <p className="m-0 px-3 py-2 text-[var(--text-dense-caption)] text-[var(--muted-foreground)]">
          Showing first 50 of {clients.length} clients.
        </p>
      )}
    </OpsSection>
  )
}
