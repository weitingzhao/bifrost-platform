import { cleanup, fireEvent, render, screen } from '@testing-library/react'
import { afterEach, expect, it } from 'vitest'
import type { NetworkClientView } from '@/api/networkTypes'
import { NetworkClientsPanel } from '../NetworkClientsPanel'
import { networkClientsTable } from '../networkClientsTable'

afterEach(cleanup)
const client = (i: number): NetworkClientView => ({ hostname: `host-${i}`, ip: `192.168.10.${i}`, mac: `aa:bb:cc:dd:ee:${String(i).padStart(2, '0')}`, network: 'LAN', is_wired: i % 2 === 0 })

// 2026-11-24, Ledger line PROD reading: /api/v1/network/clients returned 99 clients and the panel drew the first 50
// ("Showing first 50 of 99"), so clients 51–99 could not be seen. Every client reaches the Ledger.
it('keeps every client reachable instead of the first 50', () => {
  const clients = Array.from({ length: 99 }, (_, i) => client(i + 1))
  render(<NetworkClientsPanel data={{ count: 99, clients }} isLoading={false} />)
  expect(screen.getByText(/Supplied rows: 99/)).toBeTruthy()
  fireEvent.change(screen.getByLabelText('Filter supplied rows'), { target: { value: 'host-97' } })
  expect(screen.getByText('host-97')).toBeTruthy()
})

// Same change: the dense tag was info for a wireless link and plain for a wired one; the cell keeps that.
it('keeps a wireless link as info and a wired one plain', () => {
  const rows = networkClientsTable([client(1), client(2), {}]).rows
  expect(rows[0].cells[4]).toEqual({ text: 'Wireless', tone: 'info' })
  expect(rows[1].cells[4]).toBe('Wired')
  expect(rows[2].cells).toEqual(['—', '—', '—', '—', { text: 'Wireless', tone: 'info' }])
  expect(rows[2].key).toBe('2')
  // PROD 2026-11-24: some clients come with hostname "" and showed an empty cell; empty is no value, so —.
  expect(networkClientsTable([{ hostname: '', name: '', ip: '192.168.50.134' }]).rows[0].cells[0]).toBe('—')
})
