// 2026-11-23 the Owner (by the coordinator), 箭10 ruled the screen: tickets for a change of law and tickets for
// executive work no longer share one queue on Command. The kind comes from the data (a request's action, a
// question's owner_duty), never from a title; a question without a duty is an action marked "kind not declared".
import { describe, expect, it } from 'vitest'
import { approvalKind, kindSummary, questionKind, splitApprovals, splitQuestions } from '@/pages/shell/needs-you/ticketKind'

describe('ticket kind', () => {
  it('files law_amend under laws and every other action under actions', () => {
    expect(approvalKind({ action: 'law_amend' })).toBe('law')
    expect(approvalKind({ action: 'apply_manifest' })).toBe('action')
    const { laws, actions } = splitApprovals([{ action: 'law_amend' }, { action: 'owner_run_command' }, { action: 'start_pipeline_run' }])
    expect([laws.length, actions.length]).toEqual([1, 2])
  })
  it('reads a question by its owner duty, and says when the source did not declare one', () => {
    expect(questionKind({ owner_duty: 'goal' })).toEqual({ kind: 'law', declared: true })
    expect(questionKind({ owner_duty: 'rule' })).toEqual({ kind: 'law', declared: true })
    expect(questionKind({ owner_duty: 'irreversible' })).toEqual({ kind: 'action', declared: true })
    expect(questionKind({})).toEqual({ kind: 'action', declared: false })
    // A title that sounds like law does not make it one: only the declared field counts.
    expect(splitQuestions([{ owner_duty: undefined }, { owner_duty: 'rule' }]).laws).toHaveLength(1)
  })
  it('counts each kind apart, and an unread count is "?", never 0', () => {
    expect(kindSummary(2, 5)).toBe('2 laws · 5 actions waiting')
    expect(kindSummary(1, 1)).toBe('1 law · 1 action waiting')
    expect(kindSummary(null, 3)).toBe('? laws · 3 actions waiting')
  })
})
