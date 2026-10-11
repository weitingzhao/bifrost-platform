import { readFileSync, readdirSync } from 'node:fs'
import { dirname, join } from 'node:path'
import { fileURLToPath } from 'node:url'
import { render, screen } from '@testing-library/react'
import { describe, expect, it } from 'vitest'
import {
  answerRequestBody,
  parseQuestion,
  parseQuestionList,
  questionErrorMessage,
  questionLink,
  questionLinkRefused,
  type Question,
} from '@/api/questions'
import { QuestionBody } from '@/pages/question/QuestionRecord'

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

  it('treats a missing optional field as absent and a wrong type as unreadable', () => {
    const body = contract('question-open.json').body as Record<string, unknown>
    const absent = structuredClone(body)
    for (const key of ['body', 'mine', 'deliveries', 'requester_thread', 'link', 'answer', 'withdrawal']) {
      delete absent[key]
    }
    const options = absent.options as Record<string, unknown>[]
    for (const option of options) delete option.detail
    expect(parseQuestion(absent).ok).toBe(true)
    expect(parseQuestion({ ...body, answer: null }).ok).toBe(true)
    expect(parseQuestion({ ...body, withdrawal: null }).ok).toBe(true)

    const wrong: Record<string, unknown>[] = [
      { body: null },
      { mine: null },
      { number: 12.5 },
      { status: 'closed' },
      { requester: null },
      { requester_principal: 1 },
      { requester_thread: false },
      { work_id: 1 },
      { title: null },
      { recommended: 1 },
      { allow_text: null },
      { urgency: 1 },
      { meanwhile: null },
      { link: false },
      { link_text: 1 },
      { created_at: 1 },
      { expires_at: null },
      { idempotency_key: 1 },
      { request_fp: false },
      { question_fp: 1 },
      { options: null },
      { deliveries: null },
      { deliveries: [{ at: 42, result: false }] },
      { answer: 'stored' },
      { withdrawal: 1 },
    ]
    for (const patch of wrong) {
      expect(parseQuestion({ ...body, ...patch }).ok, JSON.stringify(patch)).toBe(false)
    }
    const detail = structuredClone(body)
    ;(detail.options as Record<string, unknown>[])[1].detail = 12
    expect(parseQuestion(detail).ok).toBe(false)
    const label = structuredClone(body)
    ;(label.options as Record<string, unknown>[])[0].label = null
    expect(parseQuestion(label).ok).toBe(false)
    const withAnswer = structuredClone(contract('question-answered.json').body) as Record<string, unknown>
    const answer = withAnswer.answer as Record<string, unknown>
    expect(parseQuestion({ ...withAnswer, answer: { ...answer, read: null } }).ok).toBe(true)
    expect(parseQuestion({ ...withAnswer, answer: { ...answer, approval: 'false' } }).ok).toBe(false)
    expect(parseQuestion({ ...withAnswer, answer: { ...answer, by: 1 } }).ok).toBe(false)
    expect(parseQuestion({ ...withAnswer, answer: { ...answer, text: 1 } }).ok).toBe(false)
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
    const stripped = questionLink('https://ops.bifrost.lan/#\tprogress', 'Open')
    expect(stripped.kind).toBe('anchor')
    if (stripped.kind === 'anchor') expect(stripped.href).toBe('https://ops.bifrost.lan/#progress')
    expect(questionLinkRefused('https://ops.bifrost.lan/#\tapprovals?id=ap-1')).toBe(true)
  })

  it('renders refused targets as text and allowed targets as links', () => {
    const open = parseQuestion(contract('question-open.json').body)
    expect(open.ok).toBe(true)
    if (!open.ok) return
    const show = (link: string, label: string) => {
      const question: Question = { ...open.question, link, link_text: label }
      return render(<QuestionBody question={question} />)
    }
    const encodeLetter = (target: string) => {
      const code = target.codePointAt(1)?.toString(16) ?? '61'
      return `${target[0]}%${code}${target.slice(2)}`
    }
    for (const target of ['approvals', 'maintenance', 'questions']) {
      const capital = `${target[0]?.toUpperCase() ?? ''}${target.slice(1)}`
      const encoded = encodeLetter(target)
      const twice = `${target[0] ?? ''}%25${target.codePointAt(1)?.toString(16) ?? '61'}${target.slice(2)}`
      const refused = [
        `https://ops.bifrost.lan/#\t${target}?id=ap-1`,
        `#\t${target}`,
        `#${capital}`,
        `#/${target}`,
        `#${encoded}`,
        `#%23${target}`,
        `https://ops.bifrost.lan/%23${target}`,
        `# ${target}`,
        `#a#${target}`,
        `#progress?x=#${target}`,
        `#progress?x=%23${target}`,
        `#${twice}`,
        `https://ops.bifrost.lan/${twice}`,
        `#\\${target}`,
        `#${target}\\`,
        `#%5C${target}`,
        `https://ops.bifrost.lan/#${capital}?id=1`,
        `https://ops.bifrost.lan/${capital}`,
        `https://ops.bifrost.lan/${encoded}`,
      ]
      for (const link of refused) {
        const view = show(link, 'Docs')
        expect(view.container.querySelector('a'), link).toBeNull()
        expect(screen.getByText('Docs'), link).toBeTruthy()
        view.unmount()
      }
    }
    for (const link of [
      'javascript:alert(1)',
      'data:text/html,hi',
      'https://evil.example/docs',
      'https://ops.bifrost.lan:8443/docs',
    ]) {
      const view = show(link, 'Docs')
      expect(view.container.querySelector('a'), link).toBeNull()
      expect(view.container.textContent, link).toContain('Docs')
      view.unmount()
    }
    const still = show('#p%25252572ogress', 'Open')
    expect(still.container.querySelector('a'), 'still changing after three decodes').toBeNull()
    expect(still.container.textContent).toContain('Open')
    still.unmount()
    const hash = show('#progress', 'Open')
    const hashAnchor = hash.container.querySelector('a')
    expect(hashAnchor?.getAttribute('href')).toBe('#progress')
    expect(hashAnchor?.getAttribute('target')).toBeNull()
    expect(hash.container.textContent).toContain('Agent-provided link:')
    expect(hash.container.textContent).toContain('Open')
    expect(hashAnchor?.textContent).toBe('#progress')
    hash.unmount()
    const full = show('https://ops.bifrost.lan/docs', 'Brief')
    const fullAnchor = full.container.querySelector('a')
    expect(fullAnchor?.getAttribute('href')).toBe('https://ops.bifrost.lan/docs')
    expect(fullAnchor?.getAttribute('target')).toBe('_blank')
    expect(fullAnchor?.getAttribute('rel')).toBe('noopener noreferrer')
    expect(full.container.textContent).toContain('Agent-provided link:')
    expect(full.container.textContent).toContain('Brief')
    expect(fullAnchor?.textContent).toBe('https://ops.bifrost.lan/docs')
    full.unmount()
  })
})
