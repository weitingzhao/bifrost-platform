import { afterEach, describe, expect, it, vi } from 'vitest'
import { cleanup, render, screen, waitFor, act } from '@testing-library/react'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { readQuality } from '@bifrost/ui/city/observation'
import { domainStatusObservationOptions, engineDomainsOn, engineDomainStatusOptions, useDomainStatusObservation } from './domainStatus'

const wire = vi.hoisted(() => ({ read: vi.fn(), env: 'prod' as string | undefined }))
vi.mock('@/api/core', () => ({ fetchSelfHealth: async () => ({ viewer_env: wire.env }) }))
vi.mock('@/lib/platformAuth', () => ({ getPlatformOperatorToken: () => 'test-viewer' }))
vi.mock('@/api/serverContact', () => ({ serverFetch: (...args: unknown[]) => wire.read(...args) }))
afterEach(() => { cleanup(); wire.read.mockReset(); wire.env = 'prod' })
const raw = { version: 1, at: '2026-11-23T03:00:00Z', commit: 'source-commit', note: 'partial scan', domains: [{id:'foundation',overall:'unknown',alive:{value:'good',why:'source'}}], inventory: [{repo:'platform',measured:false}], references: null }
function Consumer({id}: {id:string}) {
  const q = useDomainStatusObservation()
  return <output data-testid={id}>{JSON.stringify({data:q.data, evidence:q.evidence, quality:q.evidence ? readQuality([q.evidence], '2026-11-23T03:02:00Z') : 'pending'})}</output>
}
// (ISSUE-74) shared raw catalog response, domain-only reads and distinguish old data from failure.
it('shares the raw response, preserves backend fields, and exposes stale/error/recovered evidence', async () => {
  wire.read.mockResolvedValue({ok:true,json:async()=>raw})
  const client = new QueryClient()
  // Self-health answers first (PROD: no engine), so the one direct read below is the only one.
  await client.fetchQuery({ queryKey: ['shell', 'self-health'], queryFn: async () => ({ viewer_env: 'prod' }) })
  const view = render(<QueryClientProvider client={client}><Consumer id="console"/><Consumer id="signals"/></QueryClientProvider>)
  await waitFor(() => expect(screen.getByTestId('console').textContent).toContain('source-commit'))
  expect(wire.read).toHaveBeenCalledTimes(1)
  expect(wire.read.mock.calls[0][0]).toBe('/api/v1/catalog/status')
  expect(wire.read.mock.calls[0][1].signal).toBeInstanceOf(AbortSignal)
  expect(new Headers(wire.read.mock.calls[0][1].headers).get('Authorization')).toBe('Bearer test-viewer')
  expect(screen.getByTestId('console').textContent).toBe(screen.getByTestId('signals').textContent)
  expect(JSON.parse(screen.getByTestId('console').textContent!).data).toEqual(raw)
  expect(screen.getByTestId('console').textContent).toContain('Stale')
  wire.read.mockRejectedValue(new Error('offline'))
  await act(async () => { await client.invalidateQueries({queryKey:['status','domain-status']}) })
  await waitFor(() => expect(screen.getByTestId('console').textContent).toContain('Read failed'))
  expect(JSON.parse(screen.getByTestId('console').textContent!).data).toEqual(raw)
  wire.read.mockResolvedValue({ok:true,json:async()=>({...raw,at:undefined})})
  await act(async () => { await client.invalidateQueries({queryKey:['status','domain-status']}) })
  await waitFor(() => expect(screen.getByTestId('console').textContent).toContain('Unknown measurement time'))
  view.unmount(); client.clear()
})
it('rejects malformed answers and does not publish cancelled domain reads', async () => {
  const client = new QueryClient()
  wire.read.mockResolvedValue({ok:true,json:async()=>({version:1})})
  await expect(client.fetchQuery(domainStatusObservationOptions())).rejects.toThrow('incomplete answer')
  let finish!: (value: unknown) => void
  wire.read.mockImplementation(() => new Promise(resolve => { finish=resolve }))
  const pending = client.fetchQuery(domainStatusObservationOptions()).catch(e=>e)
  await client.cancelQueries({queryKey:['status','domain-status']})
  expect(wire.read.mock.calls[1][1].signal.aborted).toBe(true)
  finish({ok:true,json:async()=>raw})
  await pending
  expect(client.getQueryData(['status','domain-status'])).toBeUndefined()
  client.clear()
})

// 2026-11-24 UI / Engine: switch 1b+1d is one row. On STG the shared reader takes the four lights from
// /engine/v1/domains with the engine's evidence; the engine unreachable is said so, never a quiet direct read.
describe('through the engine (STG)', () => {
  const envelope = (epoch = 'e1') => ({ version: 1, model: 'domains', capturedAt: '2026-11-24T07:00:00Z', provenance: 'live', identity: { epoch, role: 'viewer' },
    evidence: [{ source: '/api/v1/catalog/status', outcome: 'read', receivedAt: '2026-11-24T07:00:00Z', evaluatedAt: raw.at }], data: raw })
  const stg = async (client: QueryClient) => client.fetchQuery({ queryKey: ['shell', 'self-health'], queryFn: async () => ({ viewer_env: 'stg' }) })

  it('switches only where the table says so', () => {
    expect(engineDomainsOn('stg')).toBe(true)
    expect(engineDomainsOn('STG')).toBe(true)
    expect(engineDomainsOn('prod')).toBe(false)
    expect(engineDomainsOn(undefined)).toBe(false)
  })

  it('reads the engine as the viewer, hands the platform answer on unchanged, and its evidence', async () => {
    wire.read.mockResolvedValue(new Response(JSON.stringify(envelope()), { status: 200 }))
    const client = new QueryClient()
    await stg(client)
    render(<QueryClientProvider client={client}><Consumer id="console"/></QueryClientProvider>)
    await waitFor(() => expect(screen.getByTestId('console').textContent).toContain('source-commit'))
    expect(wire.read.mock.calls.map(c => c[0])).toEqual(['/engine/v1/domains'])
    expect(new Headers(wire.read.mock.calls[0][1].headers).get('Authorization')).toBe('Bearer test-viewer')
    const out = JSON.parse(screen.getByTestId('console').textContent!)
    expect(out.data).toEqual(raw)
    expect(out.evidence).toMatchObject({ source: '/api/v1/catalog/status', outcome: 'read', receivedAt: '2026-11-24T07:00:00Z', evaluatedAt: raw.at })
    client.clear()
  })

  it('says engine unreachable and does not read the platform instead', async () => {
    wire.read.mockRejectedValue(new TypeError('Failed to fetch'))
    const client = new QueryClient()
    await stg(client)
    render(<QueryClientProvider client={client}><Consumer id="console"/></QueryClientProvider>)
    await waitFor(() => expect(screen.getByTestId('console').textContent).toContain('engine unreachable: Failed to fetch'))
    expect(wire.read.mock.calls.map(c => c[0])).toEqual(['/engine/v1/domains'])
    expect(JSON.parse(screen.getByTestId('console').textContent!).evidence).toMatchObject({ source: '/engine/v1/domains', outcome: 'error' })
    client.clear()
  })

  it('says why when the engine read the platform and failed', async () => {
    wire.read.mockResolvedValue(new Response(JSON.stringify({ ...envelope(), evidence: [{ source: '/api/v1/catalog/status', outcome: 'error', receivedAt: '2026-11-24T07:00:00Z', detail: 'HTTP 401' }], data: null }), { status: 200 }))
    const client = new QueryClient()
    await expect(client.fetchQuery(engineDomainStatusOptions())).rejects.toThrow('domain status (engine): HTTP 401')
    client.clear()
  })
})
