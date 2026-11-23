import { cleanup, fireEvent, render, screen } from '@testing-library/react'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { afterEach, describe, expect, it } from 'vitest'
import type { AuditRecord } from '@/api/auditTypes'
import { AuditRecordsPanel } from '../AuditRecordsPanel'
import { auditLedgerTable, auditStatusTone } from '../auditLedger'

afterEach(cleanup)

const record = (i: number, over: Partial<AuditRecord> = {}): AuditRecord => ({
  id: `a${i}`, at: new Date(Date.UTC(2026, 10, 23, 0, 0, i)).toISOString(), actor: 'ops', role: 'operator',
  action: 'cluster.delete_pod', target: `pod-${i}`, status: 'ok', detail: `detail ${i}`, ...over,
})

describe('Records → Audit in the Ledger', () => {
  // 2026-11-23, Ledger line PROD reading: the platform keeps 500 audit records and Records → Audit drew the
  // first 100 (limit=100), so records 101–500 could not be seen on the page at all.
  it('keeps every record reachable instead of cutting at the old limit', () => {
    const records = Array.from({ length: 500 }, (_, i) => record(i))
    render(<QueryClientProvider client={new QueryClient()}><AuditRecordsPanel records={records} isLoading={false} ledger /></QueryClientProvider>)
    expect(screen.getByText(/Supplied rows: 500/)).toBeTruthy()
    fireEvent.change(screen.getByLabelText('Filter supplied rows'), { target: { value: 'pod-480' } })
    expect(screen.getByText('pod-480')).toBeTruthy()
  })
  // 2026-11-23, Ledger line local check: rows without `open` could not be picked, so the detail the dense
  // table's expand row showed was gone. Picking a row shows its whole detail under the table.
  it('shows the picked record whole under the table', () => {
    const records = [record(1, { detail: 'x'.repeat(120) + ' tail-of-detail' })]
    render(<QueryClientProvider client={new QueryClient()}><AuditRecordsPanel records={records} isLoading={false} ledger /></QueryClientProvider>)
    fireEvent.click(screen.getByRole('button', { name: '2026-11-23 00:00:01Z' }))
    expect(screen.getByLabelText('Selected audit record').textContent).toContain('tail-of-detail')
  })
  // Positive control for the test above: without `ledger` the other pages keep their short dense table.
  it('leaves the dense table and its limit on other pages', () => {
    const records = Array.from({ length: 30 }, (_, i) => record(i))
    render(<QueryClientProvider client={new QueryClient()}><AuditRecordsPanel records={records} isLoading={false} limit={20} /></QueryClientProvider>)
    expect(screen.queryByText(/Supplied rows/)).toBeNull()
    expect(screen.queryByText('pod-25')).toBeNull()
  })
  // 2026-11-23, Console line review: error and failed are both fail, and a status nobody mapped is unknown,
  // never an unverified ok.
  it('maps status to the Ops words and leaves the unknown unknown', () => {
    expect(auditStatusTone('error')).toBe('fail')
    expect(auditStatusTone('FAILED')).toBe('fail')
    expect(auditStatusTone('success')).toBe('ok')
    expect(auditStatusTone('waiting')).toBe('pending')
    expect(auditStatusTone('refused')).toBe('boundary')
    expect(auditStatusTone('pending-approval')).toBe('unknown')
    expect(auditLedgerTable([record(1, { status: 'weird' })]).rows[0].cells[6]).toMatchObject({ tone: 'unknown' })
  })
})
