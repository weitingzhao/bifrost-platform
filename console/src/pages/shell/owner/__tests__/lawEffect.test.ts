import { describe, expect, it } from 'vitest'
import { effectLine } from '../LawEffect'
import type { LawEffect } from '@/api/ownerIssues'

const e = (law: string, verdict: string): LawEffect => ({ law, issues: ['ISSUE-1'], times: 3, said_after: 0, per_week_before: 1, per_week_after: 0, verdict })

// Origin: 2026-11-25, the Owner: a place apart from the laws and the complaints that says whether the complaints under
// a law eased (「提供一个新的法下吐槽是否可以缓解和解决的地方」).
describe('Law effect', () => {
  it('counts the laws by how their issues went', () => {
    expect(effectLine([e('LAW-6', 'eased'), e('LAW-8', 'not said again'), e('LAW-14', 'not eased'), e('ZONE-1', 'no law time')]))
      .toBe('4 laws answer issues · not said again 1 · eased 1 · not eased 1 · no law time 1')
    // positive control: with no issue naming a law it says so instead of zeros
    expect(effectLine([])).toBe('No issue names a law yet.')
  })
})
