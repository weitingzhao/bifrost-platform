import '@testing-library/jest-dom/vitest'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { act, cleanup, render, screen, waitFor } from '@testing-library/react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import progress from '@/api/__fixtures__/agentrt-progress-marks.testdb.json'
import { OwnerWork } from '../OwnerWork'
import { clockTime, runtimeUnreadLine } from '../ownerModel'
import { useOwnerConsole } from '../useOwnerConsole'

const clients: QueryClient[] = []
function Harness() {
  const data = useOwnerConsole({ system: false, work: true, chief: false })
  return <OwnerWork data={data} />
}
function show(firstSucceeds: boolean) {
  let reads = 0
  vi.stubGlobal('fetch', vi.fn(async (input: RequestInfo | URL) => {
    const url = typeof input === 'string' ? input : input instanceof URL ? input.href : input.url
    const success = url.includes('/agentrt/progress') && ++reads === 1 && firstSucceeds
    return new Response(JSON.stringify(success ? progress : { error: 'unavailable' }), {
      status: success ? 200 : 503, headers: { 'Content-Type': 'application/json' },
    })
  }))
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  clients.push(client)
  render(<QueryClientProvider client={client}><Harness /></QueryClientProvider>)
  return client
}
beforeEach(() => {
  Object.defineProperty(window, 'localStorage', { configurable: true, value: { getItem: () => null, setItem: () => undefined, removeItem: () => undefined, clear: () => undefined } })
})
afterEach(() => { cleanup(); clients.forEach(client => client.clear()); clients.length = 0; vi.unstubAllGlobals() })

describe('runtime unread line', () => {
  const now = Date.parse('2026-10-21T12:30:00Z')
  const openedAt = now - 3600_000
  it('says when the last successful read was', () => {
    const readAt = now - 12 * 60_000
    expect(runtimeUnreadLine(readAt, openedAt, now)).toBe(`The agent runtime could not be read; last read ${clockTime(readAt, now)} (12 min ago).`)
  })
  it('says just now for recent and future reads', () => {
    for (const readAt of [now - 10_000, now + 60_000]) {
      expect(runtimeUnreadLine(readAt, openedAt, now)).toBe(`The agent runtime could not be read; last read ${clockTime(readAt, now)} (just now).`)
    }
  })
  it('says when the page opened if never read', () => {
    expect(runtimeUnreadLine(null, openedAt, now)).toBe(`The agent runtime could not be read; not read since this page opened at ${clockTime(openedAt, now)}.`)
  })
  it('keeps the successful read time on failure and drops stale goals and hosts', async () => {
    const client = show(true)
    await screen.findByText(`Open goals · ${progress.work.missions.length}`)
    const firstReadAt = client.getQueryState(['agent-runtime-progress'])!.dataUpdatedAt
    expect(firstReadAt).toBeGreaterThan(0)
    await act(async () => { await client.refetchQueries({ queryKey: ['agent-runtime-progress'], exact: true }) })
    await waitFor(() => {
      for (const name of ['Now', 'Open goals', 'Crew']) {
        expect(screen.getByRole('region', { name })).toHaveTextContent(`last read ${clockTime(firstReadAt, Date.now())}`)
      }
    })
    expect(screen.getByRole('region', { name: 'Work' }).textContent).not.toMatch(/\bM-[12]\b|\bh[1-8]\b/)
  })
  it('says when the page opened on a failed first read', async () => {
    show(false)
    await waitFor(() => {
      for (const name of ['Now', 'Open goals', 'Crew']) {
        expect(screen.getByRole('region', { name })).toHaveTextContent('not read since this page opened at')
      }
    })
  })
})
