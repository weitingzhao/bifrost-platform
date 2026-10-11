import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { act, fireEvent, render, screen, within } from '@testing-library/react'
import type { ReactNode } from 'react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { formatLocalTime } from '@/api/agentProgress'
import { AgentWorkBoard } from '@/pages/shell/progress/AgentWorkBoard'

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

function stub(document: unknown) {
  vi.stubGlobal(
    'fetch',
    vi.fn(async () =>
      json({
        document,
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

describe('step states say what is true', () => {
  beforeEach(() => {
    installStorage()
    vi.useFakeTimers({ toFake: ['Date'] })
    vi.setSystemTime(new Date(NOW))
  })

  afterEach(() => {
    vi.useRealTimers()
    vi.unstubAllGlobals()
  })

  it('lists, labels, bars, Now and Next, and Live from the real state', async () => {
    const turn = NOW_SEC + 30
    stub({
      at: NOW_SEC,
      verdict: 'States',
      works: [
        {
          title: 'Board',
          short: 'Board',
          sure: true,
          owner: { name: 'Owner step', at: turn },
          steps: [
            {
              name: 'Run',
              state: 'running',
              who: 'Cursor',
              started: NOW_SEC - 12 * 60,
              usual_min: 30,
              events: [{ at: NOW_SEC, kind: 'say', text: 'hello' }],
            },
            { name: 'Slow', state: 'slow', who: 'Cursor', started: NOW_SEC - 60, usual_min: 30 },
            { name: 'Quiet', state: 'quiet', who: 'Cursor', started: NOW_SEC - 60, usual_min: 30 },
            { name: 'Dead', state: 'dead', who: 'Cursor' },
            { name: 'Gap', state: 'gap', who: 'Cursor', started: NOW_SEC - 10 * 60, usual_min: 40 },
            {
              name: 'OwnerRun',
              state: 'running',
              needs_owner: true,
              who: '你',
              started: NOW_SEC - 12 * 60,
              usual_min: 30,
              overdue: 600,
            },
            { name: 'OwnerGap', state: 'gap', needs_owner: true, who: '你', started: NOW_SEC - 12 * 60 },
            { name: 'Fail', state: 'failed', who: 'Cursor' },
            { name: 'Wait', state: 'waiting', who: 'Claude' },
            { name: 'Held', state: 'waiting', needs_owner: true, who: '你', hold: true, label: '还没到这一步' },
            {
              name: 'Done',
              state: 'done',
              who: 'Cursor',
              started: NOW_SEC - 90,
              ended: NOW_SEC - 30,
              events: [{ at: NOW_SEC, kind: 'say', text: 'old' }],
            },
          ],
        },
      ],
    })
    render(wrap(<AgentWorkBoard />))
    const running = await screen.findByRole('region', { name: 'Running now' })
    for (const name of ['Run', 'Slow', 'Quiet', 'Dead', 'Fail', 'Gap', 'OwnerRun', 'OwnerGap']) {
      expect(within(running).getByRole('button', { name: new RegExp(`Board · ${name}`) })).toBeTruthy()
    }
    for (const name of ['Wait', 'Held', 'Done']) {
      expect(within(running).queryByRole('button', { name: new RegExp(`Board · ${name}`) })).toBeNull()
    }

    const card = (name: string) => screen.getByRole('listitem', { name })
    expect(within(card('Run')).getByText('running')).toBeTruthy()
    expect(within(card('Run')).getByText('12m of the usual 30')).toBeTruthy()
    expect(card('Run').querySelector('[data-step-bar]')).toBeTruthy()
    expect(within(card('Slow')).getByText('slower than usual')).toBeTruthy()
    expect(card('Slow').querySelector('[data-step-bar]')).toBeTruthy()
    expect(within(card('Quiet')).getByText('may be stuck')).toBeTruthy()
    expect(card('Quiet').querySelector('[data-step-bar]')).toBeTruthy()
    expect(within(card('Dead')).getByText('process gone')).toBeTruthy()
    expect(card('Dead').querySelector('[data-step-bar]')).toBeNull()
    expect(within(card('Fail')).getByText('failed')).toBeTruthy()
    expect(card('Fail').querySelector('[data-step-bar]')).toBeNull()

    const gap = card('Gap')
    expect(within(gap).getByText('waiting for the coordinator')).toBeTruthy()
    expect(within(gap).getByText('for 10m')).toBeTruthy()
    expect(within(gap).queryByText(/of the usual/)).toBeNull()
    expect(gap.querySelector('[data-step-bar]')).toBeNull()

    const ownerRun = card('OwnerRun')
    expect(within(ownerRun).getByText('waiting for you')).toBeTruthy()
    expect(within(ownerRun).getByText('waiting 12m')).toBeTruthy()
    expect(within(ownerRun).queryByText(/late/)).toBeNull()
    expect(within(ownerRun).queryByText(/of the usual/)).toBeNull()
    expect(ownerRun.querySelector('[data-step-bar]')).toBeTruthy()

    const ownerGap = card('OwnerGap')
    expect(within(ownerGap).getByText('waiting for you')).toBeTruthy()
    expect(within(ownerGap).getByText('waiting 12m')).toBeTruthy()
    expect(within(ownerGap).queryByText(/^for /)).toBeNull()
    expect(ownerGap.querySelector('[data-step-bar]')).toBeNull()

    expect(within(card('Wait')).getByText('not started')).toBeTruthy()
    expect(card('Wait').querySelector('[data-step-bar]')).toBeNull()
    const held = card('Held')
    expect(within(held).getByText('not your turn yet')).toBeTruthy()
    expect(within(held).getByText('还没到这一步')).toBeTruthy()
    expect(within(held).queryByText('waiting for you')).toBeNull()
    expect(held.querySelector('[data-step-bar]')).toBeNull()

    const nowLine = screen.getByText(/^Now:/).textContent ?? ''
    for (const name of ['Run', 'Slow', 'Quiet', 'Dead', 'Gap', 'OwnerRun', 'OwnerGap']) {
      expect(nowLine).toContain(name)
    }
    for (const name of ['Fail', 'Wait', 'Held', 'Done']) {
      expect(nowLine).not.toContain(name)
    }
    expect(screen.getByText('Next: Wait · Claude')).toBeTruthy()
    expect(screen.getByText(`Your turn: Owner step · expected about ${formatLocalTime(turn)} (now)`)).toBeTruthy()

    expect(screen.getByRole('region', { name: 'Live Run' })).toBeTruthy()
    expect(screen.queryByRole('region', { name: 'Live Gap' })).toBeNull()
    expect(screen.queryByRole('region', { name: 'Live Wait' })).toBeNull()
    expect(screen.queryByRole('region', { name: 'Live Done' })).toBeNull()
    expect(screen.queryByText('No events yet.')).toBeNull()

    fireEvent.click(screen.getByText('1 earlier step done'))
    const done = screen.getByRole('listitem', { name: 'Done' })
    expect(within(done).getByText('done')).toBeTruthy()
    expect(done.querySelector('[data-step-bar]')).toBeNull()
    expect(screen.queryByRole('region', { name: 'Live Done' })).toBeNull()
  })

  it('does not redraw the step bar or a done step when the clock ticks', async () => {
    vi.useRealTimers()
    vi.useFakeTimers({ toFake: ['Date', 'setInterval', 'clearInterval'] })
    vi.setSystemTime(new Date(NOW))
    stub({
      at: NOW_SEC,
      verdict: 'Tick',
      works: [
        {
          title: 'Board',
          steps: [
            { name: 'Done', state: 'done', who: 'Cursor', started: NOW_SEC - 90, ended: NOW_SEC - 30 },
            { name: 'Run', state: 'running', who: 'Cursor', started: NOW_SEC - 600, usual_min: 30 },
          ],
        },
      ],
    })
    render(wrap(<AgentWorkBoard />))
    fireEvent.click(await screen.findByText('1 earlier step done'))
    const done = screen.getByRole('listitem', { name: 'Done' })
    const run = screen.getByRole('listitem', { name: 'Run' })
    const bar = run.querySelector('[data-step-bar]')
    expect(bar?.getAttribute('data-step-bar')).toBe('33')
    const doneRenders = done.getAttribute('data-renders')
    const barRenders = run.querySelector('[data-bar-renders]')?.getAttribute('data-bar-renders')
    expect(within(run).getByText('10m of the usual 30')).toBeTruthy()
    await act(async () => {
      vi.setSystemTime(NOW + 120_000)
      await vi.advanceTimersByTimeAsync(1000)
    })
    expect(done.getAttribute('data-renders')).toBe(doneRenders)
    expect(run.querySelector('[data-bar-renders]')?.getAttribute('data-bar-renders')).toBe(barRenders)
    expect(run.querySelector('[data-step-bar]')?.getAttribute('data-step-bar')).toBe('33')
    expect(within(run).getByText('12m of the usual 30')).toBeTruthy()
  })
})
