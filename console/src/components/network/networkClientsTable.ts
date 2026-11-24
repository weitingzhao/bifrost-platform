import type { Block } from '@bifrost/ui/city/cards'
import type { NetworkClientView } from '@/api/networkTypes'

/** The LAN clients as one Ledger table: the dense table's columns; a wireless link is info, a wired one plain. */
export function networkClientsTable(clients: NetworkClientView[]): Extract<Block, { kind: 'table' }> {
  return {
    kind: 'table',
    head: ['Host', 'IP', 'MAC', 'Network', 'Link'].map(label => ({ label })),
    rows: clients.map((client, index) => ({
      key: client.mac || client.ip || String(index),
      cells: [
        // An empty string is no value either (the controller sends "" for an unnamed or unaddressed client).
        client.hostname || client.name || '—',
        client.ip || '—',
        client.mac || '—',
        client.network || '—',
        client.is_wired === true ? 'Wired' : { text: 'Wireless', tone: 'info' as const },
      ],
    })),
    empty: 'No clients returned',
  }
}
