import { actionLink, type ActionLinkView } from '@/api/agentProgress'
import { readApprovalToken } from '@/api/approvals'
import { operatorToken } from '@/api/client'

/**
 * Owner questions. Shapes follow
 * api/internal/questions/testdata/contract/*.json.
 * A field of the wrong type makes that record unreadable: callers get a
 * reason, never a throw into the page and never a stand-in that looks healthy.
 *
 * Every question route requires X-Bifrost-Session. The console is not an agent
 * thread, so reads and the answer post send the fixed label below. It is not
 * the requester, so `mine` stays false.
 */
export const QUESTION_SESSION = 'owner-console'

export const QUESTION_ID_PATTERN = /^q_[0-9a-f]{16}$/

const QUESTION_STATUSES = ['open', 'answered', 'withdrawn', 'expired'] as const

export type QuestionStatus = (typeof QUESTION_STATUSES)[number]

export type QuestionOption = {
  key: string
  label: string
  detail?: string
}

export type QuestionRead = {
  at: string
  session: string
  principal: string
}

/** The Owner's reply. `approval` is the server's constant, not a grant. */
export type QuestionAnswer = {
  option?: string
  text?: string
  by: string
  at: string
  channel: string
  approval: boolean
  read?: QuestionRead
}

export type QuestionWithdrawal = {
  session: string
  principal: string
  at: string
  reason: string
}

export type Question = {
  id: string
  number: number
  status: QuestionStatus
  requester: string
  requester_principal?: string
  requester_thread?: string
  work_id?: string
  title: string
  body?: string
  options: QuestionOption[]
  recommended?: string
  allow_text: boolean
  urgency: string
  meanwhile: string
  link?: string
  link_text?: string
  created_at: string
  expires_at: string
  answer?: QuestionAnswer
  withdrawal?: QuestionWithdrawal
  mine?: boolean
}

export type QuestionFailure = { ok: false; reason: string }

export type QuestionRecordResult = { ok: true; question: Question } | QuestionFailure

export type QuestionListResult = { ok: true; questions: Question[] } | QuestionFailure

/** HTTP error whose message is words, never a raw JSON object. */
export class QuestionHttpError extends Error {
  readonly status: number
  readonly answer: QuestionAnswer | null
  readonly recordStatus: string | null

  constructor(message: string, status: number, answer: QuestionAnswer | null, recordStatus: string | null) {
    super(message)
    this.name = 'QuestionHttpError'
    this.status = status
    this.answer = answer
    this.recordStatus = recordStatus
  }
}

const REFUSED_HASH = new Set(['approvals', 'maintenance', 'questions'])

function isRecord(value: unknown): value is Record<string, unknown> {
  return value != null && typeof value === 'object' && !Array.isArray(value)
}

function fail(reason: string): QuestionFailure {
  return { ok: false, reason }
}

/** Optional string. Absent is fine. The wrong type fails the record. */
function optionalString(raw: Record<string, unknown>, key: string): string | undefined | QuestionFailure {
  if (!(key in raw) || raw[key] == null) return undefined
  if (typeof raw[key] !== 'string') return fail(`questions: ${key} is not text`)
  return raw[key] as string
}

function requiredString(raw: Record<string, unknown>, key: string): string | QuestionFailure {
  if (typeof raw[key] !== 'string') return fail(`questions: ${key} is not text`)
  return raw[key] as string
}

function isFailure(value: unknown): value is QuestionFailure {
  return isRecord(value) && value.ok === false && typeof value.reason === 'string'
}

function parseOption(value: unknown): QuestionOption | QuestionFailure {
  if (!isRecord(value)) return fail('questions: option is not a record')
  const key = requiredString(value, 'key')
  if (isFailure(key)) return key
  const label = requiredString(value, 'label')
  if (isFailure(label)) return label
  const detail = optionalString(value, 'detail')
  if (isFailure(detail)) return detail
  const option: QuestionOption = { key, label }
  if (detail != null && detail !== '') option.detail = detail
  return option
}

function parseOptions(value: unknown): QuestionOption[] | QuestionFailure {
  if (value == null) return []
  if (!Array.isArray(value)) return fail('questions: options is not a list')
  const options: QuestionOption[] = []
  for (const item of value) {
    const option = parseOption(item)
    if (isFailure(option)) return option
    options.push(option)
  }
  return options
}

function parseRead(value: unknown): QuestionRead | undefined | QuestionFailure {
  if (value == null) return undefined
  if (!isRecord(value)) return fail('questions: answer.read is not a record')
  const at = requiredString(value, 'at')
  if (isFailure(at)) return at
  const session = requiredString(value, 'session')
  if (isFailure(session)) return session
  const principal = requiredString(value, 'principal')
  if (isFailure(principal)) return principal
  return { at, session, principal }
}

/** An answer object from a question or from a 409 body. Wrong types fail. */
export function parseQuestionAnswer(value: unknown): QuestionAnswer | QuestionFailure {
  if (!isRecord(value)) return fail('questions: answer is not a record')
  const by = requiredString(value, 'by')
  if (isFailure(by)) return by
  const at = requiredString(value, 'at')
  if (isFailure(at)) return at
  const channel = requiredString(value, 'channel')
  if (isFailure(channel)) return channel
  if (typeof value.approval !== 'boolean') return fail('questions: answer.approval is not a boolean')
  const option = optionalString(value, 'option')
  if (isFailure(option)) return option
  const text = optionalString(value, 'text')
  if (isFailure(text)) return text
  const read = parseRead(value.read)
  if (isFailure(read)) return read
  const answer: QuestionAnswer = { by, at, channel, approval: value.approval }
  if (option != null && option !== '') answer.option = option
  if (text != null && text !== '') answer.text = text
  if (read != null) answer.read = read
  return answer
}

function parseWithdrawal(value: unknown): QuestionWithdrawal | undefined | QuestionFailure {
  if (value == null) return undefined
  if (!isRecord(value)) return fail('questions: withdrawal is not a record')
  const session = requiredString(value, 'session')
  if (isFailure(session)) return session
  const principal = requiredString(value, 'principal')
  if (isFailure(principal)) return principal
  const at = requiredString(value, 'at')
  if (isFailure(at)) return at
  const reason = requiredString(value, 'reason')
  if (isFailure(reason)) return reason
  return { session, principal, at, reason }
}

function parseDeliveries(value: unknown): QuestionFailure | undefined {
  if (value == null) return undefined
  if (!Array.isArray(value)) return fail('questions: deliveries is not a list')
  for (const item of value) {
    if (!isRecord(item)) return fail('questions: delivery is not a record')
  }
  return undefined
}

/** One question object, as GET /api/v1/questions/{id} returns it. */
export function parseQuestion(value: unknown): QuestionRecordResult {
  if (!isRecord(value)) return fail('questions: record is not an object')
  if (typeof value.id !== 'string' || !QUESTION_ID_PATTERN.test(value.id)) {
    return fail('questions: id is not a question id')
  }
  if (typeof value.number !== 'number' || !Number.isInteger(value.number)) {
    return fail('questions: number is not an integer')
  }
  if (typeof value.status !== 'string' || !(QUESTION_STATUSES as readonly string[]).includes(value.status)) {
    return fail('questions: status is not a question status')
  }
  const requester = requiredString(value, 'requester')
  if (isFailure(requester)) return requester
  const title = requiredString(value, 'title')
  if (isFailure(title)) return title
  const urgency = requiredString(value, 'urgency')
  if (isFailure(urgency)) return urgency
  const meanwhile = requiredString(value, 'meanwhile')
  if (isFailure(meanwhile)) return meanwhile
  if (typeof value.allow_text !== 'boolean') return fail('questions: allow_text is not a boolean')
  const created = requiredString(value, 'created_at')
  if (isFailure(created)) return created
  const expires = requiredString(value, 'expires_at')
  if (isFailure(expires)) return expires
  const options = parseOptions(value.options)
  if (isFailure(options)) return options
  const principal = optionalString(value, 'requester_principal')
  if (isFailure(principal)) return principal
  const thread = optionalString(value, 'requester_thread')
  if (isFailure(thread)) return thread
  const workId = optionalString(value, 'work_id')
  if (isFailure(workId)) return workId
  const body = optionalString(value, 'body')
  if (isFailure(body)) return body
  const recommended = optionalString(value, 'recommended')
  if (isFailure(recommended)) return recommended
  const link = optionalString(value, 'link')
  if (isFailure(link)) return link
  const linkText = optionalString(value, 'link_text')
  if (isFailure(linkText)) return linkText
  let answer: QuestionAnswer | undefined
  if (value.answer != null) {
    const parsed = parseQuestionAnswer(value.answer)
    if (isFailure(parsed)) return parsed
    answer = parsed
  }
  const withdrawal = parseWithdrawal(value.withdrawal)
  if (isFailure(withdrawal)) return withdrawal
  const deliveries = parseDeliveries(value.deliveries)
  if (deliveries != null) return deliveries
  if ('mine' in value && value.mine != null && typeof value.mine !== 'boolean') {
    return fail('questions: mine is not a boolean')
  }
  const question: Question = {
    id: value.id,
    number: value.number,
    status: value.status as QuestionStatus,
    requester,
    title,
    options,
    allow_text: value.allow_text,
    urgency,
    meanwhile,
    created_at: created,
    expires_at: expires,
  }
  if (principal != null && principal !== '') question.requester_principal = principal
  if (thread != null && thread !== '') question.requester_thread = thread
  if (workId != null && workId !== '') question.work_id = workId
  if (body != null && body !== '') question.body = body
  if (recommended != null && recommended !== '') question.recommended = recommended
  if (link != null && link !== '') question.link = link
  if (linkText != null && linkText !== '') question.link_text = linkText
  if (answer != null) question.answer = answer
  if (withdrawal != null) question.withdrawal = withdrawal
  if (value.mine === true) question.mine = true
  return { ok: true, question }
}

/** `{ questions: [...] }` — the list and inbox envelope. */
export function parseQuestionList(value: unknown): QuestionListResult {
  if (!isRecord(value) || !Array.isArray(value.questions)) {
    return fail('questions: unexpected list shape')
  }
  const questions: Question[] = []
  for (const item of value.questions) {
    const parsed = parseQuestion(item)
    if (!parsed.ok) return parsed
    questions.push(parsed.question)
  }
  return { ok: true, questions }
}

function recordStatusOf(value: unknown): string | null {
  if (!isRecord(value) || typeof value.status !== 'string') return null
  return value.status
}

/**
 * Words for an error body. Extra fields (the stored answer, the status) are
 * named in the sentence. The object itself is never the message.
 */
export function questionErrorMessage(body: unknown, status: number): {
  message: string
  answer: QuestionAnswer | null
  recordStatus: string | null
} {
  const fallback = `HTTP ${status}`
  if (!isRecord(body)) return { message: fallback, answer: null, recordStatus: null }
  const head = typeof body.error === 'string' && body.error.trim() !== '' ? body.error.trim() : fallback
  const recordStatus = recordStatusOf(body)
  let answer: QuestionAnswer | null = null
  const parts = [head]
  if (recordStatus != null && recordStatus !== head) parts.push(recordStatus)
  if ('answer' in body && body.answer != null) {
    const parsed = parseQuestionAnswer(body.answer)
    if (isFailure(parsed)) {
      parts.push('The stored answer could not be read.')
    } else {
      answer = parsed
      parts.push(storedAnswerWords(parsed))
    }
  }
  return { message: parts.join(' — '), answer, recordStatus }
}

/** The stored reply in words: the option key, then the text. */
export function storedAnswerWords(answer: QuestionAnswer): string {
  const option = answer.option ?? ''
  const text = answer.text ?? ''
  if (option !== '' && text !== '') return `${option} — ${text}`
  if (option !== '') return option
  if (text !== '') return text
  return 'an answer with no option and no text'
}

async function readError(response: Response): Promise<QuestionHttpError> {
  let body: unknown
  try {
    body = await response.json()
  } catch {
    body = null
  }
  const parsed = questionErrorMessage(body, response.status)
  return new QuestionHttpError(parsed.message, response.status, parsed.answer, parsed.recordStatus)
}

function readHeaders(): Headers {
  const headers = new Headers()
  headers.set('X-Bifrost-Session', QUESTION_SESSION)
  const token = operatorToken() !== '' ? operatorToken() : readApprovalToken()
  if (token !== '') headers.set('Authorization', `Bearer ${token}`)
  return headers
}

async function questionsRead(path: string): Promise<Response> {
  const response = await fetch(path, { headers: readHeaders() })
  if (!response.ok) throw await readError(response)
  return response
}

/** `open`, `unread`, `all`, or one status. A bad envelope throws; it is not an empty list. */
export async function fetchQuestionList(status: string): Promise<Question[]> {
  const response = await questionsRead(`/api/v1/questions?status=${encodeURIComponent(status)}`)
  const parsed = parseQuestionList(await response.json())
  if (!parsed.ok) throw new Error(parsed.reason)
  return parsed.questions
}

export async function fetchQuestion(id: string): Promise<Question> {
  const response = await questionsRead(`/api/v1/questions/${encodeURIComponent(id)}`)
  const parsed = parseQuestion(await response.json())
  if (!parsed.ok) throw new Error(parsed.reason)
  return parsed.question
}

/**
 * Body of POST .../answer. Keys match answer-request.json (`channel`, `option`);
 * `text` is added only when this reply includes words.
 */
export function answerRequestBody(option: string, text: string): { channel: 'console'; option: string; text?: string } {
  const body: { channel: 'console'; option: string; text?: string } = { channel: 'console', option }
  if (text !== '') body.text = text
  return body
}

export type PostedAnswer = {
  id: string
  status: QuestionStatus
  answer: QuestionAnswer
}

/**
 * `answer-response.json` is not a full question. It carries the id, the status,
 * and the answer that was stored.
 */
export function parsePostedAnswer(value: unknown): { ok: true; posted: PostedAnswer } | QuestionFailure {
  if (!isRecord(value)) return fail('questions: answer response is not an object')
  if (typeof value.id !== 'string' || !QUESTION_ID_PATTERN.test(value.id)) {
    return fail('questions: id is not a question id')
  }
  if (typeof value.status !== 'string' || !(QUESTION_STATUSES as readonly string[]).includes(value.status)) {
    return fail('questions: status is not a question status')
  }
  const answer = parseQuestionAnswer(value.answer)
  if (isFailure(answer)) return answer
  return { ok: true, posted: { id: value.id, status: value.status as QuestionStatus, answer } }
}

/** Admin token from the approval-token field. Does not use the operator token. */
export async function postQuestionAnswer(id: string, option: string, text: string): Promise<PostedAnswer> {
  const token = readApprovalToken()
  const headers = new Headers()
  headers.set('Content-Type', 'application/json')
  headers.set('X-Bifrost-Session', QUESTION_SESSION)
  if (token !== '') headers.set('Authorization', `Bearer ${token}`)
  const response = await fetch(`/api/v1/questions/${encodeURIComponent(id)}/answer`, {
    method: 'POST',
    headers,
    body: JSON.stringify(answerRequestBody(option, text)),
  })
  if (!response.ok) throw await readError(response)
  const parsed = parsePostedAnswer(await response.json())
  if (!parsed.ok) throw new Error(parsed.reason)
  return parsed.posted
}

/** A fetch that never produced an HTTP response: timeout, abort, or a dropped connection. */
export function isLostResponse(err: unknown): boolean {
  if (err instanceof QuestionHttpError) return false
  if (err instanceof TypeError) return true
  return err instanceof DOMException && err.name === 'AbortError'
}

function hashSegment(raw: string): string {
  const mark = raw.indexOf('#')
  if (mark < 0) return ''
  let fragment = raw.slice(mark + 1)
  const query = fragment.indexOf('?')
  if (query >= 0) fragment = fragment.slice(0, query)
  try {
    fragment = decodeURIComponent(fragment)
  } catch {
    // keep the raw fragment
  }
  return fragment.replace(/^\/+|\/+$/g, '').toLowerCase()
}

function pathSegment(raw: string): string {
  try {
    const url = new URL(raw)
    const path = url.pathname.replace(/^\/+|\/+$/g, '')
    if (path === '') return ''
    const last = path.split('/').pop() ?? ''
    return last.toLowerCase()
  } catch {
    return ''
  }
}

/**
 * True when the link's hash (before `?`, lower-cased) or a full URL's last
 * path segment is the approvals page, its old alias, or the questions page.
 */
export function questionLinkRefused(raw: string): boolean {
  const trimmed = raw.trim()
  if (trimmed === '') return false
  if (REFUSED_HASH.has(hashSegment(trimmed))) return true
  if (REFUSED_HASH.has(pathSegment(trimmed))) return true
  return false
}

/**
 * Same destinations as `actionLink`, except a refused target is text.
 * The record's words are the label, never the href of a refused target.
 */
export function questionLink(link: string, linkText?: string): ActionLinkView {
  const raw = link.trim()
  if (raw === '') return { kind: 'text', text: linkText ?? '' }
  if (questionLinkRefused(raw)) {
    const label = linkText?.trim() ?? ''
    return { kind: 'text', text: label !== '' ? label : raw }
  }
  return actionLink({ text: linkText ?? '', link: raw, link_text: linkText })
}

/** `Q12` from the server's integer. */
export function questionNumberLabel(number: number): string {
  return `Q${number}`
}
