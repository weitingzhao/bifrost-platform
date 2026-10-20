import { cleanup, render, screen } from '@testing-library/react'
import { afterEach, describe, expect, it } from 'vitest'
import ruleAnswered from '@/api/__fixtures__/question-rule-answered.testdb.json'
import open from '@/api/__fixtures__/question-blocked-card.testdb.json'
import { parseQuestion, type Question } from '@/api/questions'
import { StoredAnswer } from '@/pages/question/QuestionRecord'

function read(body: unknown): Question {
  const parsed = parseQuestion(body)
  if (!parsed.ok) throw new Error(parsed.reason)
  return parsed.question
}

/**
 * TD-390: a card the STG card-auto-answer rule answered (api/internal/server/agentrt_cardrule_test.go writes the
 * fixture from the real question store) says it was the rule, not the Owner.
 */
describe('an answer a rule gave', () => {
  afterEach(cleanup)

  it('names the rule and says it was not the Owner', () => {
    const q = read(ruleAnswered.body)
    expect(q.answer?.channel).toBe('rule')
    render(<StoredAnswer question={q} answer={q.answer!} />)
    expect(screen.getByText('Answered by rule card-auto-answer v1, with the option the card recommends; not by you')).toBeTruthy()
    expect(screen.getByText('Close mission M-3')).toBeTruthy()
  })

  it('says when the rule took the option that changes nothing, and why (TD-393)', () => {
    const q = read(open.body)
    const why = 'the card recommends c and its AI assessment b: the rule takes the option that changes nothing'
    const ruled = { option: 'b', text: why, by: 'rule card-auto-answer v3', at: q.created_at, channel: 'rule', approval: false }
    render(<StoredAnswer question={q} answer={ruled} />)
    expect(screen.getByText('Answered by rule card-auto-answer v3, with the option that changes nothing; not by you')).toBeTruthy()
    expect(screen.getByText(`b — ${why}`)).toBeTruthy()
    expect(screen.getByText('Do not do this task')).toBeTruthy()
  })

  it('says nothing of a rule on the Owner\'s own answer', () => {
    const q = read(open.body)
    const mine = { option: 'c', by: 'admin', at: q.created_at, channel: 'console', approval: false }
    const { container } = render(<StoredAnswer question={q} answer={mine} />)
    expect(container.querySelector('[data-answer-by-rule]')).toBeNull()
  })
})
