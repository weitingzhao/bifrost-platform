import { actionLink, type ActionLinkView } from '@/api/agentProgress'
import { readApprovalToken } from '@/api/approvals'
import { operatorToken } from '@/api/client'

/**
 * Owner questions. Shapes follow
 * api/internal/questions/testdata/contract/*.json.
 * A field of the wrong type makes that record unreadable: callers get a
 * reason, never a throw into the page and never a stand-in that looks healthy.
 *
 * Every question route requires X-Bifrost-Session. Reads and the answer post
 * send the fixed label below. That header is self-reported, so a record can
 * name the same session. The page and the row do not use `mine`.
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

/**
 * HTTP error whose message is words, never a raw JSON object.
 * `parsed` is true only when the body was JSON. A 4xx that did not parse is
 * not an explicit rejection.
 */
export class QuestionHttpError extends Error {
  readonly status: number
  readonly answer: QuestionAnswer | null
  readonly recordStatus: string | null
  readonly parsed: boolean

  constructor(
    message: string,
    status: number,
    answer: QuestionAnswer | null,
    recordStatus: string | null,
    parsed = false,
  ) {
    super(message)
    this.name = 'QuestionHttpError'
    this.status = status
    this.answer = answer
    this.recordStatus = recordStatus
    this.parsed = parsed
  }
}

const REFUSED_HASH = new Set(['approvals', 'maintenance', 'questions'])

function isRecord(value: unknown): value is Record<string, unknown> {
  return value != null && typeof value === 'object' && !Array.isArray(value)
}

function fail(reason: string): QuestionFailure {
  return { ok: false, reason }
}

/** Optional string. A missing key is fine. Null or any other type fails the record. */
function optionalString(raw: Record<string, unknown>, key: string): string | undefined | QuestionFailure {
  if (!(key in raw)) return undefined
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

function parseDelivery(value: unknown): QuestionFailure | undefined {
  if (!isRecord(value)) return fail('questions: delivery is not a record')
  for (const key of ['kind', 'channel', 'target', 'error']) {
    const parsed = optionalString(value, key)
    if (isFailure(parsed)) return parsed
  }
  const at = requiredString(value, 'at')
  if (isFailure(at)) return at
  const result = requiredString(value, 'result')
  if (isFailure(result)) return result
  return undefined
}

function parseDeliveries(value: unknown): QuestionFailure | undefined {
  if (!Array.isArray(value)) return fail('questions: deliveries is not a list')
  for (const item of value) {
    const parsed = parseDelivery(item)
    if (parsed != null) return parsed
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
  const options = 'options' in value ? parseOptions(value.options) : []
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
  for (const key of ['idempotency_key', 'request_fp', 'question_fp']) {
    const parsed = optionalString(value, key)
    if (isFailure(parsed)) return parsed
  }
  if ('deliveries' in value) {
    const deliveries = parseDeliveries(value.deliveries)
    if (deliveries != null) return deliveries
  }
  if ('mine' in value && typeof value.mine !== 'boolean') return fail('questions: mine is not a boolean')
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
  let parsed = false
  try {
    body = await response.json()
    parsed = true
  } catch {
    body = null
  }
  const message = questionErrorMessage(body, response.status)
  return new QuestionHttpError(message.message, response.status, message.answer, message.recordStatus, parsed)
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

const LINK_BASE = 'https://ops.bifrost.lan/'

/** WHATWG parse. A hash or a relative link is resolved on the console host so the fragment is real. */
function parseConsoleLink(raw: string): { url: URL; hashOnly: boolean } | null {
  const trimmed = raw.trim()
  if (trimmed === '') return null
  try {
    if (/^[a-zA-Z][a-zA-Z+\-.]*:/.test(trimmed)) return { url: new URL(trimmed), hashOnly: false }
    return { url: new URL(trimmed, LINK_BASE), hashOnly: trimmed.startsWith('#') }
  } catch {
    return null
  }
}

function cutQuery(segment: string): string {
  const query = segment.indexOf('?')
  return query >= 0 ? segment.slice(0, query) : segment
}

function decodeOnce(value: string): string {
  try {
    return decodeURIComponent(value)
  } catch {
    return value
  }
}

function slash(value: string): string {
  return value.replaceAll('\\', '/')
}

/**
 * Percent-decode until the value stops changing, at most three changes.
 * `\` is `/` after every pass. Still changing after that → null (refused).
 */
function decodeFixed(value: string): string | null {
  let current = slash(value)
  let changes = 0
  for (;;) {
    const next = slash(decodeOnce(current))
    if (next === current) return current
    changes += 1
    if (changes > 3) return null
    current = next
  }
}

/**
 * Page name: a second `#` anywhere in the segment is refused before the cut
 * at `?`. Decode to a fixed point and treat `\` as `/` before the name is
 * compared. Leading and trailing slashes are not part of the name.
 */
function pageName(segment: string): { name: string; refused: boolean } {
  if (segment.includes('#')) return { name: '', refused: true }
  const decoded = decodeFixed(segment)
  if (decoded == null || decoded.includes('#')) return { name: '', refused: true }
  const name = cutQuery(decoded).replace(/^\/+|\/+$/g, '').trim().toLowerCase()
  return { name, refused: false }
}

function refusedTarget(url: URL): boolean {
  const hashBody = url.hash.startsWith('#') ? url.hash.slice(1) : ''
  if (hashBody !== '') {
    const hash = pageName(hashBody)
    if (hash.refused || REFUSED_HASH.has(hash.name)) return true
  }
  const parts = url.pathname.split('/').filter(part => part !== '')
  const last = parts.length > 0 ? (parts[parts.length - 1] ?? '') : ''
  if (last === '') return false
  const path = pageName(last)
  return path.refused || REFUSED_HASH.has(path.name)
}

function hrefFromParsed(parsed: { url: URL; hashOnly: boolean }): string {
  if (parsed.hashOnly) return parsed.url.hash
  parsed.url.username = ''
  parsed.url.password = ''
  return parsed.url.href
}

/**
 * True when the parsed hash or the last path segment names the approvals page,
 * its old alias, or the questions page. The check uses the URL parser's result.
 */
export function questionLinkRefused(raw: string): boolean {
  const parsed = parseConsoleLink(raw)
  if (parsed == null) return false
  return refusedTarget(parsed.url)
}

/**
 * The refusal check and the href are the same parsed URL. A refused target is
 * text. Anything else follows `actionLink`, using that parsed href.
 */
export function questionLink(link: string, linkText?: string): ActionLinkView {
  const raw = link.trim()
  if (raw === '') return { kind: 'text', text: linkText ?? '' }
  const label = linkText?.trim() ?? ''
  const parsed = parseConsoleLink(raw)
  if (parsed == null) return { kind: 'text', text: label !== '' ? label : raw }
  if (refusedTarget(parsed.url)) return { kind: 'text', text: label !== '' ? label : raw }
  return actionLink({ text: linkText ?? '', link: hrefFromParsed(parsed), link_text: linkText })
}

/** `Q12` from the server's integer. */
export function questionNumberLabel(number: number): string {
  return `Q${number}`
}
