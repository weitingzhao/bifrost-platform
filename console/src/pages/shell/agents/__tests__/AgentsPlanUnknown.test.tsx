import '@testing-library/jest-dom/vitest'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { cleanup, render, screen } from '@testing-library/react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { parseRuntimeRound } from '@/api/agentRuntimePlan'
import { planRows } from '../../owner/ownerModel'
import { ProgressOverview } from '../../progress/ProgressOverview'
import { AgentsPlan } from '../AgentsPlan'

// A round the board could not read from the agent runtime is published as
// {unknown: reason} (CUT-plan-import-2026-10-19.md §6): it is shown as
// unavailable with its reason, never as a plan with no items.

const unknownRound = { unknown: 'PG unavailable', source: 'runtime' }

function json(body: unknown) { return new Response(JSON.stringify(body), { status: 200, headers: { 'Content-Type': 'application/json' } }) }

beforeEach(() => {
  const values = new Map<string, string>()
  Object.defineProperty(window, 'localStorage', { configurable: true, value: {
    getItem: (key: string) => values.get(key) ?? null,
    setItem: (key: string, value: string) => { values.set(key, value) },
    removeItem: (key: string) => { values.delete(key) },
    clear: () => values.clear(),
  } })
})
afterEach(() => {
  cleanup()
  vi.unstubAllGlobals()
})

describe('a round the board could not read', () => {
  it('keeps the reason and the source when parsed', () => {
    expect(parseRuntimeRound(unknownRound)).toEqual({ unknown: 'PG unavailable', source: 'runtime', items: [] })
    expect(parseRuntimeRound({ unknown: '' })).toMatchObject({ unknown: 'the board gave no reason', items: [] })
    expect(parseRuntimeRound({ n: 2, items: [], source: 'runtime' })).toMatchObject({ n: 2, source: 'runtime' })
    expect(parseRuntimeRound({ n: 2, items: [] })?.unknown).toBeUndefined()
    expect(planRows(parseRuntimeRound(unknownRound))).toEqual([])
  })

  it('shows the plan as unavailable on the agents page', async () => {
    vi.stubGlobal('fetch', vi.fn(async () => json({ document: { roadmap: { round: unknownRound } }, generated_at: 'now' })))
    const client = new QueryClient({ defaultOptions: { queries: { retry: false } } })
    render(<QueryClientProvider client={client}><AgentsPlan /></QueryClientProvider>)
    expect(await screen.findByRole('alert')).toHaveTextContent('Plan unavailable: PG unavailable')
    expect(screen.queryByText('No round items published.')).not.toBeInTheDocument()
    expect(screen.getByText('The plan from the agent runtime · architecture kept by hand')).toBeInTheDocument()
    client.clear()
  })

  it('shows the round as unavailable in the overview', () => {
    const board = { roadmap: { round: parseRuntimeRound(unknownRound) } }
    render(<ProgressOverview count={{ state: 'known', count: 0 }} board={board as never} failed={false} />)
    expect(screen.getByText('Plan unavailable: PG unavailable')).toBeInTheDocument()
    expect(screen.queryByLabelText('steps')).not.toBeInTheDocument()
  })
})
