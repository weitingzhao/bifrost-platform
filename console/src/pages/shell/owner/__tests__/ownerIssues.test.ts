import { describe, expect, it } from 'vitest'
import { cameBack, issuesLine } from '../OwnerIssues'
import type { OwnerIssue, OwnerIssuesAnswer } from '@/api/ownerIssues'

const issue = (over: Partial<OwnerIssue>): OwnerIssue => ({
  issue: 'ISSUE-1', title: 't', kinds: ['pain'], action: 'enforce', detail: 'd', status: 'open', evidence: ['e'], times: 3,
  first_at: '2026-11-01T00:00:00Z', last_at: '2026-11-25T00:00:00Z', version: 1, by: 'exec:claude', at: '2026-11-25T00:00:00Z', ...over,
})

// Origin: 2026-11-25 (ISSUE-104): the Owner's complaints need a place; the page says
// how many issues, how they go, and whether one came back after the law that answers it.
describe('Owner issues', () => {
  it('says how many, how they go and how many came back', () => {
    const a: OwnerIssuesAnswer = { issues: [issue({}), issue({ issue: 'ISSUE-2', action: 'law-new', times: 2 })], total: 2,
      by_action: { enforce: 1, 'law-new': 1 }, by_status: { open: 2 }, came_back_after_law: 0 }
    expect(issuesLine(a)).toBe('2 issues from 5 times he said them · new law 1 · law there, not enforced 1 · came back after their law: 0')
    // positive control: an empty register says so, it does not draw zeros
    expect(issuesLine({ issues: [], total: 0, by_action: {}, by_status: {}, came_back_after_law: 0 })).toBe('No issue recorded yet.')
  })
  it('says whether the pain came back after the law', () => {
    expect(cameBack(issue({}))).toBe('—')
    expect(cameBack(issue({ said_after_law: 0 }))).toBe('not again')
    expect(cameBack(issue({ said_after_law: 4 }))).toBe('again ×4')
  })
})
