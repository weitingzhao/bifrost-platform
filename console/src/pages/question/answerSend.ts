import type { Question, QuestionAnswer, QuestionStatus } from '@/api/questions'

/**
 * One question's send lifecycle. Pure: no React, no fetch.
 * Confirm answer is sendable only in `confirming`. `postFailed` is every POST
 * outcome that is not a parsed 4xx, and it lands in `unknown`. `unknown`
 * leaves only through a successful read, and a known terminal state is never
 * replaced by a later open read.
 */
export type ConfirmSnapshot = {
  id: string
  letter: string
  label: string
  detail: string
  text: string
  optionCount: number
  recommended: string
}

export type AnswerPhase =
  | { kind: 'idle'; notice: 'changed' | 'not-confirmed' | null }
  | { kind: 'confirming'; snapshot: ConfirmSnapshot; rejection: string | null }
  | { kind: 'sending'; snapshot: ConfirmSnapshot }
  | { kind: 'unknown'; snapshot: ConfirmSnapshot | null }
  | { kind: 'saved'; answer: QuestionAnswer }
  | { kind: 'closed'; status: 'answered' | 'withdrawn' | 'expired' }

export type AnswerSendState = {
  questionId: string
  phase: AnswerPhase
}

export type AnswerSendEvent =
  | { type: 'choose'; id: string; snapshot: ConfirmSnapshot }
  | { type: 'cancel'; id: string }
  | { type: 'confirm'; id: string }
  | { type: 'notSent'; id: string }
  | { type: 'leave'; id: string }
  | { type: 'return'; id: string }
  | { type: 'postSaved'; id: string; answer: QuestionAnswer }
  | {
      type: 'postRejected'
      id: string
      recordStatus: string | null
      answer: QuestionAnswer | null
      message: string
    }
  | { type: 'postFailed'; id: string }
  | { type: 'read'; id: string; record: Question }
  | { type: 'readFailed'; id: string }
  | { type: 'checkAgain'; id: string }

const IDLE: AnswerPhase = { kind: 'idle', notice: null }

export function initialAnswerSend(questionId: string): AnswerSendState {
  return { questionId, phase: IDLE }
}

function sentText(letter: string, value: string, allowText: boolean): string {
  const trimmed = value.trim()
  return letter === 'none' || allowText ? trimmed : ''
}

export function takeSnapshot(question: Question, letter: string, value: string): ConfirmSnapshot {
  const option = question.options.find(item => item.key === letter)
  return {
    id: question.id,
    letter,
    label: letter === 'none' ? 'None of these' : (option?.label ?? ''),
    detail: letter === 'none' ? '' : (option?.detail ?? ''),
    text: sentText(letter, value, question.allow_text),
    optionCount: question.options.length,
    recommended: question.recommended ?? '',
  }
}

/** The chosen row no longer means what the snapshot says. */
export function snapshotStale(question: Question, snapshot: ConfirmSnapshot): boolean {
  if (question.id !== snapshot.id) return true
  if (question.status !== 'open') return true
  if (question.options.length !== snapshot.optionCount) return true
  if ((question.recommended ?? '') !== snapshot.recommended) return true
  if (snapshot.letter === 'none') return false
  const option = question.options.find(item => item.key === snapshot.letter)
  if (option == null) return true
  if (option.label !== snapshot.label) return true
  return (option.detail ?? '') !== snapshot.detail
}

export function isTerminalPhase(phase: AnswerPhase): boolean {
  return phase.kind === 'saved' || phase.kind === 'closed'
}

/** The only phase in which Confirm answer may be enabled. */
export function confirmIsSendable(state: AnswerSendState): boolean {
  return state.phase.kind === 'confirming'
}

/** Check again reads the page's question id. It does not need a snapshot. */
export function checkAgainAvailable(state: AnswerSendState): boolean {
  return state.phase.kind === 'unknown'
}

/**
 * A successful read may be stored for this id. An open record does not replace
 * a known terminal state (a stale GET that started before the save).
 */
export function readKeepsRecord(state: AnswerSendState, record: Question): boolean {
  if (record.id !== state.questionId) return false
  if (isTerminalPhase(state.phase) && record.status === 'open') return false
  return true
}

function snapshotOf(phase: AnswerPhase): ConfirmSnapshot | null {
  if (phase.kind === 'confirming' || phase.kind === 'sending') return phase.snapshot
  if (phase.kind === 'unknown') return phase.snapshot
  return null
}

function closedStatus(status: string | null): 'answered' | 'withdrawn' | 'expired' | null {
  if (status === 'answered' || status === 'withdrawn' || status === 'expired') return status
  return null
}

function terminalFromRecord(record: Question): AnswerPhase | null {
  if (record.status === 'answered') {
    return record.answer != null ? { kind: 'saved', answer: record.answer } : { kind: 'closed', status: 'answered' }
  }
  if (record.status === 'withdrawn') return { kind: 'closed', status: 'withdrawn' }
  if (record.status === 'expired') return { kind: 'closed', status: 'expired' }
  return null
}

function reduceOpen(phase: AnswerPhase, record: Question): AnswerPhase {
  if (phase.kind === 'unknown') {
    if (phase.snapshot != null && snapshotStale(record, phase.snapshot)) return { kind: 'unknown', snapshot: null }
    return { kind: 'idle', notice: 'not-confirmed' }
  }
  if (phase.kind === 'sending') return phase
  if (phase.kind === 'confirming') {
    if (snapshotStale(record, phase.snapshot)) return { kind: 'idle', notice: 'changed' }
    return phase
  }
  return phase.kind === 'idle' ? phase : IDLE
}

function reduceRead(phase: AnswerPhase, record: Question): AnswerPhase {
  if (isTerminalPhase(phase)) {
    if (record.status === 'open') return phase
    return terminalFromRecord(record) ?? phase
  }
  if (record.status !== 'open') return terminalFromRecord(record) ?? phase
  return reduceOpen(phase, record)
}

function reduceRejected(phase: AnswerPhase, event: Extract<AnswerSendEvent, { type: 'postRejected' }>): AnswerPhase {
  if (isTerminalPhase(phase) || phase.kind === 'unknown') return phase
  if (event.answer != null) return { kind: 'saved', answer: event.answer }
  const closed = closedStatus(event.recordStatus)
  if (closed != null) return { kind: 'closed', status: closed }
  const snapshot = snapshotOf(phase)
  if (snapshot == null) return phase
  return { kind: 'confirming', snapshot, rejection: event.message }
}

function reducePhase(phase: AnswerPhase, event: AnswerSendEvent): AnswerPhase {
  switch (event.type) {
    case 'leave':
      // The choice is not a send. Leaving the question drops it.
      return phase.kind === 'confirming' ? IDLE : phase
    case 'return':
      // Mounting reads the entry that is already here. It does not start over.
      return phase
    case 'choose':
      if (phase.kind === 'sending' || phase.kind === 'unknown' || isTerminalPhase(phase)) return phase
      if (event.snapshot.id !== event.id) return phase
      return { kind: 'confirming', snapshot: event.snapshot, rejection: null }
    case 'cancel':
      return phase.kind === 'confirming' ? IDLE : phase
    case 'confirm':
      if (phase.kind !== 'confirming' || phase.snapshot.id !== event.id) return phase
      return { kind: 'sending', snapshot: phase.snapshot }
    case 'notSent':
      if (phase.kind !== 'sending') return phase
      return { kind: 'confirming', snapshot: phase.snapshot, rejection: null }
    case 'postSaved':
      if (phase.kind === 'unknown' || phase.kind === 'closed') return phase
      return { kind: 'saved', answer: event.answer }
    case 'postRejected':
      return reduceRejected(phase, event)
    case 'postFailed':
      if (isTerminalPhase(phase) || phase.kind === 'unknown') return phase
      return { kind: 'unknown', snapshot: snapshotOf(phase) }
    case 'read':
      return reduceRead(phase, event.record)
    case 'readFailed':
    case 'checkAgain':
      return phase
    default: {
      const never: never = event
      return never
    }
  }
}

export function reduceAnswerSend(state: AnswerSendState, event: AnswerSendEvent): AnswerSendState {
  if (event.id !== state.questionId) return state
  const phase = reducePhase(state.phase, event)
  if (phase === state.phase) return state
  return { questionId: state.questionId, phase }
}

/** What the page shows. A known save or close wins over a stale open record. */
export function presentedQuestion(question: Question, phase: AnswerPhase): Question {
  if (phase.kind === 'saved') return { ...question, status: 'answered', answer: phase.answer }
  if (phase.kind === 'closed') return { ...question, status: phase.status as QuestionStatus }
  return question
}
