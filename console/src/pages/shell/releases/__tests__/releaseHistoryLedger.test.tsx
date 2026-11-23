import { cleanup, fireEvent, render, screen } from '@testing-library/react'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { afterEach, expect, it } from 'vitest'
import type { ReleaseRecord } from '@/api/releases'
import { ReleaseHistorySection } from '../ReleaseHistorySection'
import { historyScope, HISTORY_LIMIT, releaseLedgerTable } from '../releaseView'

afterEach(cleanup)

const record = (i: number, deploys: boolean): ReleaseRecord => ({
  run: `deliver-${i}`, pipeline: deploys ? 'bifrost-deliver-stg' : 'build-market-data', lane: deploys ? 'trade' : 'market-data',
  env: 'stg', deploys, started_at: '2026-11-23T10:00:00Z', completed_at: `2026-11-23T10:${String(i % 60).padStart(2, '0')}:00Z`,
  repos: {}, recorded_at: '2026-11-23T10:00:00Z',
})

// 2026-11-23, Ledger line: History drew every record it read (the page reads the newest 40; PROD keeps 500 and more)
// with no way to find one lane's or one environment's releases. They go to the Ledger: filtered, sorted, 50 a page.
it('puts every release record in the Ledger and finds one lane by filter', () => {
  const records = Array.from({ length: 100 }, (_, i) => record(i, i % 2 === 0))
  render(<QueryClientProvider client={new QueryClient()}><ReleaseHistorySection records={records} recordsLoading={false} recordsError={null} superseded={[]} /></QueryClientProvider>)
  expect(screen.getByText(/Supplied rows: 100/)).toBeTruthy()
  fireEvent.change(screen.getByLabelText('Filter supplied rows'), { target: { value: 'market-data' } })
  expect(screen.getByText(/Showing 1–50 of 50 matching rows/)).toBeTruthy()
})

// Same change: the dense table's kind tag was info for a deploying run and plain for a build; the cell keeps that.
it('keeps a deploying run as info and a build plain', () => {
  const rows = releaseLedgerTable([record(1, true), record(2, false)]).rows
  expect(rows[0].cells[2]).toEqual({ text: 'deploy', tone: 'info' })
  expect(rows[0].cells[1]).toBe('STG')
  expect(rows[1].cells[2]).toBe('build')
})

// 2026-11-23 (箭10, after Ledger's change): the page reads the newest 40 for its lanes while PROD keeps 500+, and the
// table must not pass that cut off as the whole. History reads every record when opened and says what it holds.
it('says what the History table holds: all, a filled read, or the newest 40 and why', () => {
  expect(historyScope({ shown: 523, full: true, reading: false, error: null })).toBe('All 523 release records the platform keeps')
  expect(historyScope({ shown: HISTORY_LIMIT, full: true, reading: false, error: null })).toContain('may keep more')
  expect(historyScope({ shown: 40, full: false, reading: true, error: null })).toBe('Showing the newest 40 · reading the full history…')
  expect(historyScope({ shown: 40, full: false, reading: false, error: 'HTTP 502' })).toContain('could not be read (HTTP 502)')
})
