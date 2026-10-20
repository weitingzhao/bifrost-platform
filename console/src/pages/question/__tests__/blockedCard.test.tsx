import { cleanup, render, screen, within } from '@testing-library/react'
import { afterEach, describe, expect, it, vi } from 'vitest'
import fixture from '@/api/__fixtures__/question-blocked-card.testdb.json'
import { parseQuestion, recommendedLine, type Question } from '@/api/questions'
import { QuestionAnswerForm } from '@/pages/question/QuestionAnswerForm'
import { openQuestionItem } from '@/pages/shell/needs-you/mergedNeedsYou'
import { QuestionRow } from '@/pages/shell/needs-you/QuestionRow'
import { waitingRow } from '@/pages/shell/owner/ownerModel'

vi.mock('@/pages/ServerContactStatus', () => ({ ServerContactStatus: () => null }))

/**
 * A blocked task's card as GET /api/v1/questions/{id} answers it, made on a test database and the real
 * question store (api/internal/server/agentrt_blockedcard_test.go writes the fixture): three options,
 * the third closing the mission, the recommended one with the runtime's why as its detail.
 */
function blockedCard(): Question {
  const parsed = parseQuestion(fixture.body)
  if (!parsed.ok) throw new Error(parsed.reason)
  return parsed.question
}

const WHY = 'started 30 hours ago, used 15× its 2 hours, nothing has run'

describe('a blocked task card', () => {
  afterEach(() => {
    cleanup()
    vi.useRealTimers()
  })

  it('reads as the runtime wrote it', () => {
    const q = blockedCard()
    expect(q.options.map(o => `${o.key} ${o.label}`)).toEqual(['a Give it one more round', 'b Do not do this task', 'c Close mission M-3'])
    expect(q.recommended).toBe('c')
    expect(q.body?.split('\n')[0]).toBe(`Recommended: Close mission M-3 — ${WHY}`)
    expect(recommendedLine(q)).toBe(`Recommended: Close mission M-3 — ${WHY}`)
    expect(recommendedLine({ ...q, recommended: undefined })).toBeNull()
  })

  it('draws the recommended option as the main button with its why beside it', () => {
    vi.useFakeTimers({ toFake: ['Date'] })
    const q = blockedCard()
    vi.setSystemTime(new Date(Date.parse(q.created_at) + 60_000))
    render(<QuestionAnswerForm question={q} phase={{ kind: 'idle', notice: null }} blocked={null} onChoose={() => {}} onConfirm={() => {}} onCheckAgain={() => {}} />)
    const main = screen.getByRole('button', { name: /c Close mission M-3/ })
    expect(main.getAttribute('data-recommended')).toBe('true')
    expect(main.className).toContain('bg-foreground')
    expect(within(main).getByText(WHY)).toBeTruthy()
    expect(within(main).getByText('Recommended by the runtime')).toBeTruthy()
    for (const name of [/a Give it one more round/, /b Do not do this task/]) {
      const other = screen.getByRole('button', { name })
      expect(other.getAttribute('data-recommended')).toBeNull()
      expect(other.className).not.toContain('bg-foreground')
    }
  })

  it('says what it recommends on Needs you and For you', () => {
    const q = blockedCard()
    render(<ul><QuestionRow question={q} /></ul>)
    expect(screen.getByText(`Recommended: Close mission M-3 — ${WHY}`)).toBeTruthy()
    const row = waitingRow(openQuestionItem(q))
    expect(row.detail).toBe(`Console question · Recommended: Close mission M-3 — ${WHY}`)
    expect(openQuestionItem({ ...q, recommended: undefined }).instruction).toBe('Console question · Choose an option or write an answer')
  })
})
