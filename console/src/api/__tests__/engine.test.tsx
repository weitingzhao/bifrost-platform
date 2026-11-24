// 2026-11-24 箭10 / UI / Engine: step 0 of the view switch is one shared engine read in the Console shell. It reads
// /engine/v1/… as the viewer, opens the envelope, hands the evidence on as it came, drops everything when the
// identity epoch changes, and says "engine unreachable" with the reason instead of quietly reading the platform.
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { renderHook } from '@testing-library/react'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import type { ReactNode } from 'react'

const fetchMock = vi.fn()
vi.mock('@/api/serverContact', () => ({ serverFetch: (...a: unknown[]) => fetchMock(...a) }))
vi.mock('@/api/client', () => ({ authHeaders: () => new Headers({ Authorization: 'Bearer viewer-token' }) }))

import { engineEpoch, engineQueryKey, engineUnreachableText, onEngineEpochChange, readEngine, resetEngineIdentity } from '@/api/engine'
import { useEngineEpochReset } from '@/lib/shell/engineEpoch'

const env = (epoch: string, data: unknown = { ok: 1 }) => ({
  version: 1, model: 'domains', capturedAt: '2026-11-24T06:40:00Z', provenance: 'live', identity: { epoch, role: 'viewer' },
  evidence: [{ source: '/api/v1/catalog/status', outcome: 'read', receivedAt: '2026-11-24T06:40:00Z' }], data,
})
const json = (body: unknown, status = 200) => new Response(JSON.stringify(body), { status, headers: { 'content-type': 'application/json' } })

beforeEach(() => { fetchMock.mockReset(); resetEngineIdentity() })
afterEach(() => vi.restoreAllMocks())

describe('readEngine', () => {
  it('reads as the viewer and hands the envelope on as it came', async () => {
    fetchMock.mockResolvedValueOnce(json(env('e1')))
    const a = await readEngine<{ ok: number }>('/engine/v1/domains')
    const [url, init] = fetchMock.mock.calls[0] as [string, RequestInit]
    expect(url).toBe('/engine/v1/domains')
    expect(new Headers(init.headers).get('Authorization')).toBe('Bearer viewer-token')
    expect(a).toMatchObject({ reached: true, model: 'domains', identity: { epoch: 'e1', role: 'viewer' }, data: { ok: 1 } })
    if (a.reached) expect(a.evidence[0]).toEqual({ source: '/api/v1/catalog/status', outcome: 'read', receivedAt: '2026-11-24T06:40:00Z' })
  })

  it('passes a listed read its query', async () => {
    fetchMock.mockResolvedValueOnce(json(env('e1')))
    await readEngine('/engine/v1/delivery', { query: { read: 'pipelines' } })
    expect(fetchMock.mock.calls[0][0]).toBe('/engine/v1/delivery?read=pipelines')
  })

  it('says engine unreachable with the reason, never an empty answer', async () => {
    fetchMock.mockRejectedValueOnce(new TypeError('Load failed'))
    const a = await readEngine('/engine/v1/domains')
    expect(a).toEqual({ reached: false, reason: 'Load failed', status: null, lastReachedAt: null })
    if (!a.reached) expect(engineUnreachableText(a)).toBe('engine unreachable: Load failed (no answer yet)')
    fetchMock.mockResolvedValueOnce(json({ error: 'upstream down' }, 502))
    expect(await readEngine('/engine/v1/domains')).toMatchObject({ reached: false, status: 502, reason: 'HTTP 502: upstream down' })
    // An HTML page where the engine should be (no /engine route on this host) is not an engine answer.
    fetchMock.mockResolvedValueOnce(new Response('<!doctype html>', { status: 200 }))
    expect(await readEngine('/engine/v1/domains')).toMatchObject({ reached: false, reason: expect.stringContaining('not JSON') })
    fetchMock.mockResolvedValueOnce(json({ hello: 1 }))
    expect(await readEngine('/engine/v1/domains')).toMatchObject({ reached: false, reason: 'the answer is not an engine v1 envelope' })
  })

  it('says when it last reached the engine', async () => {
    fetchMock.mockResolvedValueOnce(json(env('e1')))
    await readEngine('/engine/v1/health')
    fetchMock.mockRejectedValueOnce(new TypeError('Failed to fetch'))
    const a = await readEngine('/engine/v1/health')
    expect(a.reached).toBe(false)
    if (!a.reached) expect(a.lastReachedAt).toMatch(/^\d{4}-/)
  })

  it('reads only the engine: any other path is refused', async () => {
    await expect(readEngine('/api/v1/catalog/status')).rejects.toThrow('not an engine read')
    expect(fetchMock).not.toHaveBeenCalled()
  })
})

describe('the identity epoch', () => {
  it('the first epoch is not a change; another epoch is, once', async () => {
    const seen: string[] = []
    const off = onEngineEpochChange((e, prev) => seen.push(`${prev}->${e}`))
    fetchMock.mockResolvedValueOnce(json(env('e1'))).mockResolvedValueOnce(json(env('e1'))).mockResolvedValueOnce(json(env('e2')))
    await readEngine('/engine/v1/domains')
    await readEngine('/engine/v1/domains')
    expect(seen).toEqual([])
    await readEngine('/engine/v1/domains')
    expect(seen).toEqual(['e1->e2'])
    expect(engineEpoch()).toBe('e2')
    off()
  })

  it('the shell drops every engine read when the epoch changes, and keeps the rest', async () => {
    const client = new QueryClient()
    client.setQueryData(engineQueryKey('/engine/v1/domains'), { old: true })
    client.setQueryData(engineQueryKey('/engine/v1/delivery', { read: 'pipelines' }), { old: true })
    client.setQueryData(['approvals'], { kept: true })
    const wrapper = ({ children }: { children: ReactNode }) => <QueryClientProvider client={client}>{children}</QueryClientProvider>
    renderHook(() => useEngineEpochReset(), { wrapper })
    fetchMock.mockResolvedValueOnce(json(env('e1'))).mockResolvedValueOnce(json(env('e2')))
    await readEngine('/engine/v1/domains')
    expect(client.getQueryData(engineQueryKey('/engine/v1/domains'))).toEqual({ old: true })
    await readEngine('/engine/v1/domains')
    expect(client.getQueryData(engineQueryKey('/engine/v1/domains'))).toBeUndefined()
    expect(client.getQueryData(engineQueryKey('/engine/v1/delivery', { read: 'pipelines' }))).toBeUndefined()
    expect(client.getQueryData(['approvals'])).toEqual({ kept: true })
  })
})
