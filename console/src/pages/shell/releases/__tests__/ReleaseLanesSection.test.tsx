import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { render, screen, waitFor, within } from '@testing-library/react'
import { afterEach, describe, expect, it, vi } from 'vitest'
import type { ApprovalItem } from '@/api/approvals'
import { PLATFORM_TOKEN_KEY } from '@/lib/platformAuth'
import { ReleaseLanesSection, type ReleaseLanesProps } from '@/pages/shell/releases/ReleaseLanesSection'
import { ReleasesPage } from '@/pages/shell/releases/ReleasesPage'
import { approvalHref, releaseWindowView } from '@/pages/shell/releases/releaseView'

const NOW = Date.parse('2026-11-01T06:00:00Z')
const P = 'abcd1234567890abcd1234567890abcd12345678'
const U = '1234567890abcdef1234567890abcdef12345678'
// Shapes of the Go handlers: releases.Record, delivery.PipelineRunView, releases.ImageCell, agentrtapi getRule (rules.Rule.Doc).
const record = (run: string, pipeline: string, lane: string, env: string, deploys: boolean, at: string) => ({
  run, pipeline, lane, env, deploys, started_at: at, completed_at: at, recorded_at: at,
  repos: { 'bifrost-platform': { sha: P, source: 'result' }, 'bifrost-ui': { sha: U, source: 'result' } },
})
const run = (name: string, pipeline: string, reason: string, start: string, end?: string, revision = P) => ({
  name, namespace: 'cicd', pipeline, revision, status: reason === 'Running' ? 'Unknown' : reason === 'Succeeded' ? 'True' : 'False', reason, start_time: start, completion_time: end,
})
const approval = { id: 'ap-prod-1', action: 'start_pipeline_run', tier: 'C', status: 'pending', params: { name: 'bifrost-deliver-platform-prod', revision: P } } as unknown as ApprovalItem
const RULE = { rule_id: 'landing:dev', signed_by: 'owner', in_force: { rule_id: 'landing:dev', rule_version: 7, tenant: 'dev',
  repos: [{ repo: 'bifrost-platform', allow: [], deny: [], stg_release: 'bifrost-deliver-platform' }, { repo: 'bifrost-trade-infra', allow: [], deny: [], stg_release: 'none' }],
  limits: { max_files: 20, max_changed_lines: 300, max_landings_per_day: 12, max_in_flight_per_repo: 1 } } }

function props(over: Partial<ReleaseLanesProps> = {}): ReleaseLanesProps {
  return {
    now: NOW, hasToken: true, approvals: [approval],
    window: releaseWindowView({ hasToken: true, isLoading: false, error: null, data: { open: false }, now: NOW }),
    records: { data: [record('stgrel-12-1-abcd1234', 'bifrost-deliver-platform', 'platform', 'stg', true, '2026-11-01T03:00:00Z'),
      record('bifrost-deliver-platform-prod-9', 'bifrost-deliver-platform-prod', 'platform', 'prod', true, '2026-10-31T18:00:00Z')], error: null },
    pipelines: { data: ['bifrost-deliver-stg', 'bifrost-deliver-prod', 'bifrost-build-flex-query'], error: null },
    runs: { 'bifrost-deliver-stg': { data: [run('deliver-stg-live', 'bifrost-deliver-stg', 'Running', '2026-11-01T05:50:00Z')], error: null },
      'bifrost-deliver-prod': { data: [], error: null }, 'bifrost-build-flex-query': { data: [run('flex-1', 'bifrost-build-flex-query', 'Succeeded', '2026-10-30T00:00:00Z')], error: null } },
    images: { data: [{ lane: 'research', env: 'stg', absent: true, text: 'No STG' }, { lane: 'research', env: 'prod', text: 'research-api 0.205.0' },
      { lane: 'market-data', env: 'stg', absent: true, text: 'No STG' }, { lane: 'market-data', env: 'prod', text: 'market-data-api 0.61.0 · flex-query-api 0.9.1 · ib-gateway unread' }], error: null },
    rule: { data: RULE, error: null },
    ...over,
  }
}
const lane = (id: string) => document.querySelector(`[data-lane="${id}"]`) as HTMLElement
const [STG, PRD] = ['STG · last successful delivery', 'PROD · last successful delivery']
/** Renders the section, returns one line's text, and unmounts. */
function show(id: string, over: Partial<ReleaseLanesProps>): string {
  const { unmount } = render(<ReleaseLanesSection {...props({ approvals: [], ...over })} />)
  const text = lane(id).textContent ?? ''
  unmount()
  return text
}

describe('ReleaseLanesSection', () => {
  afterEach(() => vi.unstubAllGlobals())

  it('shows each line: landing, STG and PROD commits with time, runs in flight, and a link to a waiting approval', () => {
    render(<ReleaseLanesSection {...props()} />)
    const platform = within(lane('platform'))
    expect(platform.getAllByText('platform abcd123 · ui 1234567')).toHaveLength(2)
    expect(lane('platform').textContent).toContain('2026-11-01 03:00Z · 3h ago')
    expect(platform.getByRole('link', { name: `${PRD}: approval waiting ap-prod-1` }).getAttribute('href')).toBe(approvalHref('ap-prod-1'))
    expect(lane('trade').textContent).toContain('Delivering abcd123 · started 10m ago')
    expect(lane('trade').textContent).toContain('trade-core · trade-worker · trade-api · trade-frontend · ui · trade-infra')
    expect(lane('research').textContent).toContain('No STG')
    expect(lane('flex-query').textContent).toContain('flex-query-api 0.9.1image in the Deployment spec; rollout state not read')
    expect(lane('flex-query').textContent).toContain('last image build flex-query abcd123 · time unknown')
    expect(lane('ib-gateway').textContent).toContain('ib-gateway unread')
    expect(screen.getByTestId('release-lanes-window').textContent).toContain('Release window free')
    // The daily landing quota is gone (cleanup wave 1): no quota line.
    expect(screen.queryByTestId('release-lanes-quota')).toBeNull()
  })

  it('shows the later of the newest release record and the newest successful run, STG and PROD alike (review: record A, newer run B)', () => {
    const B = 'b0b0b0b0b0b0b0b0b0b0b0b0b0b0b0b0b0b0b0b0'
    const versus = (id: string, pipeline: string, env: string, records: [string, string, string?][], runs: ReturnType<typeof run>[]) => show(id, {
      runs: { [pipeline]: { data: runs, error: null } }, records: { data: records.map(([name, at, revision]) => ({ ...record(name, pipeline, id, env, true, at), revision })), error: null } })
    // Record A, then a newer successful run B the records do not hold yet: B.
    const newerRun = versus('platform', 'bifrost-deliver-platform-prod', 'prod', [['prod-A', '2026-10-31T18:00:00Z']], [run('prod-B', 'bifrost-deliver-platform-prod', 'Succeeded', '2026-11-01T03:40:00Z', '2026-11-01T04:00:00Z', B)])
    expect(newerRun).toContain(`${PRD}platform b0b0b0b2026-11-01 04:00Z · 2h ago`)
    // The record is the newer one: the record, on the trade line's STG.
    const newerRecord = versus('trade', 'bifrost-deliver-stg', 'stg', [['stg-A', '2026-11-01T05:00:00Z']], [run('stg-old', 'bifrost-deliver-stg', 'Succeeded', '2026-10-30T00:00:00Z', '2026-10-30T01:00:00Z', B)])
    expect(newerRecord).toContain(`${STG}platform abcd123 · ui 12345672026-11-01 05:00Z · 1h ago`)
    // One release in both, its run read again later: the record's exact commits.
    const same = versus('platform', 'bifrost-deliver-platform-prod', 'prod', [['prod-9', '2026-11-01T03:00:00Z']], [run('prod-9', 'bifrost-deliver-platform-prod', 'Succeeded', '2026-11-01T02:40:00Z', '2026-11-01T03:00:00Z')])
    expect(same).toContain(`${PRD}platform abcd123 · ui 12345672026-11-01 03:00Z`)
    // A record with Go's zero time cannot be compared: the run, and the record named with time unknown.
    const unknownTime = versus('platform', 'bifrost-deliver-platform-prod', 'prod', [['prod-A', '0001-01-01T00:00:00Z']], [run('prod-B', 'bifrost-deliver-platform-prod', 'Succeeded', '2026-11-01T03:40:00Z', '2026-11-01T04:00:00Z', B)])
    expect(unknownTime).toContain(`${PRD}platform b0b0b0b2026-11-01 04:00Z · 2h agoalso platform abcd123 · ui 1234567 · time unknown; which is newer is unknown`)
    // Review round 2: record A and a newer run B both name `main`. Different runs: B, its commit not recorded; never A's commits.
    const sharedMain = versus('platform', 'bifrost-deliver-platform-prod', 'prod', [['prod-A', '2026-10-31T18:00:00Z', 'main']], [run('prod-B', 'bifrost-deliver-platform-prod', 'Succeeded', '2026-11-01T03:40:00Z', '2026-11-01T04:00:00Z', 'main')])
    expect(sharedMain).toContain(`${PRD}platform main (commit not recorded)2026-11-01 04:00Z · 2h ago`)
    expect(sharedMain).not.toContain('platform abcd123')
    // The record names run B: one release, the record's exact commits.
    const namedRun = versus('platform', 'bifrost-deliver-platform-prod', 'prod', [['prod-B', '2026-11-01T04:00:00Z', 'main']], [run('prod-B', 'bifrost-deliver-platform-prod', 'Succeeded', '2026-11-01T03:40:00Z', '2026-11-01T04:00:00Z', 'main')])
    expect(namedRun).toContain(`${PRD}platform abcd123 · ui 12345672026-11-01 04:00Z · 2h ago`)
    // One release whose record has Go's zero time: the run's time.
    const zeroRecord = versus('platform', 'bifrost-deliver-platform-prod', 'prod', [['prod-9', '0001-01-01T00:00:00Z']], [run('prod-9', 'bifrost-deliver-platform-prod', 'Succeeded', '2026-11-01T03:40:00Z', '2026-11-01T04:00:00Z')])
    expect(zeroRecord).toContain(`${PRD}platform abcd123 · ui 12345672026-11-01 04:00Z · 2h ago`)
  })

  it('1: says it is the last successful delivery, and names a newer run that ended unsuccessfully', () => {
    const { unmount } = render(<ReleaseLanesSection {...props()} />)
    expect(screen.getByText(/the last successful STG and PROD delivery \(what runs now is not confirmed\)/)).toBeTruthy()
    expect(screen.getByRole('list', { name: 'Release lanes: landing and last successful delivery per environment' })).toBeTruthy()
    unmount()
    const text = show('platform', { runs: { 'bifrost-deliver-platform-prod': { error: null, data: [
      run('prod-C', 'bifrost-deliver-platform-prod', 'Failed', '2026-11-01T04:40:00Z', '2026-11-01T05:00:00Z'),
      run('prod-old', 'bifrost-deliver-platform-prod', 'Cancelled', '2026-10-30T00:00:00Z', '2026-10-30T00:10:00Z')] } } })
    expect(text).toContain(`${PRD}platform abcd123 · ui 12345672026-10-31 18:00Z · 12h ago`)
    expect(text).toContain('newer run prod-C Failed at 2026-11-01 05:00Z · 1h ago — it may have rolled out; what runs now is not confirmed')
    expect(text).not.toContain('prod-old Cancelled at')
  })

  it('2: says pinned trade PROD runs are not in the run list, in flight, failed after rollout or not yet recorded', () => {
    expect(show('trade', {})).toContain('pinned PROD runs (release.sh prod, bifrost-deliver-prod-pinned-*) are not in this run list: '
      + 'one in flight, one that rolled out and then failed its checks, or one that succeeded in the last 5 minutes before the recorder wrote it does not show here')
    expect(show('platform', {})).not.toContain('pinned PROD runs')
  })

  it('3: under a recorded release, says when its run list was not read', () => {
    expect(show('platform', { runs: {} }))
      .toContain(`${STG}platform abcd123 · ui 12345672026-11-01 03:00Z · 3h agoruns not read: pipeline not listed; a newer run may not be recorded yet (the recorder runs every 5 minutes)`)
    expect(show('platform', { runs: { 'bifrost-deliver-platform-prod': { error: 'pipeline runs: HTTP 502' } } }))
      .toContain('runs not read: pipeline runs: HTTP 502; a newer run may not be recorded yet')
    // While the pipelines or a run list are read: Reading, and no note.
    expect(show('platform', { runs: {}, pipelines: { error: null } })).not.toContain('runs not read')
    expect(show('trade', { runs: {}, pipelines: { error: null } })).toContain(`${STG}Reading…`)
    expect(show('platform', { runs: { 'bifrost-deliver-platform': { error: null } } })).not.toContain(`${STG}platform abcd123 · ui 12345672026-11-01 03:00Z · 3h agoruns not read`)
    // The pipelines read failed: its error, not "pipeline not listed".
    const failed = { runs: {}, pipelines: { error: 'delivery pipelines: HTTP 502' } }
    expect(show('platform', failed)).toContain('runs not read: pipelines: delivery pipelines: HTTP 502')
    expect(show('trade', failed)).toContain('bifrost-deliver-stg runs not read (pipelines: delivery pipelines: HTTP 502)')
    expect(show('trade', failed)).not.toContain('pipeline not listed')
  })

  it('names the repository a running run and the runs list revision belong to', () => {
    const text = show('platform', { runs: { 'bifrost-deliver-platform': { error: null, data: [run('stgrel-14-1-abcd1234', 'bifrost-deliver-platform', 'Running', '2026-11-01T05:54:00Z')] } } })
    expect(text).toContain('Delivering platform abcd123 · started 6m ago')
    expect(text).toContain('stgrel-14-1-abcd1234 · Running · started 6m ago · platform abcd123')
  })

  it("4: says what the landing rule allows, with each repository's own limit, and when no rule is in force", () => {
    const rule = { ...RULE, in_force: { ...RULE.in_force, repos: [{ repo: 'bifrost-platform', limits: { max_in_flight: 2 } }, { repo: 'bifrost-trade-infra' }] } }
    expect(show('platform', { rule: { data: rule, error: null } }))
      .toContain('LandingUnknownlanding:dev v7 allows platform (at most 2 in flight), trade-infra (at most 1 in flight); live state and landings in flight not read')
    expect(show('research', {})).toContain('LandingNot allowed by landing:dev v7')
    expect(show('platform', { rule: { data: { in_force: null }, error: null } })).toContain('LandingNo landing rule in force')
  })

  it('5: names the commits and image digests a release record is missing', () => {
    const records = { data: [{ ...record('stgrel-12-1-abcd1234', 'bifrost-deliver-platform', 'platform', 'stg', true, '2026-11-01T03:00:00Z'),
      missing: ['clone-ui', 'build-platform-api (digest)'] }], error: null }
    expect(show('platform', { records })).toContain('3h agocommit not recorded: clone-uidigest not recorded: build-platform-api')
  })

  it('says a held window blocks every repository', () => {
    const data = { open: true, window: { who: 'stgrel-bifrost-platform-12-1', what: 'bifrost-platform', env: 'stg', pid: 0, host: 'platform-api',
      started_at: '2026-11-01T05:40:00Z', expires_at: '2026-11-01T06:04:00Z' } }
    render(<ReleaseLanesSection {...props({ window: releaseWindowView({ hasToken: true, isLoading: false, error: null, data, now: NOW }) })} />)
    const text = screen.getByTestId('release-lanes-window').textContent
    expect(text).toContain('Held by stgrel-bifrost-platform-12-1')
    expect(text).toContain('bifrost-platform · stg · 4m left')
    expect(text).toContain('Blocks landings and releases in every repository')
  })

  it('shows Unknown with the reason when the reads fail, and does not crash', () => {
    render(<ReleaseLanesSection {...props({ records: { error: 'releases: HTTP 502' }, images: { error: 'running images: HTTP 500' }, rule: { error: 'landing rule: HTTP 503' },
      runs: { 'bifrost-deliver-stg': { error: 'pipeline runs: HTTP 502' } }, approvals: [],
      window: releaseWindowView({ hasToken: true, isLoading: false, error: 'release window: HTTP 502', data: undefined, now: NOW }) })} />)
    expect(lane('trade').textContent).toContain('release records: releases: HTTP 502; bifrost-deliver-stg runs not read (pipeline runs: HTTP 502)')
    expect(lane('platform').textContent).toContain('bifrost-deliver-platform runs not read (pipeline not listed)')
    expect(lane('research').textContent).toContain('running-images: running images: HTTP 500')
    expect(lane('research').textContent).toContain('landing rule: HTTP 503')
    expect(screen.getByTestId('release-lanes-window').textContent).toContain('Unknown')
  })

  it('without records, runs or a token says none was found and why landing is unknown', () => {
    render(<ReleaseLanesSection {...props({ hasToken: false, approvals: [], records: { data: [], error: null }, images: { data: [], error: null }, rule: { error: null },
      runs: { 'bifrost-deliver-platform': { data: [], error: null }, 'bifrost-deliver-platform-prod': { data: [], error: null } },
      window: releaseWindowView({ hasToken: false, isLoading: false, error: null, data: undefined, now: NOW }) })} />)
    expect(lane('platform').textContent).toContain('None in the newest 40 records or the run list')
    expect(lane('platform').textContent).toContain('This device has no viewer token')
    expect(lane('research').textContent).toContain('running-images has no prod cell for research')
    expect(lane('platform').textContent).toContain('platform runs · 0')
  })

  /** The Releases page against a stubbed platform-api; reads matching `hang` answer only by being aborted. */
  function page(hang: RegExp | null = null) {
    vi.stubGlobal('localStorage', { getItem: (key: string) => (key === PLATFORM_TOKEN_KEY ? 'viewer-token' : null), setItem: () => {}, removeItem: () => {} })
    const state = { open: 0, most: 0, calls: [] as { url: string; signal?: AbortSignal | null }[] }
    vi.stubGlobal('fetch', vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
      const url = String(input)
      state.calls.push({ url, signal: init?.signal })
      state.most = Math.max(state.most, ++state.open)
      try {
        if (hang?.test(url)) await new Promise((_, reject) => init?.signal?.addEventListener('abort', () => reject(new Error('aborted'))))
        await new Promise(resolve => setTimeout(resolve, 5))
      } finally {
        state.open--
      }
      const json = (body: unknown) => new Response(JSON.stringify(body), { headers: { 'Content-Type': 'application/json' } })
      if (url.endsWith('/delivery/pipelines')) return json({ reachability: 'ok', pipelines: ['stg', 'prod', 'platform', 'platform-prod', 'research'].map(n => ({ name: `bifrost-deliver-${n}` })) })
      if (url.endsWith('/rules/landing:dev')) return json(RULE)
      if (url.endsWith('/releases/running-images')) return json({ cells: [{ lane: 'research', env: 'stg', absent: true, text: 'No STG' }, { lane: 'research', env: 'prod', text: 'research-api 0.205.0' }] })
      if (url.endsWith('/gitops/apps')) return json({ apps: [{ name: 'bifrost-prod', namespace: 'argocd', destination_namespace: 'bifrost-prod', revision: P, sync_status: 'Synced', health_status: 'Healthy' }] })
      if (url.endsWith('/api/v1/actions')) return json([{ id: 'gitops_rollback_app', tier: 'B', description: 'rollback', params: [{ name: 'name', type: 'string', required: true }] }])
      return json({ runs: [], records: [], cells: [], approvals: [], open: false, targets: [] })
    }))
    const { unmount } = render(<QueryClientProvider client={new QueryClient({ defaultOptions: { queries: { retry: false } } })}><ReleasesPage /></QueryClientProvider>)
    return Object.assign(state, { unmount })
  }
  const own = (c: { url: string }) => /releases|gitops|delivery|rules\/landing/.test(c.url)

  it('6: on the Releases page, with a PROD application listed, never has more than four reads in flight, and makes no heavy runtime read', async () => {
    const state = page()
    await waitFor(() => expect(screen.getByRole('button', { name: 'Request rollback' })).toBeTruthy())
    await waitFor(() => expect(state.calls.filter(c => c.url.includes('/runs')).length).toBe(5))
    await waitFor(() => expect(state.calls.some(c => c.url.endsWith('/api/v1/actions'))).toBe(true))
    expect(state.most).toBeLessThanOrEqual(4)
    expect(state.calls.some(c => /landing\/missions|plan-view|plan\?root=|landing\/operations/.test(c.url))).toBe(false)
    expect(state.calls.filter(own).every(c => c.signal instanceof AbortSignal)).toBe(true)
    await waitFor(() => expect(lane('research').textContent).toContain('STG · image in Deployment specNo STG'))
  })

  it('6: a read cancelled while it waits for a slot is never sent', async () => {
    const state = page()
    state.unmount()
    await new Promise(resolve => setTimeout(resolve, 50))
    expect(state.calls.filter(own).length).toBe(2)
  })

  it('shows Reading… while running-images is read, not that it has no cell', async () => {
    const state = page(/releases\/running-images/)
    await waitFor(() => expect(state.calls.filter(c => c.url.includes('/runs')).length).toBe(5))
    expect(lane('research').textContent).toContain('STG · image in Deployment specReading…')
    expect(lane('research').textContent).not.toContain('running-images has no')
    state.unmount()
  })

  it('6: gives up a hung read after ten seconds, so it does not keep its slot', async () => {
    vi.useFakeTimers({ shouldAdvanceTime: true, toFake: ['setTimeout', 'clearTimeout'] })
    try {
      const hung = /releases\?limit|gitops\/apps/
      const state = page(hung)
      await waitFor(() => expect(state.calls.filter(c => hung.test(c.url)).length).toBe(2))
      expect(state.calls.some(c => c.url.endsWith('/delivery/pipelines'))).toBe(false)
      await vi.advanceTimersByTimeAsync(10_001)
      await waitFor(() => expect(state.calls.filter(c => c.url.includes('/runs')).length).toBe(5))
      expect(state.calls.filter(c => hung.test(c.url)).every(c => c.signal?.aborted)).toBe(true)
    } finally {
      vi.useRealTimers()
    }
  })
})
