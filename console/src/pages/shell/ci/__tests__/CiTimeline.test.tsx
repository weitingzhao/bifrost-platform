import '@testing-library/jest-dom/vitest'
import { cleanup, fireEvent, render, screen } from '@testing-library/react'
import { afterEach, expect, it, vi } from 'vitest'
import { CiDomainCards } from '../CiDomainCards'
import { CiRunPanel } from '../CiRunPanel'
import { CiTimeline } from '../CiTimeline'
import { changeMap, domainCards, ownRuns } from '../ciPageModel'
import type { TimingRun } from '@/api/ciTimings'
import { AS_OF, change, graph, sha, unitsRun } from './ciPageFixture'

/*
 * Origin of every case in this file — 2026-11-16 (ISSUE-29): Work → CI could not tell what was running, who
 * depends on whom, or what a change pulls in; this page and its two routes replace it. Each case says what it guards.
 */

afterEach(cleanup)

const many = (count: number) => ownRuns(Array.from({ length: count }, (_, i) => unitsRun(i + 1, (count - i) * 5)), AS_OF)

// Guards "default newest 40, a show all N control": the first screen of bars is bounded, and nothing is hidden for good.
it('draws the newest 40 runs and all of them on request', () => {
  render(<CiTimeline runs={many(45)} changes={new Map()} domain="" selectedRun="" onSelectRun={() => {}} />)
  expect(screen.getAllByTestId(/^ci-bar-/)).toHaveLength(40)
  expect(screen.getByTestId('ci-bar-ci-45')).toBeInTheDocument()
  expect(screen.queryByTestId('ci-bar-ci-5')).toBeNull()
  fireEvent.click(screen.getByRole('button', { name: 'show all 45' }))
  expect(screen.getAllByTestId(/^ci-bar-/)).toHaveLength(45)
  fireEvent.click(screen.getByRole('button', { name: 'show the newest 40' }))
  expect(screen.getAllByTestId(/^ci-bar-/)).toHaveLength(40)
})

// Guards "a red run carries the failed task name; a selective run is marked": the two things the Owner looks for in the bars.
it('writes the failed task beside a red run and marks a selective one', () => {
  const detail = { name: 'ci-1', results: [{ name: 'api-scope', value: 'incremental' }, { name: 'api-packages', value: 'internal/a' }], tasks: [] } as unknown as TimingRun
  const runs = ownRuns([unitsRun(2, 5, { failed: 'test-api' }), unitsRun(1, 30)], AS_OF, [detail])
  render(<CiTimeline runs={runs} changes={new Map()} domain="" selectedRun="" onSelectRun={() => {}} />)
  expect(screen.getByTestId('ci-bar-ci-2')).toHaveTextContent('test-api')
  expect(screen.getByTestId('ci-bar-ci-1')).toHaveTextContent('selective')
  expect(screen.getByTestId('ci-bar-ci-2')).not.toHaveTextContent('selective')
  expect(screen.getByRole('button', { name: /Run of 00000002, .* failed in test-api/ })).toBeInTheDocument()
})

// Guards the shared selection in By time: runs of other domains dim (and stay), a run not read is under no domain,
// and clicking a bar selects that run, clicking it again clears it.
it('dims the runs of other domains and selects a run on click', () => {
  const runs = ownRuns([unitsRun(3, 5), unitsRun(2, 10), unitsRun(1, 30)], AS_OF)
  const changes = changeMap([change(3, { plan: 1 }), change(2, { owner: 2 }), change(1, null)])
  const onSelectRun = vi.fn()
  const { rerender } = render(<CiTimeline runs={runs} changes={changes} domain="plan" selectedRun="" onSelectRun={onSelectRun} />)
  expect(screen.getByTestId('ci-bar-ci-3')).toHaveAttribute('data-dim', 'no')
  expect(screen.getByTestId('ci-bar-ci-2')).toHaveAttribute('data-dim', 'yes')
  expect(screen.getByTestId('ci-bar-ci-1')).toHaveAttribute('data-dim', 'yes')
  fireEvent.click(screen.getByRole('button', { name: /Run of 00000003/ }))
  expect(onSelectRun).toHaveBeenLastCalledWith('ci-3')
  rerender(<CiTimeline runs={runs} changes={changes} domain="" selectedRun="ci-3" onSelectRun={onSelectRun} />)
  expect(screen.getByTestId('ci-bar-ci-2')).toHaveAttribute('data-dim', 'no')
  expect(screen.getByRole('button', { name: /Run of 00000003/ })).toHaveAttribute('aria-pressed', 'true')
  fireEvent.click(screen.getByRole('button', { name: /Run of 00000003/ }))
  expect(onSelectRun).toHaveBeenLastCalledWith('')
})

// Guards "legible at 390 px" in the drawing itself: the phone drawing is 360 units wide and no label starts past its edge.
it('draws a 360-unit timeline at phone width with every label inside it', () => {
  const red = unitsRun(3, 5, { api: 900 })
  const runs = ownRuns([{ ...red, status: 'Failed', tasks: [...red.tasks, { task: 'test-api-store', status: 'Failed', seconds: 400, waited: 5 }] }, unitsRun(2, 10), unitsRun(1, 30)], AS_OF)
  render(<CiTimeline runs={runs} changes={new Map()} domain="" selectedRun="" onSelectRun={() => {}} narrow />)
  const svg = screen.getByTestId('ci-timeline')
  expect(svg).toHaveAttribute('data-narrow', 'yes')
  expect(svg.getAttribute('viewBox')).toMatch(/^0 0 360 /)
  const label = [...svg.querySelectorAll('text')].find(t => t.textContent === 'test-api-store')!
  expect(Number(label.getAttribute('x')) + 'test-api-store'.length * 6).toBeLessThanOrEqual(360)
  const ticks = [...svg.querySelectorAll('text')].filter(t => / s$/.test(t.textContent ?? '')).map(t => Number(t.getAttribute('x')))
  for (let i = 1; i < ticks.length; i++) expect(ticks[i] - ticks[i - 1]).toBeGreaterThanOrEqual(44)
})

// Guards By domain: one card a domain plus the three places, one dot a run, the red count and median, and click selects.
it('draws one card a domain with its runs, and selects a domain on click', () => {
  const runs = ownRuns([unitsRun(4, 5), unitsRun(3, 10, { failed: 'test-api' }), unitsRun(2, 20)], AS_OF)
  const cards = domainCards(graph(), runs, changeMap([change(4, { plan: 2 }), change(3, { plan: 1, repo: 1 }), change(2, { foundation: 3 })]))
  const onSelectDomain = vi.fn()
  const { rerender } = render(<CiDomainCards cards={cards} domain="" onSelectDomain={onSelectDomain} />)
  expect(screen.getAllByTestId(/^ci-domain-/)).toHaveLength(7)
  const plan = screen.getByTestId('ci-domain-plan')
  expect(plan).toHaveTextContent('plan2 runs')
  expect(plan).toHaveTextContent('1 red · median 175 s · 0 selective')
  expect(plan.querySelectorAll('i')).toHaveLength(2)
  expect(screen.getByTestId('ci-domain-lineage')).toHaveTextContent('no retained run changed it')
  expect(screen.getByTestId('ci-domain-repo')).toHaveTextContent('repository files1 runs')
  fireEvent.click(plan)
  expect(onSelectDomain).toHaveBeenLastCalledWith('plan')
  rerender(<CiDomainCards cards={cards} domain="plan" onSelectDomain={onSelectDomain} />)
  expect(screen.getByTestId('ci-domain-plan')).toHaveAttribute('aria-pressed', 'true')
  fireEvent.click(screen.getByTestId('ci-domain-plan'))
  expect(onSelectDomain).toHaveBeenLastCalledWith('')
})

// Guards the run panel: took = waiting + before the tests + the test tasks side by side; the packages of a selective
// run; what it changed by domain; and a commit that was not read is said, not shown as "changed nothing".
it('opens a run as numbers: its parts, what it tested and what it changed', () => {
  const detail = { name: 'ci-1', results: [{ name: 'api-scope', value: 'incremental' }, { name: 'api-packages', value: 'internal/a internal/b' }], tasks: [] } as unknown as TimingRun
  const [run] = ownRuns([{ ...unitsRun(1, 30), run_modules: ' api repo ' }], AS_OF, [detail])
  const { rerender } = render(<CiRunPanel run={run} change={change(1, { plan: 2, shared: 1 })} changesRead />)
  const panel = screen.getByRole('region', { name: 'Selected run' })
  expect(panel).toHaveTextContent(`Run of ${sha(1).slice(0, 8)}`)
  expect(panel).toHaveTextContent('175 s = 8 waiting + 17 clone and scope + the longest of 60 test-api | 150 test-console (side by side)')
  expect(panel).toHaveTextContent('2 Go packages (selective); sides: api, repo')
  expect(panel).toHaveTextContent('internal/a internal/b')
  expect(panel).toHaveTextContent('plan (2 files), shared packages (1 file)')
  rerender(<CiRunPanel run={run} change={change(1, null)} changesRead />)
  expect(screen.getByRole('region', { name: 'Selected run' })).toHaveTextContent('not read: the commit is not in the repository')
})
