import '@testing-library/jest-dom/vitest'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { cleanup, render, screen, within } from '@testing-library/react'
import { afterEach, describe, expect, it } from 'vitest'
import presenceFixture from '@/api/__fixtures__/agentrt-progress-presence.testdb.json'
import { AGENT_RUNTIME_POLL_MS, parseRuntimeProgress } from '@/api/agentRuntime'
import { offlineHostItem, offlineHosts } from '@/pages/shell/needs-you/mergedNeedsYou'
import { OwnerCrew } from '../OwnerCrew'
import { RuntimeHealthNotice } from '../RuntimeHealthNotice'
import { executorWords, offeredWords } from '../crewModel'

// W-31 3.5. The fixture is GET /agentrt/progress from real rows (store TestConsolePresenceFixture, written with
// CONSOLE_PRESENCE_FIXTURE_WRITE=1) after one presence pass: h1 online offering claude, h2 offering codex and stopped
// 45 seconds before, claude's account cooling down for an hour; so h2 is reported offline, both executors are
// unavailable, each for its reason, and every job is left with no executor.
const progress = parseRuntimeProgress(presenceFixture)
const at = Date.parse(progress.generated_at)
afterEach(cleanup)

describe('presence', () => {
  it('reads what each host offers, the executors and the jobs left with none, and nothing from a platform before 3.5', () => {
    const [h1, h2] = progress.hosts
    expect(h1).toMatchObject({ host_id: 'h1', online: true, reported_offline: false, offered: [{ executor: 'exec:claude', provider: 'anthropic', model: 'opus' }] })
    expect(h2).toMatchObject({ host_id: 'h2', online: false, reported_offline: true, offered: [{ executor: 'exec:codex' }] })
    expect(progress.executors?.map(e => [e.executor, e.available])).toEqual([['exec:claude', false], ['exec:codex', false]])
    expect(progress.jobs_without_executor).toEqual(['chief_of_staff', 'implementer', 'planner', 'reviewer', 'verifier'])
    const before = parseRuntimeProgress({ hosts: [{ host_id: 'h1', online: false }] })
    expect(before.executors).toBeUndefined()
    expect(before.hosts[0].offered).toBeUndefined()
    expect(offeredWords(before.hosts[0])).toBe('')
    expect(offeredWords(parseRuntimeProgress({ hosts: [{ host_id: 'h1', offered: null }] }).hosts[0])).toBe('What it offers is not recorded yet')
    expect(executorWords({ executor: 'exec:cursor', available: null, why: 'what h1 offers is not recorded yet', hosts: [], jobs: [] }))
      .toBe('cursor not known: what h1 offers is not recorded yet')
  })

  it('says in red, at the top of Work and System, the host offline, each executor unavailable and the jobs left with none', () => {
    render(<RuntimeHealthNotice progress={progress} now={at} />)
    const alert = screen.getByRole('alert', { name: 'Agent runtime health' })
    expect(alert).toHaveTextContent('Host h2 is offline: its last poll was')
    expect(alert).not.toHaveTextContent('Host h1 is offline')
    expect(alert).toHaveTextContent("exec:claude is unavailable: the claude account's pools cool down until")
    expect(alert).toHaveTextContent('exec:codex is unavailable: host h2 offers it but has not polled since')
    expect(alert).toHaveTextContent('No executor can take these jobs now: chief_of_staff (the chief of staff cannot answer you), implementer, planner, reviewer, verifier.')
  })

  it('lists on the Crew card what each host offers, and the executors', () => {
    const client = new QueryClient({ defaultOptions: { queries: { retry: false } } })
    render(<QueryClientProvider client={client}><OwnerCrew hosts={progress.hosts} live={progress.live} missions={progress.work.missions}
      now={at} read="ok" executors={progress.executors} unserved={progress.jobs_without_executor} /></QueryClientProvider>)
    expect(screen.getByText('Crew · 1 of 2 hosts online')).toBeInTheDocument()
    const h1 = screen.getByText('h1').closest('li')!, h2 = screen.getByText('h2').closest('li')!
    expect(within(h1).getByText('Offers claude (opus)')).toBeInTheDocument()
    expect(within(h2).getByText('Offers codex (gpt-x)')).toBeInTheDocument()
    expect(within(h2).getByText(/host lost · last poll/)).toBeInTheDocument()
    expect(screen.getByText('Executors')).toBeInTheDocument()
    expect(screen.getByText(/^claude unavailable: the claude account's pools cool down until/)).toBeInTheDocument()
    expect(screen.getByText(/^codex unavailable: host h2 offers it/)).toBeInTheDocument()
    expect(screen.getByText(/^No executor can take these jobs now: chief_of_staff/)).toBeInTheDocument()
  })

  it("puts the offline host on For you, read from the runtime's hosts", () => {
    const lost = offlineHosts(progress.hosts)
    expect(lost.map(h => h.host_id)).toEqual(['h2'])
    expect(offlineHostItem(lost[0])).toMatchObject({ id: 'h2', title: 'h2 stopped polling',
      instruction: 'For you · Start its agentd again, or drain the host on Work if it is down on purpose' })
    const never = parseRuntimeProgress({ hosts: [{ host_id: 'b', reported_offline: true, last_poll_at: '2026-10-23T19:00:00Z' },
      { host_id: 'a', reported_offline: true }, { host_id: 'c', reported_offline: false }] }).hosts
    expect(offlineHosts(never).map(h => h.host_id)).toEqual(['a', 'b'])
  })

  it("keeps the Console's share of the minute: it reads the runtime every 5 seconds", () => {
    // The platform records and tells a stopped host within 35 s of its last poll (controller
    // TestPresenceIsToldWithinTheBudget); the Crew card, the red notice, For you and the Chief page read every
    // AGENT_RUNTIME_POLL_MS, so each says it within 40 s.
    expect(AGENT_RUNTIME_POLL_MS).toBeLessThanOrEqual(5_000)
  })
})
