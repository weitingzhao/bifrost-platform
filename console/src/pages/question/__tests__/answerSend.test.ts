import { readFileSync } from 'node:fs'
import { dirname, join } from 'node:path'
import { fileURLToPath } from 'node:url'
import { describe, expect, it } from 'vitest'
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
  if (event.type === 'postRejected') return 'postRejected(409 with answer)'
  return event.type
}

function snapshotOf(state: AnswerSendState) {
  const phase = state.phase
  if (phase.kind === 'confirming' || phase.kind === 'sending' || phase.kind === 'unknown') return phase.snapshot
  return null
}

function fail(sequence: string[], message: string): never {
  throw new Error(`failing sequence: ${sequence.join(' → ')}\n${message}`)
}

function check(sequence: string[], condition: boolean, message: string): void {
  if (!condition) fail(sequence, message)
}

/** Invariants 1–4 and 6, on this state and on the transition that produced it. */
function assertStep(before: AnswerSendState, event: AnswerSendEvent, after: AnswerSendState, sequence: string[]): void {
  check(sequence, confirmIsSendable(after) === (after.phase.kind === 'confirming'), 'Confirm answer is sendable outside confirming')
  check(sequence, after.phase.kind !== 'unknown' || checkAgainAvailable(after), 'unknown has no Check again')

  if (event.type === 'postFailed') {
    if (isTerminalPhase(before.phase)) {
      check(sequence, isTerminalPhase(after.phase) && after.phase.kind === before.phase.kind, 'postFailed downgraded a known terminal state')
    } else {
      check(sequence, after.phase.kind === 'unknown', 'postFailed did not lead to unknown')
    }
  }

  if (before.phase.kind === 'unknown' && event.type !== 'read') {
    check(sequence, after.phase.kind === 'unknown', `unknown left through ${event.type}`)
  }

  if (before.phase.kind === 'unknown' && event.type === 'read' && event.record.status === 'open') {
    const snap = snapshotOf(before)
    const stale = snap != null && snapshotStale(event.record, snap)
    if (stale) {
      check(sequence, after.phase.kind === 'unknown', 'a changed option during unknown left unknown')
      check(sequence, checkAgainAvailable(after), 'Check again needs a snapshot')
    } else {
      check(sequence, after.phase.kind === 'idle', 'read(open) did not return to idle')
      check(sequence, !confirmIsSendable(after), 'read(open) restored a sendable row')
    }
  }

  if (before.phase.kind === 'unknown' && event.type === 'read' && event.record.status === 'answered') {
    check(sequence, isTerminalPhase(after.phase), 'read(answered) did not reach a terminal state')
  }

  if (isTerminalPhase(before.phase) && event.type === 'read' && event.record.status === 'open') {
    check(sequence, after.phase.kind === before.phase.kind, 'an open read downgraded a known terminal state')
    if (before.phase.kind === 'saved' && after.phase.kind === 'saved') {
      check(sequence, after.phase.answer === before.phase.answer, 'an open read replaced a saved answer')
    }
  }

  if (before.phase.kind === 'confirming' && event.type === 'read' && event.record.status === 'open') {
    const stale = snapshotStale(event.record, before.phase.snapshot)
    if (stale) {
      check(sequence, after.phase.kind === 'idle' && !confirmIsSendable(after), 'a changed option left the old confirm sendable')
    } else {
      check(sequence, after.phase.kind === 'confirming', 'a matching open read cleared the confirm row')
    }
  }
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

  const events: AnswerSendEvent[] = [
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

  it('enumerates every sequence of up to five events and holds the send invariants', () => {
    expect(start.phase.kind).toBe('confirming')
    expect(snapshotStale(open, snapshot)).toBe(false)
    expect(snapshotStale(changed, snapshot)).toBe(true)
    let enumerated = 0
    const walk = (before: AnswerSendState, prefix: string[]) => {
      if (prefix.length === 5) return
      for (const event of events) {
        const name = eventName(event, changed)
        const sequence = [...prefix, name]
        const after = reduceAnswerSend(before, event)
        enumerated += 1
        assertStep(before, event, after, sequence)
        walk(after, sequence)
      }
    }
    walk(start, [])
    expect(enumerated).toBe(9 + 81 + 729 + 6561 + 59049)
  })

  it('drops an event for a different question id', () => {
    const failed = reduceAnswerSend(start, { type: 'postFailed', id: OTHER })
    expect(failed).toBe(start)
    const away = reduceAnswerSend(start, { type: 'focus', id: OTHER })
    expect(away.questionId).toBe(OTHER)
    expect(away.phase.kind).toBe('idle')
    const late = reduceAnswerSend(away, { type: 'postSaved', id, answer })
    expect(late).toBe(away)
  })
})
