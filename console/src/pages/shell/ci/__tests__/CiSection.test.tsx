import '@testing-library/jest-dom/vitest'
import { act, cleanup, fireEvent, render, screen, within } from '@testing-library/react'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { afterEach, expect, it, vi } from 'vitest'
import { fetchCiChanges, fetchCiDomains, type CiChanges, type CiDomainGraph } from '@/api/ciDomains'
import { fetchCiScorecard, type CiScorecard } from '@/api/ciScorecard'
import { fetchCITimings, type PipelineTimings, type TimingRun } from '@/api/ciTimings'
import { fetchReleaseWindow, fetchSupplyChain } from '@/api/delivery'
import type { SupplyChainResponse } from '@/api/deliveryTypes'
import { fetchApprovalReadings } from '@/api/readings'
import { fetchReleasePolicy, type ReleasePolicyStatus } from '@/api/releasePolicy'
import { fetchFullCi, fetchUnitsConfig, fetchUnitsRuns, fetchUnitsPlan, type UnitsRuns } from '@/api/units'
import { CiSection } from '../CiSection'
import { AS_OF, graph, running, sha, unitsRun } from './ciPageFixture'
import { contract } from './contract'

/*
 * Origin of the cases marked "contract" — 2026-11-13/14, the Owner: he accepts CI's configuration and runs only when
 * the Ops page shows them, computed from the source CI itself reads; and CI's own front end is tested against the
 * contract its back end keeps (api/internal/server/testdata/contract/units), not against the back end's source.
 *
 * Origin of the cases marked "page" — 2026-11-16: the Owner read Work → CI and could not tell what was running, who
 * depends on whom, or what a change pulls in; this page and its two routes replace it. The tables he had are kept,
 * each folded and read only when opened; the cases that were about them open the fold first.
 */

vi.mock('../PlatformTimings', () => ({ PlatformTimings: () => null }))
vi.mock('@/api/units', () => ({ fetchUnitsConfig: vi.fn(), fetchUnitsRuns: vi.fn(), fetchFullCi: vi.fn(), fetchUnitsPlan: vi.fn() }))
vi.mock('@/api/readings', () => ({ fetchApprovalReadings: vi.fn() }))
vi.mock('@/api/delivery', () => ({ fetchSupplyChain: vi.fn(), fetchReleaseWindow: vi.fn(), fetchCompare: vi.fn(), fetchPipelineRunLogs: vi.fn() }))
vi.mock('@/api/ciTimings', () => ({ fetchCITimings: vi.fn() }))
vi.mock('@/api/ciScorecard', () => ({ fetchCiScorecard: vi.fn() }))
vi.mock('@/api/releasePolicy', () => ({ fetchReleasePolicy: vi.fn() }))
vi.mock('@/api/ciDomains', async original => ({ ...(await original<typeof import('@/api/ciDomains')>()), fetchCiDomains: vi.fn(), fetchCiChanges: vi.fn() }))
// The graph library measures the DOM to draw; here it is a list of the nodes and edges it was given.
vi.mock('@xyflow/react', () => ({
  ReactFlow: ({ nodes, edges, nodeTypes, onNodeClick }: { nodes: { id: string; type: string; data: unknown }[]; edges: { id: string; data: { count: number; wrongWay: boolean; hot: boolean; opacity: number }; style: { strokeWidth: number } }[]; nodeTypes: Record<string, (props: { data: unknown }) => React.ReactNode>; onNodeClick: (event: unknown, node: unknown) => void }) => (
    <div>
      {nodes.map(n => { const View = nodeTypes[n.type]; return <div key={n.id} onClick={event => onNodeClick(event, n)}><View data={n.data} /></div> })}
      {edges.map(e => <span key={e.id} data-testid={`ci-dep-edge-${e.id}`} data-wrong={String(e.data.wrongWay)} data-hot={String(e.data.hot)} data-opacity={e.data.opacity} data-width={e.style.strokeWidth}>{`${e.data.count}${e.data.wrongWay ? ' wrong way' : ''}`}</span>)}
    </div>
  ),
  Handle: () => null, BaseEdge: () => null, EdgeLabelRenderer: () => null, getBezierPath: () => ['', 0, 0], getStraightPath: () => ['', 0, 0],
  Position: { Top: 'top', Bottom: 'bottom', Left: 'left', Right: 'right' }, MarkerType: { ArrowClosed: 'arrowclosed' },
}))
afterEach(() => { cleanup(); vi.resetAllMocks() })

type Over = {
  runs?: 'fail' | 'none' | UnitsRuns; fullCi?: 'none'; projects?: 'fail'; repos?: string[]
  graph?: CiDomainGraph | 'fail'; changes?: CiChanges['changes']; timings?: Record<string, TimingRun[]>; window?: 'held' | 'fail'; policy?: ReleasePolicyStatus; scorecard?: CiScorecard
}

/** Two projects the mocked platform tracks: `r` (the contract's) declares, `plain` (invented) does not. */
function setup(over: Over = {}) {
  if (over.projects === 'fail') vi.mocked(fetchSupplyChain).mockRejectedValue(new Error('supply chain: HTTP 503'))
  else vi.mocked(fetchSupplyChain).mockResolvedValue({ tracked_repos: over.repos ?? ['r', 'plain'] } as SupplyChainResponse)
  const first = (over.repos ?? ['r', 'plain'])[0]
  vi.mocked(fetchUnitsConfig).mockImplementation(async repo => repo === first ? { ...contract.configDeclared(), repo } : { ...contract.configUndeclared(), repo })
  vi.mocked(fetchUnitsRuns).mockImplementation(async repo => {
    if (over.runs === 'fail') throw new Error('CI runs: HTTP 502')
    if (typeof over.runs === 'object') return repo === first ? over.runs : { ...contract.runsNone(), repo }
    return over.runs === 'none' || repo !== first ? { ...contract.runsNone(), repo } : contract.runs()
  })
  vi.mocked(fetchFullCi).mockResolvedValue(over.fullCi === 'none' ? contract.fullCiNone() : contract.fullCi())
  vi.mocked(fetchApprovalReadings).mockResolvedValue(contract.approvalsNone())
  vi.mocked(fetchCITimings).mockImplementation(async pipeline => {
    if (!over.timings) throw new Error('CI timings: HTTP 502')
    return { as_of: AS_OF, retained: 0, limit: 60, runs: over.timings[pipeline] ?? [] } as PipelineTimings
  })
  if (over.window === 'fail') vi.mocked(fetchReleaseWindow).mockRejectedValue(new Error('release window: HTTP 401'))
  else vi.mocked(fetchReleaseWindow).mockResolvedValue(over.window === 'held' ? { open: true, window: { who: 'agent-8-comm', what: 'bifrost-platform', env: 'prod', pid: 1, host: 'h', started_at: AS_OF } } : { open: false })
  if (over.policy) vi.mocked(fetchReleasePolicy).mockResolvedValue(over.policy)
  else vi.mocked(fetchReleasePolicy).mockRejectedValue(new Error('release-policy: HTTP 502'))
  if (over.scorecard) vi.mocked(fetchCiScorecard).mockResolvedValue(over.scorecard)
  else vi.mocked(fetchCiScorecard).mockRejectedValue(new Error('CI scorecard: HTTP 502'))
  vi.mocked(fetchCiDomains).mockImplementation(async repo => {
    if (over.graph === 'fail') throw new Error('CI domains: HTTP 502')
    if (over.graph && repo === first) return { ...over.graph, repo }
    return { version: 1, repo, commit: sha(999), measured: false, note: 'this repository has no config/domain-ownership.json: its domains are not measured', domains: [], edges: [], shared_packages: [] }
  })
  vi.mocked(fetchCiChanges).mockImplementation(async (repo, commits) => ({ version: 1, repo, measured: true, note: '', changes: commits.map(commit => over.changes?.find(c => c.commit === commit) ?? { commit, read: false, domains: {} }) }))
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  render(<QueryClientProvider client={client}><CiSection /></QueryClientProvider>)
  return client
}

/** Opens one of the folded blocks. Nothing in it is mounted before. */
const open = (id: string) => fireEvent.click(within(screen.getByTestId(id)).getByText((_, el) => el?.tagName === 'SUMMARY'))
const pickProject = (name: string) => fireEvent.change(screen.getByRole('combobox', { name: 'Project' }), { target: { value: name } })
const view = (name: string) => fireEvent.click(within(screen.getByRole('group', { name: 'Arrange by' })).getByRole('button', { name }))

// ── The tables he had, now folded ───────────────────────────────────────────

// Origin: contract — a project with no declaration says that all of it runs, and shows no module row.
it('an undeclared project shows "Not declared: everything runs" and its note', async () => {
  setup()
  await screen.findByTestId('ci-verdict')
  pickProject('plain')
  open('ci-fold-config')
  const project = await screen.findByTestId('ci-config-project-plain')
  expect(await within(project).findByText('Not declared: everything runs')).toBeInTheDocument()
  expect(screen.getByText(contract.configUndeclared().note!)).toBeInTheDocument()
  expect(screen.queryByTestId(/^ci-config-module-plain-/)).toBeNull()
})

// Origin: contract — the module row is what the route answered, with the two columns it does not answer yet said so.
it('a declared project shows its pins and one row per module', async () => {
  setup()
  await screen.findByTestId('ci-verdict')
  open('ci-fold-config')
  const row = await screen.findByTestId('ci-config-module-r-svc')
  expect(row).toHaveTextContent('test ×2 (3 shards), vet ×1')
  expect(row).toHaveTextContent('1 of 3 units')
  expect(within(row).getAllByText('not declared yet')).toHaveLength(2)
  expect(screen.getByTestId('ci-config-project-r')).toHaveTextContent('tested with lib@11111111')
  expect(screen.getByTestId('ci-config-module-r-web')).toHaveTextContent('svc/a/a.go, lib:.')
  expect(vi.mocked(fetchUnitsConfig).mock.calls.map(call => call[0]).sort()).toEqual(['r'])
})

// Origin: contract — the project's runs are drawn as the route answered them; the page reads the route's most (100).
it('draws the first project\'s runs: what each ran, its kind, and its failed task', async () => {
  setup()
  await screen.findByTestId('ci-verdict')
  open('ci-fold-runs')
  const partial = await screen.findByTestId('ci-run-ci-2')
  expect(partial).toHaveTextContent('svc, repo')
  expect(partial).toHaveTextContent("not run, their keys are the base's: web")
  expect(screen.getByTestId('ci-run-ci-3')).toHaveTextContent('reused ci-2')
  expect(screen.getByTestId('ci-run-ci-1')).toHaveTextContent('not reported')
  expect(screen.getByTestId('ci-run-ci-5')).toHaveTextContent('running')
  expect(screen.getByTestId('ci-run-ci-full-444444444444-20261114')).toHaveTextContent('test-web 199s (failed)')
  expect(screen.getByText(/Read 2026-11-14 16:45Z; kept up to 30 s\./)).toBeInTheDocument()
  expect(vi.mocked(fetchUnitsRuns).mock.calls[0][0]).toBe('r')
  expect(vi.mocked(fetchUnitsRuns).mock.calls[0][2]).toBe(100)
})

// Origin: contract — not read is not none: a failed read of the runs is an alert, never an empty table, a verdict or "no run".
it('a failed read of the runs shows an alert, not a verdict or an empty table', async () => {
  setup({ runs: 'fail' })
  const alert = await screen.findByText(/The CI runs of r could not be read: CI runs: HTTP 502\./)
  expect(alert).toHaveAttribute('role', 'alert')
  expect(alert).toHaveTextContent('is not known')
  expect(screen.getByText('The CI runs could not be read: CI runs: HTTP 502. What CI is doing now is not known.')).toHaveAttribute('role', 'alert')
  expect(screen.queryByTestId('ci-verdict')).toBeNull()
  expect(screen.queryByTestId('ci-timeline')).toBeNull()
  open('ci-fold-runs')
  expect(screen.queryByText('No CI run of this project is retained')).toBeNull()
  expect(screen.queryByRole('columnheader', { name: 'Modules run' })).toBeNull()
})

// Origin: contract — empty says what is empty; picking a project reads that project's runs.
it('a project with no retained run says so, after it is picked', async () => {
  setup()
  await screen.findByTestId('ci-timeline')
  pickProject('plain')
  expect(await screen.findByText('No run that tested itself is retained for this repository.')).toBeInTheDocument()
  open('ci-fold-runs')
  expect(await screen.findByText('No CI run of this project is retained')).toBeInTheDocument()
  expect(vi.mocked(fetchUnitsRuns).mock.calls.map(call => call[0])).toContain('plain')
})

// Origin: contract — the red full run's suspects are drawn one per line, with what could not be read.
it('draws the suspect line of a red full run, and what could not be read or reconciled', async () => {
  setup()
  await screen.findByTestId('ci-verdict')
  open('ci-fold-full')
  expect(await screen.findByText('33333333 svc repo — feat: a change to svc alone')).toBeInTheDocument()
  expect(screen.getByText('1 more could not be read')).toBeInTheDocument()
  expect(screen.getByText('Not reconciled: no full run in the last 3 days was green')).toBeInTheDocument()
  expect(screen.getByText('Full CI started: ci-full-111111111111-20261114.')).toBeInTheDocument()
})

// Origin: contract — no note is "not recorded yet", not an empty table; page — and it is said in the fold, when opened.
it('says no full run has been recorded only inside its fold, when the route holds no note', async () => {
  setup({ fullCi: 'none' })
  await screen.findByTestId('ci-verdict')
  expect(screen.queryByText('No full run has been recorded yet')).toBeNull()
  expect(fetchFullCi).not.toHaveBeenCalled()
  open('ci-fold-full')
  expect(await screen.findByText('No full run has been recorded yet')).toBeInTheDocument()
  expect(screen.queryByRole('columnheader', { name: 'Suspects' })).toBeNull()
})

// Origin: contract — the project list not read is not "no project": the configuration and runs are not known, and the
// records that belong to no project can still be opened.
it('a failed read of the project list is an alert, and the full run can still be opened', async () => {
  setup({ projects: 'fail' })
  const alert = await screen.findByText(/The list of projects could not be read: supply chain: HTTP 503\./)
  expect(alert).toHaveAttribute('role', 'alert')
  expect(screen.queryByTestId('ci-verdict')).toBeNull()
  expect(screen.queryByRole('group', { name: 'Arrange by' })).toBeNull()
  open('ci-fold-full')
  expect(await screen.findByText('1 more could not be read')).toBeInTheDocument()
  expect(fetchUnitsConfig).not.toHaveBeenCalled()
})

// Origin: Owner 2026-11-15 could see repositories but not their units/dependencies.
// Inputs must be fetched for the displayed config commit, not guessed from the latest run.
it('explores units on demand and clears the context when selecting another repo', async () => {
  setup()
  vi.mocked(fetchUnitsPlan).mockResolvedValue({repo:'r', commit:contract.configDeclared().commit, units:[{unit:'svc/a', task:'test', module:'svc', input_key:'key-a', verdict:'green', inputs:{reads:['lib:.']}}]})
  await screen.findByTestId('ci-verdict')
  open('ci-fold-config')
  await screen.findByTestId('ci-config-module-r-svc')
  expect(fetchUnitsPlan).not.toHaveBeenCalled()
  fireEvent.click(screen.getByRole('button', { name:'Explore unit inputs' }))
  expect(await screen.findByText('svc/a · test')).toBeInTheDocument()
  expect(vi.mocked(fetchUnitsPlan).mock.calls[0].slice(0,2)).toEqual(['r', contract.configDeclared().commit])
  pickProject('plain')
  expect(screen.queryByText('svc/a · test')).toBeNull()
  open('ci-fold-config')
  expect(await screen.findByTestId('ci-config-project-plain')).toBeInTheDocument()
  expect(screen.queryByTestId('ci-config-project-r')).toBeNull()
})

// Origin: Owner 2026-11-15: a module choice must drive the configuration and the run list together.
// Reused/unknown scope cannot be silently treated as a module test or dropped as unrelated.
it('links module cards and runs, preserves unknown evidence, and resets on project change', async () => {
  setup()
  await screen.findByTestId('ci-verdict')
  open('ci-fold-config')
  open('ci-fold-runs')
  await screen.findByTestId('ci-config-module-r-web')
  await screen.findByTestId('ci-run-ci-2')
  fireEvent.click(within(screen.getByRole('group', { name: 'Module' })).getByRole('button', { name: 'web' }))
  expect(screen.queryByTestId('ci-config-module-r-svc')).toBeNull()
  expect(screen.getByTestId('ci-config-module-r-web')).toBeInTheDocument()
  expect(screen.queryByTestId('ci-run-ci-2')).toBeNull()
  expect(screen.getByTestId('ci-run-ci-3')).toHaveTextContent('reused ci-2')
  expect(screen.getByTestId('ci-run-ci-1')).toHaveTextContent('not reported')
  open('ci-fold-full')
  expect(screen.getByText(/Daily full run is a whole-repository check/)).toBeInTheDocument()
  pickProject('plain')
  open('ci-fold-config')
  await screen.findByTestId('ci-config-project-plain')
  expect(screen.queryByRole('button', { name: 'All modules' })).toBeNull()
  expect(screen.getByText('Module split not configured. CI tests this project as one block.')).toBeInTheDocument()
})

// Owner 2026-11-15: dependency groups share the project's workspace; records of no project say so.
it('separates project-scoped information from global records and never calls unknown independent', async () => {
  setup()
  await screen.findByTestId('ci-verdict')
  open('ci-fold-config')
  await screen.findByTestId('ci-config-module-r-web')
  const config = screen.getByTestId('ci-fold-config')
  const inputs = within(config).getByRole('group', { name: 'Uses shared files or other projects' })
  expect(within(inputs).getByRole('button', { name: 'web' })).toBeInTheDocument()
  expect(within(within(config).getByRole('group', { name: 'No extra module-wide dependencies' })).getByRole('button', { name: 'svc' })).toBeInTheDocument()
  expect(within(config).getByText(/No additional inputs does not mean independent/)).toBeInTheDocument()
  expect(screen.getByTestId('ci-fold-approvals')).toHaveTextContent('every project, outside the selection')
  pickProject('plain')
  open('ci-fold-config')
  await screen.findByTestId('ci-config-project-plain')
  expect(screen.getByText(/Module split not configured/)).toBeInTheDocument()
  expect(screen.queryByRole('group', { name: 'Uses shared files or other projects' })).toBeNull()
})

it('keeps the platform benchmark inside its project and removes it on project switch', async () => {
  setup({ repos: ['bifrost-platform', 'plain'] })
  await screen.findByTestId('ci-verdict')
  open('ci-fold-benchmark')
  expect(screen.getByText('Designed vs actual')).toBeInTheDocument()
  pickProject('plain')
  expect(screen.queryByTestId('ci-fold-benchmark')).toBeNull()
  expect(screen.queryByText('Designed vs actual')).toBeNull()
})

// Owner 2026-11-15: one selected attempt drives the details; page — it is the page's one selection, set from the run
// list or from a bar, shown at the right of the switch bar, and reset with the project.
it('selects a run from the run list as the page\'s one selection and resets it with the project', async () => {
  setup()
  await screen.findByTestId('ci-verdict')
  expect(screen.getByTestId('ci-selection')).toHaveTextContent('All domains')
  open('ci-fold-runs')
  fireEvent.click(within(await screen.findByTestId('ci-run-ci-2')).getByRole('button'))
  expect(screen.getByTestId('ci-selection')).toHaveTextContent('All domains · run 33333333clear')
  expect(within(screen.getByTestId('ci-run-ci-2')).getByRole('button')).toHaveAttribute('aria-pressed', 'true')
  expect(screen.getByRole('region', { name: 'Selected run' })).toHaveTextContent('Run of 33333333')
  expect(screen.getByRole('button', { name: /Run of 33333333/ })).toHaveAttribute('aria-pressed', 'true')
  expect(screen.getByRole('region', { name: 'Details' })).toHaveTextContent('follows the same selection: r · ci-2.')
  pickProject('plain')
  expect(await screen.findByTestId('ci-selection')).toHaveTextContent('All domains')
  expect(screen.queryByRole('region', { name: 'Selected run' })).toBeNull()
})

it('does not move the selected run when polling discovers a newer run', async () => {
  const client = setup()
  await screen.findByTestId('ci-verdict')
  fireEvent.click(screen.getByRole('button', { name: /Run of 33333333/ }))
  const original = contract.runs()
  await act(async () => { client.setQueryData(['units', 'runs', 'r'], { ...original, runs: [{ ...original.runs[0], name: 'ci-new', revision: 'a'.repeat(40) }, ...original.runs] }) })
  expect(await screen.findByTestId('ci-bar-ci-new')).toBeInTheDocument()
  expect(screen.getByTestId('ci-selection')).toHaveTextContent('run 33333333')
  expect(screen.getByRole('region', { name: 'Selected run' })).toHaveTextContent('Run of 33333333')
})

// ── The page ────────────────────────────────────────────────────────────────

const history = (): UnitsRuns => ({ repo: 'bifrost-platform', as_of: AS_OF, runs: [
  running(9, 1, [{ task: 'clone', status: 'Succeeded', seconds: 16, waited: 8 }, { task: 'changes', status: 'Succeeded', seconds: 9 }, { task: 'test-api', status: 'Running', waited: 5 }]),
  unitsRun(4, 5), unitsRun(3, 10, { failed: 'test-api' }), unitsRun(2, 20), unitsRun(1, 30),
] })
const changes = [{ commit: sha(4), read: true, domains: { plan: 2, console: 1 } }, { commit: sha(3), read: true, domains: { plan: 1 } }, { commit: sha(2), read: true, domains: { owner: 3 } }]
const stageRun = (name: string, commit: string, status: string, start: string, end: string | undefined, tasks: [string, string][] = []): TimingRun => ({
  name, pipeline: 'p', status, created_at: start, start_time: start, completion_time: end, params: {}, results: [{ name: 'commit', value: commit }], skipped: [],
  tasks: tasks.map(([task, taskStatus], i) => ({ name: `${name}-${task}`, task, status: taskStatus, created_at: `2026-11-16T19:00:0${i}Z`, results: [], steps: [] })),
})

// Origin: page — "what is CI doing now": the verdict with who it waits for, the pipeline with the run in flight lit on
// its step, and under each stage the facts computed from the reads; clicking the lit step opens that run in By time.
it('draws the verdict and the pipeline, and opens the run in flight from its lit step', async () => {
  setup({
    repos: ['bifrost-platform', 'plain'], runs: history(), window: 'held',
    policy: { valid: true, policy_id: 'rp-20261115-1745', remaining_seconds: 20 * 86400 + 5, expired: false, reasons: [], allow: [], frozen: false, reminder_windows: [], sign_command: '', unfreeze_command: '' },
    timings: {
      'bifrost-land': [stageRun('land-1', sha(2), 'True', '2026-11-16T19:40:00Z', '2026-11-16T19:40:14Z', [['freeze', 'succeeded'], ['merge', 'succeeded']])],
      'bifrost-deliver-platform': [stageRun('stg-2', sha(2), 'True', '2026-11-16T19:40:25Z', '2026-11-16T19:44:10Z', [['build', 'succeeded'], ['rollout', 'succeeded']])],
      'bifrost-deliver-platform-prod': [stageRun('prod-2', sha(5), 'False', '2026-11-16T19:50:00Z', '2026-11-16T19:51:00Z', [['check', 'succeeded'], ['promote', 'failed']]), stageRun('prod-1', sha(1), 'True', '2026-11-16T18:00:00Z', '2026-11-16T18:01:24Z')],
    },
  })
  const verdict = await screen.findByTestId('ci-verdict')
  await screen.findByText(/median 84 s/)
  expect(verdict).toHaveAttribute('data-level', 'Blocked')
  expect(verdict).toHaveTextContent('waits for PROD delivery › promote; the release window is held by agent-8-comm')
  expect(verdict).toHaveTextContent('One change is in CI, at test-api. Of the last 4 runs 3 are green, 1 red. The release window is held by agent-8-comm (bifrost-platform).')
  expect(screen.getByTestId('ci-verdict-reasons')).toHaveTextContent('PROD delivery failed last in promote')
  const ci = screen.getByTestId('ci-stage-ci')
  expect(ci).toHaveTextContent('median 175 s · 24 h')
  expect(ci).toHaveTextContent('00000009is here')
  expect(ci).toHaveTextContent('Sides skipped: 0 of 4 runs left a module out')
  expect(within(ci).getByText('setup')).toHaveAttribute('data-state', 'done')
  expect(within(ci).getByText('console')).toHaveAttribute('data-state', 'idle')
  expect(screen.getByTestId('ci-stage-land')).toHaveTextContent('median 14 s · 24 h')
  expect(screen.getByTestId('ci-stage-land')).toHaveTextContent('waits for CI')
  const stg = screen.getByTestId('ci-stage-stg')
  expect(stg).toHaveTextContent('median 225 s · 24 h')
  expect(stg).toHaveTextContent('last delivered 00000002')
  expect(stg).toHaveTextContent('Starts: median 11 s after main moves (1 delivery whose merge is loaded)')
  const prod = screen.getByTestId('ci-stage-prod')
  expect(prod).toHaveTextContent('last delivered 00000001')
  expect(prod).toHaveTextContent('Policy: rp-20261115-1745 is valid for 20 more days')
  // Frozen readings of the sketch with no live source are not on the first screen.
  expect(screen.getByRole('region', { name: 'Pipeline' })).not.toHaveTextContent(/Reconcile|Queue|no card waits/)
  view('By domain')
  fireEvent.click(within(ci).getByRole('button', { name: 'api' }))
  expect(within(screen.getByRole('group', { name: 'Arrange by' })).getByRole('button', { name: 'By time' })).toHaveAttribute('aria-pressed', 'true')
  expect(screen.getByTestId('ci-selection')).toHaveTextContent('run 00000009')
  expect(screen.getByRole('region', { name: 'Selected run' })).toHaveTextContent('running')
})

// Origin: page — ONE selection shared by the three views and shown at the right with a clear control: a domain picked
// By domain is the selected node By dependency (its arrows lit, the wrong-way one red and thicker) and dims the other
// runs By time; a run picked By time stays picked through the views; clear clears both.
it('shares one selection (domain, run) across By time, By domain and By dependency', async () => {
  setup({ repos: ['bifrost-platform', 'plain'], runs: history(), graph: graph(), changes })
  await screen.findByTestId('ci-verdict')
  view('By domain')
  fireEvent.click(await screen.findByTestId('ci-domain-plan'))
  expect(screen.getByTestId('ci-selection')).toHaveTextContent('Domain planclear')
  expect(screen.getByTestId('ci-domain-plan')).toHaveTextContent('2 runs')

  view('By dependency')
  expect(screen.getByTestId('ci-dep-node-plan')).toHaveAttribute('aria-pressed', 'true')
  expect(screen.getByTestId('ci-dep-node-owner')).toHaveAttribute('aria-pressed', 'false')
  const panel = screen.getByTestId('ci-dep-panel')
  expect(panel).toHaveTextContent('Depends onfoundation (2), lineage (1)')
  expect(panel).toHaveTextContent('Depended on bynobody')
  expect(panel).toHaveTextContent('A change here pulls inits own 5 packages, and the shared packages that import it')
  expect(panel).toHaveTextContent('Last 24 h2 runs, 1 red, median 175 s')
  expect(screen.getByTestId('ci-dep-edge-plan>lineage')).toHaveAttribute('data-hot', 'true')
  const wrong = screen.getByTestId('ci-dep-edge-foundation>owner')
  expect(wrong).toHaveAttribute('data-wrong', 'true')
  expect(wrong).toHaveTextContent('1 wrong way')
  expect(Number(wrong.getAttribute('data-width'))).toBeGreaterThan(Number(screen.getByTestId('ci-dep-edge-owner>foundation').getAttribute('data-width')))
  fireEvent.click(screen.getByTestId('ci-dep-node-owner'))
  expect(screen.getByTestId('ci-selection')).toHaveTextContent('Domain owner')
  expect(screen.getByTestId('ci-dep-panel')).toHaveTextContent('Depended on byfoundation (1, wrong way)')

  view('By time')
  expect(screen.getByTestId('ci-bar-ci-2')).toHaveAttribute('data-dim', 'no')
  expect(screen.getByTestId('ci-bar-ci-4')).toHaveAttribute('data-dim', 'yes')
  fireEvent.click(screen.getByRole('button', { name: /Run of 00000002/ }))
  expect(screen.getByTestId('ci-selection')).toHaveTextContent('Domain owner · run 00000002clear')
  expect(screen.getByRole('region', { name: 'Selected run' })).toHaveTextContent('Changedowner (3 files)')
  view('By domain')
  expect(screen.getByTestId('ci-domain-owner')).toHaveAttribute('aria-pressed', 'true')
  expect(screen.getByRole('region', { name: 'Selected run' })).toBeInTheDocument()
  // The folded run list follows the same selection: only the runs that changed the domain.
  open('ci-fold-runs')
  expect(await screen.findByTestId('ci-run-ci-2')).toBeInTheDocument()
  expect(screen.queryByTestId('ci-run-ci-4')).toBeNull()
  fireEvent.click(within(screen.getByTestId('ci-selection')).getByRole('button', { name: 'clear' }))
  expect(screen.getByTestId('ci-selection')).toHaveTextContent('All domains')
  expect(screen.queryByRole('region', { name: 'Selected run' })).toBeNull()
  expect(screen.getByTestId('ci-run-ci-4')).toBeInTheDocument()
})

// Origin: page — with nothing selected every arrow of the graph is plainly visible (2026-11-16: in the first sketch
// the Owner could not see the arrows until he clicked a node).
it('draws every dependency arrow at full strength when no domain is selected', async () => {
  setup({ repos: ['bifrost-platform', 'plain'], runs: history(), graph: graph(), changes })
  await screen.findByTestId('ci-verdict')
  view('By dependency')
  const edges = await screen.findAllByTestId(/^ci-dep-edge-/)
  expect(edges).toHaveLength(5)
  for (const edge of edges) expect(edge).toHaveAttribute('data-opacity', '1')
  expect(screen.getByTestId('ci-dep-panel')).toHaveTextContent('No domain selected')
  fireEvent.click(screen.getByTestId('ci-dep-node-plan'))
  expect(screen.getByTestId('ci-dep-edge-owner>foundation')).toHaveAttribute('data-opacity', '0.3')
  expect(screen.getByTestId('ci-dep-edge-foundation>owner')).toHaveAttribute('data-opacity', '0.75')
})

// Origin: page — domain views exist only for a repository with an ownership table; for another the two views say
// once that its domains are not measured, and By time still draws its runs.
it('says once that domains are not measured for a repository without an ownership table', async () => {
  setup()
  await screen.findByTestId('ci-timeline')
  view('By domain')
  expect(await screen.findByText('Domains are not measured for this repository: this repository has no config/domain-ownership.json: its domains are not measured.')).toBeInTheDocument()
  expect(screen.queryByTestId(/^ci-domain-/)).toBeNull()
  view('By dependency')
  expect(screen.getAllByText(/Domains are not measured for this repository/)).toHaveLength(1)
  expect(fetchCiChanges).not.toHaveBeenCalled()
})

// Origin: page — ONE folded block says everything the page could not measure: nothing of it on the first screen, and
// when opened the computed gaps, the facts with no source, the reads that failed and what the API reports unmeasured.
it('lists everything not measured in one folded block', async () => {
  const scorecard: CiScorecard = { generated_at: AS_OF, window_days: 3, standards: [], repos: [{ repo: 'bifrost-trade-infra', pipeline: 'p', tier: 'full', overall: 'unmeasured', items: [{ id: 'follows_change', label: 'Time follows the change', verdict: 'unmeasured', value: 'its pipeline reports no scope', samples: 0 }] }], provider: { overall: 'pass', items: [] } }
  setup({ repos: ['bifrost-platform', 'plain'], runs: history(), graph: graph(), changes, window: 'fail', scorecard })
  await screen.findByTestId('ci-verdict')
  await screen.findByText(/Not measured — the one place that says so \(1[0-9]\)/)
  expect(screen.queryByRole('list', { name: 'Not measured' })).toBeNull()
  open('ci-not-measured')
  const lines = within(screen.getByRole('list', { name: 'Not measured' })).getAllByRole('listitem').map(li => li.textContent)
  expect(lines).toContain('What changed in 2 of 5 runs: their commits are not in the repository (they never reached a kept branch) or could not be read, so they appear By time but under no domain')
  expect(lines.some(l => l?.startsWith('Whether 4 of 4 finished runs were selective'))).toBe(true)
  expect(lines.some(l => l?.startsWith('Reconcile:'))).toBe(true)
  expect(lines.some(l => l?.startsWith('Queue:'))).toBe(true)
  expect(lines).toContain('The release window: could not be read (release window: HTTP 401)')
  expect(lines).toContain('The STG deliveries: could not be read (CI timings: HTTP 502)')
  expect(lines).toContain('Score "Time follows the change" of bifrost-trade-infra: its pipeline reports no scope')
  // A window that was not read is not called free.
  expect(screen.getByTestId('ci-verdict')).not.toHaveTextContent(/window/)
})

// Origin: page — the Owner's first screen had a "not recorded" or "unknown" paragraph in block after block. With every
// optional read failing or empty, none of that is at the top level: a stage with no reading shows its name only.
it('renders no placeholder text at the top level when data is missing', async () => {
  setup({ repos: ['bifrost-platform', 'plain'], runs: 'none', graph: 'fail', window: 'fail' })
  await screen.findByTestId('ci-verdict')
  await screen.findByText('No run that tested itself is retained for this repository.')
  expect(document.body.textContent).not.toMatch(/Not recorded|not recorded|Unknown|unknown|not declared yet|Time unavailable/)
  expect(screen.getByTestId('ci-stage-stg')).toHaveTextContent(/^›STG$/)
  expect(screen.getByTestId('ci-stage-prod')).toHaveTextContent(/^›PROD$/)
  expect(screen.getByTestId('ci-verdict')).toHaveTextContent('HealthyNothing is in CI.')
  expect(fetchFullCi).not.toHaveBeenCalled()
  expect(fetchApprovalReadings).not.toHaveBeenCalled()
})

// Origin: page — the how-to-read strip is one quiet line until opened, and its second question ends at the run panel.
it('keeps the how-to-read strip to one line, with the three questions inside', async () => {
  setup()
  const guide = screen.getByTestId('ci-guide')
  expect(guide).not.toHaveAttribute('open')
  expect(within(guide).getAllByRole('listitem')).toHaveLength(3)
  expect(guide).toHaveTextContent('to see exactly which packages a run tested, click its bar in By time → the run panel')
})
