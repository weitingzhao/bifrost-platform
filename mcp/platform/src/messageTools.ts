/**
 * send_message / wait_for_messages / read_message behind the envelope
 * (messageEnvelope.ts). Bodies seen by wait_for_messages are kept in this
 * process until acknowledged, so read_message can hand one over without
 * another round trip; a body not kept is fetched again from the unread
 * inbox, where it stays until acknowledged. After ack_messages a thread can
 * no longer read the body: read it first.
 */
import { MESSAGE_DATA_NOTE, ackMessages, defaultRuntimeDeps, sendMessage, waitForMessages, type RuntimeDeps } from './runtimeTools.js'
import { SUMMARY_MAX, buildEnvelope, present, type Detail, type EnvelopeInput, type InboxMessage } from './messageEnvelope.js'
import { ROSTER_PREFIX, loadRoster, resolveRoster, type RosterSeat } from './roster.js'

const KEEP = 500
const kept = new Map<string, InboxMessage>()

function keep(ms: InboxMessage[]): void {
  for (const m of ms) {
    if (!m.message_id) continue
    kept.delete(m.message_id)
    kept.set(m.message_id, m)
  }
  while (kept.size > KEEP) kept.delete(kept.keys().next().value as string)
}

/** Tests only. */
export function forgetKeptMessagesForTests(): void {
  kept.clear()
}

export interface SendInput {
  to: string
  kind?: string
  body?: string
  type?: EnvelopeInput['type']
  summary?: string
  refs?: string[]
  about?: string[]
  reply_to?: string
}

/**
 * An envelope when type is given (body becomes its text), a plain message otherwise. A roster:<seat>
 * address is resolved to the thread that sits the seat now (roster.ts), and the answer names it.
 */
export async function send(input: SendInput, d: RuntimeDeps = defaultRuntimeDeps(), roster: () => RosterSeat[] = () => loadRoster()): Promise<unknown> {
  if (input.to.startsWith(ROSTER_PREFIX)) {
    let seats: RosterSeat[]
    try {
      seats = roster()
    } catch (e) {
      return { error: `${input.to}: the roster cannot be read: ${e instanceof Error ? e.message : String(e)}` }
    }
    const r = resolveRoster(input.to, seats)
    if ('error' in r) return r
    const sent = await send({ ...input, to: r.to }, d, roster)
    return sent && typeof sent === 'object' ? { ...(sent as Record<string, unknown>), resolved_to: r.to } : sent
  }
  if (input.type) {
    if (input.summary == null) return { error: 'a message with a type needs a summary (one line)' }
    const built = buildEnvelope({
      type: input.type, summary: input.summary, text: input.body, refs: input.refs, about: input.about, reply_to: input.reply_to,
    })
    if ('error' in built) return built
    if (input.kind && input.kind !== built.kind) {
      return { error: `type ${input.type} is sent as kind ${built.kind}, not ${input.kind}: leave kind out` }
    }
    return sendMessage({ to: input.to, kind: built.kind, body: built.body }, d)
  }
  if (input.summary != null || input.refs?.length || input.about?.length || input.reply_to) {
    return { error: 'summary, refs, about and reply_to go with a type' }
  }
  if (!input.kind || !input.body) return { error: 'a message without a type needs kind and body' }
  return sendMessage({ to: input.to, kind: input.kind, body: input.body }, d)
}

export async function waitDetailed(
  input: { timeout_seconds?: number; limit?: number; detail?: Detail },
  d: RuntimeDeps = defaultRuntimeDeps(),
): Promise<unknown> {
  const got = (await waitForMessages({ timeout_seconds: input.timeout_seconds, limit: input.limit }, d)) as {
    messages?: InboxMessage[]
    [k: string]: unknown
  }
  const msgs = got.messages ?? []
  keep(msgs)
  const detail = input.detail ?? 'auto'
  const shown = msgs.map((m) => present(m, detail))
  const omitted = shown.filter((m) => m.body_omitted).length
  return {
    ...got,
    messages: shown,
    ...(omitted ? { omitted_note: `${omitted} body(ies) left out: read one with read_message before you acknowledge it` } : {}),
  }
}

export async function readMessage(input: { message_id: string }, d: RuntimeDeps = defaultRuntimeDeps()): Promise<unknown> {
  let m = kept.get(input.message_id)
  if (!m) {
    const got = (await waitForMessages({ timeout_seconds: 0, limit: 100 }, d)) as { messages?: InboxMessage[] }
    keep(got.messages ?? [])
    m = kept.get(input.message_id)
  }
  if (!m) return { error: `no unread message ${input.message_id} for this thread (an acknowledged message's body is not kept)` }
  return { message: present(m, 'full'), note: MESSAGE_DATA_NOTE }
}

export async function ack(input: { message_ids: string[] }, d: RuntimeDeps = defaultRuntimeDeps()): Promise<unknown> {
  const out = (await ackMessages(input, d)) as { acknowledged?: string[] } | undefined
  for (const id of out?.acknowledged ?? []) kept.delete(id)
  return out
}

/** Where statuses go: the platform's status board reads them (api coord.StatusBoard). */
export const STATUS_BOARD = 'controller'

/** A seat's own status: the five things the Owner asks of a line (2026-11-17). */
export interface SeatStatusInput {
  goal: string; doing: string; percent: number; percent_basis: string; waiting_on?: string; next: string
  /** The seat this session is: its charter's file name, without .md. */
  charter?: string
}

/**
 * Record where this line stands: one "status" message to the status board
 * with the existing message route (no route of its own, after card #587); the
 * board shows each seat's latest on Ops. The summary is what it is doing.
 */
export async function reportStatus(input: SeatStatusInput, d: RuntimeDeps = defaultRuntimeDeps()): Promise<unknown> {
  const fields: Record<string, unknown> = {
    goal: input.goal, doing: input.doing, percent: input.percent, percent_basis: input.percent_basis, next: input.next,
  }
  if (input.waiting_on) fields.waiting_on = input.waiting_on
  if (input.charter) fields.charter = input.charter
  const line = input.doing.replace(/[\r\n]+/g, ' ').trim()
  const summary = line.length > SUMMARY_MAX ? line.slice(0, SUMMARY_MAX - 1) + '…' : line
  return send({ to: STATUS_BOARD, type: 'status', summary, body: JSON.stringify(fields) }, d)
}
