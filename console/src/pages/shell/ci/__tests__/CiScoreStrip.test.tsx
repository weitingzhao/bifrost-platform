import '@testing-library/jest-dom/vitest'
import { cleanup, fireEvent, render, screen, within } from '@testing-library/react'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { afterEach, expect, it, vi } from 'vitest'
import { fetchCiScorecard, type CiScorecard } from '@/api/ciScorecard'
import { CiScoreStrip } from '../CiScoreStrip'

/*
 * Origin of every case in this file — Owner 2026-11-16: CI scores every repository that uses it, and its users score
 * CI; only what is measured is shown, each item with its verdict and raw number, and the pass lines are on the page.
 * The same day he read Work → CI and could not tell what was running, who depends on whom, or what a change pulls in;
 * this page and its two routes replace it, and the scorecard became one line on it.
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
  render(<QueryClientProvider client={new QueryClient({ defaultOptions: { queries: { retry: false } } })}><CiScoreStrip /></QueryClientProvider>)
}

// Origin: as above — one row per repository and one for CI itself, on one line: a failing item is a pill with its
// name, a passing one a dot with a title, an unmeasured one hollow.
it('draws a row per repository and one for CI itself; only a failing item carries its name', async () => {
  setup()
  const platform = await screen.findByTestId('ci-score-row-bifrost-platform')
  expect(screen.getByRole('heading', { name: 'Three-day scores' })).toBeInTheDocument()
  const failed = within(platform).getByTestId('ci-score-bifrost-platform-landing_time')
  expect(failed).toHaveTextContent('Landing time')
  expect(failed).toHaveAttribute('data-verdict', 'fail')
  const passed = within(platform).getByTestId('ci-score-bifrost-platform-red_ratio')
  expect(passed).toHaveTextContent('')
  expect(passed).toHaveAttribute('title', 'Red runs: pass')
  expect(screen.getByTestId('ci-score-bifrost-trade-infra-follows_change')).toHaveAttribute('title', 'Time follows the change: not measured')
  expect(within(screen.getByTestId('ci-score-row-provider')).getByRole('button', { name: 'CI itself · Cleanup keeps up: pass' })).toBeInTheDocument()
  expect(screen.queryByTestId('ci-score-detail')).toBeNull()
})

// Origin: as above — the number behind a verdict and CI's pass line are one click away, for every item.
it('opens an item\'s value and its pass line on click, and closes it on a second click', async () => {
  setup()
  fireEvent.click(await screen.findByTestId('ci-score-bifrost-platform-landing_time'))
  const detail = screen.getByTestId('ci-score-detail')
  expect(detail).toHaveTextContent('Landing time · bifrost-platform · fail · median 183 s')
  expect(detail).toHaveTextContent('Pass line: median of the runs that tested a ready branch themselves is at most 120 s (at least 5 runs) · 247 samples · runs completed in the last 3 days · read 2026-11-16 12:00Z, kept up to 10 min')
  fireEvent.click(screen.getByTestId('ci-score-provider-cleanup_keeps_up'))
  expect(screen.getByTestId('ci-score-detail')).toHaveTextContent('Cleanup keeps up · CI itself · pass · 0.6 h since it last succeeded')
  expect(screen.getByTestId('ci-score-detail')).toHaveTextContent('the job that deletes old runs last succeeded at most 2 h ago')
  fireEvent.click(screen.getByTestId('ci-score-bifrost-trade-infra-follows_change'))
  expect(screen.getByTestId('ci-score-detail')).toHaveTextContent('not measured · the pipeline reports no api-scope')
  expect(screen.getByTestId('ci-score-detail')).toHaveTextContent('Pass line: not published for this item')
  fireEvent.click(screen.getByTestId('ci-score-bifrost-trade-infra-follows_change'))
  expect(screen.queryByTestId('ci-score-detail')).toBeNull()
})

// Origin: as above, and the page's rule that not read is not none — a failed read is one line, never an empty strip.
it('a failed read shows one alert and no row; a read not yet answered shows loading', async () => {
  setup('fail')
  expect(await screen.findByRole('alert')).toHaveTextContent('The CI scorecard could not be read: CI scorecard: HTTP 502. The scores are not known.')
  expect(screen.queryByTestId(/^ci-score-row-/)).toBeNull()
  cleanup()
  setup('pending')
  expect(screen.getByText('Loading…')).toBeInTheDocument()
  expect(screen.queryByRole('alert')).toBeNull()
})
