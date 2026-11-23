import { cleanup, fireEvent, render, screen } from '@testing-library/react'
import { afterEach, expect, it } from 'vitest'
import type { ReleaseRecord } from '@/api/releases'
import { ReleaseHistorySection } from '../ReleaseHistorySection'
import { releaseLedgerTable } from '../releaseView'

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
  render(<ReleaseHistorySection records={records} recordsLoading={false} recordsError={null} superseded={[]} />)
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
