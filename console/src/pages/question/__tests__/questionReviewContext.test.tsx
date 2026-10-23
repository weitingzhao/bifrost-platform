import '@testing-library/jest-dom/vitest'
import { cleanup, fireEvent, render, screen } from '@testing-library/react'
import { afterEach, expect, it, vi } from 'vitest'
import fixture from '@/api/__fixtures__/question-blocked-card.testdb.json'
import { parseQuestion } from '@/api/questions'
import { takeSnapshot } from '@/pages/question/answerSend'
import { QuestionAnswerForm } from '@/pages/question/QuestionAnswerForm'

vi.mock('@/pages/ServerContactStatus', () => ({ ServerContactStatus: () => null }))
vi.mock('@/pages/DecisionEnvironment', () => ({ DecisionEnvironment: () => <p>STG</p> }))
afterEach(() => { cleanup(); vi.useRealTimers() })

it('requires choosing again when the reviewed sign-in or environment changes', () => {
  const parsed = parseQuestion(fixture.body)
  if (!parsed.ok) throw new Error(parsed.reason)
  const question = parsed.question
  vi.useFakeTimers({ toFake: ['Date'] })
  vi.setSystemTime(new Date(Date.parse(question.created_at) + 60_000))
  const snapshot = takeSnapshot(question, 'a', '')
  const send = vi.fn()
  const choose = vi.fn()
  const form = (context: string) => <QuestionAnswerForm question={question} phase={{ kind: 'confirming', snapshot, rejection: null }}
    blocked={null} reviewContext={context} onChoose={choose} onConfirm={send} onCheckAgain={() => {}} />
  const view = render(form('fixture-identity-stg'))
  expect(screen.getByRole('button', { name: 'Confirm answer' })).toBeEnabled()
  view.rerender(form('fixture-identity-prod'))
  expect(screen.getByRole('button', { name: 'Confirm answer' })).toBeDisabled()
  view.rerender(form('fixture-identity-stg'))
  expect(screen.getByRole('button', { name: 'Confirm answer' })).toBeDisabled()
  fireEvent.click(screen.getByRole('button', { name: 'Confirm answer' }))
  expect(send).not.toHaveBeenCalled()
  fireEvent.click(screen.getByRole('button', { name: /a Give it one more round/ }))
  expect(choose).toHaveBeenCalledTimes(1)
  expect(send).not.toHaveBeenCalled()
  expect(screen.getByRole('button', { name: 'Confirm answer' })).toBeEnabled()
})
