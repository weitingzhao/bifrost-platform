import { QueryClient, QueryClientProvider, onlineManager } from '@tanstack/react-query'
import { renderHook, waitFor } from '@testing-library/react'
import type { ReactNode } from 'react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import progressFixture from '@/api/__fixtures__/agentrt-progress.stg.json'
import { parseRuntimeProgress } from '@/api/agentRuntime'
import alertsFixture from './fixtures/telemetry-alerts.prod.json'
import evidenceFixture from './fixtures/standby-evidence.prod.json'

/** The real hook, with the network replaced: what each source's failure does to the verdict and the badges. */
const api = vi.hoisted(() => ({
  alerts: vi.fn(), nodes: vi.fn(), promql: vi.fn(), targets: vi.fn(), pipelines: vi.fn(), runs: vi.fn(), window: vi.fn(), records: vi.fn(), goal: vi.fn(),
  probe: { probeReach: 'ok' as const, readFailed: false, isLoading: false },
}))
vi.mock('@/api/telemetry', () => ({ fetchTelemetryAlerts: api.alerts, fetchTelemetryTargets: api.targets, fetchPromQL: api.promql }))
vi.mock('@/api/cluster', () => ({ fetchClusterNodes: api.nodes, fetchClusterPostgresBackupStatus: vi.fn(async () => ({ fresh: true, max_age_hours: 26 })),
  fetchDataFreshness: vi.fn(async () => ({ databases: [], fresh_threshold_days: 3, stale_threshold_days: 7 })) }))
vi.mock('@/api/delivery', () => ({ fetchDeliveryPipelines: api.pipelines, fetchPipelineRuns: api.runs, fetchReleaseWindow: api.window }))
vi.mock('@/api/releases', () => ({ fetchReleaseRecords: api.records }))
vi.mock('@/api/client', async importOriginal => ({ ...(await importOriginal<object>()), operatorToken: () => 'token' }))
vi.mock('@/api/agentRuntimeGoals', () => ({ fetchRuntimeGoal: api.goal }))
vi.mock('@/api/researchEngine', async importOriginal => ({ ...(await importOriginal<object>()), fetchOrchestrationStatus: vi.fn(async () => ({ ok: true, data: { verdict: 'healthy', job_name: 'x', overdue: false, detail: '' } })) }))
vi.mock('@/api/marketDataDimensions', () => ({ fetchCoverageDimensions: vi.fn(async () => ({ session: '2026-10-16' })) }))
vi.mock('@/api/marketDataPlugin', async importOriginal => ({ ...(await importOriginal<object>()), fetchMarketStatus: vi.fn(async () => ({ ok: true, freshness_summary: [] })) }))
vi.mock('@/hooks/useIbGatewayLiveProbe', () => ({ useIbGatewayLiveProbe: () => api.probe }))
vi.mock('@/hooks/useMarketDataLiveProbe', () => ({ useMarketDataLiveProbe: () => ({ probeReach: 'ok', readFailed: false, isLoading: false }) }))
vi.mock('@/hooks/useFlexQueryLiveProbe', () => ({ useFlexQueryLiveProbe: () => ({ probeReach: 'ok', readFailed: false, isLoading: false }) }))
const runtime = parseRuntimeProgress(progressFixture)
vi.mock('@/pages/shell/needs-you/useNeedsYou', () => ({ useNeedsYou: () => ({
  count: { state: 'known', count: 0 }, openCount: { state: 'known', count: 0 }, decisionItems: [], silentItems: [], decide: [], openQuestions: [], runtimeItems: [],
  recentCount: { state: 'known', count: 0 }, runtimeQuery: { data: runtime, isError: false, isPaused: false }, progressQuery: { data: undefined }, query: { dataUpdatedAt: 0 },
}) }))

const { useOwnerConsole } = await import('../useOwnerConsole')

function run(blocks = { system: false, work: false, chief: false }) {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  const wrapper = ({ children }: { children: ReactNode }) => <QueryClientProvider client={client}>{children}</QueryClientProvider>
  return { client, ...renderHook(() => useOwnerConsole(blocks), { wrapper }) }
}
const ready = (name: string, ip: string, mode?: 'standby') => ({ name, status: mode ? 'NotReady' : 'Ready', internal_ip: ip, elastic_mode: mode, roles: '', version: '', reachability: 'ok' })

beforeEach(() => {
  for (const f of [api.alerts, api.nodes, api.promql, api.targets, api.pipelines, api.runs, api.window, api.records, api.goal]) f.mockReset()
  api.probe = { probeReach: 'ok', readFailed: false, isLoading: false }
  api.alerts.mockResolvedValue({ alerts: [] })
  api.nodes.mockResolvedValue({ nodes: [ready('ubt-k3s-02', '192.168.10.70')] })
  api.window.mockResolvedValue({ open: false })
  api.pipelines.mockResolvedValue({ pipelines: [{ name: 'bifrost-deliver-platform' }] })
  api.runs.mockResolvedValue({ runs: [] })
  api.records.mockResolvedValue({ records: [] })
  api.goal.mockResolvedValue({ mission: { goal: 'Add a comment' }, tasks: [], events: [] })
})

afterEach(() => onlineManager.setOnline(true))

describe('useOwnerConsole', () => {
  it('nothing waiting and every source read: Nothing needs you', async () => {
    const { result } = run()
    await waitFor(() => expect(result.current.verdict.headline).toBe('Nothing needs you'))
    expect(result.current.badges.you).toBeNull()
  })

  it('alerts that cannot be read make Yours unknown, never nothing (review 221 A1)', async () => {
    api.alerts.mockRejectedValue(new Error('HTTP 503'))
    const { result } = run()
    await waitFor(() => expect(result.current.verdict.tone).toBe('unknown'))
    expect(result.current.yours).toEqual({ state: 'unknown', reason: 'Alerts could not be read: HTTP 503' })
    expect(result.current.badges.you).toEqual({ text: '?', tone: 'unknown' })
    expect(result.current.badges.system).toEqual({ text: '?', tone: 'unknown' })
  })

  it('alerts read once and then failing are unknown too, not the last answer', async () => {
    const { result, client } = run()
    await waitFor(() => expect(result.current.verdict.headline).toBe('Nothing needs you'))
    api.alerts.mockRejectedValue(new Error('HTTP 502'))
    await client.refetchQueries({ queryKey: ['telemetry', 'alerts'] })
    await waitFor(() => expect(result.current.verdict.tone).toBe('unknown'))
  })

  it('counts the TWS login as his once the alerts are read', async () => {
    api.alerts.mockResolvedValue({ alerts: alertsFixture.alerts })
    const { result } = run()
    await waitFor(() => expect(result.current.verdict.headline).toBe('1 thing is yours'))
    expect(result.current.badges.you).toEqual({ text: '1', tone: 'attention' })
  })

  it('a run list that cannot be read is reported, so the lane never says nothing is delivering (review 221 A6)', async () => {
    api.runs.mockRejectedValue(new Error('HTTP 500'))
    const { result } = run({ system: true, work: false, chief: false })
    await waitFor(() => expect(result.current.runsError).toBe('HTTP 500'))
    expect(result.current.lane.state).toBe('free')
  })

  it('a run list paused offline is unread, even with a good answer before it (review 222 A-R3)', async () => {
    const { result, client } = run({ system: true, work: false, chief: false })
    await waitFor(() => expect(result.current.runsRead).toBe(true))
    onlineManager.setOnline(false)
    void client.refetchQueries({ queryKey: ['delivery', 'runs', 'bifrost-deliver-platform'] })
    await waitFor(() => expect(result.current.runsRead).toBe(false))
    expect(result.current.runsError).toBe('offline')
  })

  it('opened offline, the run lists are unread, not empty', async () => {
    onlineManager.setOnline(false)
    const { result } = run({ system: true, work: false, chief: false })
    await waitFor(() => expect(result.current.runsError).toBe('offline'))
    expect(result.current.runsRead).toBe(false)
    expect(result.current.inFlight).toEqual([])
  })

  it('reads release runs only while System is shown', async () => {
    const { result } = run()
    await waitFor(() => expect(result.current.verdict.headline).toBe('Nothing needs you'))
    expect(api.pipelines).not.toHaveBeenCalled()
    expect(api.records).not.toHaveBeenCalled()
  })

  it('a plugin whose latest read failed is Unknown, not its last OK (review 221 A7)', async () => {
    api.probe = { probeReach: 'ok', readFailed: true, isLoading: false }
    const { result } = run()
    await waitFor(() => expect(result.current.tiles[1]).toMatchObject({ name: 'Plugins', state: 'Unknown' }))
  })

  it('the chief of staff\'s block reads the last finished goal on its own (review 221 B3)', async () => {
    const { result } = run({ system: false, work: false, chief: true })
    await waitFor(() => expect(result.current.lastGoal).toBe('Add a comment'))
  })

  it('with a node on standby, the cluster\'s evidence moves its alerts to noise; without it they stay', async () => {
    api.alerts.mockResolvedValue({ alerts: alertsFixture.alerts })
    api.nodes.mockResolvedValue({ nodes: [ready('ubt-k3s-02', '192.168.10.70'), ready('gpu-server', '192.168.10.60', 'standby')] })
    api.promql.mockImplementation(async (q: string) => ({ query: q, points: q.startsWith('kube_pod_info') ? evidenceFixture.pods : evidenceFixture.gaps }))
    api.targets.mockResolvedValue({ active_targets: evidenceFixture.targets })
    const read = run()
    await waitFor(() => expect(read.result.current.alerts?.noise).toBe(13))
    expect(read.result.current.standbyEvidence).toBe('read')
    expect(api.promql).toHaveBeenCalledWith('kube_pod_info{node=~"gpu-server"}')

    api.targets.mockRejectedValue(new Error('HTTP 504'))
    const unread = run()
    await waitFor(() => expect(unread.result.current.standbyEvidence).toBe('unread'))
    await waitFor(() => expect(unread.result.current.alerts?.noise).toBe(2))
  })
})
