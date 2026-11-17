import '@testing-library/jest-dom/vitest'
import { cleanup, render, screen, within } from '@testing-library/react'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { afterEach, expect, it, vi } from 'vitest'
import { fetchCiScorecard, type CiScorecard } from '@/api/ciScorecard'
import { CiScorecardBlock } from '../CiScorecardBlock'

/*
 * Origin of every case in this file — Owner 2026-11-16: CI scores every repository that uses it, and its users score
 * CI; only what is measured is shown, each item with its verdict and raw number, and the pass lines are on the page.
 */

vi.mock('@/api/ciScorecard', () => ({ fetchCiScorecard: vi.fn() }))
afterEach(() => { cleanup(); vi.resetAllMocks() })

/** The shape GET /api/v1/ci/scorecard answers (api/internal/ciscore.Scorecard), with invented numbers. */
const scorecard = (): CiScorecard => ({
  generated_at: '2026-11-16T12:00:00Z',
  window_days: 3,
  standards: [
    { id: 'landing_time', label: 'Landing time', pass_when: 'median of the runs that tested a ready branch themselves is at most 120 s (at least 5 runs)' },
    { id: 'red_ratio', label: 'Red runs', pass_when: 'at most 10% of completed runs failed' },
    { id: 'cleanup_keeps_up', label: 'Cleanup keeps up', pass_when: 'the job that deletes old runs last succeeded at most 2 h ago' },
  ],
  repos: [
    { repo: 'bifrost-platform', pipeline: 'bifrost-ci-platform', tier: 'selective', overall: 'fail', items: [
      { id: 'landing_time', label: 'Landing time', verdict: 'fail', value: 'median 183 s', samples: 247 },
      { id: 'red_ratio', label: 'Red runs', verdict: 'pass', value: '14 failed of 430', samples: 430 },
    ] },
    { repo: 'bifrost-trade-infra', pipeline: 'bifrost-ci-infra', tier: 'full', overall: 'unmeasured', items: [
      { id: 'follows_change', label: 'Time follows the change', verdict: 'unmeasured', value: 'the pipeline reports no api-scope', samples: 0 },
      { id: 'red_ratio', label: 'Red runs', verdict: 'pass', value: '8 failed of 143', samples: 143 },
    ] },
  ],
  provider: { overall: 'pass', items: [{ id: 'cleanup_keeps_up', label: 'Cleanup keeps up', verdict: 'pass', value: '0.6 h since it last succeeded', samples: 1 }] },
})

function setup(how: 'rows' | 'fail' | 'pending' = 'rows') {
  if (how === 'fail') vi.mocked(fetchCiScorecard).mockRejectedValue(new Error('CI scorecard: HTTP 502'))
  else if (how === 'pending') vi.mocked(fetchCiScorecard).mockReturnValue(new Promise(() => {}))
  else vi.mocked(fetchCiScorecard).mockResolvedValue(scorecard())
  render(<QueryClientProvider client={new QueryClient({ defaultOptions: { queries: { retry: false } } })}><CiScorecardBlock /></QueryClientProvider>)
}

// Origin: as above — one row per repository and one for CI itself, each item with its verdict and the number behind it.
it('draws a row per repository and one for CI as provider, each cell with verdict and value', async () => {
  setup()
  const platform = await screen.findByTestId('ci-score-row-bifrost-platform')
  expect(platform).toHaveTextContent('bifrost-ci-platform · tier selective')
  const failed = within(platform).getByTestId('ci-score-bifrost-platform-landing_time')
  expect(failed).toHaveTextContent('Landing time')
  expect(failed).toHaveTextContent('fail')
  expect(failed).toHaveTextContent('median 183 s')
  expect(within(platform).getByTestId('ci-score-bifrost-platform-red_ratio')).toHaveTextContent('pass14 failed of 430')

  const infra = screen.getByTestId('ci-score-row-bifrost-trade-infra')
  expect(infra).toHaveTextContent('tier full')
  const unmeasured = within(infra).getByTestId('ci-score-bifrost-trade-infra-follows_change')
  expect(unmeasured).toHaveTextContent('unmeasured')
  expect(unmeasured).toHaveTextContent('the pipeline reports no api-scope')

  const provider = screen.getByTestId('ci-score-row-provider')
  expect(provider).toHaveTextContent('CI as provider')
  expect(within(provider).getByTestId('ci-score-provider-cleanup_keeps_up')).toHaveTextContent('0.6 h since it last succeeded')
  expect(screen.getByText(/Read 2026-11-16 12:00Z; kept up to 10 min\./)).toBeInTheDocument()
})

// Origin: as above — the pass lines are CI's and are published where the scores are.
it('lists the pass line of every standard', async () => {
  setup()
  const lines = within(await screen.findByRole('list', { name: 'Pass lines' })).getAllByRole('listitem')
  expect(lines.map(li => li.textContent)).toEqual([
    'Landing time: median of the runs that tested a ready branch themselves is at most 120 s (at least 5 runs)',
    'Red runs: at most 10% of completed runs failed',
    'Cleanup keeps up: the job that deletes old runs last succeeded at most 2 h ago',
  ])
})

// Origin: as above, and the page's rule that not read is not none — a failed read is one line, never an empty scorecard.
it('a failed read shows one alert and no row; a read not yet answered shows loading', async () => {
  setup('fail')
  expect(await screen.findByRole('alert')).toHaveTextContent('The CI scorecard could not be read: CI scorecard: HTTP 502. The scores are not known.')
  expect(screen.queryByTestId(/^ci-score-row-/)).toBeNull()
  cleanup()
  setup('pending')
  expect(screen.getByText('Loading…')).toBeInTheDocument()
  expect(screen.queryByRole('alert')).toBeNull()
})
