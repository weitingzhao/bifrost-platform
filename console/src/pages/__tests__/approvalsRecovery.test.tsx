import { QueryClient, QueryClientProvider, onlineManager } from '@tanstack/react-query'
import { act, cleanup, fireEvent, render, screen } from '@testing-library/react'
import { afterEach, beforeEach, expect, it, vi } from 'vitest'
import { buildPlatformApproval } from '@/api/approvalsApiFixture'
import type { AuthCapabilities } from '@/api/matrixTypes'
import { PlatformAuthProvider } from '@/hooks/PlatformAuthProvider'
import { PLATFORM_TOKEN_KEY } from '@/lib/platformAuth'
import { ApprovalsPage } from '@/pages/ApprovalsPage'

const ADMIN_CAPS: AuthCapabilities = {
  authenticated: true,
  role: 'admin',
  can_operate: true,
  can_admin: true,
}

vi.mock('@/api/cluster', async () => {
  const actual = await vi.importActual<typeof import('@/api/cluster')>('@/api/cluster')
  return {
    ...actual,
    fetchAuthCapabilities: vi.fn(async () => ({
      authenticated: true,
      role: 'admin',
      can_operate: true,
      can_admin: true,
    })),
  }
})
import { APPROVAL_WAITING_NOTICE } from '@/pages/approvalWaitingNotice'

const checking = /Could not read this request\. Last attempt: .+Refresh this page/
const retry = 'The request is still pending. That attempt did not take effect. Try again.'
const json = (body: unknown, status = 200) => new Response(JSON.stringify(body), { status })
const pending = () => buildPlatformApproval({ id: 'ap-recovery' })
let client: QueryClient
let reads: number
let post: () => Promise<Response>
let read: () => Promise<Response>

beforeEach(() => {
  const values = new Map<string, string>()
  Object.defineProperty(window, 'localStorage', { configurable: true, value: {
    getItem: (key: string) => values.get(key) ?? null,
    setItem: (key: string, value: string) => { values.set(key, value) },
    removeItem: (key: string) => { values.delete(key) },
    clear: () => values.clear(),
  } })
  window.localStorage.setItem(PLATFORM_TOKEN_KEY, 'test-only')
  window.location.hash = '#approvals?id=ap-recovery'
  client = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  client.setQueryData(['platform', 'auth', 'capabilities', 'test-only'], ADMIN_CAPS)
  reads = 0
  post = async () => json({ status: 'executed' })
  read = async () => json(pending())
  vi.stubGlobal('fetch', vi.fn((input: RequestInfo | URL, init?: RequestInit) => {
    const url = String(input)
    if (init?.method === 'POST') return post()
    if (url.endsWith('/ap-recovery')) { reads++; return read() }
    return Promise.resolve(json({ approvals: [] }))
  }))
})
afterEach(() => {
  cleanup()
  client.clear()
  onlineManager.setOnline(true)
  vi.useRealTimers()
  vi.unstubAllGlobals()
  window.localStorage.clear()
})
async function open() {
  render(
    <QueryClientProvider client={client}>
      <PlatformAuthProvider>
        <ApprovalsPage />
      </PlatformAuthProvider>
    </QueryClientProvider>,
  )
  return screen.findByRole('button', { name: 'Approve gitops_sync_app' })
}

it('retains successful POST status when subsequent queries pause offline', async () => {
  const button = await open()
  post = async () => { onlineManager.setOnline(false); return json({ status: 'executed' }) }
  fireEvent.click(button)
  await screen.findByText('executed')
  expect(screen.queryByText('Waiting for your decision.')).toBeNull()
  expect(screen.queryByRole('button', { name: /Approve/ })).toBeNull()
})
it('retains successful POST status after refresh returns 503', async () => {
  const button = await open()
  read = async () => json({ error: 'unavailable' }, 503)
  fireEvent.click(button)
  await screen.findByText('executed')
  expect(screen.queryByRole('button', { name: /Approve/ })).toBeNull()
})
it('keeps a failed POST locked after unreadable confirmation and retries the record', async () => {
  const button = await open()
  post = async () => { throw new TypeError('offline') }
  read = async () => json({ error: 'unavailable' }, 503)
  vi.useFakeTimers({ toFake: ['setTimeout', 'clearTimeout'] })
  fireEvent.click(button)
  await act(async () => { await vi.advanceTimersByTimeAsync(1) })
  expect(screen.getByText(checking)).toBeTruthy()
  expect(screen.getByRole('button', { name: /Approve/ }).hasAttribute('disabled')).toBe(true)
  read = async () => json({ ...pending(), status: 'approved' })
  await act(async () => { await vi.advanceTimersByTimeAsync(30_000) })
  expect(screen.getByText('Approved. Running now.')).toBeTruthy()
})
it('cannot replace a confirmed decision with an earlier GET arriving late', async () => {
  const button = await open()
  let finish!: (r: Response) => void
  read = () => new Promise(resolve => { finish = resolve })
  void client.refetchQueries({ queryKey: ['approvals', 'item', 'ap-recovery'] })
  post = async () => { throw new TypeError('offline') }
  read = async () => json({ ...pending(), status: 'approved' })
  fireEvent.click(button)
  await screen.findByText('Approved. Running now.')
  await act(async () => { finish(json(pending())) })
  // Query notifications are scheduled; wait for the stale response's render too.
  await act(async () => { await new Promise(resolve => setTimeout(resolve, 10)) })
  expect(screen.getByText('Approved. Running now.')).toBeTruthy()
  expect(screen.queryByText('Waiting for your decision.')).toBeNull()
  expect(screen.queryByRole('button', { name: /Approve/ })).toBeNull()
})
it.each([409, 500])('immediately reconciles a %s response including a status field', async status => {
  const button = await open()
  post = async () => json({ error: 'already decided', status: 'approved' }, status)
  read = async () => json({ ...pending(), status: 'approved' })
  fireEvent.click(button)
  await screen.findByText('Approved. Running now.')
  expect(reads).toBe(2)
})
it('shows still waiting at 30 seconds while POST is still hanging', async () => {
  const button = await open()
  vi.mocked(fetch).mockImplementation((input, init) => {
    if (init?.method === 'POST') return new Promise((_resolve, reject) => {
      init.signal?.addEventListener('abort', () => reject(new DOMException('', 'AbortError')))
    })
    if (String(input).endsWith('/ap-recovery')) return new Promise(() => undefined)
    return Promise.resolve(json({ approvals: [] }))
  })
  vi.useFakeTimers({ toFake: ['setTimeout', 'clearTimeout'] })
  fireEvent.click(button)
  await act(async () => { await vi.advanceTimersByTimeAsync(30_000) })
  expect(screen.getByText(APPROVAL_WAITING_NOTICE)).toBeTruthy()
  expect(screen.getByRole('button', { name: 'Sending…' }).hasAttribute('disabled')).toBe(true)
})
it('restores the button only after confirmation reads pending', async () => {
  const button = await open()
  post = async () => { throw new TypeError('offline') }
  fireEvent.click(button)
  await screen.findByText(retry)
  expect(screen.getByRole('button', { name: /Approve/ }).hasAttribute('disabled')).toBe(false)
})
it('synchronously guards two clicks before React commits disabled state', async () => {
  const button = await open()
  post = () => new Promise(() => undefined)
  act(() => { button.click(); button.click() })
  expect(vi.mocked(fetch).mock.calls.filter(([, init]) => init?.method === 'POST')).toHaveLength(1)
})
