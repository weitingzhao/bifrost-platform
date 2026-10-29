import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { cleanup, renderHook, waitFor } from '@testing-library/react'
import type { ReactNode } from 'react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import wire from '@/api/__fixtures__/landing-grant.go.json'
import { buildPlatformApproval } from '@/api/approvalsApiFixture'
import { installStorage, json } from '@/pages/__tests__/approvalsPageTestKit'
import { useLandingDecisionWaits } from '../useLandingDecisionWaits'

beforeEach(installStorage)
afterEach(() => { cleanup(); vi.unstubAllGlobals() })
function wrapper({ children }: { children: ReactNode }) {
  return <QueryClientProvider client={new QueryClient({ defaultOptions: { queries: { retry: false } } })}>{children}</QueryClientProvider>
}
const item = () => buildPlatformApproval({ id: 'approval_console', number: 51, action: 'approve_core_landing', params: { op: wire.core.material.candidate.op } })
describe('C-6 Owner waiting eligibility', () => {
  it.each(['ready', 'stale', 'authorized', 'unknown main', 'unreadable'] as const)('classifies %s from operation evidence', async state => {
    vi.stubGlobal('fetch', vi.fn(async () => json({ ...wire.wait, approval_status: 'pending',
      main_known: state !== 'unknown main', main_changed: state === 'stale', owner_todo: state === 'ready',
      shown: state === 'ready' ? 'awaiting_owner' : state === 'stale' ? 'request_stale' : 'authorized',
    }, state === 'unreadable' ? 503 : 200)))
    const { result } = renderHook(() => useLandingDecisionWaits([item()]), { wrapper })
    await waitFor(() => expect(result.current.loading).toBe(false))
    expect(result.current.eligible.has('approval_console')).toBe(state === 'ready')
    expect(result.current.unreadable).toBe(['unknown main', 'unreadable'].includes(state))
  })
  it('does not turn another request or generation into this request’s Owner task', async () => {
    vi.stubGlobal('fetch', vi.fn(async () => json({ ...wire.wait, owner_todo: true, approval_id: 'another-request', approval_number: 52 })))
    const { result } = renderHook(() => useLandingDecisionWaits([item()]), { wrapper })
    await waitFor(() => expect(result.current.loading).toBe(false))
    expect(result.current.eligible.size).toBe(0)
  })
  it('does not read an operation for other actions or drills', () => {
    const fetch = vi.fn()
    vi.stubGlobal('fetch', fetch)
    const { result } = renderHook(() => useLandingDecisionWaits([buildPlatformApproval({ id: 'ordinary' }), { ...item(), drill: true }]), { wrapper })
    expect(fetch).not.toHaveBeenCalled()
    expect(result.current.unreadable).toBe(false)
    expect(result.current.loading).toBe(false)
  })
})
