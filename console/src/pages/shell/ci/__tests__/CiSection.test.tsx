import '@testing-library/jest-dom/vitest'
import { cleanup, fireEvent, render, screen, within } from '@testing-library/react'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { afterEach, expect, it, vi } from 'vitest'
import { fetchSupplyChain } from '@/api/delivery'
import type { SupplyChainResponse } from '@/api/deliveryTypes'
import { fetchApprovalReadings } from '@/api/readings'
import { fetchFullCi, fetchUnitsConfig, fetchUnitsRuns, fetchUnitsPlan } from '@/api/units'
import { CiSection } from '../CiSection'
import { contract } from './contract'

/*
 * Origin of every case in this file — 2026-11-13/14, the Owner: he accepts CI's configuration and runs only when the
 * Ops page shows them, computed from the source CI itself reads; and CI's own front end is tested against the contract
 * its back end keeps (api/internal/server/testdata/contract/units), not against the back end's source.
 */

vi.mock('../PlatformTimings', () => ({ PlatformTimings: () => null }))
vi.mock('@/api/units', () => ({ fetchUnitsConfig: vi.fn(), fetchUnitsRuns: vi.fn(), fetchFullCi: vi.fn(), fetchUnitsPlan: vi.fn() }))
vi.mock('@/api/readings', () => ({ fetchApprovalReadings: vi.fn() }))
vi.mock('@/api/delivery', () => ({ fetchSupplyChain: vi.fn() }))
afterEach(() => { cleanup(); vi.resetAllMocks() })

/** Two projects the mocked platform tracks: `r` (the contract's) declares, `plain` (invented) does not. */
function setup(over: { runs?: 'fail' | 'none'; fullCi?: 'none'; projects?: 'fail' } = {}) {
  if (over.projects === 'fail') vi.mocked(fetchSupplyChain).mockRejectedValue(new Error('supply chain: HTTP 503'))
  else vi.mocked(fetchSupplyChain).mockResolvedValue({ tracked_repos: ['r', 'plain'] } as SupplyChainResponse)
  vi.mocked(fetchUnitsConfig).mockImplementation(async repo => repo === 'r' ? contract.configDeclared() : { ...contract.configUndeclared(), repo })
  vi.mocked(fetchUnitsRuns).mockImplementation(async repo => {
    if (over.runs === 'fail') throw new Error('CI runs: HTTP 502')
    return over.runs === 'none' || repo !== 'r' ? { ...contract.runsNone(), repo } : contract.runs()
  })
  vi.mocked(fetchFullCi).mockResolvedValue(over.fullCi === 'none' ? contract.fullCiNone() : contract.fullCi())
  vi.mocked(fetchApprovalReadings).mockResolvedValue(contract.approvalsNone())
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  render(<QueryClientProvider client={client}><CiSection /></QueryClientProvider>)
}

// Origin: as above — a project with no declaration says that all of it runs, and shows no module row.
it('an undeclared project shows "Not declared: everything runs" and its note', async () => {
  setup()
  await screen.findByTestId('ci-config-project-r')
  fireEvent.change(screen.getByRole('combobox', { name: 'Project' }), { target: { value: 'plain' } })
  const project = await screen.findByTestId('ci-config-project-plain')
  expect(await within(project).findByText('Not declared: everything runs')).toBeInTheDocument()
  expect(screen.getByText(contract.configUndeclared().note!)).toBeInTheDocument()
  expect(screen.queryByTestId(/^ci-config-module-plain-/)).toBeNull()
})

// Origin: as above — the module row is what the route answered, with the two columns it does not answer yet said so.
it('a declared project shows its pins and one row per module', async () => {
  setup()
  const row = await screen.findByTestId('ci-config-module-r-svc')
  expect(row).toHaveTextContent('test ×2 (3 shards), vet ×1')
  expect(row).toHaveTextContent('1 of 3 units')
  expect(within(row).getAllByText('not declared yet')).toHaveLength(2)
  expect(screen.getByTestId('ci-config-project-r')).toHaveTextContent('tested with lib@11111111')
  expect(screen.getByTestId('ci-config-module-r-web')).toHaveTextContent('svc/a/a.go, lib:.')
  expect(vi.mocked(fetchUnitsConfig).mock.calls.map(call => call[0]).sort()).toEqual(['r'])
})

// Origin: as above — the first project's runs are drawn as the route answered them.
it('draws the first project\'s runs: what each ran, its kind, and its failed task', async () => {
  setup()
  const partial = await screen.findByTestId('ci-run-ci-2')
  expect(partial).toHaveTextContent('svc, repo')
  expect(partial).toHaveTextContent("not run, their keys are the base's: web")
  expect(screen.getByTestId('ci-run-ci-3')).toHaveTextContent('reused ci-2')
  expect(screen.getByTestId('ci-run-ci-1')).toHaveTextContent('not reported')
  expect(screen.getByTestId('ci-run-ci-5')).toHaveTextContent('running')
  expect(screen.getByTestId('ci-run-ci-full-444444444444-20261114')).toHaveTextContent('test-web 199s (failed)')
  expect(screen.getByText(/Read 2026-11-14 16:45Z; kept up to 30 s\./)).toBeInTheDocument()
  expect(vi.mocked(fetchUnitsRuns).mock.calls[0][0]).toBe('r')
})

// Origin: as above — not read is not none: a failed read of the runs is an alert, never an empty table or "no run".
it('a failed read of the runs shows an alert, not an empty table', async () => {
  setup({ runs: 'fail' })
  const alert = await screen.findByText(/The CI runs of r could not be read: CI runs: HTTP 502\./)
  expect(alert).toHaveAttribute('role', 'alert')
  expect(alert).toHaveTextContent('is not known')
  expect(screen.queryByText('No CI run of this project is retained')).toBeNull()
  expect(screen.queryByRole('columnheader', { name: 'Modules run' })).toBeNull()
})

// Origin: as above — empty says what is empty; picking a project reads that project's runs.
it('a project with no retained run says so, after it is picked', async () => {
  setup()
  await screen.findByTestId('ci-run-ci-2')
  fireEvent.change(screen.getByRole('combobox', { name: 'Project' }), { target: { value: 'plain' } })
  expect(await screen.findByText('No CI run of this project is retained')).toBeInTheDocument()
  expect(vi.mocked(fetchUnitsRuns).mock.calls.map(call => call[0])).toContain('plain')
})

// Origin: as above — the red full run's suspects are drawn one per line, with what could not be read.
it('draws the suspect line of a red full run, and what could not be read or reconciled', async () => {
  setup()
  expect(await screen.findByText('33333333 svc repo — feat: a change to svc alone')).toBeInTheDocument()
  expect(screen.getByText('1 more could not be read')).toBeInTheDocument()
  expect(screen.getByText('Not reconciled: no full run in the last 3 days was green')).toBeInTheDocument()
  expect(screen.getByText('Full CI started: ci-full-111111111111-20261114.')).toBeInTheDocument()
})

// Origin: as above — no note is "not recorded yet", not an empty table.
it('says no full run has been recorded when the route holds no note', async () => {
  setup({ fullCi: 'none' })
  expect(await screen.findByText('No full run has been recorded yet')).toBeInTheDocument()
  expect(screen.queryByRole('columnheader', { name: 'Suspects' })).toBeNull()
})

// Origin: as above — the project list not read is not "no project": the configuration and runs are not known.
it('a failed read of the project list is an alert, and the full run is still drawn', async () => {
  setup({ projects: 'fail' })
  const alert = await screen.findByText(/The list of projects could not be read: supply chain: HTTP 503\./)
  expect(alert).toHaveAttribute('role', 'alert')
  expect(screen.queryByRole('columnheader', { name: 'Module' })).toBeNull()
  expect(await screen.findByText('1 more could not be read')).toBeInTheDocument()
  expect(fetchUnitsConfig).not.toHaveBeenCalled()
})

// Origin: Owner 2026-11-15 could see repositories but not their units/dependencies.
// Inputs must be fetched for the displayed config commit, not guessed from the latest run.
it('explores units on demand and clears the context when selecting another repo', async () => {
  setup()
  vi.mocked(fetchUnitsPlan).mockResolvedValue({repo:'r', commit:contract.configDeclared().commit, units:[{unit:'svc/a', task:'test', module:'svc', input_key:'key-a', verdict:'green', inputs:{reads:['lib:.']}}]})
  await screen.findByTestId('ci-config-module-r-svc')
  expect(fetchUnitsPlan).not.toHaveBeenCalled()
  fireEvent.click(screen.getByRole('button', { name:'Explore unit inputs' }))
  expect(await screen.findByText('svc/a · test')).toBeInTheDocument()
  expect(vi.mocked(fetchUnitsPlan).mock.calls[0].slice(0,2)).toEqual(['r', contract.configDeclared().commit])
  fireEvent.change(screen.getByRole('combobox', { name:'Project' }), {target:{value:'plain'}})
  expect(await screen.findByTestId('ci-config-project-plain')).toBeInTheDocument()
  expect(screen.queryByText('svc/a · test')).toBeNull()
  expect(screen.queryByTestId('ci-config-project-r')).toBeNull()
})

// Origin: Owner 2026-11-15: a single module choice must drive the whole CI page.
// Reused/unknown scope cannot be silently treated as a module test or dropped as unrelated.
it('links module cards and runs, preserves unknown evidence, and resets on project change', async () => {
  setup()
  await screen.findByTestId('ci-config-module-r-web')
  await screen.findByTestId('ci-run-ci-2')
  fireEvent.change(screen.getByRole('combobox', { name: 'Module' }), { target: { value: 'web' } })
  expect(screen.queryByTestId('ci-config-module-r-svc')).toBeNull()
  expect(screen.getByTestId('ci-config-module-r-web')).toBeInTheDocument()
  expect(screen.queryByTestId('ci-run-ci-2')).toBeNull()
  expect(screen.getByTestId('ci-run-ci-3')).toHaveTextContent('reused ci-2')
  expect(screen.getByTestId('ci-run-ci-1')).toHaveTextContent('not reported')
  expect(screen.getByText(/Daily full run is a whole-repository check/)).toBeInTheDocument()
  fireEvent.change(screen.getByRole('combobox', { name: 'Project' }), { target: { value: 'plain' } })
  await screen.findByTestId('ci-config-project-plain')
  expect(screen.getByRole('combobox', { name: 'Module' })).toHaveValue('')
})
