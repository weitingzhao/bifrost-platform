import { afterEach, expect, it, vi } from 'vitest'
import { cleanup, render, screen, waitFor } from '@testing-library/react'
import { QueryClient, QueryClientProvider, useQuery } from '@tanstack/react-query'
import { readQuality, objectsIn, pathKey } from '@bifrost/ui/city/observation'
import { hardwareObservationOptions } from './index'
import { captureReads, snapshotOf } from '../runtime-observation/read'
import { HARDWARE_QUERIES } from '@/domains/owner/city/gen2/content/hardware'

const transport = vi.hoisted(() => ({ failed: false, calls: [] as string[], gate: undefined as Promise<void> | undefined }))
vi.mock('@/api/client', () => ({ authedFetch: async (_prefix: string, path: string, init: RequestInit) => {
  expect(init.method ?? 'GET').toBe('GET')
  transport.calls.push(path)
  await transport.gate
  if (transport.failed) throw new Error('HTTP 503')
  return { json: async () => path.includes('/topology') ? { generated_at: new Date().toISOString(), nodes: [{id:'node-a',host:'10.0.0.1'}] }
    : path.includes('/alerts') ? {generated_at:new Date().toISOString(),alerts:[]}
    : {points:[{labels:{instance:'10.0.0.1:9100',mountpoint:'/'},value:decodeURIComponent(path).includes('up{') ? 1 : 0,timestamp:Date.now()/1000}]} }
} }))
afterEach(() => { cleanup(); transport.calls = []; transport.failed = false; transport.gate = undefined })
function Consumer({ name, legacy = false }: {name: string; legacy?: boolean}) {
  // Existing Signals options, with its unchanged adapter and cache key.
  const options = legacy ? { queryKey:['runtime-observation','hardware'], queryFn: async () => {
    const { authedFetch } = await import('@/api/client')
    return snapshotOf(await captureReads(async path => (await authedFetch('Runtime observation',path,{})).json()),'live')
  } } : hardwareObservationOptions()
  const read = useQuery(options)
  return <output data-testid={name}>{read.data ? JSON.stringify(read.data) : 'loading'}</output>
}
// 2026-11-22 shared-access request: independent views must share capture, not duplicate reads or change timestamps.
it('shares one capture with the deployed Signals query and reuses it when another view mounts', async () => {
  const client = new QueryClient({defaultOptions:{queries:{retry:false}}})
  const view = render(<QueryClientProvider client={client}><Consumer name="console"/><Consumer name="signals" legacy/></QueryClientProvider>)
  await waitFor(() => expect(screen.getByTestId('console').textContent).not.toBe('loading'))
  expect(screen.getByTestId('console').textContent).toBe(screen.getByTestId('signals').textContent)
  expect(transport.calls.length).toBe(Object.keys(HARDWARE_QUERIES).length + 2)
  expect(new Set(transport.calls).size).toBe(transport.calls.length)
  const captured = screen.getByTestId('console').textContent
  view.rerender(<QueryClientProvider client={client}><Consumer name="console"/><Consumer name="extra"/></QueryClientProvider>)
  expect(screen.getByTestId('extra').textContent).toBe(captured)
  expect(transport.calls.length).toBe(Object.keys(HARDWARE_QUERIES).length + 2)
  expect(transport.calls.every(p => /\/topology|\/telemetry\//.test(p))).toBe(true)
  view.unmount(); client.clear()
})
// Same request: ordinary read errors are evidence, recovery refreshes it; a retained snapshot stays unchanged.
it('preserves failed evidence, recovers via the shared query, and retains source identity and zero', async () => {
  const client = new QueryClient({defaultOptions:{queries:{retry:false}}})
  transport.failed = true
  const failed = await client.fetchQuery(hardwareObservationOptions())
  const pin = JSON.stringify(failed)
  expect(readQuality(failed.evidence[''], failed.capturedAt)).toBe('Read failed')
  transport.failed = false
  await client.invalidateQueries({queryKey:['runtime-observation','hardware']})
  const recovered = await client.fetchQuery(hardwareObservationOptions())
  expect(readQuality(recovered.evidence[''],recovered.capturedAt)).toBe('Within refresh window')
  expect(JSON.stringify(failed)).toBe(pin)
  expect(objectsIn(recovered.content.tree).some(r => pathKey(r.path)==='utilities/node-a')).toBe(true)
  expect(objectsIn(recovered.content.tree).some(r=>r.place.meter?.used===0)).toBe(true)
  expect(readQuality(recovered.evidence[''],new Date(Date.parse(recovered.capturedAt)+61000).toISOString())).toBe('Stale')
  expect(readQuality([{...recovered.evidence[''][0],evaluatedAt:undefined}],recovered.capturedAt)).toBe('Unknown measurement time')
  client.clear()
})
// 2026-11-22 capture audit: adapters catch read failures, so cancellation must not publish a partial snapshot.
it('does not publish a cancelled capture', async () => {
  const client = new QueryClient({defaultOptions:{queries:{retry:false}}})
  let release!: () => void
  transport.gate = new Promise<void>(resolve => { release = resolve })
  const pending = client.fetchQuery(hardwareObservationOptions()).catch(error => error)
  await waitFor(() => expect(transport.calls.length).toBeGreaterThan(0))
  await client.cancelQueries({queryKey:['runtime-observation','hardware']})
  release()
  expect(await pending).toBeInstanceOf(Error)
  expect(client.getQueryData(['runtime-observation','hardware'])).toBeUndefined()
  client.clear()
})
