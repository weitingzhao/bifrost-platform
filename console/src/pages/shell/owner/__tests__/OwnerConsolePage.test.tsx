import '@testing-library/jest-dom/vitest'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { cleanup, fireEvent, render, screen, within } from '@testing-library/react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import type { TelemetryAlertEntry } from '@/api/clusterTypes'
import { splitAlerts } from '../ownerModel'
import type { OwnerConsoleData } from '../useOwnerConsole'
import alertsFixture from './fixtures/telemetry-alerts.prod.json'

const NOW = Date.parse('2026-10-16T20:10:43Z')
const mobile = vi.hoisted(() => ({ value: false }))
const standbyKnown = vi.hoisted(() => ({ value: true }))
vi.mock('@bifrost/ui', async importOriginal => ({ ...(await importOriginal<object>()), useIsMobile: () => mobile.value }))

function fake(): OwnerConsoleData {
  const alerts = splitAlerts(alertsFixture.alerts as TelemetryAlertEntry[], standbyKnown.value ? [{ name: 'gpu-server', internalIp: '192.168.10.60' }] : [], 6, NOW)
  const coordinator = { id: 'coordinator-0', title: 'Look at the goal cards', href: '#progress', instruction: 'Open Progress', coordinator: {} }
  return {
    now: NOW,
    needsYou: { count: { state: 'known', count: 1 }, decisionItems: [coordinator], silentItems: [{ id: 't1', title: 'thread', href: '#needs-you', instruction: '', kind: 'silent_thread' }],
      decide: [], openQuestions: [], runtimeItems: [], recentCount: { state: 'known', count: 11 } },
    verdict: { tone: 'attention', headline: '2 things are yours', line: 'Nothing to approve · agents idle · release lane free · 5 of 5 nodes ready' },
    badges: { you: { text: '2', tone: 'attention' }, system: { text: '1', tone: 'fail' }, data: { text: '2', tone: 'muted' } },
    alerts, alertsError: null,
    work: { running: 0, stuck: 0, open: 0, lastFinished: { mission: 'M-2', at: '2026-10-16T04:17:22Z' } },
    lastGoal: 'Add a comment above Repos.',
    board: { coordinator: { doing: 'Owner console, first version', next: '2.3 inbox' }, roadmap: { round: { n: 2, items: [] } } },
    boardStale: false,
    plan: [{ key: '2.2b', name: 'First slice through', done: 10, total: 10, tone: 'ok', tag: '' }, { key: '2.3', name: 'Inbox', done: 0, total: 4, tone: 'muted', tag: 'next' }],
    arch: [{ state: 'done', what: 'a', now: '' }, { state: 'part', what: 'b', now: '' }],
    quota: [{ id: 'claude.weekly', name: 'Claude · weekly', used: 91, readAt: NOW - 5 * 86_400_000, stale: true, tone: 'muted' }],
    hosts: [],
    nodes: [{ name: 'ubt-k3s-02', status: 'Ready', roles: 'worker', internal_ip: '192.168.10.70', version: '', reachability: 'ok', cpu_usage_percent: 4.5, memory_usage_percent: 35.2 },
      { name: 'gpu-server', status: 'NotReady', roles: 'worker', internal_ip: '192.168.10.60', version: '', reachability: 'fail', elastic_mode: 'standby' }],
    nodeCount: { ready: 1, total: 1, standby: 1 }, nodesError: null,
    lane: { state: 'free', text: 'Release window free', detail: '' },
    inFlight: [], records: [{ run: 'r1', pipeline: 'bifrost-deliver-platform-prod', lane: 'platform', env: 'prod', deploys: true, started_at: '', completed_at: '2026-10-16T05:29:00Z', repos: {}, recorded_at: '' }],
    runsLoading: false,
    tiles: [{ name: 'Test copies', tone: 'warn', state: 'Stale', figure: '13 days', detail: 'dev and stg copies of PROD, oldest', href: '#infrastructure' }],
    updatedAt: NOW,
  } as unknown as OwnerConsoleData
}

vi.mock('../useOwnerConsole', () => ({ useOwnerConsole: () => fake() }))

const { OwnerConsolePage } = await import('../OwnerConsolePage')

function show() {
  return render(<QueryClientProvider client={new QueryClient()}><OwnerConsolePage /></QueryClientProvider>)
}
const headings = () => screen.getAllByRole('heading', { level: 2 }).map(h => h.textContent)

beforeEach(() => {
  mobile.value = false
  standbyKnown.value = true
  window.history.replaceState(null, '', '#owner')
})
afterEach(cleanup)

describe('Owner Console', () => {
  it('opens on All on a desktop: the verdict, then every block in order', () => {
    show()
    expect(screen.getByRole('region', { name: 'Verdict' })).toHaveTextContent('2 things are yours')
    expect(headings()).toEqual(['For you', 'Chief of staff', 'Work', 'System', 'Data'])
    expect(screen.getByRole('button', { name: 'For you, 2' })).toHaveAttribute('aria-pressed', 'false')
    expect(screen.getByRole('button', { name: 'All' })).toHaveAttribute('aria-pressed', 'true')
  })

  it('opens on For you on a phone', () => {
    mobile.value = true
    show()
    expect(headings()).toEqual(['For you'])
  })

  it('a tab shows its block alone, keeps the tab in the hash, and the verdict shrinks to one line', () => {
    show()
    fireEvent.click(screen.getByRole('button', { name: 'Work' }))
    expect(headings()).toEqual(['Work'])
    expect(window.location.hash).toBe('#owner?tab=work')
    expect(screen.queryByRole('region', { name: 'Verdict' })).toBeNull()
    fireEvent.click(screen.getByRole('button', { name: /2 things are yours/ }))
    expect(headings()).toEqual(['For you'])
  })

  it('opens the tab the hash names', () => {
    window.history.replaceState(null, '', '#owner?tab=system')
    show()
    expect(headings()).toEqual(['System'])
    expect(screen.getByRole('region', { name: 'Release lane' })).toHaveTextContent('nobody holds it; nothing delivering')
    expect(screen.getByRole('region', { name: 'Health' })).toHaveTextContent('critical · yours')
  })

  it('lists what is his, then what is worth a look with whose it is', () => {
    window.history.replaceState(null, '', '#owner?tab=you')
    show()
    const yours = screen.getByRole('region', { name: 'Yours' })
    expect(within(yours).getAllByRole('listitem').map(li => li.querySelector('.font-semibold')?.textContent)).toEqual([
      'Log in to TWS for the IB secondary slot', 'Look at the goal cards'])
    expect(yours).toHaveTextContent('✎ from the coordinator thread')
    expect(yours).toHaveTextContent('No approvals, questions or agent cards waiting')
    const worth = screen.getByRole('region', { name: 'Worth a look' })
    expect(within(worth).getAllByText('No goal yet')).toHaveLength(4)
    expect(worth).toHaveTextContent('1 Claude thread went quiet mid-turn')
    expect(worth).toHaveTextContent('The coordinator checks them')
  })

  it('shows six rows worth a look, and the rest on request', () => {
    standbyKnown.value = false
    window.history.replaceState(null, '', '#owner?tab=you')
    show()
    const worth = screen.getByRole('region', { name: 'Worth a look' })
    const total = Number(/Worth a look · (\d+)/.exec(worth.textContent ?? '')?.[1])
    expect(total).toBeGreaterThan(6)
    expect(within(worth).getAllByRole('listitem')).toHaveLength(6)
    fireEvent.click(within(worth).getByRole('button', { name: `Show ${total - 6} more` }))
    expect(within(worth).getAllByRole('listitem')).toHaveLength(total)
  })

  it('Make it a goal puts a draft in the chief of staff\'s box; nothing is sent', () => {
    window.history.replaceState(null, '', '#owner?tab=you')
    show()
    const worth = screen.getByRole('region', { name: 'Worth a look' })
    fireEvent.click(within(worth).getAllByRole('button', { name: 'Make it a goal' })[0])
    expect(headings()).toEqual(['Chief of staff'])
    expect((screen.getByLabelText('Goal') as HTMLTextAreaElement).value).toContain('kube-prometheus-stack-grafana targets down')
  })

  it('marks quota read days ago as stale and the plan as kept by hand', () => {
    window.history.replaceState(null, '', '#owner?tab=work')
    show()
    expect(screen.getByRole('region', { name: 'Quota' })).toHaveTextContent('stale')
    expect(screen.getByRole('region', { name: 'Quota' })).toHaveTextContent('read 5 days ago')
    expect(screen.getByRole('region', { name: 'The plan' })).toHaveTextContent('✎ by hand')
    expect(screen.getByRole('region', { name: 'Now' })).toHaveTextContent('M-2 Add a comment above Repos.')
  })
})
