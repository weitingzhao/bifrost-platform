import { act, cleanup, fireEvent, render, renderHook, screen, waitFor } from '@testing-library/react'
import { QueryClient, QueryClientProvider, onlineManager } from '@tanstack/react-query'
import type { ReactNode } from 'react'
import { afterEach, beforeEach, expect, it, vi } from 'vitest'
import wire from '@/api/__fixtures__/landing-grant.go.json'
import { buildPlatformApproval } from '@/api/approvalsApiFixture'
import { LandingGrantContent } from '@/pages/landing-grant/LandingGrantPage'
import { LandingAuthorization } from '@/pages/shell/progress/LandingAuthorization'
import { approvalsPageHooks, json, wrapper } from '@/pages/__tests__/approvalsPageTestKit'
import { PLATFORM_TOKEN_KEY } from '@/lib/platformAuth'
import { useLandingDecisionWaits } from '../useLandingDecisionWaits'

// Review 698 R-03 and R-04 (C-6): a grant of an operation no longer waiting
// never reads "waiting for push"; offline, a read that succeeded before is
// no current fact — its conclusions and the revoke button go at once, and
// come back only with a read that succeeded after the network returned.

approvalsPageHooks()
beforeEach(() => { vi.setSystemTime(new Date('2026-10-29T13:00:00Z')); window.localStorage.setItem(PLATFORM_TOKEN_KEY, 'fixture-only') })
afterEach(() => { cleanup(); onlineManager.setOnline(true) })
const op = wire.core.material.candidate.op
function serve(status: unknown = structuredClone(wire.wait)) {
  const calls = { status: 0 }
  vi.stubGlobal('fetch', vi.fn(async (input: unknown) => {
    const url = String(input)
    if (url.includes('/auth/capabilities')) return json({ authenticated: true, role: 'admin', can_admin: true, can_operate: true })
    if (url.includes('/material?')) return json(wire.core)
    if (url.includes('/operations/')) { calls.status++; return json(status) }
    return json({}, 404)
  }))
  return calls
}
async function goOffline() {
  await act(async () => { onlineManager.setOnline(false); window.dispatchEvent(new Event('offline')) })
}

it('R-04: offline after a successful read hides the authorization and the revoke button without a Refresh', async () => {
  serve()
  render(wrapper(<LandingGrantContent op={op} />))
  const button = await screen.findByRole('button', { name: 'Revoke grant #51' })
  await waitFor(() => expect(button.hasAttribute('disabled')).toBe(false))
  expect(screen.queryAllByText('Authorized, waiting for push').length).toBeGreaterThan(0)
  await goOffline()
  expect(screen.queryAllByText('Authorized, waiting for push')).toHaveLength(0)
  expect(screen.queryByRole('button', { name: 'Revoke grant #51' })).toBeNull()
  expect(screen.getByText('Landing status unavailable. Refresh to read the landing stream.')).toBeTruthy()
})

it('R-04: back online, only a read after the reconnect restores the conclusion', async () => {
  let release: (() => void) | undefined
  let hold = false
  vi.stubGlobal('fetch', vi.fn(async (input: unknown) => {
    const url = String(input)
    if (url.includes('/auth/capabilities')) return json({ authenticated: true, role: 'admin', can_admin: true, can_operate: true })
    if (url.includes('/material?')) return json(wire.core)
    if (url.includes('/operations/')) {
      if (hold) await new Promise<void>(r => { release = r })
      return json(structuredClone(wire.wait))
    }
    return json({}, 404)
  }))
  render(wrapper(<LandingGrantContent op={op} />))
  await screen.findByRole('button', { name: 'Revoke grant #51' })
  hold = true
  await goOffline()
  vi.setSystemTime(new Date('2026-10-29T13:01:00Z'))
  await act(async () => { onlineManager.setOnline(true); window.dispatchEvent(new Event('online')) })
  // Online again, the read from before the drop is not a current fact
  // while its re-read has not come back.
  expect(screen.queryByRole('button', { name: 'Revoke grant #51' })).toBeNull()
  expect(screen.queryAllByText('Authorized, waiting for push')).toHaveLength(0)
  fireEvent.click(screen.getByRole('button', { name: 'Refresh material and status' }))
  await waitFor(() => expect(release).toBeDefined())
  vi.setSystemTime(new Date('2026-10-29T13:02:00Z'))
  await act(async () => { release?.() })
  await screen.findByRole('button', { name: 'Revoke grant #51' })
})

it('R-04: the progress card and the Owner-task hook do the same', async () => {
  serve()
  render(wrapper(<LandingAuthorization op={op} />))
  await screen.findAllByText('Authorized, waiting for push')
  await goOffline()
  expect(screen.queryAllByText('Authorized, waiting for push')).toHaveLength(0)
  cleanup()
  onlineManager.setOnline(true)
  serve({ ...structuredClone(wire.wait), approval_status: 'pending', owner_todo: true, shown: 'awaiting_owner', approval_id: 'approval_console', approval_number: 51 })
  const item = buildPlatformApproval({ id: 'approval_console', number: 51, action: 'approve_core_landing', params: { op } })
  const client = ({ children }: { children: ReactNode }) => <QueryClientProvider client={new QueryClient({ defaultOptions: { queries: { retry: false } } })}>{children}</QueryClientProvider>
  const { result } = renderHook(() => useLandingDecisionWaits([item]), { wrapper: client })
  await waitFor(() => expect(result.current.eligible.has('approval_console')).toBe(true))
  await goOffline()
  await waitFor(() => expect(result.current.eligible.size).toBe(0))
  expect(result.current.unreadable).toBe(true)
})

it('R-03: a discarded operation never says its old grant waits for push', async () => {
  serve({ ...structuredClone(wire.wait), phase: 'discarded', shown: 'discarded', main_changed: true })
  render(wrapper(<LandingGrantContent op={op} />))
  await screen.findByText('Landing discarded')
  expect(screen.queryAllByText('Authorized, waiting for push')).toHaveLength(0)
  expect(screen.getByText(/Not usable: operation discarded/)).toBeTruthy()
})

it('R-03: the server says why (ended) and the page follows it', async () => {
  const wait = structuredClone(wire.wait)
  wait.grants = wait.grants.map(g => ({ ...g, valid: false, ended: 'operation_superseded' }))
  serve(wait)
  render(wrapper(<LandingGrantContent op={op} />))
  await screen.findByText(/Not usable: operation superseded/)
  expect(screen.queryAllByText('Authorized, waiting for push')).toHaveLength(0)
})
