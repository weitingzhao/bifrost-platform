import '@testing-library/jest-dom/vitest'
import { cleanup, render, screen, within } from '@testing-library/react'
import { afterEach, describe, expect, it, vi } from 'vitest'
import { serverFetch } from '@/api/serverContact'
import type { CheckValueResponse, LeakValueResponse } from '@/api/checkValue'
import { goalHref } from '@/lib/shell/consoleRoutes'
import { CheckValuePage } from '../CheckValueTable'

vi.mock('@/api/serverContact', () => ({ serverFetch: vi.fn() }))
afterEach(() => { cleanup(); vi.resetAllMocks() })
const weeks = ['2026-10-26', '2026-11-02']
const cell = { week: weeks[1], ran: 81, caught: 12, unique: 7, noise: 5, minutes: 32, owner_minutes: null, unknown: { owner_minutes: 'No owner time recorded' } }
const checks: CheckValueResponse = {
  as_of: '2026-11-05', weeks,
  rows: [
    { key: 'retry', kind: 'retry', label: 'Retry build', order: 0, cells: [cell], mark: null },
    { key: 'build', kind: 'check', label: 'Build', order: 2, cells: [cell], mark: { kind: 'delete', since: '2026-11-01', mission: 'M-123' } },
    { key: 'lint', kind: 'check', label: 'Lint', order: 1, cells: [cell], mark: null },
  ],
}
const leaks: LeakValueResponse = {
  as_of: '2026-11-05', weeks,
  rows: [{ place: 'deploy', label: 'Deploy', cells: [{ week: weeks[1], stg: 3, prod: null, unknown: { prod: 'PROD not measured' } }], mark: { kind: 'add_back', since: '2026-11-01', mission: 'M-124' } }],
}

describe('Check value', () => {
  it('reads once and draws weekly counts, costs, unknown reasons and proposed goals', async () => {
    vi.mocked(serverFetch).mockImplementation(async url => new Response(JSON.stringify(String(url).endsWith('/check-value') ? checks : leaks)))
    const { rerender } = render(<CheckValuePage />)
    await screen.findByText('Build')
    const checksSection = screen.getByRole('region', { name: 'Weekly checks' })
    expect(within(checksSection).getAllByText('81')).toHaveLength(3)
    for (const value of ['12', '7', '5', '32']) expect(within(checksSection).getAllByText(value)).toHaveLength(3)
    expect(within(checksSection).getAllByText(/min · Owner/)).toHaveLength(6)
    expect(within(checksSection).getAllByTitle('No owner time recorded')[0]).toHaveTextContent('?')
    expect(screen.getByTitle('PROD not measured')).toHaveTextContent('?')
    expect(screen.getByText('3', { selector: 'span' })).toBeInTheDocument()
    expect(screen.getByRole('link', { name: 'delete?' })).toHaveAttribute('href', goalHref('M-123'))
    expect(screen.getByRole('link', { name: 'add a check back' })).toHaveAttribute('href', goalHref('M-124'))
    const rows = within(checksSection).getAllByRole('row')
    expect(rows.slice(2).map(row => within(row).getByRole('rowheader').textContent)).toEqual(['Lint check ', 'Build check delete?', 'Retry build retry '])
    expect(within(rows[0]).getAllByRole('columnheader').map(h => h.textContent)).toEqual(['Check / retry', weeks[1], weeks[0]])
    rerender(<CheckValuePage />)
    expect(serverFetch).toHaveBeenCalledTimes(2)
  })
  it('explains routes that are not deployed yet', async () => {
    vi.mocked(serverFetch).mockResolvedValue(new Response('', { status: 404 }))
    render(<CheckValuePage />)
    expect(await screen.findByText('check-value route is not deployed yet.')).toBeInTheDocument()
    expect(await screen.findByText('leak-value route is not deployed yet.')).toBeInTheDocument()
  })
})
