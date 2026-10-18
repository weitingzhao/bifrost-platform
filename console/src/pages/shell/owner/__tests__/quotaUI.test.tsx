import '@testing-library/jest-dom/vitest'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { act, cleanup, fireEvent, render, screen, waitFor, within } from '@testing-library/react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { PlatformAuthContext, type PlatformAuthContextValue } from '@/hooks/platformAuthContext'
import { fetchPools, parsePools, recordPoolReading } from '@/api/agentRuntimePools'
import { OwnerQuota } from '../OwnerQuota'
import chicago from './fixtures/pools-chicago.json'
import utc from './fixtures/pools-utc.json'
const mocks = vi.hoisted(() => ({ fetch: vi.fn() }))
vi.mock('@/api/client', () => ({ authedFetch: mocks.fetch }))
const reset = '2026-10-21T14:00:00Z'
const now = Date.parse('2026-10-18T14:00:00Z')
const pool = { vendor_pool_id: 'claude:owner:weekly', tool: 'claude', account: 'owner', window: 'weekly',
  used_fraction: 0.4, resets_at: reset, measured_at: '2026-10-18T13:00:00Z', measure_source: 'owner',
  cooldown_until: null, cooling: false, pace: 0.15, used: { tokens: 1200, usd: 2.5, wall_s: 7200 }, used_since: '2026-10-14T14:00:00Z' }
const auth: PlatformAuthContextValue = { token: '', caps: undefined, capsLoading: false, capsError: false, canAdmin: true, canOperate: true,
  setToken: vi.fn(), signOut: vi.fn(), refreshCapabilities: vi.fn() }
const clients: QueryClient[] = []
function show(role: PlatformAuthContextValue | null = auth) {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } }); clients.push(client)
  const node = (value: PlatformAuthContextValue | null) => <QueryClientProvider client={client}><PlatformAuthContext.Provider value={value}><OwnerQuota now={now} /></PlatformAuthContext.Provider></QueryClientProvider>
  return { client, ...render(node(role)), node }
}
function fill(percent = '40', resetAt = '2026-10-21T14:00') {
  fireEvent.change(screen.getByLabelText('Pool'), { target: { value: pool.vendor_pool_id } })
  fireEvent.change(screen.getByLabelText('Used percent'), { target: { value: percent } })
  fireEvent.change(screen.getByLabelText('Reset (local time)'), { target: { value: resetAt } })
}
beforeEach(() => {
  mocks.fetch.mockReset()
  mocks.fetch.mockImplementation(async (_prefix, _path, init) => new Response(JSON.stringify(init?.method === 'POST'
    ? { vendor_pool_id: pool.vendor_pool_id, recorded: true } : { pools: [pool] })))
})
afterEach(() => { cleanup(); clients.forEach(c => c.clear()); clients.length = 0; vi.useRealTimers() })
describe('quota card', () => {
  it('renders API rows, local reset, source, age, usage and warning pace', async () => {
    show(); expect(screen.getByText('Reading quota…')).toBeInTheDocument()
    const row = await screen.findByRole('listitem')
    for (const text of ['claude · owner · weekly', '40.0% used', `Resets ${new Date(reset).toLocaleString()}`, 'owner · 1 h ago'])
      expect(within(row).getByText(text)).toBeInTheDocument()
    expect(within(row).getByText(/1,200 tokens · \$2.50 USD · 2.00 hours/)).toBeInTheDocument()
    expect(within(row).getByText('Pace +15.0%')).toHaveStyle({ color: 'var(--color-warning)' })
    for (const label of ['Pool', 'Used percent', 'Reset (local time)']) expect(screen.getByLabelText(label)).toHaveClass('text-base', 'min-w-0')
  })
  it('colors the entire cooling row red with its end time', async () => {
    mocks.fetch.mockResolvedValue(new Response(JSON.stringify({ pools: [{ ...pool, cooling: true, cooldown_until: reset }] })))
    show()
    expect(await screen.findByText(`Cooling until ${new Date(reset).toLocaleString()}`)).toBeInTheDocument()
    expect(screen.getByRole('listitem')).toHaveStyle({ color: 'var(--color-danger)' })
    expect(screen.getByText('Pace +15.0%')).not.toHaveStyle({ color: 'var(--color-warning)' })
  })
  it.each([0.1, -0.2, null])('does not warn for pace %s and preserves unread usage', async pace => {
    mocks.fetch.mockResolvedValue(new Response(JSON.stringify({ pools: [{ ...pool, pace, used_fraction: null }] })))
    show(); const row = await screen.findByRole('listitem')
    expect(within(row).getByText('not read yet')).toBeInTheDocument()
    expect(within(row).getByText(pace === null ? 'Pace unknown' : pace < 0 ? 'Pace -20.0%' : 'Pace +10.0%')).not.toHaveStyle({ color: 'var(--color-warning)' })
  })
  it.each([['non-Owner', { ...auth, canAdmin: false }], ['loading stale Owner', { ...auth, capsLoading: true }],
    ['failed stale Owner', { ...auth, capsError: true }], ['missing provider', null]] as const)('hides the form for %s', async (_label, role) => {
    show(role); await screen.findByRole('listitem'); expect(screen.queryByRole('form')).not.toBeInTheDocument()
  })
  it('removes the form immediately when a role check fails', async () => {
    const view = show(); await screen.findByRole('listitem'); expect(screen.getByRole('form')).toBeInTheDocument()
    view.rerender(view.node({ ...auth, capsError: true })); expect(screen.queryByRole('form')).not.toBeInTheDocument()
  })
  it('submits a fraction and ISO reset and reads the list again', async () => {
    show(); await screen.findByRole('listitem'); fill(); fireEvent.click(screen.getByRole('button', { name: 'Submit' }))
    await screen.findByText('Reading recorded.')
    const call = mocks.fetch.mock.calls.find(c => c[2]?.method === 'POST')!
    expect(call[1]).toBe('/api/v1/agentrt/pools/claude%3Aowner%3Aweekly/reading')
    expect(JSON.parse(call[2].body)).toEqual({ idempotency_key: expect.any(String), used_fraction: 0.4, resets_at: new Date('2026-10-21T14:00').toISOString() })
    await waitFor(() => expect(mocks.fetch.mock.calls.filter(c => !c[2]?.method)).toHaveLength(2))
  })
  it('requires a reset for 100 percent and lists known plus discovered pools', async () => {
    mocks.fetch.mockResolvedValue(new Response(JSON.stringify({ pools: [{ ...pool, vendor_pool_id: 'claude:team:weekly', account: 'team' }] })))
    show(); await screen.findByRole('listitem'); fill('100', '')
    expect(screen.getByLabelText('Reset (local time)')).toBeRequired()
    fireEvent.submit(screen.getByRole('form')); expect(mocks.fetch.mock.calls.some(c => c[2]?.method === 'POST')).toBe(false)
    expect(screen.getAllByRole('option')).toHaveLength(6)
  })
  it('shows the server message for a 422 refusal', async () => {
    mocks.fetch.mockImplementation(async (prefix, _path, init, errorParser) => {
      if (init?.method === 'POST') throw await errorParser(prefix, new Response(JSON.stringify({ error: 'invalid', message: 'Reset must be within 35 days.' }), { status: 422 }))
      return new Response(JSON.stringify({ pools: [pool] }))
    })
    show(); await screen.findByRole('listitem'); fill(); fireEvent.click(screen.getByRole('button', { name: 'Submit' }))
    expect(await screen.findByRole('alert')).toHaveTextContent('Reset must be within 35 days.')
  })
  it('shows read errors', async () => {
    mocks.fetch.mockRejectedValue(new Error('Quota offline')); show()
    expect(await screen.findByRole('alert')).toHaveTextContent('Quota offline')
  })
  it('polls every 30 seconds and stops on unmount', async () => {
    vi.useFakeTimers(); const view = show()
    await act(async () => { await vi.advanceTimersByTimeAsync(1) }); expect(mocks.fetch).toHaveBeenCalledTimes(1)
    await act(async () => { await vi.advanceTimersByTimeAsync(30_000) }); expect(mocks.fetch).toHaveBeenCalledTimes(2)
    view.unmount(); await act(async () => { await vi.advanceTimersByTimeAsync(30_000) }); expect(mocks.fetch).toHaveBeenCalledTimes(2)
  })
})
describe('quota API contract', () => {
  it.each([null, {}, { pools: null }, { pools: [null] }, { pools: [{ ...pool, used_fraction: 2 }] }, { pools: [{ ...pool, pace: '0.1' }] },
    { pools: [{ ...pool, resets_at: 'tomorrow' }] }, { pools: [{ ...pool, measure_source: 'unknown' }] }, { pools: [{ ...pool, used: { tokens: '2' } }] },
    { pools: [{ ...pool, cooling: 'false' }] }, { pools: [{ ...pool, account: 'other' }] }, { pools: [pool, pool] }, { pools: [{ ...pool, measured_at: undefined }] },
  ])('rejects malformed shapes %#', value => expect(() => parsePools(value)).toThrow('invalid platform response'))
  it('accepts null readings and an empty usage map', () => {
    expect(parsePools({ pools: [{ ...pool, used_fraction: null, pace: null, used: {} }] })[0].used_fraction).toBeNull()
    expect(parsePools({ pools: [] })).toEqual([])
  })
  it('passes cancellation and generates a fresh key for each submit', async () => {
    const signal = new AbortController().signal; await fetchPools(signal); expect(mocks.fetch.mock.calls[0][2]).toEqual({ signal })
    await recordPoolReading(pool.vendor_pool_id, 0.4); await recordPoolReading(pool.vendor_pool_id, 0.4)
    const bodies = mocks.fetch.mock.calls.slice(1).map(c => JSON.parse(c[2].body))
    expect(bodies[0].idempotency_key).not.toBe(bodies[1].idempotency_key); expect(bodies[0]).not.toHaveProperty('resets_at')
  })
})

// Review 281: responses taken from the real platform (store.ListPools through
// agentrtapi), in UTC and in a zone with an offset, with microseconds, a pool
// only charged, an empty ledger.
describe('the real API shape', () => {
  it.each([['America/Chicago', chicago], ['UTC', utc]])('parses a response written in %s', (_zone, body) => {
    const pools = parsePools(body)
    expect(pools.length).toBeGreaterThan(0)
    expect(pools.map(p => p.vendor_pool_id)).toContain('cursor:owner:monthly')
  })
  it('says a reading whose window has reset since is the window before (review 281 B1)', async () => {
    mocks.fetch.mockResolvedValue(new Response(JSON.stringify({ pools: [{ ...pool, used_fraction: 1, resets_at: '2026-10-18T13:30:00Z', pace: null }] })))
    show(); const row = await screen.findByRole('listitem')
    expect(within(row).getByText(/last reading 100.0% is from the window before/)).toBeInTheDocument()
    expect(within(row).getByText(/Reset at .* passed/)).toBeInTheDocument()
  })
  it('clears the form after a reading is recorded, and the note once anything changes (review 281 C1)', async () => {
    show(); await screen.findByRole('listitem'); fill(); fireEvent.click(screen.getByRole('button', { name: 'Submit' }))
    await screen.findByText('Reading recorded.')
    expect(screen.getByLabelText('Used percent')).toHaveValue(null)
    expect(screen.getByLabelText('Reset (local time)')).toHaveValue('')
    fireEvent.change(screen.getByLabelText('Used percent'), { target: { value: '10' } })
    expect(screen.queryByText('Reading recorded.')).not.toBeInTheDocument()
  })
  it("offers codex's five-hour pool before agentd has reported it (review 281 C2)", async () => {
    show(); await screen.findByRole('listitem')
    expect(within(screen.getByLabelText('Pool')).getByRole('option', { name: 'codex:owner:short' })).toBeInTheDocument()
  })
})
