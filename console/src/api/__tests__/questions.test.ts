import { readFileSync, readdirSync } from 'node:fs'
import { dirname, join } from 'node:path'
import { fileURLToPath } from 'node:url'
import { describe, expect, it } from 'vitest'
import {
  answerRequestBody,
  parseQuestion,
  parseQuestionList,
  questionErrorMessage,
  questionLink,
  questionLinkRefused,
} from '@/api/questions'

const CONTRACT = join(dirname(fileURLToPath(import.meta.url)), '../../../../api/internal/questions/testdata/contract')

function contract(name: string): { status: number; body: unknown } {
  return JSON.parse(readFileSync(join(CONTRACT, name), 'utf8')) as { status: number; body: unknown }
}

describe('question contract', () => {
  it('reads an open question and the inbox list from the contract files', () => {
    const open = parseQuestion(contract('question-open.json').body)
    expect(open.ok).toBe(true)
    if (!open.ok) return
    expect(open.question.id).toBe('q_0123456789abcdef')
    expect(open.question.number).toBe(12)
    expect(open.question.status).toBe('open')
    expect(open.question.recommended).toBe('b')
    const inbox = parseQuestionList(contract('inbox.json').body)
    expect(inbox.ok).toBe(true)
    if (!inbox.ok) return
    expect(inbox.questions).toHaveLength(1)
    expect(inbox.questions[0]?.status).toBe('answered')
  })

  it('builds the answer body exactly as answer-request.json, and adds text only when there is text', () => {
    const request = JSON.parse(readFileSync(join(CONTRACT, 'answer-request.json'), 'utf8')) as unknown
    expect(answerRequestBody('b', '')).toEqual(request)
    expect(answerRequestBody('none', 'see the thread')).toEqual({
      channel: 'console',
      option: 'none',
      text: 'see the thread',
    })
  })

  it('makes a record unreadable when a field has the wrong type', () => {
    const body = contract('question-open.json').body as Record<string, unknown>
    expect(parseQuestion({ ...body, title: 12 }).ok).toBe(false)
    expect(parseQuestion({ ...body, allow_text: 'false' }).ok).toBe(false)
    expect(parseQuestionList({ questions: [{ ...body, options: 'a,b' }] }).ok).toBe(false)
    expect(parseQuestionList({ items: [] }).ok).toBe(false)
  })

  it('shows every contract error as words, never as a raw object', () => {
    const names = readdirSync(CONTRACT).filter(name => name.startsWith('error-')).sort()
    expect(names.length).toBeGreaterThan(0)
    for (const name of names) {
      const file = contract(name)
      const shown = questionErrorMessage(file.body, file.status)
      const raw = JSON.stringify(file.body)
      expect(shown.message.startsWith('{'), name).toBe(false)
      expect(shown.message).not.toBe(raw)
      expect(shown.message.includes('"error"'), name).toBe(false)
      const error = (file.body as { error?: string }).error ?? ''
      expect(shown.message, name).toContain(error)
    }
    const conflict = questionErrorMessage(contract('error-already-answered.json').body, 409)
    expect(conflict.message).toContain('already answered')
    expect(conflict.answer?.option).toBe('b')
    expect(conflict.message).toContain('b')
  })
})

describe('question links', () => {
  it('shows approvals, maintenance, and questions as text, hash form and full URL', () => {
    for (const target of ['approvals', 'maintenance', 'questions']) {
      const hash = questionLink(`#${target}?id=q_0123456789abcdef`, 'Docs')
      const fullHash = questionLink(`https://ops.bifrost.lan/#${target}?id=1`, 'Docs')
      const fullPath = questionLink(`https://ops.bifrost.lan/${target}?id=1`, 'Docs')
      const mixed = questionLink(`#${target[0]?.toUpperCase()}${target.slice(1)}`, 'Docs')
      expect(hash, target).toEqual({ kind: 'text', text: 'Docs' })
      expect(fullHash, target).toEqual({ kind: 'text', text: 'Docs' })
      expect(fullPath, target).toEqual({ kind: 'text', text: 'Docs' })
      expect(mixed, target).toEqual({ kind: 'text', text: 'Docs' })
      expect(questionLinkRefused(`#${target}?id=1`)).toBe(true)
    }
    expect(questionLink('#progress', 'Open').kind).toBe('anchor')
    expect(questionLink('https://example.com/docs', 'Docs').kind).toBe('blocked')
  })
})
