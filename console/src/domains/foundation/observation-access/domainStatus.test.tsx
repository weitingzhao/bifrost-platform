import { afterEach, expect, it, vi } from 'vitest'
import { cleanup, render, screen, waitFor, act } from '@testing-library/react'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { readQuality } from '@bifrost/ui/city/observation'
import { domainStatusObservationOptions, useDomainStatusObservation } from './domainStatus'

const wire = vi.hoisted(() => ({ read: vi.fn() }))
vi.mock('@/lib/platformAuth', () => ({ getPlatformOperatorToken: () => 'test-viewer' }))
vi.mock('@/api/serverContact', () => ({ serverFetch: (...args: unknown[]) => wire.read(...args) }))
afterEach(() => { cleanup(); wire.read.mockReset() })
const raw = { version: 1, at: '2026-11-23T03:00:00Z', commit: 'source-commit', note: 'partial scan', domains: [{id:'foundation',overall:'unknown',alive:{value:'good',why:'source'}}], inventory: [{repo:'platform',measured:false}], references: null }
function Consumer({id}: {id:string}) {
  const q = useDomainStatusObservation()
  return <output data-testid={id}>{JSON.stringify({data:q.data, evidence:q.evidence, quality:q.evidence ? readQuality([q.evidence], '2026-11-23T03:02:00Z') : 'pending'})}</output>
}
// Owner 2026-11-22: shared raw catalog response, domain-only reads and distinguish old data from failure.
it('shares the raw response, preserves backend fields, and exposes stale/error/recovered evidence', async () => {
  wire.read.mockResolvedValue({ok:true,json:async()=>raw})
  const client = new QueryClient()
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
