import '@testing-library/jest-dom/vitest'
import { describe, expect, it } from 'vitest'
import { parseRuntimeRound } from '@/api/agentRuntimePlan'
import { planRows } from '../../owner/ownerModel'

// A round the board could not read from the agent runtime is published as
// {unknown: reason} (CUT-plan-import-2026-10-19.md §6): parsed with its reason,
// never as a plan with no items. No page shows the board's round any more (the
// board is retired, 2.5 ⑤): the plan panes read the runtime only
// (runtimePlanPanes.test.tsx).

const unknownRound = { unknown: 'PG unavailable', source: 'runtime' }

describe('a round the board could not read', () => {
  it('keeps the reason and the source when parsed', () => {
    expect(parseRuntimeRound(unknownRound)).toEqual({ unknown: 'PG unavailable', source: 'runtime', items: [] })
    expect(parseRuntimeRound({ unknown: '' })).toMatchObject({ unknown: 'the board gave no reason', items: [] })
    expect(parseRuntimeRound({ n: 2, items: [], source: 'runtime' })).toMatchObject({ n: 2, source: 'runtime' })
    expect(parseRuntimeRound({ n: 2, items: [] })?.unknown).toBeUndefined()
    expect(planRows(parseRuntimeRound(unknownRound))).toEqual([])
  })

})
