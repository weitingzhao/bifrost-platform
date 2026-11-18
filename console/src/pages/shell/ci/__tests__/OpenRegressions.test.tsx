import '@testing-library/jest-dom/vitest'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { cleanup, render, screen } from '@testing-library/react'
import { afterEach, expect, it, vi } from 'vitest'
import { fetchOpenRegressions } from '@/api/regressions'
import { OpenRegressions } from '../OpenRegressions'

vi.mock('@/api/regressions', () => ({ fetchOpenRegressions: vi.fn() }))
afterEach(() => { cleanup(); vi.resetAllMocks() })

const show = () => render(
  <QueryClientProvider client={new QueryClient({ defaultOptions: { queries: { retry: false } } })}><OpenRegressions repo="r" /></QueryClientProvider>,
)
const commit = 'a25c27aa'.padEnd(40, '0')

// Origin: 2026-11-18, a25c27aa left main's unit declaration unreadable for 34 commits with CI green and nothing on Ops said so.
it('names each open regression by its commit and what it broke', async () => {
  vi.mocked(fetchOpenRegressions).mockResolvedValue([{ repo: 'r', introduced: commit, impact: 'bifrost.units.yaml does not read: x', by: 'controller', known_at: '2026-11-18T07:25:00Z' }])
  show()
  expect(await screen.findByText('Known regressions on main: 1')).toBeInTheDocument()
  expect(screen.getByTestId('ci-regression-a25c27aa')).toHaveTextContent('bifrost.units.yaml does not read: x')
  expect(fetchOpenRegressions).toHaveBeenCalledWith('r', expect.anything())
})

// Origin: 2026-11-18, the same: this page's rule, a read that failed is said and never drawn as none.
it('says when the read failed instead of showing none', async () => {
  vi.mocked(fetchOpenRegressions).mockRejectedValue(new Error('down'))
  show()
  expect(await screen.findByRole('alert')).toHaveTextContent('could not be read')
  expect(screen.queryByTestId('ci-regressions-none')).toBeNull()
})

// Origin: 2026-11-18, the same: the control of the two above, an empty answer is shown as none.
it('says none when nothing is open', async () => {
  vi.mocked(fetchOpenRegressions).mockResolvedValue([])
  show()
  expect(await screen.findByTestId('ci-regressions-none')).toBeInTheDocument()
})
