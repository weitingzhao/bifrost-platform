import '@testing-library/jest-dom/vitest'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { cleanup, render, screen } from '@testing-library/react'
import { afterEach, expect, it, vi } from 'vitest'
import { fetchProdJudgements } from '@/api/prodJudgements'
import { ProdJudgement } from '../ProdJudgement'

vi.mock('@/api/prodJudgements', () => ({ fetchProdJudgements: vi.fn() }))
afterEach(() => { cleanup(); vi.resetAllMocks() })

const show = () => render(<QueryClientProvider client={new QueryClient({ defaultOptions: { queries: { retry: false } } })}><ProdJudgement /></QueryClientProvider>)

// Origin: 2026-11-22, law 93: the PROD loop held releases for hours and its reasons were only in a pod's log;
// the page shows the newest judgement, each failing condition with its reason.
it('shows why PROD is held, condition by condition', async () => {
  vi.mocked(fetchProdJudgements).mockResolvedValue({ mode: 'on', judgements: [{ at: '2026-11-22T05:03:34Z', result: 'wait',
    record: { verdict: 'wait: spaced, in_hours', from: 'abe26b2b'.padEnd(40, '0'), commits: 15, failing: ['spaced', 'in_hours'],
      reasons: { spaced: 'the last prod delivery finished 910s ago, of 1200s' } } }] })
  show()
  expect(await screen.findByText('PROD wait: spaced, in_hours')).toBeInTheDocument()
  expect(screen.getByTestId('ci-prod-failing-spaced')).toHaveTextContent('910s ago, of 1200s')
  expect(screen.getByTestId('ci-prod-failing-in_hours')).toBeInTheDocument()
})

// Origin: the same: this page's rule, a read that failed is said and never drawn as none.
it('says when the read failed', async () => {
  vi.mocked(fetchProdJudgements).mockRejectedValue(new Error('down'))
  show()
  expect(await screen.findByRole('alert')).toHaveTextContent('could not be read')
  expect(screen.queryByTestId('ci-prod-judgement-none')).toBeNull()
})

// Origin: the same: the control of the two above, no judgement is shown as none.
it('says none when there is no judgement', async () => {
  vi.mocked(fetchProdJudgements).mockResolvedValue({ mode: 'on', judgements: [] })
  show()
  expect(await screen.findByTestId('ci-prod-judgement-none')).toBeInTheDocument()
})
