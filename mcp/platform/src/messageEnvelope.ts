/**
 * Message envelope and summaries (LANE-agent-tool-comm, design v1 §4).
 *
 * Why: a message that lands in a model's context is paid for on every turn
 * after it, until the context is compacted. Most messages between seats are
 * status and notices that the receiver only needs to know exist; the body is
 * needed only for the ones it acts on. So a sender may say what a message is
 * (type) and give a one-line summary and references, and a receiver may read
 * the summaries first and fetch a body only when it needs it.
 *
 * The envelope is JSON in the message body: the platform stores the body as
 * text and keeps the five kinds it has (no migration). A body that is not an
 * envelope is still a message; its summary is its first line.
 */

/** What a message is; 'fact' is the platform's own and is not sent from a tool. */
export const ENVELOPE_TYPES = ['ask', 'decide', 'deliver', 'status', 'note'] as const
export type EnvelopeType = (typeof ENVELOPE_TYPES)[number] | 'fact'

/** The platform kind each type is stored as. */
export const KIND_OF: Record<EnvelopeType, 'ask' | 'handover' | 'note'> = {
  ask: 'ask',
  decide: 'ask',
  deliver: 'handover',
  status: 'note',
  note: 'note',
  fact: 'note',
}

export const SUMMARY_MAX = 200
export const BODY_MAX = 8192

export interface Envelope {
  v: 1
  type: EnvelopeType
  summary: string
  refs?: string[]
  about?: string[]
  reply_to?: string
  text?: string
}

export interface EnvelopeInput {
  type: (typeof ENVELOPE_TYPES)[number]
  summary: string
  text?: string
  refs?: string[]
  about?: string[]
  reply_to?: string
}

/** The kind and body to send for an envelope, or why it cannot be sent. */
export function buildEnvelope(input: EnvelopeInput): { kind: string; body: string } | { error: string } {
  const summary = input.summary.trim()
  if (summary === '' || summary.length > SUMMARY_MAX || /[\r\n]/.test(summary)) {
    return { error: `summary is one line of 1 to ${SUMMARY_MAX} characters` }
  }
  const env: Envelope = { v: 1, type: input.type, summary }
  if (input.refs?.length) env.refs = input.refs
  if (input.about?.length) env.about = input.about
  if (input.reply_to) env.reply_to = input.reply_to
  if (input.text) env.text = input.text
  const body = JSON.stringify(env)
  if (Buffer.byteLength(body) > BODY_MAX) {
    return { error: `the message is ${Buffer.byteLength(body)} bytes; at most ${BODY_MAX}: put the long part in an artifact (put_artifact) and pass its id in refs` }
  }
  return { kind: KIND_OF[input.type], body }
}

/** The envelope a body carries, or undefined for a plain body. */
export function parseEnvelope(body: string): Envelope | undefined {
  if (!body.startsWith('{')) return undefined
  try {
    const o = JSON.parse(body) as Partial<Envelope>
    if (o && o.v === 1 && typeof o.type === 'string' && typeof o.summary === 'string') return o as Envelope
  } catch {
    // a body that only looks like JSON is a plain body
  }
  return undefined
}

export interface InboxMessage {
  message_id: string
  from?: string
  to?: string
  kind?: string
  body?: string
  created_at?: string
  [k: string]: unknown
}

export type Detail = 'auto' | 'summary' | 'full'

/**
 * Whether a message is shown whole under detail 'auto': what the receiver is
 * asked to act on (an ask, an answer, a request for help, a handover) is;
 * a notice (kind note, or an envelope of type status, note or fact) is
 * summarized.
 */
export function wholeUnderAuto(m: InboxMessage): boolean {
  const env = parseEnvelope(m.body ?? '')
  if (env) return env.type === 'ask' || env.type === 'decide' || env.type === 'deliver'
  return m.kind !== 'note'
}

function firstLine(s: string): string {
  const line = s.split('\n', 1)[0].trim()
  return line.length > SUMMARY_MAX ? `${line.slice(0, SUMMARY_MAX - 1)}…` : line
}

/** A message as the receiver sees it under detail. */
export function present(m: InboxMessage, detail: Detail): Record<string, unknown> {
  const body = m.body ?? ''
  const env = parseEnvelope(body)
  const whole = detail === 'full' || (detail === 'auto' && wholeUnderAuto(m))
  const { body: _drop, ...meta } = m
  const head: Record<string, unknown> = { ...meta, bytes: Buffer.byteLength(body) }
  if (env) {
    head.type = env.type
    head.summary = env.summary
    if (env.refs) head.refs = env.refs
    if (env.about) head.about = env.about
    if (env.reply_to) head.reply_to = env.reply_to
    if (whole) head.text = env.text ?? ''
  } else {
    head.summary = firstLine(body)
    if (whole) head.body = body
  }
  if (!whole) head.body_omitted = true
  return head
}
