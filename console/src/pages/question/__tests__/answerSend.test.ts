import { readFileSync } from 'node:fs'
import { dirname, join } from 'node:path'
import { fileURLToPath } from 'node:url'
import { QueryClient } from '@tanstack/react-query'
import { beforeEach, describe, expect, it } from 'vitest'
import { parseQuestion, type Question, type QuestionAnswer } from '@/api/questions'
import {
  checkAgainAvailable,
  confirmIsSendable,
  initialAnswerSend,
  isTerminalPhase,
  reduceAnswerSend,
  snapshotStale,
  takeSnapshot,
  type AnswerSendEvent,
  type AnswerSendState,
} from '@/pages/question/answerSend'
import {
  answerSendSnapshot,
  answerSendStoreHas,
  answerSendStoreSize,
  dispatchAnswerSend,
  resetAnswerSendStore,
} from '@/pages/question/answerSendStore'
import { acceptQuestionRead, questionItemKey } from '@/pages/question/useAnswerSend'

const CONTRACT = join(dirname(fileURLToPath(import.meta.url)), '../../../../../api/internal/questions/testdata/contract')
const OTHER = 'q_0000000000000002'

function contractQuestion(name: string): Question {
  const file = JSON.parse(readFileSync(join(CONTRACT, name), 'utf8')) as { body: unknown }
  const parsed = parseQuestion(file.body)
  if (!parsed.ok) throw new Error(parsed.reason)
  return parsed.question
}

function eventName(event: AnswerSendEvent, changed: Question): string {
  if (event.type === 'read') {
    if (event.record === changed) return 'read(open, option b changed)'
    return `read(${event.record.status})`
  }
  if (event.type === 'postRejected') {
    return event.answer == null ? 'postRejected(no answer)' : 'postRejected(409 with answer)'
  }
  return event.type
}

function snapshotOf(state: AnswerSendState) {
  const phase = state.phase
  if (phase.kind === 'confirming' || phase.kind === 'sending' || phase.kind === 'unknown') return phase.snapshot
  return null
}

function fail(names: string[], length: number, message: string): never {
  throw new Error(`failing sequence: ${names.slice(0, length).join(' → ')}\n${message}`)
}

function check(names: string[], length: number, condition: boolean, message: string): void {
  if (!condition) fail(names, length, message)
}

/** Invariants 1–4 and 6, on this state and on the transition that produced it. */
function assertStep(before: AnswerSendState, event: AnswerSendEvent, after: AnswerSendState, names: string[], length: number): void {
  check(names, length, confirmIsSendable(after) === (after.phase.kind === 'confirming'), 'Confirm answer is sendable outside confirming')
  check(names, length, after.phase.kind !== 'unknown' || checkAgainAvailable(after), 'unknown has no Check again')

  if (event.type === 'postFailed') {
    if (isTerminalPhase(before.phase)) {
      check(names, length, isTerminalPhase(after.phase) && after.phase.kind === before.phase.kind, 'postFailed downgraded a known terminal state')
    } else {
      check(names, length, after.phase.kind === 'unknown', 'postFailed did not lead to unknown')
    }
  }

  if (before.phase.kind === 'unknown' && event.type !== 'read') {
    check(names, length, after.phase.kind === 'unknown', `unknown left through ${event.type}`)
  }

  if (before.phase.kind === 'unknown' && event.type === 'read' && event.record.status === 'open') {
    const snap = snapshotOf(before)
    const stale = snap != null && snapshotStale(event.record, snap)
    if (stale) {
      check(names, length, after.phase.kind === 'unknown', 'a changed option during unknown left unknown')
      check(names, length, checkAgainAvailable(after), 'Check again needs a snapshot')
    } else {
      check(names, length, after.phase.kind === 'idle', 'read(open) did not return to idle')
      check(names, length, !confirmIsSendable(after), 'read(open) restored a sendable row')
    }
  }

  if (before.phase.kind === 'unknown' && event.type === 'read' && event.record.status === 'answered') {
    check(names, length, isTerminalPhase(after.phase), 'read(answered) did not reach a terminal state')
  }

  if (isTerminalPhase(before.phase) && event.type === 'read' && event.record.status === 'open') {
    check(names, length, after.phase.kind === before.phase.kind, 'an open read downgraded a known terminal state')
    if (before.phase.kind === 'saved' && after.phase.kind === 'saved') {
      check(names, length, after.phase.answer === before.phase.answer, 'an open read replaced a saved answer')
    }
  }

  if (before.phase.kind === 'confirming' && event.type === 'read' && event.record.status === 'open') {
    const stale = snapshotStale(event.record, before.phase.snapshot)
    if (stale) {
      check(names, length, after.phase.kind === 'idle' && !confirmIsSendable(after), 'a changed option left the old confirm sendable')
    } else {
      check(names, length, after.phase.kind === 'confirming', 'a matching open read cleared the confirm row')
    }
  }

  if (event.type === 'leave') {
    if (before.phase.kind === 'confirming') {
      check(names, length, after.phase.kind === 'idle' && !confirmIsSendable(after), 'leave kept a choice')
    } else {
      check(names, length, after.phase.kind === before.phase.kind, 'leave changed a phase other than confirming')
      if (before.phase.kind === 'saved' && after.phase.kind === 'saved') {
        check(names, length, after.phase.answer === before.phase.answer, 'leave replaced a saved answer')
      }
    }
  }

  if (event.type === 'return') {
    check(names, length, after === before, 'return rebuilt the entry')
  }

  if (event.type === 'choose' && (before.phase.kind === 'idle' || before.phase.kind === 'confirming')) {
    check(names, length, after.phase.kind === 'confirming' && confirmIsSendable(after), 'choose did not arm Confirm answer')
  }

  if (event.type === 'cancel' && before.phase.kind === 'confirming') {
    check(names, length, after.phase.kind === 'idle' && !confirmIsSendable(after), 'cancel left Confirm answer sendable')
  }

  if (
    event.type === 'postRejected' &&
    event.answer == null &&
    event.recordStatus !== 'answered' &&
    event.recordStatus !== 'withdrawn' &&
    event.recordStatus !== 'expired'
  ) {
    if (before.phase.kind === 'confirming' || before.phase.kind === 'sending') {
      check(names, length, after.phase.kind === 'confirming', 'a rejection without an answer did not return to confirming')
    }
  }

  if (event.type === 'postRejected' && (isTerminalPhase(before.phase) || before.phase.kind === 'unknown')) {
    check(names, length, after.phase.kind === before.phase.kind, 'postRejected left a known state')
  }
}

function enumerate(list: AnswerSendEvent[], depth: number, start: AnswerSendState, changed: Question): number {
  const names: string[] = []
  let enumerated = 0
  const walk = (before: AnswerSendState, length: number) => {
    if (length === depth) return
    for (const event of list) {
      names[length] = eventName(event, changed)
      const after = reduceAnswerSend(before, event)
      enumerated += 1
      assertStep(before, event, after, names, length + 1)
      walk(after, length + 1)
    }
  }
  walk(start, 0)
  return enumerated
}

function qid(n: number): string {
  return `q_${n.toString(16).padStart(16, '0')}`
}

describe('answer send reducer', () => {
  const open = contractQuestion('question-open.json')
  const changed: Question = structuredClone(open)
  const choice = changed.options.find(option => option.key === 'b')
  if (choice == null) throw new Error('option b missing')
  choice.label = 'Later'
  const answered = contractQuestion('question-answered.json')
  const stored = answered.answer
  if (stored == null) throw new Error('answered contract has no answer')
  const answer: QuestionAnswer = stored
  const id = open.id
  const snapshot = takeSnapshot(open, 'b', '')
  const start = reduceAnswerSend(initialAnswerSend(id), { type: 'choose', id, snapshot })

  const original: AnswerSendEvent[] = [
    { type: 'confirm', id },
    { type: 'postSaved', id, answer },
    { type: 'postFailed', id },
    { type: 'postRejected', id, recordStatus: 'answered', answer, message: 'already answered' },
    { type: 'read', id, record: open },
    { type: 'read', id, record: changed },
    { type: 'read', id, record: answered },
    { type: 'readFailed', id },
    { type: 'checkAgain', id },
  ]

  const full: AnswerSendEvent[] = [
    ...original,
    { type: 'choose', id, snapshot },
    { type: 'cancel', id },
    { type: 'postRejected', id, recordStatus: null, answer: null, message: 'rejected' },
    { type: 'leave', id },
    { type: 'return', id },
  ]

  beforeEach(() => {
    resetAnswerSendStore()
  })

  it('enumerates every sequence of up to five events and holds the send invariants', () => {
    expect(start.phase.kind).toBe('confirming')
    expect(snapshotStale(open, snapshot)).toBe(false)
    expect(snapshotStale(changed, snapshot)).toBe(true)
    expect(enumerate(original, 5, start, changed)).toBe(9 + 81 + 729 + 6561 + 59049)
  })

  it('enumerates leave, return, choose, cancel, and a rejection without an answer', () => {
    expect(full).toHaveLength(14)
    expect(enumerate(full, 5, start, changed)).toBe(14 + 196 + 2744 + 38416 + 537824)
  }, 30_000)

  it('drops an event for a different question id and keeps a late save after leave', () => {
    const failed = reduceAnswerSend(start, { type: 'postFailed', id: OTHER })
    expect(failed).toBe(start)
    const left = reduceAnswerSend(start, { type: 'leave', id })
    expect(left.questionId).toBe(id)
    expect(left.phase.kind).toBe('idle')
    expect(confirmIsSendable(left)).toBe(false)
    const back = reduceAnswerSend(left, { type: 'return', id })
    expect(back).toBe(left)
    const sending = reduceAnswerSend(start, { type: 'confirm', id })
    const away = reduceAnswerSend(sending, { type: 'leave', id })
    expect(away.phase.kind).toBe('sending')
    const late = reduceAnswerSend(away, { type: 'postSaved', id, answer })
    expect(late.phase.kind).toBe('saved')
    if (late.phase.kind === 'saved') expect(late.phase.answer).toBe(answer)
  })

  it('keeps the lifecycle in the store across leave and return', () => {
    dispatchAnswerSend({ type: 'return', id })
    dispatchAnswerSend({ type: 'choose', id, snapshot })
    dispatchAnswerSend({ type: 'leave', id })
    expect(answerSendSnapshot(id).phase.kind).toBe('idle')
    dispatchAnswerSend({ type: 'return', id })
    expect(answerSendSnapshot(id).phase.kind).toBe('idle')
    expect(answerSendStoreHas(id)).toBe(true)

    dispatchAnswerSend({ type: 'choose', id, snapshot })
    dispatchAnswerSend({ type: 'confirm', id })
    dispatchAnswerSend({ type: 'leave', id })
    expect(answerSendSnapshot(id).phase.kind).toBe('sending')
    dispatchAnswerSend({ type: 'postFailed', id: OTHER })
    expect(answerSendSnapshot(id).phase.kind).toBe('sending')
    expect(answerSendSnapshot(OTHER).phase.kind).toBe('unknown')
    dispatchAnswerSend({ type: 'postSaved', id, answer })
    dispatchAnswerSend({ type: 'return', id })
    const saved = answerSendSnapshot(id)
    expect(saved.phase.kind).toBe('saved')
    if (saved.phase.kind === 'saved') expect(saved.phase.answer).toBe(answer)
    expect(answerSendSnapshot(OTHER).phase.kind).toBe('unknown')
  })

  it('drops the oldest idle entries past 50 and never drops unknown, sending, saved, or closed', () => {
    for (let i = 0; i <= 50; i += 1) dispatchAnswerSend({ type: 'return', id: qid(i) })
    expect(answerSendStoreSize()).toBe(50)
    expect(answerSendStoreHas(qid(0))).toBe(false)
    expect(answerSendStoreHas(qid(1))).toBe(true)
    expect(answerSendStoreHas(qid(50))).toBe(true)

    resetAnswerSendStore()
    dispatchAnswerSend({ type: 'return', id: qid(0) })
    dispatchAnswerSend({ type: 'postSaved', id: qid(0), answer })
    dispatchAnswerSend({ type: 'return', id: qid(1) })
    dispatchAnswerSend({ type: 'postFailed', id: qid(1) })
    dispatchAnswerSend({ type: 'return', id: qid(2) })
    dispatchAnswerSend({ type: 'choose', id: qid(2), snapshot: { ...snapshot, id: qid(2) } })
    dispatchAnswerSend({ type: 'confirm', id: qid(2) })
    const withdrawn = structuredClone(answered)
    withdrawn.id = qid(3)
    withdrawn.status = 'withdrawn'
    delete withdrawn.answer
    dispatchAnswerSend({ type: 'return', id: qid(3) })
    dispatchAnswerSend({ type: 'read', id: qid(3), record: withdrawn })
    for (let i = 4; i <= 60; i += 1) dispatchAnswerSend({ type: 'return', id: qid(i) })
    expect(answerSendStoreSize()).toBe(50)
    expect(answerSendSnapshot(qid(0)).phase.kind).toBe('saved')
    expect(answerSendSnapshot(qid(1)).phase.kind).toBe('unknown')
    expect(answerSendSnapshot(qid(2)).phase.kind).toBe('sending')
    expect(answerSendSnapshot(qid(3)).phase.kind).toBe('closed')
    expect(answerSendStoreHas(qid(4))).toBe(false)
    expect(answerSendStoreHas(qid(60))).toBe(true)

    resetAnswerSendStore()
    for (let i = 0; i <= 50; i += 1) {
      dispatchAnswerSend({ type: 'return', id: qid(i) })
      dispatchAnswerSend({ type: 'postSaved', id: qid(i), answer })
    }
    expect(answerSendStoreSize()).toBe(51)
    expect(answerSendStoreHas(qid(0))).toBe(true)
    expect(answerSendSnapshot(qid(0)).phase.kind).toBe('saved')

    resetAnswerSendStore()
    for (let i = 0; i <= 50; i += 1) {
      const snap = { ...snapshot, id: qid(i) }
      dispatchAnswerSend({ type: 'return', id: qid(i) })
      dispatchAnswerSend({ type: 'choose', id: qid(i), snapshot: snap })
    }
    expect(answerSendStoreSize()).toBe(51)
    expect(answerSendSnapshot(qid(0)).phase.kind).toBe('confirming')
  })

  it('does not let an open read replace a terminal record in the store or the cache', () => {
    const client = new QueryClient()
    client.setQueryData(questionItemKey(open.id), answered)
    const kept = acceptQuestionRead(open, client)
    expect(kept.status).toBe('answered')
    expect(client.getQueryData<Question>(questionItemKey(open.id))?.status).toBe('answered')
    const phase = answerSendSnapshot(open.id).phase
    expect(phase.kind).toBe('saved')
    if (phase.kind === 'saved') expect(phase.answer).toBe(answered.answer)

    resetAnswerSendStore()
    const fresh = new QueryClient()
    fresh.setQueryData(questionItemKey(open.id), open)
    dispatchAnswerSend({ type: 'postSaved', id, answer })
    const again = acceptQuestionRead(open, fresh)
    expect(again.status).toBe('answered')
    expect(fresh.getQueryData<Question>(questionItemKey(open.id))?.status).toBe('answered')
    const saved = answerSendSnapshot(id).phase
    expect(saved.kind).toBe('saved')
    if (saved.kind === 'saved') expect(saved.answer).toBe(answer)
  })
})
