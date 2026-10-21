import '@testing-library/jest-dom/vitest'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { cleanup, render, screen } from '@testing-library/react'
import { afterEach, describe, expect, it } from 'vitest'
import fixture from '@/api/__fixtures__/agentrt-progress-owner-next.testdb.json'
import { parseOwnerNext, parseRuntimeProgress } from '@/api/agentRuntime'
import { OwnerYou } from '../OwnerYou'
import { clockTime } from '../ownerModel'
import { candidateWords, ownerNextLine } from '../ownerNextModel'
import type { OwnerConsoleData } from '../useOwnerConsole'

// When the Owner is next needed (W-31 2.5 gap 1; v4 §20.2's top line, §20.5 scenario 8). The fixture is GET /progress from
// real rows (store TestProgressSaysWhenTheOwnerIsNextNeeded, written with CONSOLE_OWNER_NEXT_FIXTURE_WRITE=1): M-1's next
// step is the Owner's, two implementations of 100 s usual before it; M-2's next step is his too, its one implementation
// waiting on M-1's, so it is due when M-1 is.
const runtime = parseRuntimeProgress(fixture)
const now = Date.parse(runtime.generated_at)
afterEach(cleanup)

describe('when the Owner is next needed', () => {
  it('reads the runtime\'s answer: the goal due first, its step and time, and every goal whose next step is his', () => {
    const next = runtime.owner_next!
    expect(next.state).toBe('estimated')
    expect(next.next).toMatchObject({ mission_id: 'M-1', step: { plan_node_id: 'R2:2.1-1', title: 's1' }, state: 'estimated', after: [], ready: false })
    expect(next.candidates.map(c => [c.mission_id, c.after])).toEqual([['M-1', []], ['M-2', ['M-1']]])
    expect(next.candidates[1].earliest).toBe(next.next!.earliest)
    expect(next.candidates[1].latest).toBe(next.next!.latest)
    // A platform before owner_next says nothing; an estimate whose time does not read is unknown, never a guess.
    expect(parseRuntimeProgress({}).owner_next).toBeUndefined()
    for (const [earliest, latest] of [['soon', 'later'], ['2026-10-21T18:20:00Z', '2026-10-21T18:10:00Z']]) { // unreadable; backwards
      expect(parseOwnerNext({ state: 'estimated', mission_id: 'M-1', step: {}, earliest, latest, candidates: [] }))
        .toEqual({ state: 'unknown', reason: 'the runtime\'s answer could not be read', candidates: [] })
    }
  })

  it('says it in one line: the goal, its step and when; or why it cannot be told and the earliest that can', () => {
    const next = runtime.owner_next!
    const lo = clockTime(Date.parse(next.next!.earliest!), now), hi = clockTime(Date.parse(next.next!.latest!), now)
    expect(ownerNextLine(next, now)).toEqual({ text: `M-1 · s1 — ${lo}–${hi}, in 3 min to 5 min`, mission: 'M-1' })
    expect(candidateWords(next.candidates[1], now)).toBe(`M-2 · (you) approve M-2's release — ${lo}–${hi}, in 3 min to 5 min, after M-1`)
    // A time that has passed is not his turn: the work before it is not done (review 477 B1).
    expect(ownerNextLine(next, Date.parse(next.next!.latest!) + 60_000)?.text).toBe(`M-1 · s1 — expected by ${hi}; that time has passed, waiting for progress`)
    // Nothing unfinished before his step: his turn by the plan, and Needs you says whether a request is up.
    const ready = { ...next.next!, ready: true, earliest: runtime.generated_at, latest: runtime.generated_at }
    expect(candidateWords(ready, now + 3_600_000)).toBe('M-1 · s1 — your turn by the plan (nothing unfinished before it); Needs you lists any request')
    const unknown = parseOwnerNext({ state: 'unknown', reason: 'M-2: the mission is paused: a wait for a decision has no usual time',
      candidates: [next.candidates[0], { ...next.candidates[1], state: 'unknown', earliest: undefined, latest: undefined }] })
    expect(ownerNextLine(unknown, now)).toEqual({ mission: null,
      text: `Unknown — M-2: the mission is paused: a wait for a decision has no usual time. The earliest that can be timed: M-1 · s1 — ${lo}–${hi}, in 3 min to 5 min` })
    expect(ownerNextLine(parseOwnerNext({ state: 'none', reason: "no open goal's next step is yours", candidates: [] }), now))
      .toEqual({ text: 'No open goal\'s next step is yours.', mission: null })
    expect(ownerNextLine(undefined, now)).toBeNull()
  })

  it('shows it on For you, under what is yours now, linked to the goal', () => {
    const data = {
      now, runtime, controls: [], controlsReadable: true, alerts: { yours: [], worth: [], noise: 0, merged: 0 }, alertsError: null,
      standbyEvidence: 'read', yours: { state: 'known', count: 0 },
      needsYou: { decisionItems: [], silentItems: [], count: { state: 'known', count: 0 }, recentCount: { state: 'known', count: 0 },
        decide: [], openQuestions: [], runtimeItems: [] },
    } as unknown as OwnerConsoleData
    render(<QueryClientProvider client={new QueryClient()}><OwnerYou data={data} onGoal={() => undefined} /></QueryClientProvider>)
    const line = document.querySelector('[data-owner-next]')!
    expect(line).toHaveTextContent(/^Next time you are needed: M-1 · s1 — /)
    expect(screen.getByRole('link', { name: /^M-1 · s1 — / })).toHaveAttribute('href', '#work?goal=M-1')
    cleanup()
    render(<QueryClientProvider client={new QueryClient()}><OwnerYou data={{ ...data, runtime: undefined }} onGoal={() => undefined} /></QueryClientProvider>)
    expect(document.querySelector('[data-owner-next]')).toBeNull()
  })
})
