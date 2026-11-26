import '@testing-library/jest-dom/vitest'
import { cleanup, render, screen, within } from '@testing-library/react'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { afterEach, expect, it, vi } from 'vitest'
import { fetchApprovalReadings } from '@/api/readings'
import { approvalHref } from '@/lib/shell/consoleRoutes'
import { ApprovalReadingsBlock } from '../ApprovalReadingsBlock'
import { contract } from './contract'

/*
 * Origin of every case in this file — 2026-11-14 (ISSUE-79): the Ops page is a window on the system and shows what the
 * system measured, with no drift from a ledger kept by hand; that day how many cards went to the Owner and how many
 * bought nothing was told only by word, and the page showed none of it.
 */

vi.mock('@/api/readings', () => ({ fetchApprovalReadings: vi.fn() }))
afterEach(() => { cleanup(); vi.resetAllMocks() })

function setup(how: 'rows' | 'none' | 'fail' = 'rows') {
  if (how === 'fail') vi.mocked(fetchApprovalReadings).mockRejectedValue(new Error('Approval readings: HTTP 502'))
  else vi.mocked(fetchApprovalReadings).mockResolvedValue(how === 'none' ? contract.approvalsNone() : contract.approvals())
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  render(<QueryClientProvider client={client}><ApprovalReadingsBlock /></QueryClientProvider>)
}
const cellOf = (row: HTMLElement, column: string) => {
  const cell = row.querySelector<HTMLElement>(`[data-column="${column}"]`)
  if (!cell) throw new Error(`the row has no ${column} cell`)
  return cell
}

// Origin: as above (2026-11-14, ISSUE-79) — each count on the page leads to the requests behind it.
it('draws a count with a link to each request behind it', async () => {
  setup()
  const row = await screen.findByTestId('approval-readings-2026-11-14/line-a')
  const bought = cellOf(row, 'bought-nothing')
  expect(bought).toHaveTextContent('2 · #2 #3')
  expect(within(bought).getAllByRole('link').map(a => a.getAttribute('href'))).toEqual([approvalHref('2'), approvalHref('3')])
  expect(cellOf(row, 'requests')).toHaveTextContent('7 · #1 #2 #3 #4 #5 #6 #7')
  expect(cellOf(row, 'time-to-approve')).toHaveTextContent('40s')
  expect(within(row).getByText('line a')).toHaveAttribute('title', 'line-a')
  expect(row).toHaveTextContent('LANE-a, TD-9')
  const second = screen.getByTestId('approval-readings-2026-11-14/line-b')
  expect(cellOf(second, 'not-approved')).toHaveTextContent(/^0$/)
  expect(within(cellOf(second, 'not-approved')).queryByRole('link')).toBeNull()
  expect(cellOf(second, 'bought-nothing')).toHaveTextContent('1 · #9')
  expect(screen.getAllByText('2026-11-14')).toHaveLength(1)
  expect(screen.queryByRole('button')).toBeNull()
})

// Origin: as above — no drift: the page says how far back the records reach; earlier days are not kept, not empty.
it('says how many records are kept and from when', async () => {
  setup()
  expect(await screen.findByText(/11 records are kept, the oldest from 2026-11-13 23:00Z: earlier days are not kept, not empty\./)).toBeInTheDocument()
  expect(screen.getByText(/at 2026-11-15 05:00Z; days are in America\/Chicago;/)).toBeInTheDocument()
})

// Origin: as above — not read is not none: a failed read is an alert, never an empty table or "no record".
it('a failed read is an alert, not a table', async () => {
  setup('fail')
  const alert = await screen.findByRole('alert')
  expect(alert).toHaveTextContent('The approval records could not be read: Approval readings: HTTP 502.')
  expect(alert).toHaveTextContent('is not known')
  expect(screen.queryByRole('table')).toBeNull()
  expect(screen.queryByText('No approval record is kept.')).toBeNull()
})

// Origin: as above — no record kept is said as that, with how it was computed still shown.
it('no retained record says so, with the description and no table', async () => {
  setup('none')
  expect(await screen.findByText('No approval record is kept.')).toBeInTheDocument()
  expect(screen.getByText(/0 records are kept\./)).toBeInTheDocument()
  expect(screen.queryByRole('table')).toBeNull()
})
