import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { fireEvent, render, screen, within } from '@testing-library/react'
import type { ReactNode } from 'react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { formatDuration } from '@/api/agentProgress'
import { AgentWorkBoard } from '@/pages/shell/progress/AgentWorkBoard'
import { formatLocalDateTime } from '@/pages/shell/progress/quotaModel'

const NOW = Date.parse('2026-10-10T15:00:00Z')
const NOW_SEC = NOW / 1000

function json(body: unknown): Response {
  return new Response(JSON.stringify(body), { status: 200, headers: { 'Content-Type': 'application/json' } })
}

function installStorage(): void {
  const mem = new Map<string, string>()
  const store: Storage = {
    get length() {
      return mem.size
    },
    clear() {
      mem.clear()
    },
    getItem(key) {
      return mem.has(key) ? (mem.get(key) ?? null) : null
    },
    key(index) {
      return [...mem.keys()][index] ?? null
    },
    removeItem(key) {
      mem.delete(key)
    },
    setItem(key, value) {
      mem.set(key, String(value))
    },
  }
  Object.defineProperty(window, 'localStorage', { configurable: true, value: store })
}

function wrap(children: ReactNode) {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  return <QueryClientProvider client={client}>{children}</QueryClientProvider>
}

function stub(usage: unknown) {
  vi.stubGlobal(
    'fetch',
    vi.fn(async () =>
      json({
        document: { at: NOW_SEC, verdict: 'Quiet', usage },
        received_at: new Date(NOW).toISOString(),
        age_seconds: 0,
        stale: false,
        host: 'vision-mac',
        generated_at: new Date(NOW).toISOString(),
        generation_lag_seconds: 0,
      }),
    ),
  )
}

describe('quota by pace', () => {
  beforeEach(() => {
    installStorage()
    vi.useFakeTimers({ toFake: ['Date'] })
    vi.setSystemTime(new Date(NOW))
  })

  afterEach(() => {
    vi.useRealTimers()
    vi.unstubAllGlobals()
  })

  it('keeps Usage today when pools are absent and replaces it when they are present', async () => {
    stub({ cursor: { runs: 1, min: 2 } })
    const first = render(wrap(<AgentWorkBoard />))
    expect(await screen.findByRole('region', { name: 'Usage today' })).toBeTruthy()
    expect(screen.queryByRole('region', { name: 'Quota' })).toBeNull()
    first.unmount()

    stub({ pools: [] })
    render(wrap(<AgentWorkBoard />))
    const quota = await screen.findByRole('region', { name: 'Quota' })
    expect(within(quota).getByText('Left of the tick: room to spare. Right of it: being used faster than the clock.')).toBeTruthy()
    expect(screen.queryByRole('region', { name: 'Usage today' })).toBeNull()
  })

  it('renders every verdict and leaves out phrases whose numbers are missing', async () => {
    const runsOut = '2026-10-17T15:37:39Z'
    const resets = '2026-10-18T03:58:22Z'
    stub({
      cursor: { runs: 1, min: 23, in: 10, out: 2, cache: 3 },
      codex: { runs: 2, tokens: 100 },
      pools: [
        {
          vendor: 'Claude',
          id: 'weekly',
          label: '每周 · 全部模型',
          used: 78,
          resets_at: '2026-10-12T12:59:59Z',
          elapsed_pct: 81.6,
          left_pct: 22,
          allow_per_day: 17.1,
          rate_per_day: 13.6,
          rate_basis: '本窗口平均',
          projected: 96,
          verdict: 'on_pace',
        },
        {
          vendor: 'Claude',
          label: 'Spare pool',
          verdict: 'spare',
          spare_pct: 12.5,
          used: 3,
        },
        {
          vendor: 'Codex',
          label: '每周',
          verdict: 'over',
          used: 3,
          resets_at: resets,
          runs_out_at: runsOut,
          elapsed_pct: 1.3,
          left_pct: 97,
          allow_per_day: 14,
          rate_per_day: 15.2,
          rate_basis: '近 33 小时实测',
          projected: 108,
        },
        { vendor: 'Claude', label: 'Early', verdict: 'too_early', used: 1, resets_at: '2026-11-01T00:00:00Z' },
        { vendor: 'Claude', label: 'Burst', verdict: 'short', used: 0, resets_at: '2026-11-01T00:00:00Z' },
        { vendor: 'Claude', label: 'Fable', verdict: 'sublimit', used: 1, resets_at: '2026-11-01T00:00:00Z' },
        { vendor: 'Cursor', label: 'Grok', verdict: 'unknown', note: '读不到：命令行不给', used: 1, resets_at: '2026-11-01T00:00:00Z' },
        { vendor: 'Cursor', label: 'Due', verdict: 'reset_due', used: 1, resets_at: '2026-11-01T00:00:00Z' },
        { vendor: 'Claude', label: 'Bare', verdict: 'on_pace', used: 1, resets_at: '2026-11-01T00:00:00Z' },
        { vendor: 'Codex', label: 'Over bare', verdict: 'over', used: 1, resets_at: '2026-11-01T00:00:00Z' },
        { vendor: 'Claude', label: 'Spare bare', verdict: 'spare', used: 1, resets_at: '2026-11-01T00:00:00Z' },
      ],
    })
    render(wrap(<AgentWorkBoard />))
    const quota = await screen.findByRole('region', { name: 'Quota' })
    expect(within(quota).getByText('每周 · 全部模型')).toBeTruthy()
    expect(within(quota).getByText(/used 78%/)).toBeTruthy()
    expect(within(quota).getByText(/resets in 1d 21h/)).toBeTruthy()
    expect(
      within(quota).getByText('81.6% of the window gone · 22 pts left · 17.1/day allowed · 13.6/day lately (本窗口平均) · about 96% at reset'),
    ).toBeTruthy()
    expect(within(quota).getAllByText('On pace').length).toBeGreaterThan(0)
    expect(within(quota).getByText('Spare — about 12.5 pts will expire unused')).toBeTruthy()
    const when = formatLocalDateTime(Date.parse(runsOut))
    const before = formatDuration((Date.parse(resets) - Date.parse(runsOut)) / 1000)
    expect(within(quota).getByText(`Runs out ${when}, ${before} before the reset`)).toBeTruthy()
    expect(within(quota).getByText('Too early to tell')).toBeTruthy()
    expect(within(quota).getByText('Burst limit')).toBeTruthy()
    expect(within(quota).getByText('Sub-limit of another pool')).toBeTruthy()
    expect(within(quota).getByText('Not readable 读不到：命令行不给')).toBeTruthy()
    expect(within(quota).getByText('Reset due')).toBeTruthy()
    expect(within(quota).getByText('Spare')).toBeTruthy()
    expect(within(quota).queryByText(/undefined|NaN/)).toBeNull()
    expect(quota.textContent).not.toContain('undefined')
    expect(quota.textContent).not.toContain('NaN')
    const bare = within(quota).getByText('Bare').closest('div')
    expect(bare?.textContent).not.toContain('% of the window')
    expect(bare?.textContent).not.toContain('pts left')
    expect(bare?.textContent).not.toContain('/day')
    expect(within(quota).getByText('Over bare').closest('div')?.textContent).not.toContain('Runs out')
    expect(quota.querySelector('[data-window-tick="81.6"]')).toBeTruthy()
    const fill = quota.querySelector('[data-quota-fill="78"]')
    expect(fill).toBeTruthy()
    expect(within(quota).getByText('Cursor · 1 run · 23 min · 10 in · 2 out · 3 cached')).toBeTruthy()
    expect(within(quota).getByText('Codex · 2 runs · 100 tokens')).toBeTruthy()
    expect(screen.queryByRole('region', { name: 'Usage today' })).toBeNull()
  })

  it('does not call bad numbers healthy, and formats run totals', async () => {
    const future = '2026-10-20T00:00:00Z'
    const past = '2020-01-01T00:00:00Z'
    stub({
      cursor: { runs: 1, min: 23.783166666666666, in: 364769, out: 144618, cache: 10146048 },
      codex: { runs: 4.2, tokens: 280000 },
      pools: [
        { vendor: 'Claude', label: 'Missing used', verdict: 'spare', spare_pct: 12, resets_at: future },
        { vendor: 'Claude', label: 'Not numeric', verdict: 'spare', used: 'nope', spare_pct: 12, resets_at: future },
        { vendor: 'Claude', label: 'Negative', verdict: 'on_pace', used: -3, resets_at: future, elapsed_pct: 10 },
        { vendor: 'Claude', label: 'Too high', verdict: 'spare', used: 1001, spare_pct: 4, resets_at: future },
        { vendor: 'Claude', label: 'Over full', verdict: 'on_pace', used: 140, resets_at: future, elapsed_pct: 10 },
        { vendor: 'Claude', label: 'At cap', verdict: 'on_pace', used: 1000, resets_at: future },
        { vendor: 'Claude', label: 'Bad reset', verdict: 'on_pace', used: 50, resets_at: 'not-a-date', elapsed_pct: 80 },
        {
          vendor: 'Claude',
          label: 'Past reset',
          verdict: 'on_pace',
          used: 40,
          resets_at: past,
          elapsed_pct: 90,
          left_pct: 10,
          allow_per_day: 5,
          rate_per_day: 4,
          projected: 80,
        },
        {
          vendor: 'Cursor',
          label: 'Empty pool',
          verdict: 'exhausted',
          used: 100,
          resets_at: future,
          note: '已用完；按量付费关着',
        },
        { vendor: 'Cursor', label: 'Weird', verdict: 'sideways', used: 10, resets_at: future },
        { vendor: 'Codex', label: 'Codex pool', verdict: 'on_pace', used: 3, resets_at: future },
      ],
    })
    render(wrap(<AgentWorkBoard />))
    const quota = await screen.findByRole('region', { name: 'Quota' })
    const row = (label: string) => within(quota).getByText(label).closest('div')
    for (const label of ['Missing used', 'Not numeric', 'Negative', 'Too high', 'Bad reset']) {
      const body = row(label)
      expect(body?.textContent).toContain('Unknown')
      expect(body?.textContent).not.toContain('On pace')
      expect(body?.textContent).not.toContain('Spare')
      expect(body?.querySelector('[data-quota-fill]')).toBeNull()
      expect(body?.textContent).not.toContain('of the window')
    }
    const over = row('Over full')
    expect(over?.textContent).toContain('used 140%')
    expect(over?.querySelector('[data-quota-fill="100"]')).toBeTruthy()
    expect(over?.textContent).toContain('On pace')
    const cap = row('At cap')
    expect(cap?.textContent).toContain('used 1000%')
    expect(cap?.querySelector('[data-quota-fill="100"]')).toBeTruthy()
    const pastRow = row('Past reset')
    expect(pastRow?.textContent).toContain('Reset due')
    expect(pastRow?.textContent).toContain('used 40%')
    expect(pastRow?.textContent).not.toContain('On pace')
    expect(pastRow?.textContent).not.toContain('of the window')
    expect(within(quota).getByText('Used up — not available until the reset')).toBeTruthy()
    expect(within(quota).getByText('已用完；按量付费关着')).toBeTruthy()
    expect(row('Weird')?.textContent).toContain('Unknown')
    expect(within(quota).getByText('Cursor · 1 run · 24 min · 365K in · 145K out · 10.1M cached')).toBeTruthy()
    expect(within(quota).getByText('Codex · 4 runs · 280K tokens')).toBeTruthy()
    expect(quota.textContent).not.toContain('23.783')
    expect(quota.textContent).not.toContain('364769')
    expect(quota.textContent).not.toContain('280000')
  })

  it('draws at most 12 pools until the rest are asked for', async () => {
    const future = '2026-10-20T00:00:00Z'
    stub({
      pools: Array.from({ length: 13 }, (_, index) => ({
        vendor: 'Claude',
        label: `Pool ${index + 1}`,
        used: 10,
        resets_at: future,
        verdict: 'on_pace',
      })),
    })
    render(wrap(<AgentWorkBoard />))
    const quota = await screen.findByRole('region', { name: 'Quota' })
    expect(within(quota).getByText('Pool 1')).toBeTruthy()
    expect(within(quota).getByText('Pool 12')).toBeTruthy()
    expect(within(quota).queryByText('Pool 13')).toBeNull()
    fireEvent.click(within(quota).getByRole('button', { name: 'Show 1 more' }))
    expect(within(quota).getByText('Pool 13')).toBeTruthy()
    expect(within(quota).queryByRole('button', { name: /Show .* more/ })).toBeNull()
  })
})
