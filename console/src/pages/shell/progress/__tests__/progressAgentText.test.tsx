import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { fireEvent, render, screen } from '@testing-library/react'
import type { ReactNode } from 'react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { AgentWorkBoard } from '@/pages/shell/progress/AgentWorkBoard'
import { AGENT_TEXT_MAX } from '@/pages/shell/progress/progressSummaryModel'

const NOW = Date.parse('2026-10-10T15:00:00Z')
const NOW_SEC = NOW / 1000
const RTL = '\u202E'
const POISON_TAIL = `<script>x</script>${'A'.repeat(10000)}`

/**
 * Strings the page maps to a Console label or a lamp, rather than showing as the agent's words.
 * The walker leaves these tokens alone so the mapping can run. Every other string leaf is poison.
 */
const CONSOLE_VALUES: { path: RegExp; reason: string }[] = [
  { path: /^level$/, reason: 'document level is mapped to the status lamp and is not shown as prose' },
  { path: /^works\[\d+\]\.steps\[\d+\]\.state$/, reason: 'a step state is mapped to a lane label; an unrecognised state is Unknown' },
  { path: /^works\[\d+\]\.steps\[\d+\]\.events\[\d+\]\.kind$/, reason: 'an event kind selects an icon, not prose' },
  { path: /^roadmap\.groups\[\d+\]\.state$/, reason: 'a plan group state is mapped to a label such as In progress or Done' },
  { path: /^roadmap\.stages\[\d+\]\.state$/, reason: 'a stage state is mapped to the current-stage marker' },
  { path: /^roadmap\.exit\[\d+\]\.state$/, reason: 'an exit state is mapped to a label such as Done' },
  { path: /^feed\[\d+\]\.state$/, reason: 'a feed state is mapped to a step label; an unrecognised state is Unknown' },
  { path: /^usage\.pools\[\d+\]\.verdict$/, reason: 'a pool verdict is mapped to a quota chip' },
]

function isConsoleValue(path: string): boolean {
  return CONSOLE_VALUES.some(row => row.path.test(path))
}

function poison(path: string, value: string): string {
  if (isConsoleValue(path)) return value
  return `${path}${RTL}${POISON_TAIL}`
}

function walk(value: unknown, path: string, leaves: string[]): unknown {
  if (typeof value === 'string') {
    leaves.push(path)
    return poison(path, value)
  }
  if (Array.isArray(value)) return value.map((item, index) => walk(item, `${path}[${index}]`, leaves))
  if (value != null && typeof value === 'object') {
    const out: Record<string, unknown> = {}
    for (const [key, child] of Object.entries(value)) {
      out[key] = walk(child, path === '' ? key : `${path}.${key}`, leaves)
    }
    return out
  }
  return value
}

function skeleton() {
  return {
    at: NOW_SEC,
    verdict: 'quiet',
    level: 'run',
    coordinator: { at: NOW_SEC - 30, state: 'watching', note: 'note', doing: 'doing', next: 'next' },
    owner_next: { name: 'later', at: NOW_SEC + 3600, work: 'lane' },
    owner_items: [
      {
        name: 'Sign',
        work: 'Open',
        now: true,
        not_before: 'the freeze',
        deadline_text: 'Friday',
        blocks: 'the merge',
        blocks_text: 'held',
        takes: '10 min',
        advice: 'ship it',
        where: 'the thread',
        reply: 'reply there',
        actions: [{ text: 'Read', link: '#brief', link_text: 'Open brief' }],
      },
    ],
    standing_items: [
      {
        name: 'Stand',
        now: false,
        deadline_text: 'Monday',
        blocks_text: 'the queue',
        takes: '1 hour',
        advice: 'wait',
        where: 'the terminal',
        reply: 'no reply',
        actions: [{ text: 'Look' }],
      },
    ],
    needs_you: ['extra'],
    works: [
      {
        title: 'Open work',
        short: 'Open',
        goal: 'see it',
        risk: 'slip',
        after: 'then',
        next: { name: 'Then', who: 'Cursor' },
        owner: { name: 'You later' },
        steps: [
          {
            name: 'Run',
            who: 'Cursor',
            state: 'running',
            label: 'label',
            note: 'a note',
            doing: 'editing',
            waits_for: 'the merge',
            events: [{ kind: 'run', text: 'echo hi', out: 'hi' }],
          },
        ],
      },
    ],
    feed: [{ at: NOW_SEC, work: 'Open', step: 'Run', who: 'Cursor', state: 'running', text: 'a line' }],
    recent: ['shipped'],
    queue: [{ title: 'Queued', why_waiting: 'morning' }],
    roadmap: {
      goal: 'the goal',
      note: 'a note',
      generated_from: 'the ledger',
      uncovered_exit: 'an exit',
      stages: [{ name: 'Now', what: 'this stage', state: 'current' }],
      groups: [
        {
          name: 'Doing',
          state: 'doing',
          items: ['slice'],
          detail: [{ name: 'Item', est: { done: 1, total: 2, mine: 1, owner: 0, blocked: 'nobody', sure: 'high', note: 'est note', as_of: 'today' } }],
        },
        { name: 'Shipped', state: 'done', items: ['old'] },
      ],
      exit: [{ text: 'Owner signs', state: 'done', owner: true }],
      pace: { per_night: 2, basis: 'recent nights', owner_wait: 'releases' },
      delta: { since: 'yesterday', lines: ['changed'] },
    },
    usage: {
      pools: [
        {
          vendor: 'Claude',
          id: 'weekly',
          label: 'Weekly',
          verdict: 'on_pace',
          used: 10,
          elapsed_pct: 20,
          left_pct: 80,
          allow_per_day: 4,
          rate_per_day: 3,
          rate_basis: 'this window',
          projected: 40,
          resets_at: '2026-11-01T00:00:00Z',
        },
        {
          vendor: 'Codex',
          id: 'burn',
          label: 'Burn',
          verdict: 'exhausted',
          used: 100,
          note: 'used up',
          resets_at: '2026-11-01T00:00:00Z',
        },
      ],
    },
  }
}

function textNodes(root: HTMLElement): Text[] {
  const walker = document.createTreeWalker(root, NodeFilter.SHOW_TEXT)
  const nodes: Text[] = []
  let node = walker.nextNode()
  while (node != null) {
    nodes.push(node as Text)
    node = walker.nextNode()
  }
  return nodes
}

function openEverything(): void {
  for (const button of document.querySelectorAll('#needs-you button[aria-expanded="false"]')) {
    fireEvent.click(button)
  }
  for (let pass = 0; pass < 8; pass += 1) {
    const closed = [...document.querySelectorAll('details:not([open]) > summary')]
    if (closed.length === 0) break
    for (const summary of closed) fireEvent.click(summary)
  }
}

describe('document strings on the Progress page', () => {
  beforeEach(() => {
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
  })

  afterEach(() => {
    vi.useRealTimers()
    vi.unstubAllGlobals()
  })

  it('cleans, bounds, and marks every rendered string leaf', async () => {
    const leaves: string[] = []
    const documentBody = walk(skeleton(), '', leaves)
    expect(leaves.length).toBeGreaterThan(20)
    expect(CONSOLE_VALUES.every(row => row.reason !== '')).toBe(true)
    vi.stubGlobal(
      'fetch',
      vi.fn(async () => new Response(JSON.stringify({
        document: documentBody,
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
    const view = render(wrap(<AgentWorkBoard />))
    await screen.findByTestId('agent-work-document')
    openEverything()
    const root = view.container
    expect(root.textContent ?? '').not.toContain(RTL)
    expect(root.querySelector('script')).toBeNull()
    for (const node of textNodes(root)) {
      expect(Array.from(node.textContent ?? '').length).toBeLessThanOrEqual(AGENT_TEXT_MAX)
    }
    const from = textNodes(root).find(node => (node.textContent ?? '').includes('roadmap.generated_from'))
    expect(from).toBeTruthy()
    expect(from?.parentElement?.closest('[data-agent-text]')).not.toBeNull()
    expect(from?.textContent ?? '').not.toMatch(/^Generated from/)
    for (const path of leaves) {
      if (isConsoleValue(path)) {
        expect(root.textContent ?? '').not.toContain(`${path}<script>`)
        continue
      }
      const signature = `${path}<script>x</script>`
      for (const node of textNodes(root)) {
        if (!(node.textContent ?? '').includes(signature)) continue
        expect(node.parentElement?.closest('[data-agent-text]'), path).not.toBeNull()
      }
    }
  })
})
