import { act, cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react'
import { QueryClient, QueryClientProvider, onlineManager } from '@tanstack/react-query'
import { afterEach, beforeEach, expect, it, vi } from 'vitest'
import wire from '@/api/__fixtures__/landing-grant.go.json'
import { LandingGrantContent } from '../LandingGrantPage'
import { PlatformAuthProvider } from '@/hooks/PlatformAuthProvider'
import { ADMIN_CAPS, approvalsPageHooks, json } from '@/pages/__tests__/approvalsPageTestKit'
import { PLATFORM_TOKEN_KEY } from '@/lib/platformAuth'

approvalsPageHooks()
const clients: QueryClient[] = []
beforeEach(() => {
  onlineManager.setOnline(true)
  vi.setSystemTime(new Date('2026-10-29T13:00:00Z'))
  window.localStorage.setItem(PLATFORM_TOKEN_KEY, 'fixture-only')
})
afterEach(() => { cleanup(); clients.splice(0).forEach(c => c.clear()); onlineManager.setOnline(true) })

function setup() {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  clients.push(client)
  client.setQueryData(['platform', 'auth', 'capabilities', 'fixture-only'], ADMIN_CAPS)
  const pending: (() => void)[] = []
  const reads = { hold: false, status: 0, material: 0 }
  vi.stubGlobal('fetch', vi.fn(async (input: unknown) => {
    const url = String(input)
    if (url.includes('/auth/capabilities')) return json(ADMIN_CAPS)
    if (url.includes('/material?') || url.includes('/operations/')) {
      if (url.includes('/material?')) reads.material++; else reads.status++
      if (reads.hold) await new Promise<void>(resolve => { pending.push(resolve) })
      return json(url.includes('/material?') ? wire.core : wire.wait)
    }
    return json({}, 404)
  }))
  const view = () => <QueryClientProvider client={client}><PlatformAuthProvider><LandingGrantContent op={wire.core.material.candidate.op} /></PlatformAuthProvider></QueryClientProvider>
  const release = async () => { await act(async () => { pending.splice(0).forEach(resolve => resolve()) }) }
  return { client, reads, view, release }
}
async function network(online: boolean) {
  await act(async () => { onlineManager.setOnline(online) })
}
function unconfirmed() {
  expect(screen.queryAllByText('Authorized, waiting for push')).toHaveLength(0)
  expect(screen.queryByRole('button', { name: 'Revoke grant #51' })).toBeNull()
  expect(screen.queryByRole('region', { name: 'Patch' })).toBeNull()
}

// Review 701's remount counterexample, plus a whole offline/online cycle
// without any mounted consumer. Same-millisecond events must also work.
it.each(['unmount offline', 'unmount before drop'] as const)('A-02: %s retains the reconnect boundary across the cache lifetime', async mode => {
  const { view, reads, release } = setup()
  const first = render(view())
  await screen.findByRole('button', { name: 'Revoke grant #51' })
  reads.hold = true
  if (mode === 'unmount before drop') first.unmount()
  await network(false)
  if (mode === 'unmount offline') { unconfirmed(); first.unmount() }
  await network(true)
  const before = reads.status
  render(view())
  await waitFor(() => expect(reads.status).toBeGreaterThan(before))
  unconfirmed()
  await release()
  await screen.findByRole('button', { name: 'Revoke grant #51' })
  expect(screen.queryAllByText('Authorized, waiting for push').length).toBeGreaterThan(0)
})

it.each(['offline', 'after reconnect'] as const)('A-02: an old in-flight response completed %s cannot confirm the new generation', async completion => {
  const { client, view, reads, release } = setup()
  render(view())
  await screen.findByRole('button', { name: 'Revoke grant #51' })
  reads.hold = true
  fireEvent.click(screen.getByRole('button', { name: 'Refresh material and status' }))
  await waitFor(() => expect(reads.status).toBe(2))
  vi.setSystemTime(new Date('2026-10-29T13:01:00Z'))
  await network(false)
  unconfirmed()
  if (completion === 'after reconnect') await network(true)
  vi.setSystemTime(new Date('2026-10-29T13:02:00Z'))
  await release()
  await waitFor(() => expect(client.isFetching()).toBe(0))
  if (completion === 'offline') {
    vi.setSystemTime(new Date('2026-10-29T13:03:00Z'))
    await network(true)
  }
  unconfirmed()
  fireEvent.click(screen.getByRole('button', { name: 'Refresh material and status' }))
  await waitFor(() => expect(reads.status).toBeGreaterThan(2))
  unconfirmed()
  await release()
  await screen.findByRole('button', { name: 'Revoke grant #51' })
})

it('A-02: a second disconnect invalidates the first reconnect read, even when the payload is identical', async () => {
  const { view, reads, release } = setup()
  render(view())
  await screen.findByRole('button', { name: 'Revoke grant #51' })
  reads.hold = true
  await network(false)
  await network(true)
  await waitFor(() => expect(reads.status).toBe(2))
  await network(false)
  await network(true)
  await release()
  unconfirmed()
  fireEvent.click(screen.getByRole('button', { name: 'Refresh material and status' }))
  await waitFor(() => expect(reads.status).toBe(3))
  await release()
  await screen.findByRole('button', { name: 'Revoke grant #51' })
})
