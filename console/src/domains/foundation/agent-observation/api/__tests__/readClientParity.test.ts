// 2026-11-16 read-client migration: preserve the complete HTTP boundary, not only imported names.
import { beforeEach, describe, expect, it, vi } from 'vitest'
import * as delivery from '@/domains/delivery/api/delivery'
import * as releases from '@/domains/delivery/api/releases'
import * as promote from '@/domains/delivery/api/promote'
import * as releasePolicy from '@/domains/delivery/api/releasePolicy'
import * as agentOps from '@/domains/foundation/agent-observation/api/agentOps'
import * as checklist from '@/domains/foundation/checklist/api/checklist'
import * as marketDataDocs from '@/domains/tenant/market-observation/api/marketDataDocs'
import * as marketDataCapabilities from '@/domains/tenant/market-observation/api/marketDataCapabilities'
import * as marketDataDimensions from '@/domains/tenant/market-observation/api/marketDataDimensions'
import * as marketDataQueueHistory from '@/domains/tenant/market-observation/api/marketDataQueueHistory'
const transport = vi.hoisted(() => ({ request: vi.fn() }))
vi.mock('@/api/serverContact', () => ({ serverFetch: transport.request }))
vi.mock('@/lib/platformAuth', () => ({ getPlatformOperatorToken: () => '' }))
const calls = [
{ name: "window", call: () => delivery.fetchReleaseWindow(), url: "/api/v1/delivery/release-window", prefix: "release window", auth: true },
{ name: "pipelines", call: () => delivery.fetchDeliveryPipelines(), url: "/api/v1/delivery/pipelines", prefix: "delivery pipelines", auth: false },
{ name: "runs encoded", call: () => delivery.fetchPipelineRuns("a/b c"), url: "/api/v1/delivery/pipelines/a%2Fb%20c/runs", prefix: "pipeline runs", auth: false },
{ name: "supply", call: () => delivery.fetchSupplyChain(), url: "/api/v1/delivery/supply-chain", prefix: "supply chain", auth: false },
{ name: "compare encoded", call: () => delivery.fetchCompare("repo x", "ready/a", "main+1"), url: "/api/v1/delivery/compare?repo=repo+x&from=ready%2Fa&to=main%2B1", prefix: "compare", auth: false },
{ name: "logs encoded", call: () => delivery.fetchPipelineRunLogs("run/a", "ns x"), url: "/api/v1/delivery/runs/run%2Fa/logs?ns=ns%20x", prefix: "pipeline logs", auth: false },
{ name: "logs default", call: () => delivery.fetchPipelineRunLogs("run"), url: "/api/v1/delivery/runs/run/logs", prefix: "pipeline logs", auth: false },
{ name: "logs empty ns", call: () => delivery.fetchPipelineRunLogs("run", ""), url: "/api/v1/delivery/runs/run/logs", prefix: "pipeline logs", auth: false },
{ name: "images", call: () => releases.fetchRunningImages(), url: "/api/v1/releases/running-images", prefix: "running images", auth: false },
{ name: "records default", call: () => releases.fetchReleaseRecords(), url: "/api/v1/releases?limit=100", prefix: "releases", auth: false },
{ name: "records zero", call: () => releases.fetchReleaseRecords(0), url: "/api/v1/releases?limit=0", prefix: "releases", auth: false },
{ name: "smoke", call: () => promote.fetchStgSmoke(), url: "/api/v1/delivery/stg/smoke", prefix: "stg smoke", auth: false },
{ name: "policy", call: () => releasePolicy.fetchReleasePolicy(), url: "/api/v1/release-policy", prefix: "release-policy", auth: true },
{ name: "deploy observation", call: () => agentOps.fetchAgentDeployStatus(), url: "/api/v1/agent/deploy", prefix: "agent deploy status", auth: false },
{ name: "bridge", call: () => agentOps.fetchAgentBridge(), url: "/api/v1/agent/bridge", prefix: "agent bridge", auth: false },
{ name: "checklist", call: () => checklist.fetchChecklistSignals(), url: "/api/v1/checklist/signals", prefix: "checklist signals", auth: false },
{ name: "docs", call: () => marketDataDocs.fetchMarketDataDoc("blueprint"), url: "/api/v1/plugins/market-data/api/market/docs/blueprint", prefix: "market docs blueprint", auth: false },
{ name: "capabilities", call: () => marketDataCapabilities.fetchMarketDataCapabilities(), url: "/api/v1/plugins/market-data/api/market/capabilities", prefix: "market capabilities", auth: false },
{ name: "dimensions", call: () => marketDataDimensions.fetchCoverageDimensions(), url: "/api/v1/plugins/market-data/api/market/coverage/dimensions", prefix: "coverage dimensions", auth: false },
{ name: "queue optional", call: () => marketDataQueueHistory.fetchQueueHistory(12), url: "/api/v1/plugins/market-data/api/market/ingest/queue-history?hours=12", prefix: "queue history", auth: false },
{ name: "queue encoded", call: () => marketDataQueueHistory.fetchQueueHistory(0, "bars / daily"), url: "/api/v1/plugins/market-data/api/market/ingest/queue-history?hours=0&kind=bars+%2F+daily", prefix: "queue history", auth: false },
]
beforeEach(() => { transport.request.mockReset() })
describe('read-client transport preservation', () => {
  it.each(calls)('$name preserves GET URL, omitted signal and response handling', async ({ call, url, auth, name }) => {
    const body = { ok: true, data: { marker: 'nested' }, marker: 'whole', cells: [{ lane: 'platform' }], records: [{ run: 'r' }], status: { namespace: 'cicd' } }
    transport.request.mockResolvedValue(new Response(JSON.stringify(body)))
    const result = await call()
    expect(transport.request).toHaveBeenCalledTimes(1)
    const [input, init] = transport.request.mock.calls[0]
    expect(input).toBe(url)
    expect(init?.method ?? 'GET').toBe('GET')
    expect(init?.body).toBeUndefined()
    expect(init?.signal).toBeUndefined()
    if (auth) expect(new Headers(init?.headers).get('Content-Type')).toBe('application/json')
    else expect(init).toBeUndefined()
    if (name === 'images') expect(result).toEqual({ cells: body.cells, error: undefined })
    else if (name.startsWith('records')) expect(result).toEqual({ status: body.status, records: body.records, error: undefined })
    else if (['docs', 'dimensions'].includes(name) || name.startsWith('queue')) expect(result).toEqual(body.data)
    else expect(result).toEqual(body)
  })
  it.each(calls)('$name keeps HTTP failures visible', async ({ call, prefix }) => {
    transport.request.mockResolvedValue(new Response('{}', { status: 503 }))
    await expect(call()).rejects.toThrow(`${prefix}: HTTP 503`)
  })
  it.each(calls)('$name propagates request aborts', async ({ call }) => {
    transport.request.mockRejectedValue(new DOMException('request aborted', 'AbortError'))
    const caught = await call().then(() => null, error => [error.name, error.message])
    expect(caught).toEqual(['AbortError', 'request aborted'])
  })
  it('keeps release-list missing values as the existing empty collections', async () => {
    transport.request.mockImplementation(() => Promise.resolve(new Response('{}')))
    expect(await releases.fetchRunningImages()).toEqual({ cells: [], error: undefined })
    expect(await releases.fetchReleaseRecords()).toEqual({ status: {}, records: [], error: undefined })
  })
  it('retains plugin not-ok errors rather than returning an empty dataset', async () => {
    transport.request.mockImplementation(() => Promise.resolve(new Response('{"ok":false}')))
    await expect(marketDataDocs.fetchMarketDataDoc('calibration')).rejects.toThrow('market docs calibration: not ok')
    await expect(marketDataDimensions.fetchCoverageDimensions()).rejects.toThrow('coverage dimensions: not ok')
    await expect(marketDataQueueHistory.fetchQueueHistory(1)).rejects.toThrow('queue history: not ok')
  })
})
