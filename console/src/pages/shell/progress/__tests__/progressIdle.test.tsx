import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { render, screen } from '@testing-library/react'
import type { ReactNode } from 'react'
import { afterEach, describe, expect, it, vi } from 'vitest'
import type { ProgressWork } from '@/api/agentProgress'
import { AgentWorkBoard } from '@/pages/shell/progress/AgentWorkBoard'
import { asksFromDocument, idleForText, idleQueue, type AskSource } from '@/pages/shell/progress/progressSummaryModel'

const NOW = Date.parse('2026-10-10T15:00:00Z')
const NOW_SEC = NOW / 1000
const KNOWN = ['waiting', 'running', 'slow', 'quiet', 'dead', 'failed', 'gap', 'done'] as const
const DRAW = [...KNOWN, 'alien']

function mulberry32(seed: number): () => number {
  let state = seed >>> 0
  return () => {
    state = (state + 0x6d2b79f5) | 0
    let t = Math.imul(state ^ (state >>> 15), 1 | state)
    t = (t + Math.imul(t ^ (t >>> 7), 61 | t)) ^ t
    return ((t ^ (t >>> 14)) >>> 0) / 4294967296
  }
}

function pick<T>(rand: () => number, values: readonly T[]): T {
  return values[Math.floor(rand() * values.length)]!
}

/** The red rule, written out so it does not call the implementation. */
function provenOwnerWait(works: ProgressWork[], asks: readonly AskSource[]): boolean {
  const open = works.filter(work => work.finished !== true)
  if (open.length === 0) return false
  return open.every(work => {
    const step = (work.steps ?? []).find(row => row.state !== 'done')
    if (step == null) return false
    const who = step.who?.trim() ?? ''
    const owner = step.needs_owner === true || who.startsWith('你') || /^you\b/i.test(who) || /^owner\b/i.test(who)
    if (!owner) return false
    const workName = (work.short ?? '').trim() !== '' ? work.short : work.title
    return asks.some(ask => {
      if (ask.now !== true || ask.unreadable) return false
      if (step.approval?.id != null && step.approval.id !== '' && ask.approval?.id === step.approval.id) return true
      return (ask.work ?? '') === (workName ?? '') && (ask.name ?? '') === (step.name ?? '')
    })
  })
}

function hasUnknownState(works: ProgressWork[]): boolean {
  return works
    .filter(work => work.finished !== true)
    .some(work => (work.steps ?? []).some(step => !KNOWN.includes((step.state ?? '') as (typeof KNOWN)[number])))
}

function buildCase(rand: () => number): { works: ProgressWork[]; asks: AskSource[] } {
  const workCount = 1 + Math.floor(rand() * 2)
  const works: ProgressWork[] = []
  const rawAsks: AskSource[] = []
  for (let index = 0; index < workCount; index += 1) {
    const title = `Work ${index}`
    const stepCount = 1 + Math.floor(rand() * 3)
    const steps = Array.from({ length: stepCount }, (_, stepIndex) => {
      const state = pick(rand, DRAW)
      const owner = rand() < 0.5
      return {
        name: `Step ${index}-${stepIndex}`,
        state,
        who: owner ? pick(rand, ['you', 'Owner', '你']) : 'Cursor',
        needs_owner: owner && rand() < 0.5,
      }
    })
    works.push({ title, finished: rand() < 0.2, steps })
    const head = steps.find(step => step.state !== 'done')
    const mode = pick(rand, ['now', 'later', 'absent'] as const)
    if (head != null && mode !== 'absent') {
      rawAsks.push({
        unreadable: false,
        index: rawAsks.length,
        name: head.name,
        work: title,
        now: mode === 'now',
      })
    }
  }
  return { works, asks: rawAsks }
}

describe('an idle queue is red only when the document proves it', () => {
  it('holds the colour rule across a few hundred seeded step states', () => {
    const rand = mulberry32(62)
    for (let index = 0; index < 240; index += 1) {
      const { works, asks } = buildCase(rand)
      const result = idleQueue(works, asks, undefined, 'ready', NOW)
      const open = works.some(work => work.finished !== true)
      if (result.tone === 'red') {
        expect(provenOwnerWait(works, asks), `case ${index}`).toBe(true)
        expect(hasUnknownState(works), `case ${index}`).toBe(false)
      }
      if (result.tone === 'green') expect(open, `case ${index}`).toBe(false)
      if (hasUnknownState(works)) {
        expect(result.tone, `case ${index}`).toBe('unknown')
        expect(result.line, `case ${index}`).toBe("Unknown — a step's state is not recognised")
      }
      const moving = works.some(work => (work.steps ?? []).some(step => step.state === 'running' || step.state === 'slow'))
      if (!moving && open && !hasUnknownState(works)) expect(result.line, `case ${index}`).not.toBeNull()
    }
  })

  it('keeps an unrecognised state ahead of running, and a future end is Unknown', () => {
    const alien = idleQueue(
      [{ title: 'Open', steps: [{ name: 'Go', state: 'running' }, { name: 'Odd', state: 'alien' }] }],
      [{ unreadable: false, index: 0, name: 'Odd', work: 'Open', now: true }],
      undefined,
      'ready',
      NOW,
    )
    expect(alien.tone).not.toBe('red')
    expect(alien.line).toBe("Unknown — a step's state is not recognised")

    const gap = idleQueue(
      [{ title: 'Open', steps: [{ name: 'Wait', state: 'gap' }, { name: 'Fail', state: 'failed' }] }],
      [],
      undefined,
      'ready',
      NOW,
    )
    expect(gap.line).not.toBeNull()
    expect(gap.tone).toBe('amber')

    const matched = asksFromDocument(
      [{ unreadable: false, name: 'Wait', work: 'Open', now: true }],
      undefined,
    ).asks
    const red = idleQueue(
      [{ title: 'Open', steps: [{ name: 'Wait', state: 'waiting', who: 'you', needs_owner: true }] }],
      matched,
      undefined,
      'ready',
      NOW,
    )
    expect(red.tone).toBe('red')
    expect(red.line).toBe('Stopped — waiting on you')

    const blank = idleQueue([{ title: 'Open', steps: [{ name: 'X', state: '' }] }], [], undefined, 'ready', NOW)
    expect(blank.tone).toBe('unknown')
    const missing = idleQueue([{ title: 'Open', steps: [{ name: 'X' }] }], [], undefined, 'ready', NOW)
    expect(missing.line).toBe("Unknown — a step's state is not recognised")

    const ahead = idleForText([{ steps: [{ ended: NOW_SEC + 120 }] }], NOW)
    expect(ahead).toBe('Unknown')
    const close = idleForText([{ steps: [{ ended: NOW_SEC + 30 }] }], NOW)
    expect(close).toBe('for 0 min')
  })

  it('lists a gap lane and still shows the idle line', async () => {
    const mem = new Map<string, string>()
    Object.defineProperty(window, 'localStorage', {
      configurable: true,
      value: {
        get length() { return mem.size },
        clear() { mem.clear() },
        getItem(key: string) { return mem.get(key) ?? null },
        key(index: number) { return [...mem.keys()][index] ?? null },
        removeItem(key: string) { mem.delete(key) },
        setItem(key: string, value: string) { mem.set(key, String(value)) },
      },
    })
    vi.useFakeTimers({ toFake: ['Date'] })
    vi.setSystemTime(new Date(NOW))
    vi.stubGlobal(
      'fetch',
      vi.fn(async () => new Response(JSON.stringify({
        document: {
          at: NOW_SEC,
          verdict: 'Gap',
          works: [{ title: 'Open', short: 'Open', steps: [{ name: 'Wait', state: 'gap', who: 'Cursor' }] }],
        },
        received_at: new Date(NOW).toISOString(),
        age_seconds: 0,
        stale: false,
        host: 'vision-mac',
        generated_at: new Date(NOW).toISOString(),
        generation_lag_seconds: 0,
      }), { status: 200, headers: { 'Content-Type': 'application/json' } })),
    )
    const client = new QueryClient({ defaultOptions: { queries: { retry: false } } })
    const wrap = (children: ReactNode) => <QueryClientProvider client={client}>{children}</QueryClientProvider>
    render(wrap(<AgentWorkBoard />))
    const summary = await screen.findByTestId('progress-summary')
    expect(summary.querySelector('[data-running-row]')).toBeTruthy()
    expect(summary.querySelector('[data-idle-line]')?.textContent).toContain('Stopped — waiting on the coordinator — nothing for you')
    expect(summary.querySelector('[data-run-state]')?.getAttribute('data-run-state')).not.toBe('red')
  })
})

afterEach(() => {
  vi.useRealTimers()
  vi.unstubAllGlobals()
})
