import { LedgerGrid } from '@bifrost/ui/city/ledger'
import type { NetworkClientsResponse } from '@/api/networkTypes'
import { OpsSection } from '@/components/layout/OpsSection'
import { networkClientsTable } from './networkClientsTable'

interface NetworkClientsPanelProps {
  data: NetworkClientsResponse | undefined
  isLoading: boolean
}

/**
 * LAN clients in the shared Ledger (Ledger line, 2026-11-24: PROD returned 99 clients and the table drew the first 50,
 * so 51–99 could not be seen; now every client, filtered, sorted, 50 a page). No row opens anything.
 */
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
      <div className="min-w-0 px-3 py-2">
        {isLoading ? (
          <p className="m-0 text-[var(--muted-foreground)]">…</p>
        ) : (
          <LedgerGrid table={networkClientsTable(clients)} selected="" onSelect={() => {}}
            scope={`LAN clients the network controller returns: ${clients.length}${data?.count != null && data.count !== clients.length ? ` (it counts ${data.count})` : ''}`} />
        )}
      </div>
    </OpsSection>
  )
}
