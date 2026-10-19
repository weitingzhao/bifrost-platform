import { QueryClient, QueryClientProvider, onlineManager } from '@tanstack/react-query'
import { act, renderHook, waitFor } from '@testing-library/react'
import type { ReactNode } from 'react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import runtimeFixture from '@/api/__fixtures__/agentrt-progress.stg.json'
import { buildApprovalListResponse } from '@/api/approvalsApiFixture'
import { useNeedsYou } from '../useNeedsYou'

function json(value: unknown) { return new Response(JSON.stringify(value), { status: 200, headers: { 'Content-Type': 'application/json' } }) }
function mockEmpty() {
  vi.stubGlobal('fetch', vi.fn(async (input: RequestInfo | URL) => {
    const url = String(input)
    if (url.includes('/agentrt/progress')) return json({ ...runtimeFixture, needs_you: { count: 0, items: [] } })
    if (url.includes('/agent/progress')) return json({ document: {}, generated_at: runtimeFixture.generated_at, received_at: new Date().toISOString() })
    if (url.includes('/approvals')) return json(buildApprovalListResponse([]))
    if (url.includes('/questions')) return json({ questions: [] })
    if (url.includes('/agent/threads')) return json({ threads: [], hosts: [] })
    throw new Error(`Unexpected URL ${url}`)
  }))
}
beforeEach(() => {
  const values = new Map<string, string>()
  Object.defineProperty(window, 'localStorage', { configurable: true, value: {
    getItem: (k: string) => values.get(k) ?? null, setItem: (k: string, v: string) => values.set(k, v), removeItem: (k: string) => values.delete(k), clear: () => values.clear(),
  } })
})
afterEach(() => { onlineManager.setOnline(true); vi.unstubAllGlobals() })

// A source whose refresh is paused offline is not read: its last list is not a complete count, even
// while the other sources still hold a read from before (review 351 A1).
describe('a source paused offline', () => {
  it.each([[['questions', 'all'], 'Could not read questions — offline.'], [['agent-threads'], 'Could not read agent threads — offline.']])(
    'makes the count unknown when only %j is paused', async (key, reason) => {
      mockEmpty()
      const client = new QueryClient({ defaultOptions: { queries: { retry: false } } })
      const wrapper = ({ children }: { children: ReactNode }) => <QueryClientProvider client={client}>{children}</QueryClientProvider>
      const { result } = renderHook(() => useNeedsYou(), { wrapper })
      await waitFor(() => expect(result.current.count).toEqual({ state: 'known', count: 0 }))
      act(() => { onlineManager.setOnline(false) })
      act(() => { void client.refetchQueries({ queryKey: key, exact: true }) })
      await waitFor(() => expect(client.getQueryState(key)?.fetchStatus).toBe('paused'))
      for (const other of [['approvals', 'open'], ['agent-runtime-progress']]) expect(client.getQueryState(other)?.fetchStatus).toBe('idle')
      await waitFor(() => expect(result.current.count).toEqual({ state: 'unknown', reason }))
      client.clear()
    })
})
